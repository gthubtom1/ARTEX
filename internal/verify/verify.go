// Package verify 是「三门校验」：在一条结论进报告之前，强制过三道关：
//
//	1) grounding     证据在不在？请求/响应/命令输出能不能对上入口？
//	2) negation      有没有对照组？"试了没差异"必须真的发生过。
//	3) interrogation 能不能用另一条独立路径复现？（P3 及以上必须）
//
// 产物是**事实分类**（verified / inferred / rejected），不是拒绝——
// 任何结论都会被保留，只是标注它现在值几分熟。
package verify

import (
	"fmt"
	"strings"
)

// Gate 是单道关的结果。
type Gate struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// Verdict 是三门校验的结论。
type Verdict struct {
	Level      string   `json:"level"`       // verified|inferred|rejected
	ProofLevel string   `json:"proof_level"` // 归一后的证明级别
	Gates      []Gate   `json:"gates"`
	Reasons    []string `json:"reasons,omitempty"`
}

// Options 是调用方拿到的旁证。
type Options struct {
	ControlTested bool   // 是否做过对照组（负向请求/无 payload 的同请求）
	ControlNote   string // 对照组怎么做的（写进报告）
	RetestOK      bool   // 复现门是否通过（retest 工具）
	Independent   bool   // 是否用独立路径复现过
	ToolVerified  bool   // 是否有工具级证据（nuclei 模板命中 / OAST 回连）
}

// Claim 是一条待校验的结论。
type Claim struct {
	Title      string
	Surface    string
	Evidence   string
	ProofLevel string
}

// Check 跑三次校验，返回结论分类。
func Check(c Claim, opt Options) Verdict {
	proof := strings.ToUpper(strings.TrimSpace(c.ProofLevel))
	if proof == "" {
		proof = "P2"
	}
	v := Verdict{ProofLevel: proof}
	reasons := []string{}

	// ── 门 1：grounding ──
	g := Gate{Name: "grounding"}
	ev := strings.TrimSpace(c.Evidence)
	switch {
	case ev == "":
		g.OK = false
		g.Reason = "没有任何证据文本（请求/响应/命令输出）"
	case len([]rune(ev)) < 24:
		g.OK = false
		g.Reason = "证据太短，无法对应到具体入口（至少写出请求行 + 关键响应片段）"
	case hasHedging(ev):
		g.OK = false
		g.Reason = "证据里含猜测性措辞（可能/据说/未验证），这不是证据"
	case CVEClaimWithoutTrace(ev):
		g.OK = false
		g.Reason = "证据只有版本号/CVE 断言，没有实际触发记录（需要请求行+响应片段，或命令+输出）"
	default:
		if hits, ok := LooksLikeTrace(ev); ok {
			g.OK = true
			g.Reason = "命中痕迹：" + strings.Join(hits, "/")
		} else {
			g.OK = false
			g.Reason = "证据里没有请求行/响应片段/命令输出的痕迹，无法对应到具体入口"
		}
	}
	v.Gates = append(v.Gates, g)
	if !g.OK {
		reasons = append(reasons, "grounding 未过："+g.Reason)
	}

	// ── 门 2：negation ──
	n := Gate{Name: "negation"}
	if opt.ControlTested {
		n.OK = true
	} else {
		n.OK = false
		n.Reason = "没有对照组（同一入口去掉 payload / 换正常参数再请求一次）"
	}
	v.Gates = append(v.Gates, n)
	if !n.OK {
		reasons = append(reasons, "negation 未过："+n.Reason)
	}

	// ── 门 3：interrogation ──
	q := Gate{Name: "interrogation"}
	needIndependent := proof == "P3" || proof == "P4" || proof == "D3" || proof == "D4"
	if opt.Independent || opt.RetestOK || opt.ToolVerified {
		q.OK = true
	} else if needIndependent {
		q.OK = false
		q.Reason = "P3+ 需要独立路径复现（换一个入口/身份/工具再确认一次）"
	} else {
		q.OK = true // P1/P2 只要求单条样本，独立复现是加分项
	}
	v.Gates = append(v.Gates, q)
	if !q.OK {
		reasons = append(reasons, "interrogation 未过："+q.Reason)
	}

	// 结论分类：
	//   证据都写不出来 / 含猜测措辞 → rejected（不进报告，留作线索）
	//   任一门未过             → inferred（写进"待确认"，不写成结论）
	//   全过                   → verified
	switch {
	case !g.OK && (ev == "" || hasHedging(ev) || CVEClaimWithoutTrace(ev)):
		v.Level = "rejected"
		v.ProofLevel = "P1"
	case !g.OK || !n.OK || !q.OK:
		v.Level = "inferred"
		if r := ProofRankOf(proof); r > 2 {
			v.ProofLevel = "P2"
		}
	case opt.ToolVerified || opt.RetestOK:
		v.Level = "verified"
		// 只有工具级证据、没有独立路径复现：P3+ 一律按 P2 记（工具能骗人）。
		if ProofRankOf(proof) > 2 && !opt.Independent {
			v.ProofLevel = "P2"
		}
	default:
		// 三门全过但没有工具级复现：仍算 verified，但证明级别不超过 P2。
		v.Level = "verified"
		if ProofRankOf(proof) > 2 && !opt.Independent {
			v.ProofLevel = "P2"
		}
	}
	v.Reasons = reasons
	return v
}

// hasHedging 判断文本是否只是猜测。
func hasHedging(s string) bool {
	low := strings.ToLower(s)
	bad := []string{"可能", "据说", "推测", "疑似（未验证）", "未验证", "猜的", "todo", "placeholder", "probably", "maybe", "might be"}
	for _, b := range bad {
		if strings.Contains(low, b) {
			return true
		}
	}
	return false
}

// ProofRankOf 与 capability.ProofRank 同口径（避免循环依赖，这里本地实现）。
func ProofRankOf(level string) int {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "P1", "D1":
		return 1
	case "P2", "D2":
		return 2
	case "P3", "D3":
		return 3
	case "P4", "D4":
		return 4
	}
	return 0
}

// Describe 生成一行人类可读结论（写进证据时间线 / 活动流）。
func (v Verdict) Describe(title string) string {
	bad := 0
	for _, g := range v.Gates {
		if !g.OK {
			bad++
		}
	}
	return fmt.Sprintf("「%s」→ %s（%s，%d/3 门通过）", strings.TrimSpace(title), v.Level, v.ProofLevel, 3-bad)
}
