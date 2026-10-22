package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"

	actool "github.com/Autumn-27/norma/tool"
)

// DeferredInfo carries the deferred-tools wiring an agent needs to build its
// Options: the MCP tool names whose schemas are withheld, the subset listed in the
// global system-prompt block (non-skill-gated), and the shared session unlock set.
// UnlockSkill unlocks a named skill's MCPs — hosts call it to rebuild the unlock set
// from history on a resumed session (design doc C2).
type DeferredInfo struct {
	IntelligenceGuidance string
	CaseGuidance         string // independent of optional traffic binding

	FindingGuidance string            // derived from the final permitted tools, including DB overrides
	Deferred        []string          // all MCP tool names (schema withheld)
	GlobalNames     []string          // MCP names to list in the system-prompt block
	Unlock          *actool.UnlockSet // shared call-gate; nil when no MCP tools
	UnlockSkill     func(skillName string)
}

// ToolAugment, if set, returns the EXTRA tools an agent should see beyond its
// built-in base set — the agent's visible skills (packed into one Skill meta-tool)
// and visible MCP servers (expanded to mcp__server__tool). It also returns the
// DeferredInfo describing how those MCP tools are deferred/gated. The server wires
// it to the PG agent_visibility table. cleanup releases any spawned MCP clients.
//
// When nil, agents run with only their built-in tools — behavior is unchanged
// until a user assigns a skill/MCP to the agent in the UI.
var ToolAugment func(ctx context.Context, agentKey string) (extra []actool.CoreTool, def DeferredInfo, cleanup func())

// 写操作的通用护栏（由 server 注入；nil = 关闭，行为与原来完全一致）。
//
// WriteGuard 做幂等去重：同一次 (task,intent,step,tool,input) 重复写回时返回
// skip=true，用 message 代替真实执行——避免重跑把图污染成重复资产/重复事实。
// SnapshotHook 在写文件类工具执行前留快照（可回滚），由 server 决定要不要做。
var (
	WriteGuard   func(ctx context.Context, tool string, input json.RawMessage) (skip bool, message string)
	SnapshotHook func(ctx context.Context, tool string, input json.RawMessage)
)

// writeGuardedTools 是需要幂等/快照护栏的写入类工具名（含 SDK 的文件工具）。
var writeGuardedTools = map[string]bool{
	"insert_assets": true, "record_fact": true, "report_finding": true,
	"add_intent": true, "add_hint": true, "set_goals": true,
	"set_constraints": true, "record_intelligence": true,
	"Write": true, "Edit": true, "MultiEdit": true,
}

// wrapWriteGuard 把幂等与快照包在写入类工具外面。两个钩子都为 nil 时是纯透传。
type writeGuardedTool struct{ actool.CoreTool }

func (w *writeGuardedTool) Call(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	if SnapshotHook != nil {
		SnapshotHook(ctx, w.Name(), in)
	}
	if WriteGuard != nil {
		if skip, msg := WriteGuard(ctx, w.Name(), in); skip {
			if msg == "" {
				msg = "该写回已存在（幂等去重），本次不重复提交。"
			}
			return actool.Text(msg), nil
		}
	}
	return w.CoreTool.Call(ctx, in, tc)
}

// AugmentTools returns base plus the agent's visible skill/MCP tools, the
// DeferredInfo, and a cleanup func the caller must defer (closes MCP clients).
// Built-in base tools are kept as-is — never filtered (内置工具留代码层，不做可见性过滤).
func AugmentTools(ctx context.Context, agentKey string, base []actool.CoreTool) ([]actool.CoreTool, DeferredInfo, func()) {
	var (
		def     DeferredInfo
		cleanup = func() {}
		out     = base
	)
	if ToolAugment != nil {
		var extra []actool.CoreTool
		var cl func()
		extra, def, cl = ToolAugment(ctx, agentKey)
		if cl != nil {
			cleanup = cl
		}
		if len(extra) > 0 {
			out = append(append([]actool.CoreTool{}, base...), extra...)
		}
	}
	// DB tools table has the final say on the built-in tools: drop the ones this
	// agent isn't bound to (or that are disabled) and swap in overridden
	// descriptions/schemas + default injection. MCP/skill/host tools have no row
	// and pass through untouched, so deferred/unlock wiring stays consistent.
	if ToolResolve != nil {
		out = ToolResolve(ctx, agentKey, out)
	}
	if agentKey == "reporter" && isFindingCaseReview(ctx) {
		out = historicalFindingCaseTools(out)
	}
	out, def.FindingGuidance = findingWorkflowTools(agentKey, out)
	out, def.CaseGuidance = findingCaseWorkflow(agentKey, out)
	out, def.IntelligenceGuidance = intelligenceWorkflow(ctx, out)
	if agentKey == "reporter" && isFindingCaseReview(ctx) {
		def.FindingGuidance = ""
		def.CaseGuidance += historicalFindingReviewGuidance
	}
	for i, t := range out {
		if writeGuardedTools[t.Name()] {
			t = &writeGuardedTool{CoreTool: t}
		}
		out[i] = guardPanic(t)
	}
	return out, def, cleanup
}

// guardPanic turns a panicking tool handler into an ordinary tool error. The
// harness runs each tool on its own goroutine, so a panic inside a handler can't
// be recovered by the caller that started the run — it takes the whole process
// down, and on restart the agent replays the same call and crashes again. Applied
// last, so it covers every tool the agent can reach: domain, SDK, MCP and skill.
func guardPanic(t actool.CoreTool) actool.CoreTool { return &guardedTool{CoreTool: t} }

type guardedTool struct{ actool.CoreTool }

func (g *guardedTool) Call(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (res actool.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[tools] %s panic: %v\n%s", g.Name(), r, debug.Stack())
			res, err = actool.Errorf(fmt.Sprintf("工具 %s 内部错误：%v（本次调用已失败，可换个参数或改用别的工具）", g.Name(), r)), nil
		}
	}()
	return g.CoreTool.Call(ctx, in, tc)
}
