package agent

import (
	"context"
	"strings"
	"testing"
)

// P2 装配后，规划者/执行者/人机界面都应拿到对应工具（防止以后改工具列表时静默丢掉）。
func TestP2ToolsRegistered(t *testing.T) {
	p2, err := NewP2(nil, "")
	if err != nil || p2 == nil {
		t.Fatalf("NewP2: %v", err)
	}
	ts := NewToolSet(nil, "test")
	ts.SetP2(p2)

	planner := map[string]bool{}
	for _, tl := range ts.PlannerTools() {
		planner[tl.Name()] = true
	}
	for _, want := range []string{"rank_intents", "diagnose_progress", "list_capabilities", "owned_list", "nmap_scan", "httpx_probe", "nuclei_scan", "owned_due"} {
		if !planner[want] {
			t.Fatalf("planner missing tool %q", want)
		}
	}

	worker := map[string]bool{}
	for _, tl := range ts.WorkerTools() {
		worker[tl.Name()] = true
	}
	for _, want := range []string{"record_capability", "verify_claim", "cred_add", "cred_reuse_plan",
		"session_register", "session_lease", "owned_register", "owned_check", "owned_cleanup",
		"privesc_plan", "lateral_plan", "route_plan",
		"privesc_run", "lateral_run", "cleanup_run", "data_locate", "cred_harvest",
		"session_exec", "cred_reuse_run", "route_resolve", "data_mask"} {
		if !worker[want] {
			t.Fatalf("worker missing tool %q", want)
		}
	}

	main := map[string]bool{}
	for _, tl := range ts.MainAgentTools() {
		main[tl.Name()] = true
	}
	if !main["list_capabilities"] || !main["owned_list"] || !main["decide_capability"] {
		t.Fatal("main agent should be able to read capability cards, the ledger and decide")
	}
}

// 没有任务上下文时诊断应给明确错误，而不是 panic。
func TestDiagnoseProgressWithoutTask(t *testing.T) {
	ts := NewToolSet(nil, "test")
	if _, _, _, err := ts.DiagnoseProgress(context.Background(), false); err == nil {
		t.Fatal("expected error without exploration store")
	}
}

// 能力卡自动派生：没有 P2 时应安全返回错误（不 panic）。
func TestSyncCapabilitiesWithoutP2(t *testing.T) {
	ts := NewToolSet(nil, "test")
	if _, err := ts.SyncCapabilities(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "P2") {
		t.Fatalf("expected P2-required error, got %v", err)
	}
}

// 安全目标校验：命令注入字符必须被拒。
func TestCheckTargetRejectsInjection(t *testing.T) {
	bad := []string{"example.com; id", "example.com && whoami", "$(id)", "a|b", "`id`", "x\nrm -rf /"}
	for _, b := range bad {
		if err := checkTarget(b); err == nil {
			t.Fatalf("target %q must be rejected", b)
		}
	}
	for _, ok := range []string{"example.com", "10.0.0.0/24", "https://a.example.com:8443", "a.com,b.com"} {
		if err := checkTarget(ok); err != nil {
			t.Fatalf("target %q should pass: %v", ok, err)
		}
	}
}
