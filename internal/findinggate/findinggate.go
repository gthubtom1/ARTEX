// Package findinggate 是「发现质量闸」：把 internal/verify 的三门结论
// （verified / inferred / rejected）翻译成 report_finding 该做什么。
//
// 它**不是权限门**：不存在"允不允许上报"这回事。任何结论都会被保留，
// 只是被放到不同的位置（finding / 待验证 fact / 带标注的 finding），
// 并给模型一句可以照做的话。所以这里所有文案都不许出现"禁止"这类词。
//
// 本包零依赖：只用标准库，刻意不 import internal/verify（避免循环依赖，
// 也让它能独立单测）。调用方把 verify.Verdict.Level / Reasons 拆成
// string / []string 传进来即可。
package findinggate

import (
	"regexp"
	"strings"
)

// Mode 是质量闸档位（不是权限门，别写成"是否允许"）。
type Mode string

const (
	ModeOff    Mode = "off"
	ModeInfer  Mode = "infer" // 默认
	ModeStrict Mode = "strict"
)

// ParseMode 归一配置；空/非法 → infer。
func ParseMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(ModeOff):
		return ModeOff
	case string(ModeStrict):
		return ModeStrict
	case string(ModeInfer):
		return ModeInfer
	default:
		return ModeInfer
	}
}

// Action 是闸门给出的动作。
type Action string

const (
	ActStore        Action = "store"         // 正常入库
	ActStoreFlagged Action = "store_flagged" // 入库但标注未独立复现
	ActToFact       Action = "to_fact"       // 不入 finding，改记 inferred 事实
	ActReject       Action = "reject"        // 拒绝，要求补证据
)

// Decision 是一次判定的结果。
type Decision struct {
	Action         Action `json:"action"`
	SummaryPrefix  string `json:"summary_prefix,omitempty"`  // ActStoreFlagged 时给 summary 加的前缀
	EvidenceSuffix string `json:"evidence_suffix,omitempty"` // ActStoreFlagged 时追加到 evidence 末尾
	Message        string `json:"message,omitempty"`         // 回给模型的文案
}

// SummaryFlag 是 store_flagged 时加到 summary 前面的标记，导出便于前端/报告识别。
const SummaryFlag = "【未独立复现】"

// EvidenceTag 是 store_flagged 时追加到 evidence 末尾的标记前缀。
const EvidenceTag = "[gate:inferred] "

// Decide 按档位 + verify 的三门结论给出动作。
// level ∈ "verified" | "inferred" | "rejected"（其它值按 rejected 处理）。
func Decide(mode Mode, level string, reasons []string) Decision {
	lv := strings.ToLower(strings.TrimSpace(level))
	switch lv {
	case "verified", "inferred", "rejected":
	default:
		lv = "rejected"
	}
	joined := JoinReasons(reasons)

	switch ParseMode(string(mode)) {
	case ModeOff:
		// 关闸：行为与改动前完全一致。
		return Decision{Action: ActStore}

	case ModeStrict:
		switch lv {
		case "verified":
			return Decision{Action: ActStore}
		case "inferred":
			return Decision{Action: ActReject, Message: rejectMessage(joined)}
		default: // rejected
			return Decision{Action: ActToFact, Message: toFactMessage(joined)}
		}

	default: // ModeInfer
		switch lv {
		case "verified":
			return Decision{Action: ActStore}
		case "inferred":
			return Decision{
				Action:         ActStoreFlagged,
				SummaryPrefix:  SummaryFlag,
				EvidenceSuffix: "\n\n" + EvidenceTag + joined,
				Message:        flaggedMessage(joined),
			}
		default: // rejected
			return Decision{Action: ActToFact, Message: toFactMessage(joined)}
		}
	}
}

func toFactMessage(joined string) string {
	const tail = "补齐请求/响应原文或独立复现后可重新上报。"
	if joined == "" {
		return "证据不足，已记为待验证（inferred）。" + tail
	}
	return "证据不足，已记为待验证（inferred）：" + joined + "。" + tail
}

func rejectMessage(joined string) string {
	const tail = "严格模式下不入库。请补一次独立复现后再上报。"
	if joined == "" {
		return "证据只能判到 inferred。" + tail
	}
	return "证据只能判到 inferred（" + joined + "）。" + tail
}

func flaggedMessage(joined string) string {
	const head = "已入库，但按未独立复现标注（inferred）"
	if joined == "" {
		return head + "；补齐一次独立复现后可去掉标注。"
	}
	return head + "，原因：" + joined + "；补齐一次独立复现后可去掉标注。"
}

// ── 旁证自动推导（不要靠模型自己填） ──

var toolMarks = []string{"nuclei", "oast", "dnslog", "interactsh", "callback", "回连"}

// ToolVerifiedFromEvidence：证据里出现工具级标记 → true。
// 命中词（大小写不敏感）：nuclei / oast / dnslog / interactsh / callback / 回连。
func ToolVerifiedFromEvidence(evidence string) bool {
	low := strings.ToLower(evidence)
	for _, m := range toolMarks {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

var (
	httpMethodRe = regexp.MustCompile(`(?i)\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\b`)
	// 去掉 "HTTP/1.1" 这类版本串，否则 "/1.1" 会被当成一条路径。
	httpVersionRe = regexp.MustCompile(`(?i)\bHTTP/\d+(?:\.\d+)?`)
	httpPathRe    = regexp.MustCompile(`(?:https?://[^\s/]+)?(/[A-Za-z0-9._~!$&'()*+,;=:@%/-]{1,200})`)
)

// IndependentFromEvidence：证据里出现 ≥2 个不同 HTTP 方法，或 ≥2 个不同的
// 请求路径 → 认为是独立路径复现。方法词表：GET POST PUT PATCH DELETE HEAD OPTIONS
// 路径判定：正则 `(?:https?://[^\s/]+)?(/[A-Za-z0-9._~!$&'()*+,;=:@%/-]{1,200})`
// 去重后 ≥2 个不同路径。
func IndependentFromEvidence(evidence string) bool {
	if strings.TrimSpace(evidence) == "" {
		return false
	}
	methods := map[string]struct{}{}
	for _, m := range httpMethodRe.FindAllString(evidence, -1) {
		methods[strings.ToUpper(m)] = struct{}{}
	}
	if len(methods) >= 2 {
		return true
	}
	clean := httpVersionRe.ReplaceAllString(evidence, " ")
	paths := map[string]struct{}{}
	for _, m := range httpPathRe.FindAllStringSubmatch(clean, -1) {
		if len(m) < 2 {
			continue
		}
		p := strings.TrimRight(m[1], ".,;:")
		if p == "" || p == "/" {
			continue
		}
		paths[p] = struct{}{}
	}
	return len(paths) >= 2
}

// ControlHint：没有对照组时给模型的一句提示（用于 Message 的补充），有则返回空。
func ControlHint(controlTested bool, controlNote string) string {
	if controlTested {
		return ""
	}
	return "建议补一组对照：同一入口去掉 payload 或换正常参数再请求一次，并把对照请求与结果写进 evidence。"
}

// ── 辅助 ──

// JoinReasons 去空、去重、最多保留 3 条，用 "；" 连接。
func JoinReasons(rs []string) string {
	out := make([]string, 0, 3)
	seen := make(map[string]struct{}, len(rs))
	for _, r := range rs {
		t := strings.TrimSpace(r)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
		if len(out) == 3 {
			break
		}
	}
	return strings.Join(out, "；")
}
