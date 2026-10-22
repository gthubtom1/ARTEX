package agent

import (
	"strings"

	"github.com/Autumn-27/artex/internal/findinggate"
	"github.com/Autumn-27/artex/internal/verify"
)

// P8-3：把三门校验接进 report_finding。
//
// 目的：现在 report_finding 完全不检查证据，"只有版本号" / "只有状态码" /
// "参数看起来可注入" 这类结论能直接入库并进报告。三门（grounding / negation /
// interrogation）的代码早就在 internal/verify，只是没人调。
//
// 注意这是**质量闸**，不是权限门（ADR D01）：三档 off/infer/strict，默认 infer，
// 文案里不出现"禁止/不允许"。

// FindingGateMode 由 server 注入：返回 settings 里的 findings.verify_gate。
// 未注入时按 infer 处理（默认档）。
var FindingGateMode func() string

func findingGateMode() findinggate.Mode {
	if FindingGateMode == nil {
		return findinggate.ModeInfer
	}
	return findinggate.ParseMode(FindingGateMode())
}

// gateArgs 是进闸所需的最小信息（便于单测，不依赖 ToolSet）。
type gateArgs struct {
	Name          string
	VulnClass     string
	Summary       string
	Evidence      string
	ControlTested bool
	ControlNote   string
}

// gateOutcome 是闸门裁决结果。
type gateOutcome struct {
	Store    bool                 // 是否继续入库
	Summary  string               // 原样回传（闸门不改写，见下）
	Evidence string               // 原样回传（闸门不改写，见下）
	Message  string               // 回给模型的文案（Store=false 时必填）
	Note     string               // Store=true 时写日志用的说明（不进 tool 结果，避免破坏返回契约）
	Decision findinggate.Decision // 原始裁决（测试/日志用）
	Verdict  verify.Verdict       // 三门结论（测试/日志用）
}

// runGate 跑三门 + 按档位裁决。纯函数式（不碰 DB），便于单测。
//
// boundTraffic=true 表示本次上报已经绑定了录制的流量快照（traffic_refs / evidence_hint_id）：
// 那是真实抓到的请求/响应，本身就是 grounding 证据，直接放行。否则"evidence 文本为空
// 但证据在流量里"的合法上报会被打成待验证。
func runGate(mode findinggate.Mode, a gateArgs, boundTraffic bool) gateOutcome {
	out := gateOutcome{Store: true, Summary: a.Summary, Evidence: a.Evidence}
	if mode == findinggate.ModeOff || boundTraffic {
		return out
	}

	// 没写证据：不判"假"，但要标"未独立复现"。
	// 仓库里存在"只给 summary、证据在流量绑定里或由报告 Agent 后补"的合法流程，
	// 把空证据直接打成 rejected 会误杀这些上报；而"有证据文本但只是版本号断言"才是幻觉，那条走下面。
	if strings.TrimSpace(a.Evidence) == "" {
		d := findinggate.Decide(mode, "inferred", []string{"没有提供证据文本（请求行 / 响应片段 / 命令输出）"})
		out.Decision = d
		out.Verdict = verify.Verdict{Level: "inferred", ProofLevel: "P2"}
		switch d.Action {
		case findinggate.ActStoreFlagged:
			out.Note = "已入库，但只能判到 inferred（没有证据文本）：报告里按未独立复现对待。"
		case findinggate.ActToFact, findinggate.ActReject:
			out.Store = false
			out.Message = d.Message
		}
		return out
	}

	// 旁证自动推导：能算的都算，不让模型自己填（模型总会说自己验证过）。
	independent := findinggate.IndependentFromEvidence(a.Evidence)
	toolVerified := findinggate.ToolVerifiedFromEvidence(a.Evidence)

	v := verify.Check(
		verify.Claim{
			Title:      a.Name,
			Surface:    a.VulnClass,
			Evidence:   a.Evidence,
			ProofLevel: "P2",
		},
		verify.Options{
			ControlTested: a.ControlTested,
			ControlNote:   a.ControlNote,
			ToolVerified:  toolVerified,
			Independent:   independent,
			RetestOK:      independent,
		},
	)
	d := findinggate.Decide(mode, v.Level, v.Reasons)

	out.Decision = d
	out.Verdict = v

	switch d.Action {
	case findinggate.ActStore:
		// 原样入库
	case findinggate.ActStoreFlagged:
		// 刻意不改写 summary / evidence：report_finding 的返回契约（"finding recorded: N" + 一行
		// JSON）和调用方对 evidence 原文的比对都不能被质量闸破坏。闸门的职责是"挡住幻觉"，
		// 不是"给真结论贴标签"；未独立复现由 confidence / 能力卡证明级别承担。
		out.Note = "已入库，但只能判到 inferred：" + strings.Join(v.Reasons, "；")
	case findinggate.ActToFact, findinggate.ActReject:
		out.Store = false
		out.Message = d.Message
	}
	return out
}

// gateMessage 给 Store=false 的情况拼上"补齐什么"的下一步（让模型知道怎么补）。
func gateMessage(out gateOutcome) string {
	msg := out.Message
	if msg == "" {
		msg = "证据不足，已记为待验证。"
	}
	if hint := findinggate.ControlHint(false, ""); hint != "" {
		msg += " " + hint
	}
	if out.Verdict.ProofLevel == "P1" {
		msg += " 当前证明级别只能到 P1，至少要 P2（有一条真实样本或一次独立复现）才能作为确认漏洞。"
	}
	return msg
}
