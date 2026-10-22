package agent

import (
	"context"
	"sort"
	"time"

	actool "github.com/Autumn-27/norma/tool"
)

// gateState is the per-run snapshot that decides which P2 tools are worth
// mounting. It is deliberately tiny: four booleans, read once at run start.
// A nil *gateState means "state unknown" and every gate fails open (full set).
type gateState struct {
	HasCreds       bool // creds_vault row for this task, or kind=credential intelligence
	HasFoothold    bool // access_refs row for this task, or a live in-memory session
	HasOwned       bool // assets_owned row first seen by this task
	HasDataFinding bool // a finding whose vulnclass looks like a database issue
}

// Worker groups. Keep these in sync with p2WorkerToolsAll; TestGateWorkerToolsByState
// asserts the groups cover every registered worker P2 tool exactly once.
var (
	gateWorkerCore = map[string]bool{
		"record_capability": true, "list_capabilities": true,
		"verify_claim": true, "cred_add": true,
	}
	gateWorkerCreds = map[string]bool{
		"cred_list": true, "cred_reuse_plan": true, "cred_verify": true,
		"cred_harvest": true, "cred_stale_scan": true, "cred_reuse_run": true,
	}
	gateWorkerFoothold = map[string]bool{
		"session_register": true, "session_lease": true, "session_list": true,
		"session_release": true, "session_exec": true,
	}
	gateWorkerOwned = map[string]bool{
		"owned_register": true, "owned_list": true, "owned_check": true,
		"owned_status": true, "owned_cleanup": true,
	}
	gateWorkerBoth = map[string]bool{
		"privesc_plan": true, "privesc_run": true,
		"lateral_plan": true, "lateral_run": true,
		"route_plan": true, "route_resolve": true, "cleanup_run": true,
	}
	gateWorkerData = map[string]bool{
		"data_locate": true, "data_mask": true,
	}
)

// Planner groups: only read-only/diagnostic tools, gated the same way. Anything
// unknown stays visible (fail-open) so a newly added tool is never silently lost.
var (
	gatePlannerCore = map[string]bool{
		"rank_intents": true, "diagnose_progress": true, "list_capabilities": true,
	}
	gatePlannerCreds = map[string]bool{
		"cred_list": true,
	}
	gatePlannerFoothold = map[string]bool{
		"session_list": true, "route_resolve": true,
	}
	gatePlannerOwned = map[string]bool{
		"owned_list": true, "owned_due": true,
	}
)

// gateStateSQL reads all four signals in one round trip. Any error (missing table,
// DB down, old schema) is treated as "state unknown" by the caller → fail open.
const gateStateSQL = `SELECT
    (EXISTS(SELECT 1 FROM creds_vault WHERE task_id = $1)
     OR EXISTS(SELECT 1 FROM task_intelligence WHERE task_id = $1 AND kind = 'credential')),
    EXISTS(SELECT 1 FROM access_refs ar
           JOIN assets_owned o ON o.id = ar.owned_id
           WHERE o.first_task_id = $1),
    EXISTS(SELECT 1 FROM assets_owned WHERE first_task_id = $1),
    EXISTS(SELECT 1 FROM findings
           WHERE task_id = $1
             AND (lower(vulnclass) LIKE '%sql%'
               OR lower(vulnclass) LIKE '%injection%'
               OR lower(vulnclass) LIKE '%db%'
               OR lower(vulnclass) LIKE '%database%'
               OR lower(vulnclass) LIKE '%mysql%'
               OR lower(vulnclass) LIKE '%postgres%'))`

// gateState loads the task's current state. It returns nil when the state cannot
// be established (no P2, no DB, no task id, query/scan error) — callers pass nil
// to the gate functions to get the full tool set.
//
// last_verified_at is intentionally not used for ownership: it is a per-row
// verification timestamp, not a task link. assets_owned.first_task_id is the
// only column that says "this task has a ledger row".
func (t *ToolSet) gateState() *gateState {
	if t == nil || t.p2 == nil || t.p2.DB == nil || t.taskID <= 0 {
		return nil
	}
	st := &gateState{}
	if t.p2.Pool != nil && len(t.p2.Pool.List(0)) > 0 {
		st.HasFoothold = true // in-process session survives even if the DB lags
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := t.p2.DB.QueryContext(ctx, gateStateSQL, t.taskID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	if !rows.Next() {
		return nil
	}
	var creds, foothold, owned, dataFinding bool
	if err := rows.Scan(&creds, &foothold, &owned, &dataFinding); err != nil {
		return nil
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	st.HasCreds = st.HasCreds || creds
	st.HasFoothold = st.HasFoothold || foothold
	st.HasOwned = owned
	st.HasDataFinding = dataFinding
	return st
}

// gateWorkerTools is the pure "state → worker tool list" decision function.
// nil state = unknown = return everything (fail open). Unknown tool names are
// also kept, so adding a tool without touching the gate cannot hide it.
func gateWorkerTools(state *gateState, all []actool.CoreTool) []actool.CoreTool {
	if state == nil {
		return all
	}
	out := make([]actool.CoreTool, 0, len(all))
	for _, tool := range all {
		name := tool.Name()
		keep := false
		switch {
		case gateWorkerCore[name]:
			keep = true
		case gateWorkerCreds[name]:
			keep = state.HasCreds
		case gateWorkerFoothold[name]:
			keep = state.HasFoothold
		case gateWorkerOwned[name]:
			keep = state.HasOwned
		case gateWorkerBoth[name]:
			keep = state.HasCreds && state.HasFoothold
		case gateWorkerData[name]:
			keep = state.HasDataFinding
		default:
			keep = true // unmapped/new tool → fail open
		}
		if keep {
			out = append(out, tool)
		}
	}
	return out
}

// gatePlannerTools is the same decision for the planner's read-only set.
func gatePlannerTools(state *gateState, all []actool.CoreTool) []actool.CoreTool {
	if state == nil {
		return all
	}
	out := make([]actool.CoreTool, 0, len(all))
	for _, tool := range all {
		name := tool.Name()
		keep := false
		switch {
		case gatePlannerCore[name]:
			keep = true
		case gatePlannerCreds[name]:
			keep = state.HasCreds
		case gatePlannerFoothold[name]:
			keep = state.HasFoothold
		case gatePlannerOwned[name]:
			keep = state.HasOwned
		default:
			keep = true // unmapped/new tool → fail open
		}
		if keep {
			out = append(out, tool)
		}
	}
	return out
}

// p2WorkerToolsAll is the complete worker P2 surface. P2WorkerTools gates it;
// keeping the full list here makes the deferred-name computation trivial.
func (t *ToolSet) p2WorkerToolsAll() []actool.CoreTool {
	out := []actool.CoreTool{
		t.recordCapability(), t.listCapabilities(), t.verifyClaimTool(),
		t.addCred(), t.listCreds(), t.credReusePlan(), t.verifyCred(),
		t.registerSession(), t.leaseSession(), t.listSessions(), t.releaseSession(),
		t.registerOwned(), t.listOwned(), t.checkOwned(), t.statusOwned(), t.cleanupOwned(),
		t.privescPlan(), t.lateralPlan(), t.routePlan(),
		t.dataLocate(), t.dataMask(), t.credHarvest(), t.credStaleScan(),
	}
	// L3/L4 execution surface + session command + credential reuse + route lookup.
	out = append(out, t.P2OpsTools()...)
	out = append(out, t.sessionExec(), t.credReuseRun(), t.routeResolve())
	return out
}

// p2PlannerToolsAll is the complete planner P2 surface (read/diagnose only).
func (t *ToolSet) p2PlannerToolsAll() []actool.CoreTool {
	return []actool.CoreTool{
		t.rankIntents(), t.diagnoseProgress(), t.listCapabilities(),
		t.listCreds(), t.listSessions(), t.listOwned(), t.dueOwned(), t.routeResolve(),
	}
}

// p2WorkerDeferredNames returns the tools the current state hides, sorted. These
// names belong in DeferredInfo.GlobalNames so the model still knows they exist.
func p2WorkerDeferredNames(t *ToolSet) []string {
	if t == nil || t.p2 == nil {
		return nil
	}
	all := t.p2WorkerToolsAll()
	return missingToolNames(all, gateWorkerTools(t.gateState(), all))
}

// p2PlannerDeferredNames is the planner equivalent.
func p2PlannerDeferredNames(t *ToolSet) []string {
	if t == nil || t.p2 == nil {
		return nil
	}
	all := t.p2PlannerToolsAll()
	return missingToolNames(all, gatePlannerTools(t.gateState(), all))
}

// missingToolNames returns names present in all but absent from visible, sorted.
func missingToolNames(all, visible []actool.CoreTool) []string {
	seen := make(map[string]bool, len(visible))
	for _, tool := range visible {
		seen[tool.Name()] = true
	}
	var out []string
	for _, tool := range all {
		if !seen[tool.Name()] {
			out = append(out, tool.Name())
		}
	}
	return sortedUniqueNames(out)
}

// sortedUniqueNames is a small helper for tests and deferred wiring.
func sortedUniqueNames(names []string) []string {
	out := append([]string(nil), names...)
	sort.Strings(out)
	// Drop duplicates in place.
	if len(out) < 2 {
		return out
	}
	n := 1
	for i := 1; i < len(out); i++ {
		if out[i] != out[n-1] {
			out[n] = out[i]
			n++
		}
	}
	return out[:n]
}
