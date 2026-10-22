// Package guard implements the safety boundary layer (docs §11): an audit log,
// user-configured intercept-rule evaluation, and Observer/G5 failure attribution.
// Every tool call passes through the PreToolUse hook before executing.
// (The RoE authorization-scope mechanism was removed; a replacement may be added
// later.) Destructive/exfil gating is no longer hard-coded here — it lives in the
// DB intercept rules (seeded as ordinary [内置] rules, so users can disable or
// delete them), evaluated via applyIntercept.
package guard

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/intercept"
	"github.com/Autumn-27/artex/internal/errev"
	"github.com/Autumn-27/norma/hook"
)

// AuditEntry records one gated tool call.
type AuditEntry struct {
	TS      int64  `json:"ts"`
	Tool    string `json:"tool"`
	Action  string `json:"action"` // allow|block
	Reason  string `json:"reason,omitempty"`
	Command string `json:"command,omitempty"`
}

// Guard enforces the side-effect policy via agent-core hooks.
type Guard struct {
	mu          sync.Mutex
	audit       []AuditEntry
	attrib      map[string]int // failure attribution counts (Observer / G5)
	reg         *hook.Registry
	interceptor *intercept.Interceptor // optional; nil disables user-configured rules
	auditSink   func(AuditEntry)       // optional external sink (audit_log); nil = off
}

// AutoDecide 是可选的外部自动裁决钩子（server 注入）：对规则命中 ask 的调用先问一次。
// ok=true 时 action=allow|deny 直接生效；ok=false 保持原有人工/模型审批路径。
var AutoDecide func(ctx context.Context, tool string, input json.RawMessage) (action, reason string, ok bool)

// ToolPresence 是"本机有没有这个命令"的查询钩子，由 server 注入（nil = 不检查）。
// 返回 ok=false 时 hint 是一条**正向**建议（例如"可用 masscan+nc 继续，或去环境页安装"）。
// 注意：它只影响"命令首词不存在"这一种情况，不做任何权限判断。
var ToolPresence func(name string) (ok bool, hint string)

// TaskIDOf 由 server 注入：从 ctx 里取当前任务 id（取不到返回 0）。
// 只用于把错误归类落到 task_error_events，不参与任何判定。
var TaskIDOf func(context.Context) int64

// errEvent 把一条工具侧错误写进任务级错误事件（P8-8）。
// 未注入 TaskIDOf / errev.Sink 时是空操作。
func errEvent(ctx context.Context, class, tool, detail string) {
	taskID := int64(0)
	if TaskIDOf != nil {
		taskID = TaskIDOf(ctx)
	}
	errev.Emit(ctx, taskID, class, tool, detail)
}

// firstCommand 从一行 shell 里取出真正要执行的命令名：
// 跳过 VAR=val 前缀、sudo/timeout/env 包装与路径前缀。
func firstCommand(cmd string) string {
	fields := strings.Fields(cmd)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if strings.Contains(f, "=") && !strings.HasPrefix(f, "/") && !strings.HasPrefix(f, "./") {
			continue // 环境变量前缀 FOO=bar
		}
		switch f {
		case "sudo", "env", "timeout", "nohup", "nice", "stdbuf", "proxychains4", "proxychains":
			// 包装器：继续看下一个（timeout 还带一个时长参数）
			if f == "timeout" && i+1 < len(fields) {
				i++
			}
			continue
		}
		if strings.HasPrefix(f, "-") {
			continue
		}
		if idx := strings.LastIndex(f, "/"); idx >= 0 {
			f = f[idx+1:]
		}
		return f
	}
	return ""
}

// New creates a Guard without user-configured intercept rules (used for pentest
// tasks where the Interceptor is not yet available).
func New() *Guard { return newGuard(nil) }

// NewWithInterceptor creates a Guard with user-configured intercept rules.
func NewWithInterceptor(ic *intercept.Interceptor) *Guard { return newGuard(ic) }

func newGuard(ic *intercept.Interceptor) *Guard {
	g := &Guard{attrib: map[string]int{}, interceptor: ic}
	g.reg = hook.NewRegistry().
		On(hook.PreToolUse, g.preToolUse).
		On(hook.PostToolUse, g.postToolUse)
	return g
}

// Hooks returns the hook registry to attach to an agent session.
func (g *Guard) Hooks() *hook.Registry { return g.reg }

func (g *Guard) preToolUse(ctx context.Context, ev hook.Event) hook.Result {
	// Extract the shell-command surface for the audit log: Bash + the interactive-shell
	// tools (shell_open's command, shell_send's text). Destructive/exfil gating is no
	// longer hard-coded here — it now lives in the DB intercept rules, evaluated by
	// applyIntercept below. Other tools record an empty command.
	var cmd string
	switch ev.ToolName {
	case "Bash", "shell_open":
		var in struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(ev.Input, &in)
		cmd = in.Command
	case "shell_send":
		var in struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(ev.Input, &in)
		cmd = in.Text
	}
	g.record(ev.ToolName, "allow", "", cmd)
	// 工具存在性：命令首词不在本机时，给出**可执行的替代方案**，
	// 而不是把 "command not found" 甩给模型（那会让它开始乱搓）。
	if ToolPresence != nil && cmd != "" && (ev.ToolName == "Bash" || ev.ToolName == "shell_open" || ev.ToolName == "shell_send") {
		if name := firstCommand(cmd); name != "" {
			if ok, hint := ToolPresence(name); !ok {
				if hint == "" {
					hint = "本机未找到 " + name + "。可以改用等价工具继续，或到环境页安装后再试。"
				}
				errEvent(ctx, errev.ClassToolMissing, name, cmd)
				return g.block(ev.ToolName, hint, cmd)
			}
		}
	}
	// P8-6：出口闸 —— 只在"这次调用真的要发包"时校验，纯本地命令不打扰。
	// ARTEX_ALLOW_DIRECT=1 时 server 不会注入 EgressCheck，这里自然跳过。
	if EgressCheck != nil && egressNeeded(ev.ToolName, cmd) {
		if err := EgressCheck(ctx); err != nil {
			errEvent(ctx, errev.ClassEnvError, ev.ToolName, err.Error())
			return g.block(ev.ToolName, egressBlockMessage(err.Error()), cmd)
		}
	}
	return g.applyIntercept(ctx, ev)
}

// applyIntercept evaluates user-configured intercept rules against the tool call.
// Both rules and the fallback judge receive the complete tool input.
func (g *Guard) applyIntercept(ctx context.Context, ev hook.Event) hook.Result {
	if g.interceptor == nil {
		return hook.Result{}
	}
	if !g.interceptor.IsToolEnabled(ev.ToolName) {
		return hook.Result{}
	}
	ctx = intercept.WithCall(ctx, ev.ToolName, ev.Input)
	dec, matched := g.interceptor.Match(ev.ToolName, ev.Input)
	if !matched {
		// No rule matched. Ask the LLM fallback judge (if enabled); when it is off
		// or unwired, keep current behavior and allow.
		d, judged := g.interceptor.Judge(ctx, ev.ToolName, ev.Input)
		if !judged {
			return hook.Result{}
		}
		dec = d
	}
	switch dec.Action {
	case "deny":
		// 观测:deny 命中不阻塞审批,直接记一条 denied（历史/任务拦截页可见）。
		g.interceptor.Log(ctx, intercept.ConvIDFromContext(ctx), dec, ev.ToolName, ev.Input, "denied")
		errEvent(ctx, errev.ClassPolicyBlocked, ev.ToolName, dec.Message)
		return g.block(ev.ToolName, systemBlockMessage(dec.Message), "")
	case "allow":
		// Record explicit rule and model approvals so review details remain auditable.
		g.interceptor.Log(ctx, intercept.ConvIDFromContext(ctx), dec, ev.ToolName, ev.Input, "allowed")
		return hook.Result{}
	case "ask":
		// 自动裁决：规则命中 ask 时先给外部钩子一次机会（off 模式钩子返回 ok=false）。
		if AutoDecide != nil {
			if action, reason, decided := AutoDecide(ctx, ev.ToolName, ev.Input); decided {
				if action == "allow" {
					g.interceptor.Log(ctx, intercept.ConvIDFromContext(ctx), dec, ev.ToolName, ev.Input, "allowed")
					return hook.Result{}
				}
				if reason == "" {
					reason = "自动裁决拒绝"
				}
				g.interceptor.Log(ctx, intercept.ConvIDFromContext(ctx), dec, ev.ToolName, ev.Input, "denied")
				errEvent(ctx, errev.ClassPolicyBlocked, ev.ToolName, reason)
				return g.block(ev.ToolName, systemBlockMessage(reason), "")
			}
		}
		// If the worker context is already cancelled (task stopped / killed), block
		// immediately without creating a pending record — avoids orphaned DB entries
		// and makes execOne complete fast, reducing the race against drainSynthetic.
		if ctx.Err() != nil {
			return g.block(ev.ToolName, systemBlockMessage("工作已取消，平台安全管控阻止执行"), "")
		}
		convID := intercept.ConvIDFromContext(ctx)
		if !g.interceptor.HandleAsk(ctx, convID, dec, ev.ToolName, ev.Input) {
			return g.block(ev.ToolName, systemBlockMessage("人工审批未通过（用户拒绝或审批超时）"), "")
		}
		return hook.Result{}
	}
	return hook.Result{}
}

// systemBlockMessage frames an intercept block as an ARTEX platform-governance
// decision so the agent does not mistake it for a target-side defense.
//
// The bare reasons ("禁止执行此工具" / "用户拒绝") read exactly like a WAF/403 on
// the target, so a pentest agent's instinct is to bypass them — rewrite the
// command, swap the payload, re-encode, retry. That is both futile (the platform
// blocks the class of action, not one string) and wrong (it's a policy decision,
// not an obstacle to defeat). This prefix states plainly that the block comes
// from the platform, is not the target's protection, and that the operation is
// forbidden — so the agent pivots to another approach instead of evading it.
// Audit/history rows keep the raw reason (see Interceptor.Log); only the
// model-facing tool_result carries this framing.
func systemBlockMessage(reason string) string {
	return "【ARTEX 平台管控·非目标防御】此调用被平台拦截。" +
		"原因：" + reason + "。此操作被禁止。"
}

var reBlocked = regexp.MustCompile(`(?i)\b(403|forbidden|waf|blocked|rate.?limit|429|captcha|denied)\b`)

// postToolUse is the Observer failure-attribution hook (G5): it classifies tool
// results into blocked / error / ok so the planner can change strategy instead
// of giving up at a WAF.
func (g *Guard) postToolUse(_ context.Context, ev hook.Event) hook.Result {
	if ev.ToolName != "Bash" {
		return hook.Result{}
	}
	class := "ok"
	switch {
	case reBlocked.Match(ev.Result):
		class = "blocked"
	case ev.IsError:
		class = "error"
	}
	g.mu.Lock()
	g.attrib[class]++
	g.mu.Unlock()
	return hook.Result{}
}

// Attributions returns failure-attribution counts (Observer / G5).
func (g *Guard) Attributions() map[string]int {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[string]int, len(g.attrib))
	for k, v := range g.attrib {
		out[k] = v
	}
	return out
}

func (g *Guard) block(tool, reason, cmd string) hook.Result {
	g.record(tool, "block", reason, cmd)
	return hook.Result{Decision: "block", Message: reason}
}

// SetAuditSink installs an external audit sink (e.g. db-backed audit_log).
// 审计是后台记录：sink 失败不影响工具执行，也不阻断运行。
func (g *Guard) SetAuditSink(fn func(AuditEntry)) {
	g.mu.Lock()
	g.auditSink = fn
	g.mu.Unlock()
}

func (g *Guard) record(tool, action, reason, cmd string) {
	entry := AuditEntry{TS: time.Now().Unix(), Tool: tool, Action: action, Reason: reason, Command: cmd}
	g.mu.Lock()
	g.audit = append(g.audit, entry)
	if len(g.audit) > 2000 {
		g.audit = g.audit[len(g.audit)-2000:]
	}
	sink := g.auditSink
	g.mu.Unlock()
	if sink != nil {
		sink(entry)
	}
}

// Audit returns a snapshot of recent gated calls (most recent last).
func (g *Guard) Audit() []AuditEntry {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]AuditEntry, len(g.audit))
	copy(out, g.audit)
	return out
}
