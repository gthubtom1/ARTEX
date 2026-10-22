package agent

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

// gateFailDB satisfies dbiface.DB and makes every query fail, so the gate must
// fall back to the full P2 tool set (fail-open).
type gateFailDB struct{}

func (gateFailDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("db down")
}
func (gateFailDB) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("db down")
}
func (gateFailDB) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return &sql.Row{}
}

func gateTestToolSet(t *testing.T) *ToolSet {
	t.Helper()
	p2, err := NewP2(nil, "")
	if err != nil || p2 == nil {
		t.Fatalf("NewP2: %v", err)
	}
	ts := NewToolSet(nil, "test")
	ts.SetP2(p2)
	return ts
}

func toolNameSet(tools []actool.CoreTool) map[string]bool {
	out := make(map[string]bool, len(tools))
	for _, tool := range tools {
		out[tool.Name()] = true
	}
	return out
}

func TestGateWorkerToolsByState(t *testing.T) {
	ts := gateTestToolSet(t)
	all := ts.p2WorkerToolsAll()
	if len(all) != 29 {
		t.Fatalf("worker P2 tool total = %d, want 29 (%v)", len(all), sortedUniqueNames(toolNames(all)))
	}
	if len(ts.p2PlannerToolsAll()) != 8 {
		t.Fatalf("planner P2 tool total = %d, want 8", len(ts.p2PlannerToolsAll()))
	}

	cases := []struct {
		name      string
		state     *gateState
		wantCount int
	}{
		{"empty", &gateState{}, 10},
		{"creds", &gateState{HasCreds: true}, 16},
		{"foothold", &gateState{HasFoothold: true}, 14},
		{"creds+foothold", &gateState{HasCreds: true, HasFoothold: true}, 24},
		{"owned", &gateState{HasOwned: true}, 14},
		{"data-finding", &gateState{HasDataFinding: true}, 11},
		{"all", &gateState{HasCreds: true, HasFoothold: true, HasOwned: true, HasDataFinding: true}, 29},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gateWorkerTools(tc.state, all)
			if len(got) != tc.wantCount {
				t.Fatalf("state %s: %d tools, want %d (%v)", tc.name, len(got), tc.wantCount, toolNames(got))
			}
		})
	}

	// Empty state: exactly the four always-on tools.
	empty := toolNameSet(gateWorkerTools(&gateState{}, all))
	for _, name := range []string{"record_capability", "list_capabilities", "verify_claim", "cred_add",
		"session_register", "owned_register", "privesc_plan", "lateral_plan", "route_plan", "data_locate"} {
		if !empty[name] {
			t.Fatalf("empty state missing core tool %q", name)
		}
	}
	for _, name := range []string{"cred_list", "session_exec", "owned_list", "privesc_run", "data_mask"} {
		if empty[name] {
			t.Fatalf("empty state should gate %q", name)
		}
	}

	// Both creds + foothold: +7 planning/execution tools on top of creds+foothold.
	both := toolNameSet(gateWorkerTools(&gateState{HasCreds: true, HasFoothold: true}, all))
	for _, name := range []string{"privesc_plan", "privesc_run", "lateral_plan", "lateral_run",
		"route_plan", "route_resolve", "cleanup_run"} {
		if !both[name] {
			t.Fatalf("creds+foothold missing %q", name)
		}
	}
}

func TestGatePlannerToolsByState(t *testing.T) {
	ts := gateTestToolSet(t)
	all := ts.p2PlannerToolsAll()

	cases := []struct {
		name      string
		state     *gateState
		wantCount int
	}{
		{"empty", &gateState{}, 3},
		{"creds", &gateState{HasCreds: true}, 4},
		{"foothold", &gateState{HasFoothold: true}, 5},
		{"owned", &gateState{HasOwned: true}, 5},
		{"all", &gateState{HasCreds: true, HasFoothold: true, HasOwned: true, HasDataFinding: true}, 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gatePlannerTools(tc.state, all)
			if len(got) != tc.wantCount {
				t.Fatalf("state %s: %d tools, want %d (%v)", tc.name, len(got), tc.wantCount, toolNames(got))
			}
			if len(got) > 12 {
				t.Fatalf("planner gate must stay <=12, got %d", len(got))
			}
		})
	}
}

func TestGateUnknownToolFailsOpen(t *testing.T) {
	ts := gateTestToolSet(t)
	all := append(ts.p2WorkerToolsAll(), actool.Build(actool.Spec{Name: "future_p2_tool"}))
	got := toolNameSet(gateWorkerTools(&gateState{}, all))
	if !got["future_p2_tool"] {
		t.Fatal("unmapped tool must stay visible (fail-open)")
	}
}

func TestGateNilStateReturnsAll(t *testing.T) {
	ts := gateTestToolSet(t)
	workerAll := ts.p2WorkerToolsAll()
	if got := gateWorkerTools(nil, workerAll); len(got) != len(workerAll) {
		t.Fatalf("nil worker state: %d tools, want full %d", len(got), len(workerAll))
	}
	plannerAll := ts.p2PlannerToolsAll()
	if got := gatePlannerTools(nil, plannerAll); len(got) != len(plannerAll) {
		t.Fatalf("nil planner state: %d tools, want full %d", len(got), len(plannerAll))
	}
}

func TestGateDeferredNamesCoverBlockedTools(t *testing.T) {
	ts := gateTestToolSet(t)
	all := ts.p2WorkerToolsAll()
	deferred := missingToolNames(all, gateWorkerTools(&gateState{}, all))
	// 29 个 P2 工具 - 空状态下常驻的 10 个 = 19 个进 deferred 名单
	if len(deferred) != 19 {
		t.Fatalf("empty-state worker deferred = %d names, want 19 (%v)", len(deferred), deferred)
	}
	want := map[string]bool{"cred_list": true, "session_exec": true, "owned_list": true, "privesc_run": true, "data_mask": true}
	got := make(map[string]bool, len(deferred))
	for _, name := range deferred {
		got[name] = true
	}
	for name := range want {
		if !got[name] {
			t.Fatalf("deferred list missing blocked tool %q: %v", name, deferred)
		}
	}
	for i := 1; i < len(deferred); i++ {
		if deferred[i-1] > deferred[i] {
			t.Fatalf("deferred names must be sorted: %v", deferred)
		}
	}

	plannerAll := ts.p2PlannerToolsAll()
	plannerDeferred := missingToolNames(plannerAll, gatePlannerTools(&gateState{}, plannerAll))
	if len(plannerDeferred) != 5 {
		t.Fatalf("empty-state planner deferred = %d names, want 5 (%v)", len(plannerDeferred), plannerDeferred)
	}
}

func TestP2GateDBErrorFailsOpen(t *testing.T) {
	p2, err := NewP2(gateFailDB{}, "")
	if err != nil || p2 == nil {
		t.Fatalf("NewP2: %v", err)
	}
	ts := NewToolSet(nil, "test")
	ts.SetP2(p2)
	ts.SetTaskID(42) // a task id alone must not let a DB error hide tools

	if got := len(ts.P2WorkerTools()); got != 29 {
		t.Fatalf("DB error worker tools = %d, want full 29", got)
	}
	if got := len(ts.P2PlannerTools()); got != 8 {
		t.Fatalf("DB error planner tools = %d, want full 8", got)
	}
	if got := ts.P2WorkerDeferred(); len(got) != 0 {
		t.Fatalf("DB error deferred = %v, want none", got)
	}
}

func TestP2GateNoTaskIDFailsOpen(t *testing.T) {
	ts := gateTestToolSet(t)
	if got := len(ts.P2WorkerTools()); got != 29 {
		t.Fatalf("no task id worker tools = %d, want full 29", got)
	}
	if got := len(ts.P2PlannerTools()); got != 8 {
		t.Fatalf("no task id planner tools = %d, want full 8", got)
	}
}

func toolNames(tools []actool.CoreTool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name())
	}
	return out
}
