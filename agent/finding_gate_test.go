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
		out := runGate(findinggate.ModeOff, gateArgsFor(ev))
		if !out.Store {
			t.Fatalf("off 档不应拦任何 finding，ev=%q", ev)
		}
		if out.Summary != gateArgsFor(ev).Summary {
			t.Fatal("off 档不应改写 summary")
		}
	}
}

func TestRunGate_InferRejectsVersionOnly(t *testing.T) {
	out := runGate(findinggate.ModeInfer, gateArgsFor(gateVersionOnly))
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
	out := runGate(findinggate.ModeInfer, gateArgsFor(gateGoodEvidence))
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

	relaxed := runGate(findinggate.ModeInfer, a)
	strict := runGate(findinggate.ModeStrict, a)

	if !relaxed.Store {
		t.Fatalf("infer 档下缺对照组不应直接拒（level=%s）", relaxed.Verdict.Level)
	}
	if relaxed.Verdict.Level != "verified" && !strings.HasPrefix(relaxed.Summary, findinggate.SummaryFlag) {
		t.Fatalf("inferred 的结论应带【未独立复现】前缀，实际 summary=%q", relaxed.Summary)
	}
	if strict.Store && relaxed.Verdict.Level != "verified" {
		t.Fatal("strict 档下 inferred 不应入库")
	}
}

// 闸门必须真的改变了入库内容（而不是只返回一个 decision 没人用）。
func TestRunGate_FlaggedRewritesFields(t *testing.T) {
	a := gateArgsFor(gateGoodEvidence)
	a.ControlTested = false
	a.ControlNote = ""
	if strings.Contains(a.Evidence, findinggate.SummaryFlag) {
		t.Fatal("测试前提不成立")
	}
	out := runGate(findinggate.ModeInfer, a)
	if out.Verdict.Level == "verified" {
		t.Skip("该证据被判为 verified，本用例不适用")
	}
	if !strings.HasPrefix(out.Summary, findinggate.SummaryFlag) {
		t.Fatalf("summary 未加前缀：%q", out.Summary)
	}
	if !strings.Contains(out.Evidence, "[gate:inferred]") {
		t.Fatalf("evidence 未追加 [gate:inferred]：%q", out.Evidence)
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
