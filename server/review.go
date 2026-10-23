package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/review"
	"github.com/Autumn-27/artex/llmrec"
)

// 审查官（CARD-C01，原「复盘官」）：结论卡 + 分级投递 + 低风险自动执行。
//
// 规格：docs/09-详设-C01-复盘官.md
// 关键约束：
//   - 复用 agentKey=adviser 的模型绑定，不新建 agent 行；
//   - 快照里不含对话/thinking/推理；
//   - apply 只允许 kill_intent / retry_intent / set_next_stage，永不改证据/改目标/收工；
//   - 模型失败 → 规则版兜底，不阻断任务、不弹红字。
func (s *Server) registerReviewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/tasks/{id}/review", s.reviewRun)
	mux.HandleFunc("GET /api/tasks/{id}/review/latest", s.reviewLatest)
	mux.HandleFunc("GET /api/tasks/{id}/review/runs", s.reviewListRuns)
	mux.HandleFunc("POST /api/tasks/{id}/review/{run}/apply", s.reviewApply)
	mux.HandleFunc("POST /api/tasks/{id}/review/{run}/ack", s.reviewAck) // ack 窗口：held|accepted|rejected
	mux.HandleFunc("POST /api/tasks/{id}/review/{run}/rebut", s.reviewRebut)
	mux.HandleFunc("GET /api/tasks/{id}/review/notes", s.reviewNotes)
	mux.HandleFunc("POST /api/tasks/{id}/review/seen", s.reviewSeen)     // 人看过了 → 红点消
	mux.HandleFunc("GET /api/tasks/{id}/review/state", s.reviewStateAPI) // 档位/配额/熔断/未读数
	mux.HandleFunc("PUT /api/tasks/{id}/review/tier", s.reviewSetTier)   // 任务级三档覆盖
}

// notImplemented 保留给 CARD-000 未接线的桩（cando.go 里也有一个同名函数，
// 这里不再重复定义，避免重复声明）。

var reviewMu sync.Map // taskID -> *sync.Mutex

func reviewLock(taskID int64) *sync.Mutex {
	v, _ := reviewMu.LoadOrStore(taskID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

type reviewRunRow struct {
	ID          int64           `json:"id"`
	TaskID      int64           `json:"task_id"`
	Trigger     string          `json:"trigger"`
	Status      string          `json:"status"`
	Verdict     string          `json:"verdict"`
	Body        string          `json:"body"`
	Cites       []review.Cite   `json:"cites"`
	Actions     []review.Action `json:"actions"`
	Confidence  string          `json:"confidence"`
	Model       string          `json:"model"`
	CreatedAt   string          `json:"created_at"`
	FinishedAt  string          `json:"finished_at,omitempty"`
	Level       string          `json:"level"`        // read|action（docs/13 §1 分级投递）
	ReasonClass string          `json:"reason_class"` // retry:self_fault / kill:target_blocked
	SeenAt      string          `json:"seen_at,omitempty"`
	AckStatus   string          `json:"ack_status,omitempty"` // '|held|accepted|rejected|escalated
	AckReason   string          `json:"ack_reason,omitempty"`
	AckDeadline string          `json:"ack_deadline,omitempty"` // 无人值守窗口到期时间（面板倒计时用）
	AutoApplied bool            `json:"auto_applied"`
	Expectation string          `json:"expectation,omitempty"` // 一句话可机检预期（E1）
	Outcome     string          `json:"outcome,omitempty"`     // effective|ineffective|voided（观察窗回访）
}

// agentPromptOrConst 取 Agent 页当前提示词；含未渲染模板变量时回退常量。
// 修坑（docs/09 §13-1）：历史上 adviser 用的是代码常量，Agent 页改了不生效。
func (s *Server) agentPromptOrConst(agentKey, fallback string) string {
	if s.m == nil || s.m.PG() == nil {
		return fallback
	}
	var txt string
	err := s.m.PG().QueryRow(`SELECT p.template_text FROM agents a JOIN agent_prompts p ON p.id=a.current_prompt_id WHERE a.key=$1`, agentKey).Scan(&txt)
	if err != nil || strings.TrimSpace(txt) == "" || strings.Contains(txt, "{{") {
		return fallback
	}
	return txt
}

// reviewRun 立即跑一次评审（手动/条件/定时共用）。
func (s *Server) reviewRun(w http.ResponseWriter, r *http.Request) {
	s.runReview(w, r, "manual", "")
}

// reviewRebut 反驳：带着用户的反对意见重跑一次，模型必须改口或写清坚持的理由。
func (s *Server) reviewRebut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Text) == "" {
		writeErr(w, http.StatusBadRequest, "请写一句你的理由")
		return
	}
	// 反驳 = 人拒绝这条建议：先把上一条回执记成 rejected（R4 降权的依据），再重跑。
	if t, ok := s.m.Task(r.PathValue("id")); ok && t != nil {
		taskID, _ := strconv.ParseInt(t.ID, 10, 64)
		var runID int64
		if err := s.m.pg.QueryRowContext(r.Context(), `SELECT id FROM review_runs
			WHERE task_id=$1 AND status='done' ORDER BY id DESC LIMIT 1`, taskID).Scan(&runID); err == nil && runID > 0 {
			_, _ = s.m.pg.ExecContext(r.Context(), `UPDATE review_runs SET ack_status='rejected', ack_reason=$2, observe_until=NULL WHERE id=$1`,
				runID, body.Text)
			_, _ = s.m.pg.ExecContext(r.Context(), `INSERT INTO review_feedback(run_id,action,by_user,rebuttal)
				VALUES($1,'rebutted','ui',$2)`, runID, body.Text)
		}
	}
	s.runReview(w, r, "rebut", body.Text)
}

func (s *Server) runReview(w http.ResponseWriter, r *http.Request, trigger, rebuttal string) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	if s.m.PG() == nil {
		writeErr(w, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)

	mu := reviewLock(taskID)
	if !mu.TryLock() {
		writeErr(w, http.StatusTooManyRequests, "前面还有一个在评审，请稍后")
		return
	}
	defer mu.Unlock()

	runID, err := s.reviewOnce(r.Context(), t.ID, taskID, trigger, rebuttal)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	row := s.latestReviewRun(taskID)
	writeJSON(w, http.StatusOK, map[string]any{"run_id": runID, "cached": false, "run": row})
}

// reviewOnce 是审查官的一次评审（不含 HTTP）：手动/条件/定时共用同一条路径。
// 调用方负责串行化（HTTP 用 reviewLock；条件触发在 progressHook 里自己加锁）。
func (s *Server) reviewOnce(parent context.Context, taskIDStr string, taskID int64, trigger, rebuttal string) (int64, error) {
	if s.m == nil || s.m.PG() == nil {
		return 0, fmt.Errorf("数据库不可用")
	}
	task, _ := s.m.Task(taskIDStr)
	if task == nil {
		return 0, fmt.Errorf("任务不在运行")
	}
	// 成本归因：独立调用的 worker 标签（否则 llm_usage.worker 为空，分摊不到角色）
	ctx, cancel := context.WithTimeout(llmrec.WithWorker(parent, "adviser"), 60*time.Second)
	defer cancel()

	notes, ver, _ := review.LoadNotes(ctx, s.m.PG(), taskID)
	snap, err := review.BuildWithKB(ctx, s.m.PG(), taskID, notes, s.kbRoot())
	if err != nil {
		log.Printf("[review] snapshot build failed (task=%d): %v", taskID, err)
		return 0, fmt.Errorf("快照生成失败")
	}
	rs := s.reviewSettings()
	st := s.loadReviewState(ctx, taskID)
	runID := s.insertReviewRun(taskID, trigger, snap.Hash(), snap.RoundNo, len(snap.Facts), ver)

	user := snap.Prompt()
	if rebuttal != "" {
		user = fmt.Sprintf("用户不同意你上次的判断，理由是：%s\n请重新判一遍；如果你改口，必须写明「旧结论 + 被哪条证据推翻」。\n\n%s", rebuttal, user)
	}
	// 分工：结构化动作（actions/cites）由规则引擎给——确定性、不编造；
	// 模型只负责把现状说人话。这样既不会因 JSON 被截断而丢掉整份结论，
	// 也不会让模型凭空造动作。
	v := review.RuleBased(snap)
	modelName := "rule"
	rt := &taskLLMRuntime{s: s, taskID: taskIDStr, agentKey: "adviser"}
	// 提示词单一来源：Agent 页改的「审查官」提示词对**两条调用路径**都生效
	// （停顿诊断走 adviser_llm.go，全局复盘走这里）。DB 里没有/含未渲染占位符时
	// 回落到 review.ProsePrompt，保证老库不改也能跑。
	sys := s.agentPromptOrConst("adviser", review.ProsePrompt)
	raw, lerr := collectText(ctx, rt, true, sys, user, 1200)
	if lerr == nil {
		if txt := cleanReviewText(raw); len([]rune(txt)) >= 12 {
			v.Body = txt
			v.Confidence = "medium"
			modelName = "adviser"
		} else {
			log.Printf("[review] 模型正文太短，用规则版（%d 字）", len([]rune(raw)))
		}
	} else {
		log.Printf("[review] llm failed (task=%d): %v", taskID, lerr)
	}

	// ── 门：自检五问 + 分级 + 自动动作筛选 + 8 条防抖（internal/review/gate.go）──
	tier := rs.Tier
	if t := strings.TrimSpace(st.Tier); t != "" {
		tier = t
	}
	g := review.Decide(review.GateIn{
		Snap: *snap, Verdict: v, State: st, Recent: s.recentReviewRuns(ctx, taskID, 30),
		Now: time.Now(), Trigger: trigger, DefaultTier: tier,
		Cooldown: rs.Cooldown, MaxInterventions: rs.MaxInterv, MaxAuto: rs.MaxAuto,
		FactCount: len(snap.Facts),
	})

	// 自动执行（只允许减浪费的两类；每步都写 feedback 供审计）。
	autoApplied := 0
	ackStatus, ackReason := "", ""
	if g.Drop == "" && len(g.AutoApplied) > 0 {
		var done []string
		for _, a := range g.AutoApplied {
			if ok, note := s.applyReviewAction(task, a); ok {
				autoApplied++
				done = append(done, note)
			}
		}
		if autoApplied > 0 {
			// ack_status 故意留 ''：这次可能还有需要人点头的动作，ack 窗口要能扫到它。
			ackReason = "自动执行：" + strings.Join(done, "；")
			_, _ = s.m.pg.ExecContext(ctx, `INSERT INTO review_feedback(run_id,action,by_user,outcome)
				VALUES($1,'applied','auto','')`, runID)
			if s.engine != nil {
				s.engine.emitActivity(task, db.Activity{Worker: "adviser", Kind: "text",
					Summary: "审查官自行处理：" + strings.Join(done, "；")})
			}
		}
	}

	s.finishReviewRun(ctx, runID, v, modelName, g, ver, autoApplied, ackStatus, ackReason)

	// ack 窗口（docs/14 §4）：只有「还要人点头的动作」才开 90 秒倒计时。
	// 已自动执行的减浪费动作不再等，否则超时会重复处理同一件事。
	if g.Drop == "" && g.Level == review.LevelAction && reviewPendingActions(g) > 0 {
		s.setReviewAckDeadline(ctx, runID, reviewAckWindow)
	}

	// 投递：A 级永远只留结论卡；B 级（有动作）额外给主链路一条"建议"。
	if g.Drop == "" {
		if g.Level == review.LevelAction && len(g.Actions) > 0 {
			text := review.AdviceText(v.Body, g.Actions, g.AutoApplied)
			s.deliverReviewAdvice(ctx, task, runID, text)
			// 有动作且已投递 → 开观察窗（docs/15 §3）：10 分钟后回访预期信号。
			// 门（E1）保证这条路径上的 expectation 非空。
			s.setReviewObserveWindow(ctx, runID, reviewObserveWindow)
		}
		st.Interventions++
		st.LastSentAt = time.Now()
	} else {
		log.Printf("[review] task=%d 不投递：%s", taskID, g.Drop)
	}
	st.LastFacts = len(snap.Facts)
	if g.Trip || (st.Tripped && !g.Trip) {
		st.Tripped = g.Trip || st.Tripped
		if g.TripReason != "" {
			st.TripReason = g.TripReason
		}
	}
	st.AutoActions += autoApplied
	// 任务级覆盖只记"用户显式改过的"档位；''表示跟全局。
	s.saveReviewState(ctx, taskID, st, len(snap.Facts))
	for _, n := range g.Notes {
		log.Printf("[review] task=%d %s", taskID, n)
	}

	if strings.TrimSpace(v.Notes) != "" {
		_, _ = review.SaveNotes(ctx, s.m.PG(), taskID, v.Notes, ver)
	}

	return runID, nil
}

func (s *Server) insertReviewRun(taskID int64, trigger, hash string, round int64, facts, notesVer int) int64 {
	var id int64
	_ = s.m.pg.QueryRow(`INSERT INTO review_runs(task_id,trigger,snapshot_hash,round_no,facts,status,notes_version)
		VALUES($1,$2,$3,$4,$5,'running',$6) RETURNING id`, taskID, trigger, hash, round, facts, notesVer).Scan(&id)
	return id
}

func (s *Server) latestReviewRun(taskID int64) *reviewRunRow {
	rows, _, _ := s.listReviewRuns(taskID, 1)
	if len(rows) == 0 {
		return nil
	}
	return &rows[0]
}

func (s *Server) listReviewRuns(taskID int64, limit int) ([]reviewRunRow, error, bool) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.m.pg.Query(`SELECT id,task_id,trigger,status,COALESCE(verdict,''),COALESCE(body,''),
		COALESCE(cites::text,'[]'),COALESCE(actions::text,'[]'),
		COALESCE((SELECT 'high' ),''),
		COALESCE(model,''),to_char(created_at,'YYYY-MM-DD HH24:MI'),COALESCE(to_char(finished_at,'YYYY-MM-DD HH24:MI'),''),
		COALESCE(level,'read'),COALESCE(reason_class,''),COALESCE(to_char(seen_at,'YYYY-MM-DD HH24:MI'),''),
		COALESCE(ack_status,''),COALESCE(ack_reason,''),auto_applied,
		COALESCE(to_char(ack_deadline,'YYYY-MM-DD HH24:MI:SS'),''),
		COALESCE(expectation,''),COALESCE(outcome,'')
		FROM review_runs WHERE task_id=$1 AND status IN ('running','done','superseded')
		ORDER BY id DESC LIMIT $2`, taskID, limit)
	if err != nil {
		return nil, err, false
	}
	defer rows.Close()
	out := []reviewRunRow{}
	for rows.Next() {
		var x reviewRunRow
		var cites, actions, conf string
		if err := rows.Scan(&x.ID, &x.TaskID, &x.Trigger, &x.Status, &x.Verdict, &x.Body, &cites, &actions, &conf,
			&x.Model, &x.CreatedAt, &x.FinishedAt, &x.Level, &x.ReasonClass, &x.SeenAt,
			&x.AckStatus, &x.AckReason, &x.AutoApplied, &x.AckDeadline, &x.Expectation, &x.Outcome); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(cites), &x.Cites)
		_ = json.Unmarshal([]byte(actions), &x.Actions)
		out = append(out, x)
	}
	return out, nil, true
}

// reviewLatest 返回最近一次结论（含 notes 版本）。
func (s *Server) reviewLatest(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	notes, ver, _ := review.LoadNotes(r.Context(), s.m.PG(), taskID)
	writeJSON(w, http.StatusOK, map[string]any{
		"run":           s.latestReviewRun(taskID),
		"notes_version": ver,
		"notes":         notes,
		"state":         s.reviewStatePayload(r.Context(), taskID),
	})
}

// reviewStatePayload 是面板顶部那一行所需的全部状态：档位 / 介入与自动次数 / 熔断 / 未读数。
func (s *Server) reviewStatePayload(ctx context.Context, taskID int64) map[string]any {
	st := s.loadReviewState(ctx, taskID)
	var unseen int
	_ = s.m.pg.QueryRowContext(ctx, `SELECT count(*) FROM review_runs
		WHERE task_id=$1 AND status='done' AND seen_at IS NULL`, taskID).Scan(&unseen)
	tier := s.reviewSettings().Tier
	effective := tier
	if strings.TrimSpace(st.Tier) != "" {
		effective = st.Tier
	}
	trippedAt := ""
	if st.Tripped {
		trippedAt = st.TripReason
	}
	return map[string]any{
		"tier":          effective, // 生效档位（任务覆盖优先）
		"tier_override": st.Tier,   // 任务级覆盖（''=跟全局）
		"global_tier":   tier,
		"interventions": st.Interventions,
		"auto_actions":  st.AutoActions,
		"tripped":       st.Tripped,
		"trip_reason":   trippedAt,
		"unseen":        unseen,
	}
}

// reviewSeen 把该任务未读结论标记为已读（打开面板 / 点红点时调用）。
func (s *Server) reviewSeen(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	_, err := s.m.pg.ExecContext(r.Context(), `UPDATE review_runs SET seen_at=now()
		WHERE task_id=$1 AND status='done' AND seen_at IS NULL`, taskID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// reviewStateAPI 返回 state 载荷（前端轮询红点用，比 latest 轻）。
func (s *Server) reviewStateAPI(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	writeJSON(w, http.StatusOK, s.reviewStatePayload(r.Context(), taskID))
}

// reviewSetTier 设任务级档位（observe|lowrisk|auto|空=跟全局）。
func (s *Server) reviewSetTier(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	var body struct {
		Tier string `json:"tier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	tier := strings.ToLower(strings.TrimSpace(body.Tier))
	switch tier {
	case "", review.TierObserve, review.TierLowRisk, review.TierAuto:
	default:
		writeErr(w, http.StatusBadRequest, "tier 只接受 observe|lowrisk|auto|空")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	st := s.loadReviewState(r.Context(), taskID)
	st.Tier = tier
	// 手工改档位 = 人重新授权，解除熔断（否则永远停在 observe）。
	st.Tripped = false
	st.TripReason = ""
	s.saveReviewState(r.Context(), taskID, st, st.LastFacts)
	writeJSON(w, http.StatusOK, s.reviewStatePayload(r.Context(), taskID))
}

// reviewListRuns 返回历史介入列表。
func (s *Server) reviewListRuns(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err, _ := s.listReviewRuns(taskID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": rows})
}

// reviewAckWindow 是无人值守时的等待窗口（docs/14 §4：默认 90 秒）。
const reviewAckWindow = 90 * time.Second

// reviewObserveWindow 是「有动作且已投递」的观察窗（docs/15 §3：10 分钟 / 3 轮）。
const reviewObserveWindow = 10 * time.Minute

// reviewPendingActions 数「还需要人点头」的动作：已在自动路径执行过的减浪费动作不计。
func reviewPendingActions(g review.GateOut) int {
	done := map[string]bool{}
	for _, a := range g.AutoApplied {
		done[fmt.Sprintf("%s:%d", a.Kind, a.Intent)] = true
	}
	n := 0
	for _, a := range g.Actions {
		if !done[fmt.Sprintf("%s:%d", a.Kind, a.Intent)] {
			n++
		}
	}
	return n
}

// setReviewAckDeadline 给「要动作的投递」开 ack 窗口。到点且 ack_status 为空 → reviewAckSweep
// 置 escalated，再按档位执行（docs/14 §4）。
func (s *Server) setReviewAckDeadline(ctx context.Context, runID int64, w time.Duration) {
	if s.m == nil || s.m.pg == nil || runID <= 0 || w <= 0 {
		return
	}
	if _, err := s.m.pg.ExecContext(ctx, `UPDATE review_runs
		SET ack_deadline = now() + make_interval(secs => $2)
		WHERE id=$1 AND COALESCE(ack_status,'')=''`, runID, int(w.Seconds())); err != nil {
		log.Printf("[review] set ack deadline run=%d: %v", runID, err)
	}
}

// setReviewObserveWindow 给「有动作且已投递」的结论开观察窗。到点后 reviewObserveSweep
// 比对 expectation 与新 fact，写 outcome（docs/15 §3）。
func (s *Server) setReviewObserveWindow(ctx context.Context, runID int64, w time.Duration) {
	if s.m == nil || s.m.pg == nil || runID <= 0 || w <= 0 {
		return
	}
	if _, err := s.m.pg.ExecContext(ctx, `UPDATE review_runs
		SET observe_until = now() + make_interval(secs => $2)
		WHERE id=$1 AND COALESCE(outcome,'')=''`, runID, int(w.Seconds())); err != nil {
		log.Printf("[review] set observe window run=%d: %v", runID, err)
	}
}

// applyReviewActionsForRun 是「人点采纳」与「ack=accepted」共用的执行路径，
// 保证手动/回执/自动三条路行为一致（docs/13 §1）。它永不改目标、永不收工。
func (s *Server) applyReviewActionsForRun(ctx context.Context, t *Task, runID int64, actions []review.Action) (int, []string) {
	applied := 0
	notes := []string{}
	for _, a := range actions {
		switch a.Kind {
		case "set_next_stage":
			// 引擎没有"换阶段"接口：只记账，不假称已执行。
			notes = append(notes, "换阶段需要你在任务设置里手动确认，已记录")
			continue
		case "propose_intent":
			// 只提议：写成一条 hint 给主链路 + 一条 worker="mainagent" 的活动（「会话」tab 看得见），
			// 不派生任务、不派活，并记 feedback=proposed。走与 B 级建议同一条投递路径，
			// 所以「同一条 run 只投递一次」的哨兵对它同样生效。
			text := proposeIntentText(a)
			if ok := s.deliverReviewHintOnce(ctx, t, runID, text, "审查官提议了一个新方向（只提议，未派活）"); ok {
				notes = append(notes, "已把新方向写成一条提示（未派活）")
			}
			if runID > 0 {
				_, _ = s.m.pg.ExecContext(ctx, `INSERT INTO review_feedback(run_id,action,by_user) VALUES($1,'proposed','ui')`, runID)
			}
			continue
		}
		if ok, note := s.applyReviewAction(t, a); ok {
			applied++
			notes = append(notes, note)
		}
	}
	if runID > 0 && applied > 0 {
		_, _ = s.m.pg.ExecContext(ctx, `INSERT INTO review_feedback(run_id,action,by_user) VALUES($1,'applied','ui')`, runID)
	}
	return applied, notes
}

// proposeIntentText 把 propose_intent 渲染成一条可读的提示：新方向 + 依据 + 权责声明。
func proposeIntentText(a review.Action) string {
	dir := strings.TrimSpace(a.Why)
	if dir == "" {
		dir = "（没写具体方向，见审查官结论卡）"
	}
	return "审查官建议新方向（只提议，不是命令；它没有派活）：" + dir +
		"\n依据：见审查官结论卡引用的 intent/finding 与经验命中。\n" +
		"按协议：先核对现状；会改目标/改数据/收工的一律交给人。"
}

// reviewApply 采纳动作（只允许白名单里的四类；propose_intent 只写 hint）。
func (s *Server) reviewApply(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	runID, _ := strconv.ParseInt(r.PathValue("run"), 10, 64)
	var body struct {
		Actions []review.Action `json:"actions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Actions) == 0 {
		writeErr(w, http.StatusBadRequest, "actions 不能为空")
		return
	}
	applied, notes := s.applyReviewActionsForRun(r.Context(), t, runID, body.Actions)
	if runID > 0 {
		// 人点了采纳 → 回执落库（docs/13 §5：accepted），并关掉 ack 窗口。
		_, _ = s.m.pg.ExecContext(r.Context(), `UPDATE review_runs SET ack_status='accepted', ack_at=now(),
			ack_deadline=NULL, ack_reason=$2 WHERE id=$1`, runID, strings.Join(notes, "；"))
	}
	writeJSON(w, http.StatusOK, map[string]any{"applied": applied, "notes": notes})
}

// reviewAck 是人（或面板）对一条结论的显式回执（docs/14 §4）：
//
//	held     —— 我在，我自己处理：取消本次自动投递，红点保留（不动 seen_at）；
//	accepted —— 采纳：走已有 apply 路径（动作取自该 run 的 actions）；
//	rejected —— 不要：记 negative，触发已有降权逻辑（同类建议少发/只留红点）。
func (s *Server) reviewAck(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	if s.m.PG() == nil {
		writeErr(w, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	runID, _ := strconv.ParseInt(r.PathValue("run"), 10, 64)
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	action := strings.ToLower(strings.TrimSpace(body.Action))
	switch action {
	case "held", "accepted", "rejected":
	default:
		writeErr(w, http.StatusBadRequest, "action 只接受 held|accepted|rejected")
		return
	}
	if runID <= 0 {
		writeErr(w, http.StatusBadRequest, "run 不合法")
		return
	}

	// run 必须属于该任务，防止跨任务改别人的结论。
	var actionsRaw string
	if err := s.m.pg.QueryRowContext(r.Context(), `SELECT COALESCE(actions::text,'[]')
		FROM review_runs WHERE id=$1 AND task_id=$2 AND status='done'`, runID, taskID).Scan(&actionsRaw); err != nil {
		writeErr(w, http.StatusNotFound, "结论不存在")
		return
	}

	applied := 0
	notes := []string{}
	switch action {
	case "held":
		// 红点保留：seen_at 不动，只把本次自动处理取消。
		_, _ = s.m.pg.ExecContext(r.Context(), `UPDATE review_runs SET ack_status='held', ack_at=now(),
			ack_deadline=NULL, ack_reason=$2 WHERE id=$1`, runID, "人已接手，本次取消自动处理（红点保留）")
		_, _ = s.m.pg.ExecContext(r.Context(), `INSERT INTO review_feedback(run_id,action,by_user) VALUES($1,'held','ui')`, runID)
		notes = append(notes, "好，我不动（红点保留给你）")
	case "rejected":
		// negative → R4 降权：同 reason_class 再被拒 2 次后只留结论卡。
		// 被拒的建议不再进观察窗（docs/15 §3：rejected → 读理由，不进入 observing）。
		_, _ = s.m.pg.ExecContext(r.Context(), `UPDATE review_runs SET ack_status='rejected', ack_at=now(),
			ack_deadline=NULL, observe_until=NULL, ack_reason=$2 WHERE id=$1`, runID, "人明确不要这条建议")
		_, _ = s.m.pg.ExecContext(r.Context(), `INSERT INTO review_feedback(run_id,action,by_user) VALUES($1,'rejected','ui')`, runID)
		notes = append(notes, "已记下：同类建议将降权")
	case "accepted":
		var acts []review.Action
		_ = json.Unmarshal([]byte(actionsRaw), &acts)
		applied, notes = s.applyReviewActionsForRun(r.Context(), t, runID, acts)
		_, _ = s.m.pg.ExecContext(r.Context(), `UPDATE review_runs SET ack_status='accepted', ack_at=now(),
			ack_deadline=NULL, ack_reason=$2 WHERE id=$1`, runID, strings.Join(notes, "；"))
	}

	t.Notify()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": action, "applied": applied, "notes": notes})
}

// reviewNotes 返回当前评审笔记。
func (s *Server) reviewNotes(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	body, ver, _ := review.LoadNotes(r.Context(), s.m.PG(), taskID)
	writeJSON(w, http.StatusOK, map[string]any{"version": ver, "body": body})
}

// cleanReviewText 去掉代码围栏与 JSON 残渣，留下可读正文。
func cleanReviewText(raw string) string {
	t := strings.TrimSpace(raw)
	if strings.HasPrefix(t, "{") && !strings.Contains(t, "body") {
		return ""
	}
	t = strings.TrimPrefix(t, "```json")
	t = strings.TrimPrefix(t, "```")
	t = strings.TrimSuffix(t, "```")
	return strings.TrimSpace(t)
}
