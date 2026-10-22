// Package verifier 是「专判」角色：用独立模型（agentKey=verifier）对已上报的 finding
// 做对抗式复核——只看证据，不看上下文叙事，输出 verified / inferred / rejected。
//
// 与 internal/verify 的分工：
//   - verify 是零 token 的确定性三门校验（证据在不在/有无对照/能否独立复现）；
//   - verifier 是 LLM 复核（证据是否真的支持结论、需要补什么对照），结果落证据时间线。
package verifier

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Verdict 是复核结论。
type Verdict string

const (
	VerdictVerified Verdict = "verified"
	VerdictInferred Verdict = "inferred"
	VerdictRejected Verdict = "rejected"
)

// Input 是一条待复核的 finding。
type Input struct {
	FindingID int64  `json:"finding_id"`
	Title     string `json:"title"`
	Severity  string `json:"severity"`
	Surface   string `json:"surface"`
	Evidence  string `json:"evidence"`
}

// Review 是复核结果（落 evidence_timeline.meta）。
type Review struct {
	FindingID int64    `json:"finding_id"`
	Verdict   Verdict  `json:"verdict"`
	Reason    string   `json:"reason"`
	Missing   []string `json:"missing,omitempty"` // 缺哪些证据才能升级
	Source    string   `json:"source"`            // llm
}

// Prompt 生成复核提示词。要求只输出 JSON，且证据不足必须降级。
func Prompt(goal string, in Input) string {
	var b strings.Builder
	b.WriteString("你是独立复核员。只看证据本身，不看叙述。判断这条漏洞结论是否被证据支持。\n\n")
	if g := strings.TrimSpace(goal); g != "" {
		b.WriteString("任务目标：" + g + "\n")
	}
	fmt.Fprintf(&b, "结论：%s（严重性 %s）\n", in.Title, in.Severity)
	if in.Surface != "" {
		fmt.Fprintf(&b, "入口：%s\n", in.Surface)
	}
	b.WriteString("证据：\n")
	if strings.TrimSpace(in.Evidence) == "" {
		b.WriteString("（空）\n")
	} else {
		b.WriteString(in.Evidence)
		b.WriteString("\n")
	}
	b.WriteString(`
判定规则：
1) 证据里能看到"实际请求/命令 + 实际响应/输出"，且响应确实体现了该漏洞 → verified；
2) 证据只是版本匹配、模板命中、现象推断，或只验证了半条链 → inferred，并列出缺什么；
3) 证据为空、含猜测措辞、或与结论无关 → rejected。
只输出 JSON：{"verdict":"verified|inferred|rejected","reason":"一句话","missing":["要补的证据"]}`)
	return b.String()
}

// SystemPrompt 是复核员的系统提示。
const SystemPrompt = `你是渗透报告的独立复核员。你只做一件事：判断证据是否支持结论。
宁可降级，不要放过。证据不足就写 inferred 并列出缺什么；证据为空或含猜测措辞就写 rejected。
只输出 JSON。`

// ParseJSON 解析模型输出（容忍围栏与前后废话），并把 verdict 归一。
func ParseJSON(raw string) (Review, error) {
	var r Review
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		if j := strings.Index(rest, "```"); j >= 0 {
			rest = rest[:j]
		}
		s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "json"))
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return r, fmt.Errorf("verifier: 输出里没有 JSON 对象")
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &r); err != nil {
		return r, fmt.Errorf("verifier: JSON 解析失败：%w", err)
	}
	switch strings.ToLower(strings.TrimSpace(string(r.Verdict))) {
	case "verified":
		r.Verdict = VerdictVerified
	case "inferred":
		r.Verdict = VerdictInferred
	case "rejected":
		r.Verdict = VerdictRejected
	default:
		return r, fmt.Errorf("verifier: 非法 verdict %q", r.Verdict)
	}
	if strings.TrimSpace(r.Reason) == "" {
		r.Reason = "模型未给出理由"
	}
	r.Source = "llm"
	return r, nil
}
