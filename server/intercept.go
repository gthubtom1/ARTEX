package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/guard"
	"github.com/Autumn-27/artex/intercept"
	"github.com/Autumn-27/artex/internal/audit"
	"github.com/Autumn-27/artex/internal/reviewer"
	"github.com/Autumn-27/artex/llmrec"
	"github.com/Autumn-27/norma/llm"
)

// judgeWorkerLane is the usage-ledger "worker" label for intercept fallback-judge
// calls, so judge spend can be queried apart from the worker/planner/main lanes.
const judgeWorkerLane = "judge"

// chatGuard returns a guard wired with the manager's interceptor, used for chat
// conversations. Called once per applyLLM so a new LLM config always gets a fresh guard.
func (s *Server) chatGuard() *guard.Guard {
	return guard.NewWithInterceptor(s.m.interceptor)
}

// wireInterceptReviewer installs the LLM fallback judge into the interceptor. The
// judge runs only on tool calls that matched no rule (see intercept.Judge). It
// resolves the configured judge profile (0 → active/default), builds a provider,
// runs a one-shot JSON classification with an explanation for every verdict.
func (s *Server) wireInterceptReviewer() {
	// 规则命中 ask 的路径也要能被自动裁决覆盖（guard 在 HandleAsk 之前回调这里）。
	guard.AutoDecide = func(ctx context.Context, tool string, input json.RawMessage) (string, string, bool) {
		dec, decided := s.autoDecideReview(ctx, intercept.ReviewInput{Tool: tool, Arguments: input})
		if !decided {
			return "", "", false
		}
		action := dec.Action
		if action == "" {
			action = "ask"
		}
		return action, dec.Message, true
	}
	s.m.interceptor.SetReviewer(func(ctx context.Context, profileID int64, prompt string, input intercept.ReviewInput) (intercept.Decision, error) {
		// Auto-decision runs closest to the ask/judge boundary without changing the
		// intercept.Reviewer signature: allow/deny are terminal, ask preserves the
		// existing human-approval path untouched.
		if dec, decided := s.autoDecideReview(ctx, input); decided {
			return dec, nil
		}
		if profileID == 0 {
			if p, err := s.m.pg.ActiveProfile(); err == nil && p != nil {
				profileID = p.ID
			}
		}
		if profileID == 0 {
			return intercept.Decision{}, fmt.Errorf("未配置可用的裁判模型")
		}
		prov, _, ok := s.providerForProfile(profileID)
		if !ok {
			return intercept.Decision{ProfileID: profileID}, fmt.Errorf("裁判模型 profile %d 不可用", profileID)
		}
		// Tag this call's usage as the "judge" lane so the config page can report
		// how much the fallback approval has spent, separate from model profiles.
		ctx = llmrec.WithWorker(ctx, judgeWorkerLane)
		text, err := reviewCompletion(ctx, prov, prompt, input)
		if err != nil {
			return intercept.Decision{ProfileID: profileID}, err
		}
		v := intercept.ParseVerdict(text)
		if v.Action == "" {
			return intercept.Decision{ProfileID: profileID}, fmt.Errorf("模型裁决格式无效，必须包含裁决、实际操作、成功后的后果和命中规则")
		}
		return intercept.Decision{Action: v.Action, Message: v.Reason, ProfileID: profileID}, nil
	})
}

// autoDecideMode reads intercept_auto_decide. Anything other than the two
// explicit opt-ins is treated as off, preserving the legacy behavior.
func (s *Server) autoDecideMode() string {
	if s.settings == nil {
		return "off"
	}
	switch strings.ToLower(strings.TrimSpace(s.settings.String("intercept_auto_decide", "off"))) {
	case "allow_all":
		return "allow_all"
	case "reviewer":
		return "reviewer"
	default:
		return "off"
	}
}

// autoDecideReview applies the zero-token reviewer to the current judge request.
// The second return value is false when the existing approval path must run
// unchanged (off, or the reviewer itself is uncertain).
func (s *Server) autoDecideReview(ctx context.Context, in intercept.ReviewInput) (intercept.Decision, bool) {
	mode := s.autoDecideMode()
	if mode == "off" {
		return intercept.Decision{}, false
	}
	req := reviewRequestForAutoDecide(in)
	req.TaskConstraints = s.taskConstraintsForCtx(ctx)
	if mode == "allow_all" {
		return s.recordAutoDecision(ctx, req, mode, reviewer.Result{
			Decision: reviewer.Allow, Reason: "allow_all 模式：直接放行", Confidence: "high",
		}), true
	}
	res := reviewer.Review(req)
	if res.Decision == reviewer.Ask {
		return intercept.Decision{}, false
	}
	return s.recordAutoDecision(ctx, req, mode, res), true
}

// taskConstraintsForCtx 从当前工具调用上下文（RunInfo.TaskID）读取任务 allow/deny 约束。
func (s *Server) taskConstraintsForCtx(ctx context.Context) []string {
	if s.m == nil || s.m.pg == nil {
		return nil
	}
	ri := agent.RunInfoFrom(ctx)
	if ri.TaskID <= 0 {
		return nil
	}
	task, err := s.m.pg.GetTask(ri.TaskID)
	if err != nil || task == nil {
		return nil
	}
	list, err := s.m.pg.Exploration(task.ExplorationID).ListConstraints()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, c := range list {
		out = append(out, c.Kind+": "+c.Text)
	}
	return out
}

func (s *Server) recordAutoDecision(ctx context.Context, req reviewer.Request, mode string, res reviewer.Result) intercept.Decision {
	action, label := "allow", "auto_allowed"
	if res.Decision == reviewer.Deny {
		action, label = "deny", "auto_denied"
	}
	reason := strings.TrimSpace(res.Reason)
	if reason == "" {
		reason = "未提供理由"
	}
	confidence := strings.TrimSpace(res.Confidence)
	if confidence == "" {
		confidence = "medium"
	}
	if s.auditLog != nil {
		s.auditLog.Log(ctx, audit.Entry{
			Tool: req.Tool, Target: req.Target, Action: "intercept", Result: label,
			Args: []byte(req.Args), Meta: map[string]any{
				"auto_allowed": action == "allow", "auto_decided": true,
				"mode": mode, "reason": reason, "confidence": confidence,
			},
		})
	}
	return intercept.Decision{
		Action:        action,
		Message:       fmt.Sprintf("[自动裁决][%s] %s（置信度 %s）", label, reason, confidence),
		ModelFallback: true,
		RuleName:      "auto:" + mode,
	}
}

func reviewRequestForAutoDecide(in intercept.ReviewInput) reviewer.Request {
	req := reviewer.Request{Tool: in.Tool, Args: string(in.Arguments)}
	var payload map[string]any
	if json.Unmarshal(in.Arguments, &payload) == nil {
		req.Command = autoDecideString(payload, "command", "cmd", "script", "text", "code", "input")
		req.Target = autoDecideString(payload, "target", "host", "url", "uri", "ip", "domain", "path", "file")
	}
	if req.Target == "" {
		req.Target = strings.TrimSpace(in.WorkingDir)
	}
	return req
}

func autoDecideString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := payload[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func reviewCompletion(ctx context.Context, prov llm.Provider, prompt string, input intercept.ReviewInput) (string, error) {
	user, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	return streamCollectText(ctx, prov, prompt, string(user))
}

// collectText 是一次性取文本的通用入口：
//   - nonStreaming=true 时走 Complete（openai-responses 等流式不稳定的端点用这条）；
//   - 否则走 Stream 累积 SETextDelta。
// 与 streamCollectText 的区别只有"传输方式"，提示词与预算由调用方给。
func collectText(ctx context.Context, prov llm.Provider, nonStreaming bool, system, user string, maxTokens int) (string, error) {
	temp := 0.0
	if maxTokens <= 0 || maxTokens > 2048 {
		maxTokens = 1024
	}
	req := llm.CompletionRequest{
		System:      []string{system},
		Messages:    []llm.Message{llm.UserText(user)},
		MaxTokens:   maxTokens,
		Temperature: &temp,
		Thinking:    "disabled",
	}
	if nonStreaming {
		msg, stop, _, err := prov.Complete(ctx, req)
		if err != nil {
			return "", err
		}
		if stop == "max_tokens" || stop == "length" {
			return "", fmt.Errorf("模型输出达到上限")
		}
		return strings.TrimSpace(msg.Text()), nil
	}
	var sb strings.Builder
	for ev, err := range prov.Stream(ctx, req) {
		if err != nil {
			// 某些端点（如 openai-responses 经代理）会在 output_item.done 之后
			// 直接断流、不发 terminal 事件，但文本其实已经完整。
			// 已积累到可用长度时按成功返回，避免把完整结论丢掉。
			if sb.Len() >= 40 {
				log.Printf("[llm] stream closed early, keep %d bytes: %v", sb.Len(), err)
				return sb.String(), nil
			}
			return "", err
		}
		if ev.Type == llm.SETextDelta {
			sb.WriteString(ev.Text)
		}
	}
	return sb.String(), nil
}

// streamCollectText runs a single non-streaming-style completion (thinking off,
// low temperature, bounded output) and returns the concatenated text. The
// budget includes the explanation and complete closing JSON delimiters.
func streamCollectText(ctx context.Context, prov llm.Provider, system, user string) (string, error) {
	temp := 0.0
	req := llm.CompletionRequest{
		System:      []string{system},
		Messages:    []llm.Message{llm.UserText(user)},
		MaxTokens:   1024,
		Temperature: &temp,
		Thinking:    "disabled",
	}
	var sb strings.Builder
	for ev, err := range prov.Stream(ctx, req) {
		if err != nil {
			// 某些端点（如 openai-responses 经代理）会在 output_item.done 之后
			// 直接断流、不发 terminal 事件，但文本其实已经完整。
			// 已积累到可用长度时按成功返回，避免把完整结论丢掉。
			if sb.Len() >= 40 {
				log.Printf("[llm] stream closed early, keep %d bytes: %v", sb.Len(), err)
				return sb.String(), nil
			}
			return "", err
		}
		if ev.Type == llm.SETextDelta {
			sb.WriteString(ev.Text)
		}
	}
	return sb.String(), nil
}

// --- intercept rule CRUD ---

func (s *Server) interceptListRules(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	rules, err := pg.ListInterceptRules()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if rules == nil {
		rules = []db.InterceptRule{}
	}
	writeJSON(w, 200, map[string]any{"rules": rules})
}

func (s *Server) interceptCreateRule(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req interceptRuleReq
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := validateInterceptRuleReq(req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	rule, err := pg.CreateInterceptRule(req.Name, req.MatchTarget, req.MatchType, req.Pattern, req.Action, req.Message, req.Priority, req.Enabled, req.TimeoutEnabled, req.TimeoutSeconds, req.TimeoutAction)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.m.interceptor.Invalidate()
	writeJSON(w, 200, rule)
}

func (s *Server) interceptUpdateRule(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "bad rule id")
		return
	}
	var req interceptRuleReq
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := validateInterceptRuleReq(req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	rule, err := pg.UpdateInterceptRule(id, req.Name, req.MatchTarget, req.MatchType, req.Pattern, req.Action, req.Message, req.Priority, req.Enabled, req.TimeoutEnabled, req.TimeoutSeconds, req.TimeoutAction)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.m.interceptor.Invalidate()
	writeJSON(w, 200, rule)
}

func (s *Server) interceptDeleteRule(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "bad rule id")
		return
	}
	if err := pg.DeleteInterceptRule(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.m.interceptor.Invalidate()
	writeJSON(w, 200, map[string]any{"deleted": id})
}

func (s *Server) interceptToggleRule(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "bad rule id")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := pg.ToggleInterceptRule(id, req.Enabled); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.m.interceptor.Invalidate()
	writeJSON(w, 200, map[string]any{"ok": true, "enabled": req.Enabled})
}

// --- pending (ask) ---

func (s *Server) interceptListPending(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	pending, err := pg.ListPendingIntercepts()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if pending == nil {
		pending = []db.InterceptPending{}
	}
	writeJSON(w, 200, map[string]any{"pending": pending})
}

func (s *Server) interceptGetOne(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "bad pending id")
		return
	}
	p, err := pg.GetInterceptPending(id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if p == nil {
		writeErr(w, 404, "not found")
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) interceptListTaskItems(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	taskID := r.PathValue("taskID")
	if taskID == "" {
		writeErr(w, 400, "bad task id")
		return
	}
	q := r.URL.Query()
	filter, err := interceptFilterParams(q)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if q.Get("page") == "" && q.Get("size") == "" && filter == (db.InterceptApprovalFilter{}) {
		items, err := pg.ListTaskIntercepts(taskID)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if items == nil {
			items = []db.InterceptApprovalRow{}
		}
		writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
		return
	}
	page, size := interceptPageParams(q)
	items, total, err := pg.ListTaskInterceptsPage(taskID, page, size, filter)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if items == nil {
		items = []db.InterceptApprovalRow{}
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size})
}

func (s *Server) interceptHistory(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	q := r.URL.Query()
	filter, err := interceptFilterParams(q)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if q.Get("page") == "" && q.Get("size") == "" && filter == (db.InterceptApprovalFilter{}) {
		items, err := pg.ListAllIntercepts(200)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if items == nil {
			items = []db.InterceptApprovalRow{}
		}
		writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
		return
	}
	page, size := interceptPageParams(q)
	items, total, err := pg.ListAllInterceptsPage(page, size, filter)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if items == nil {
		items = []db.InterceptApprovalRow{}
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size})
}

func interceptFilterParams(q url.Values) (db.InterceptApprovalFilter, error) {
	filter := db.InterceptApprovalFilter{
		Status:         q.Get("status"),
		DecisionSource: q.Get("decision_source"),
		Search:         strings.TrimSpace(q.Get("q")),
		CreatedFrom:    q.Get("from"),
		CreatedTo:      q.Get("to"),
	}
	if utf8.RuneCountInString(filter.Search) > 200 {
		return filter, fmt.Errorf("q 不能超过 200 个字符")
	}
	if strings.HasPrefix(filter.Search, "#") {
		id, err := strconv.ParseInt(strings.TrimPrefix(filter.Search, "#"), 10, 64)
		if err != nil || id <= 0 {
			return filter, fmt.Errorf("审批编号必须为 # 后接正整数")
		}
	}
	for name, value := range map[string]string{"from": filter.CreatedFrom, "to": filter.CreatedTo} {
		if value == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return filter, fmt.Errorf("%s 必须是 YYYY-MM-DD 日期", name)
		}
	}
	if filter.CreatedFrom != "" && filter.CreatedTo != "" && filter.CreatedFrom > filter.CreatedTo {
		return filter, fmt.Errorf("from 不能晚于 to")
	}
	switch filter.Status {
	case "", "pending", "allowed", "denied", "timeout":
	default:
		return filter, fmt.Errorf("status 必须是 pending、allowed、denied 或 timeout")
	}
	switch filter.DecisionSource {
	case "", "model", "rule", "manual", "unknown":
	default:
		return filter, fmt.Errorf("decision_source 必须是 model、rule、manual 或 unknown")
	}
	return filter, nil
}

func interceptPageParams(q url.Values) (int, int) {
	page := atoiDefault(q.Get("page"), 1)
	size := atoiDefault(q.Get("size"), 20)
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

func (s *Server) interceptDecide(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "bad pending id")
		return
	}
	var req struct {
		Decision string `json:"decision"` // "allowed" | "denied"
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Decision != "allowed" && req.Decision != "denied" {
		writeErr(w, 400, "decision 必须是 allowed 或 denied")
		return
	}
	if err := s.m.interceptor.Decide(id, req.Decision == "allowed"); err != nil {
		if errors.Is(err, intercept.ErrAlreadyDecided) {
			writeErr(w, 409, err.Error())
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// --- tool-config (全局工具拦截范围) ---

// interceptGetToolConfig returns the list of tool names that are currently
// configured to enter the intercept rule system.
func (s *Server) interceptGetToolConfig(w http.ResponseWriter, r *http.Request) {
	tools, err := s.m.interceptor.GetEnabledTools()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"enabled_tools": tools})
}

// interceptSetToolConfig replaces the list of tool names that should enter
// the intercept rule system.
func (s *Server) interceptSetToolConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EnabledTools []string `json:"enabled_tools"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.EnabledTools == nil {
		req.EnabledTools = []string{}
	}
	if err := s.m.interceptor.SetEnabledTools(req.EnabledTools); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// --- LLM fallback judge config (全局模型兜底) ---

// interceptGetJudgeConfig returns the resolved judge configuration. Prompt is the
// effective prompt (built-in template when unset), so the UI can prefill it.
func (s *Server) interceptGetJudgeConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.m.interceptor.GetJudgeConfig())
}

// interceptSetJudgeConfig persists the judge configuration.
func (s *Server) interceptSetJudgeConfig(w http.ResponseWriter, r *http.Request) {
	var req intercept.JudgeConfig
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	switch req.FailAction {
	case "allow", "ask", "deny":
	default:
		writeErr(w, 400, "fail_action 必须是 allow、ask 或 deny")
		return
	}
	switch req.AskTimeoutAction {
	case "allow", "deny":
	default:
		writeErr(w, 400, "ask_timeout_action 必须是 allow 或 deny")
		return
	}
	switch req.IncompleteScriptAction {
	case "":
		req.IncompleteScriptAction = intercept.DefaultIncompleteScriptAction
	case "ask", "model":
	default:
		writeErr(w, 400, "incomplete_script_action 必须是 ask 或 model")
		return
	}
	if err := req.ValidateRetry(); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := s.m.interceptor.SetJudgeConfig(req); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// interceptJudgeUsage returns the fallback judge's cumulative token spend plus a
// recent daily series, for the config page. ?days bounds the daily series (default 30).
func (s *Server) interceptJudgeUsage(w http.ResponseWriter, r *http.Request) {
	pg := s.m.PG()
	if pg == nil {
		writeJSON(w, 200, db.JudgeUsage{Daily: []db.JudgeDayUsage{}})
		return
	}
	days := atoiDefault(r.URL.Query().Get("days"), 30)
	usage, err := pg.JudgeUsageStats(days)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, usage)
}

// --- helpers ---

type interceptRuleReq struct {
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	Priority       int    `json:"priority"`
	MatchTarget    string `json:"match_target"`
	MatchType      string `json:"match_type"`
	Pattern        string `json:"pattern"`
	Action         string `json:"action"`
	Message        string `json:"message"`
	TimeoutEnabled bool   `json:"timeout_enabled"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	TimeoutAction  string `json:"timeout_action"`
}

func validateInterceptRuleReq(req interceptRuleReq) error {
	if req.Name == "" {
		return fmt.Errorf("name 不能为空")
	}
	switch req.MatchTarget {
	case "tool_name", "tool_input":
	default:
		return fmt.Errorf("match_target 必须是 tool_name 或 tool_input")
	}
	switch req.MatchType {
	case "string", "regex":
	default:
		return fmt.Errorf("match_type 必须是 string 或 regex")
	}
	if req.Pattern == "" {
		return fmt.Errorf("pattern 不能为空")
	}
	switch req.Action {
	case "allow", "deny", "ask":
	default:
		return fmt.Errorf("action 必须是 allow、deny 或 ask")
	}
	if req.MatchType == "regex" {
		if _, err := regexp.Compile(req.Pattern); err != nil {
			return fmt.Errorf("pattern 不是有效正则：%w", err)
		}
	}
	return nil
}

func (s *Server) interceptDetail(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok || id <= 0 {
		writeErr(w, 400, "bad approval id")
		return
	}
	detail, err := pg.GetInterceptDetail(id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if detail == nil {
		writeErr(w, 404, "not found")
		return
	}
	writeJSON(w, 200, detail)
}

// The navigation endpoint returns only the original call and its paired result.
func (s *Server) interceptExecution(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok || id <= 0 {
		writeErr(w, 400, "bad approval id")
		return
	}
	target, err := pg.GetInterceptExecution(id)
	if errors.Is(err, db.ErrInterceptTaskDeleted) || errors.Is(err, db.ErrInterceptSessionDeleted) {
		writeErr(w, http.StatusGone, err.Error())
		return
	}
	if errors.Is(err, db.ErrInterceptExecutionUnavailable) {
		writeErr(w, 409, err.Error())
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if target == nil {
		// Conversation deletion cascades approval rows. A stale source link still
		// carries its conversation ID, allowing a precise message without retaining
		// deleted conversations or changing their deletion semantics.
		if convID, parseErr := strconv.ParseInt(r.URL.Query().Get("conversation"), 10, 64); parseErr == nil && convID > 0 {
			conv, getErr := pg.GetConversation(convID)
			if getErr != nil {
				writeErr(w, 500, getErr.Error())
				return
			}
			if conv == nil {
				writeErr(w, http.StatusGone, "对话已被删除")
				return
			}
		}
		writeErr(w, 404, "审批记录已被删除或不存在")
		return
	}
	writeJSON(w, 200, map[string]any{"conversation_id": target.ConversationID, "task_id": target.TaskID, "session": target.Session, "seq": target.Seq, "items": activityDTOs(target.Items)})
}
