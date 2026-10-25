package agent

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

// 守一条容易被人"顺手删掉"的 SQL：继承型台账必须也算 HasOwned。
// 删掉它 -> 新任务即使继承了来源任务的台账，也挂不上 owned_* 工具，
// "跨任务长期持有"这个核心卖点在界面上就变成空的。
func TestGateStateSQLCoversInheritedOwned(t *testing.T) {
	if !strings.Contains(gateStateSQL, "source_task_ids") {
		t.Fatal("gateStateSQL 丢了继承判定（source_task_ids）：继承来的台账不会挂 owned_* 工具")
	}
	if !strings.Contains(gateStateSQL, "first_task_id = $1") {
		t.Fatal("gateStateSQL 丢了本任务台账判定（first_task_id）")
	}
}

// 守 R1 抓到的那个 bug：被门控的工具必须"对象"与"名字"成对出现——
// 对象进 Options.DeferredTools（仍可调用，只隐藏 schema），名字进 <available-deferred-tools>。
// 只给名字不给对象 = 诱导模型调一个会失败的工具；只给对象不给名字 = 模型不知道它存在。
func TestDeferredToolsMatchNames(t *testing.T) {
	ts := gateTestToolSet(t)

	wObjs := ts.P2WorkerDeferredTools()
	wNames := ts.P2WorkerDeferred()
	if len(wObjs) != len(wNames) {
		t.Fatalf("worker: 对象 %d 个 vs 名字 %d 个，必须成对", len(wObjs), len(wNames))
	}
	if got, want := deferredToolNames(wObjs), wNames; !sameStringSet(got, want) {
		t.Fatalf("worker 对象名与 deferred 名单不一致\n got=%v\nwant=%v", got, want)
	}

	pObjs := ts.P2PlannerDeferredTools()
	pNames := ts.P2PlannerDeferred()
	if len(pObjs) != len(pNames) || !sameStringSet(deferredToolNames(pObjs), pNames) {
		t.Fatalf("planner 对象名与 deferred 名单不一致")
	}

	// 被门控的工具**不能**再出现在可见集里（否则等于没藏 schema）
	visible := map[string]bool{}
	for _, tl := range ts.P2WorkerTools() {
		visible[tl.Name()] = true
	}
	for _, n := range wNames {
		if visible[n] {
			t.Fatalf("%q 同时出现在可见集与 deferred 名单里", n)
		}
	}
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]bool{}
	for _, s := range a {
		m[s] = true
	}
	for _, s := range b {
		if !m[s] {
			return false
		}
	}
	return true
}

// 审计口径：worker 常态工具 ≤14、planner ≤12（含基础领域工具，不只是 P2 子集）。
// 这条测试直接量"最终可见集"，防止再用 P2 子集的数字冒充整体。
func TestVisibleToolCountMeetsBudget(t *testing.T) {
	ts := gateTestToolSet(t)

	wv, wd := splitToolSet(ts.WorkerTools(), workerBaseDeferred)
	// 再叠加状态门（空状态）
	wvGated := gateWorkerTools(&gateState{}, wv)
	total := len(wvGated) + len(ts.P2WorkerDeferredTools())
	// 预算 14 → 15（2026-10-25）：新增常驻 kb_playbook（按形状查"这一类怎么验"）。
	// 它替代的是"模型自由发挥 + 漏步"造成的重试，+1 个 schema（约 60 token）小于省下的浪费，
	// 且它必须在常驻位（藏进 deferred 就不会被查，等于没接线）。
	t.Logf("worker: 基础可见=%d 基础deferred=%d 空态可见总计=%d (预算 15)", len(wv), len(wd), total)
	if total > 15 {
		t.Fatalf("worker 空态可见工具 %d 个，超过预算 15（%v）", total, toolNames(wvGated))
	}

	pv, pd := splitToolSet(ts.PlannerTools(), plannerBaseDeferred)
	pvGated := gatePlannerTools(&gateState{}, pv)
	ptotal := len(pvGated) + len(ts.P2PlannerDeferredTools())
	t.Logf("planner: 基础可见=%d 基础deferred=%d 空态可见总计=%d (预算 12)", len(pv), len(pd), ptotal)
	if ptotal > 12 {
		t.Fatalf("planner 空态可见工具 %d 个，超过预算 12（%v）", ptotal, toolNames(pvGated))
	}
}

// 守门：工具名单与门控分组必须严丝合缝 —— 这是"功能没接上"最容易出问题的地方。
//
//	① 门控分组里出现的名字，必须在 p2WorkerToolsAll() 里真的存在（否则是死名字，永远不生效）
//	② p2WorkerToolsAll() 里的每个工具，必须被某个分组覆盖（漏了就会永远可见，白占 token）
//	③ 同一个名字不许出现在两个分组里（判定会互相覆盖）
func TestGateGroupsMatchToolSurface(t *testing.T) {
	ts := gateTestToolSet(t)
	all := toolNames(ts.p2WorkerToolsAll())
	exists := map[string]bool{}
	for _, n := range all {
		exists[n] = true
	}

	groups := map[string]map[string]bool{
		"core": gateWorkerCore, "creds": gateWorkerCreds, "foothold": gateWorkerFoothold,
		"owned": gateWorkerOwned, "both": gateWorkerBoth, "data": gateWorkerData,
	}
	seen := map[string]string{}
	for gname, g := range groups {
		for n := range g {
			if !exists[n] {
				t.Errorf("分组 %s 里的 %q 在 p2WorkerToolsAll() 里不存在（死名字）", gname, n)
			}
			if prev, dup := seen[n]; dup {
				t.Errorf("%q 同时出现在 %s 与 %s 两个分组里（判定会互相覆盖）", n, prev, gname)
			}
			seen[n] = gname
		}
	}
	for _, n := range all {
		if _, ok := seen[n]; !ok {
			t.Errorf("%q 没有被任何门控分组覆盖（会永远可见，白占 token）", n)
		}
	}

	// 基础工具的延迟名单同样要能对上
	base := map[string]bool{}
	for _, n := range toolNames(ts.WorkerTools()) {
		base[n] = true
	}
	for n := range workerBaseDeferred {
		if !base[n] {
			t.Errorf("workerBaseDeferred 里的 %q 不在 WorkerTools() 里（死名字）", n)
		}
	}
}
