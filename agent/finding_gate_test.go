package agent

import (
	"strings"
	"testing"

	"github.com/Autumn-27/artex/internal/findinggate"
)

// 一条"只有版本号"的证据：这是最典型的幻觉来源，必须被拦下。
const gateVersionOnly = "ThinkPHP 5.0.23 存在 CVE-2018-20062，应该可以 RCE"

// 一条完整 PoC：带请求行、响应片段、两个不同路径。
const gateGoodEvidence = `GET /index.php?s=captcha HTTP/1.1
Host: target
Content-Type: application/x-www-form-urlencoded

_method=__construct&filter[]=system&method=get&server[REQUEST_METHOD]=id
--- 响应 ---
HTTP/1.1 200 OK
uid=33(www-data) gid=33(www-data)

对照组：GET /index.php?s=captcha（去掉 _method 参数）返回 200 且无 uid 输出。
独立复现：POST /public/index.php?s=captcha 同样回显 uid=33(www-data)。`

func gateArgsFor(ev string) gateArgs {
	return gateArgs{
		Name:          "ThinkPHP 5.0.x 远程代码执行",
		VulnClass:     "rce",
		Summary:       "通过 _method=__construct 覆盖 filter 造成命令执行",
		Evidence:      ev,
		ControlTested: true,
		ControlNote:   "同请求去掉 _method 参数，无回显",
	}
}

func TestRunGate_OffAlwaysStores(t *testing.T) {
	for _, ev := range []string{gateVersionOnly, gateGoodEvidence, ""} {
		out := runGate(findinggate.ModeOff, gateArgsFor(ev), false)
		if !out.Store {
			t.Fatalf("off 档不应拦任何 finding，ev=%q", ev)
		}
		if out.Summary != gateArgsFor(ev).Summary {
			t.Fatal("off 档不应改写 summary")
		}
	}
}

func TestRunGate_InferRejectsVersionOnly(t *testing.T) {
	out := runGate(findinggate.ModeInfer, gateArgsFor(gateVersionOnly), false)
	if out.Store {
		t.Fatalf("只有版本号的结论不应入库（verdict level=%s gates=%+v）", out.Verdict.Level, out.Verdict.Gates)
	}
	msg := gateMessage(out)
	if !strings.Contains(msg, "证据不足") && !strings.Contains(msg, "待验证") {
		t.Fatalf("拒绝文案应说明证据不足/已记为待验证，实际：%s", msg)
	}
	if strings.Contains(msg, "禁止") || strings.Contains(msg, "不允许") {
		t.Fatalf("质量闸文案不应出现权限口吻：%s", msg)
	}
}

func TestRunGate_InferStoresGoodEvidence(t *testing.T) {
	out := runGate(findinggate.ModeInfer, gateArgsFor(gateGoodEvidence), false)
	if !out.Store {
		t.Fatalf("完整 PoC 应入库（verdict level=%s reasons=%v）", out.Verdict.Level, out.Verdict.Reasons)
	}
	if out.Verdict.Level == "verified" && strings.HasPrefix(out.Summary, findinggate.SummaryFlag) {
		t.Fatal("verified 的结论不应带【未独立复现】前缀")
	}
}

func TestRunGate_StrictRejectsInferred(t *testing.T) {
	// 有证据但没有对照组 → 最多到 inferred，严格档应拒绝。
	a := gateArgsFor(gateGoodEvidence)
	a.ControlTested = false
	a.ControlNote = ""

	relaxed := runGate(findinggate.ModeInfer, a, false)
	strict := runGate(findinggate.ModeStrict, a, false)

	if !relaxed.Store {
		t.Fatalf("infer 档下证据不足但非幻觉不应拒（level=%s）", relaxed.Verdict.Level)
	}
	if relaxed.Verdict.Level == "verified" {
		t.Skip("该证据被判为 verified，本用例不适用")
	}
	if strict.Store {
		t.Fatalf("strict 档下 inferred 不应入库（level=%s）", strict.Verdict.Level)
	}
	if strict.Message == "" {
		t.Fatal("strict 拒绝时必须给模型一句可执行的补证说明")
	}
}

// 闸门**不许**改写 summary / evidence：
// report_finding 的返回契约（"finding recorded: N" + 一行 JSON）和调用方对 evidence 原文的
// 比对都不能被质量闸破坏。闸门的职责是挡住幻觉，不是给真结论贴标签。
func TestRunGate_FlaggedDoesNotRewriteFields(t *testing.T) {
	a := gateArgsFor(gateGoodEvidence)
	a.ControlTested = false
	a.ControlNote = ""
	out := runGate(findinggate.ModeInfer, a, false)
	if out.Verdict.Level == "verified" {
		t.Skip("该证据被判为 verified，本用例不适用")
	}
	if !out.Store {
		t.Fatalf("infer 档下证据不足但非幻觉不应拒（level=%s）", out.Verdict.Level)
	}
	if out.Summary != a.Summary {
		t.Fatalf("闸门改写了 summary：%q", out.Summary)
	}
	if out.Evidence != a.Evidence {
		t.Fatalf("闸门改写了 evidence：%q", out.Evidence)
	}
	if out.Note == "" {
		t.Fatal("store_flagged 应带一条日志说明（Note）")
	}
}

func TestFindingGateModeDefault(t *testing.T) {
	prev := FindingGateMode
	t.Cleanup(func() { FindingGateMode = prev })
	FindingGateMode = nil
	if got := findingGateMode(); got != findinggate.ModeInfer {
		t.Fatalf("未注入时应默认 infer，实际 %q", got)
	}
	FindingGateMode = func() string { return "STRICT" }
	if got := findingGateMode(); got != findinggate.ModeStrict {
		t.Fatalf("大小写应归一，实际 %q", got)
	}
	FindingGateMode = func() string { return "bogus" }
	if got := findingGateMode(); got != findinggate.ModeInfer {
		t.Fatalf("非法值应回落 infer，实际 %q", got)
	}
}

// 绑定了录制的流量快照时不过闸：证据在 traffic_refs 里，evidence 文本为空是正常的。
// 这条守的是"报告 Agent 的合法上报不能被质量闸打成待验证"。
func TestRunGateBoundTrafficSkipsGate(t *testing.T) {
	a := gateArgs{Name: "报告 Agent 的上报", VulnClass: "idor", Summary: "越权读取他人订单", Evidence: ""}
	for _, mode := range []findinggate.Mode{findinggate.ModeInfer, findinggate.ModeStrict} {
		out := runGate(mode, a, true)
		if !out.Store {
			t.Fatalf("mode=%s 且绑定流量时不应拦，实际拒了：%s", mode, out.Message)
		}
		if out.Summary != a.Summary {
			t.Fatalf("绑定流量时不应改写 summary：%q", out.Summary)
		}
	}
}
