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
	mux.HandleFunc("POST /api/tasks/{id}/review/{run}/rebut", s.reviewRebut)
	mux.HandleFunc("GET /api/tasks/{id}/review/notes", s.reviewNotes)
	mux.HandleFunc("POST /api/tasks/{id}/review/seen", s.reviewSeen)      // 人看过了 → 红点消
	mux.HandleFunc("GET /api/tasks/{id}/review/state", s.reviewStateAPI) // 档位/配额/熔断/未读数
	mux.HandleFunc("PUT /api/tasks/{id}/review/tier", s.reviewSetTier)  // 任务级三档覆盖
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
	AckStatus   string          `json:"ack_status,omitempty"` // accepted|rejected|stale
	AckReason   string          `json:"ack_reason,omitempty"`
	AutoApplied bool            `json:"auto_applied"`
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
			_, _ = s.m.pg.ExecContext(r.Context(), `UPDATE review_runs SET ack_status='rejected', ack_reason=$2 WHERE id=$1`,
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
	snap, err := review.Build(ctx, s.m.PG(), taskID, notes)
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
			ackStatus, ackReason = "accepted", "自动执行："+strings.Join(done, "；")
			_, _ = s.m.pg.ExecContext(ctx, `INSERT INTO review_feedback(run_id,action,by_user,outcome)
				VALUES($1,'applied','auto','')`, runID)
			if s.engine != nil {
				s.engine.emitActivity(task, db.Activity{Worker: "adviser", Kind: "text",
					Summary: "审查官自行处理：" + strings.Join(done, "；")})
			}
		}
	}

	s.finishReviewRun(ctx, runID, v, modelName, g, ver, autoApplied, ackStatus, ackReason)

	// 投递：A 级永远只留结论卡；B 级（有动作）额外给主链路一条"建议"。
	if g.Drop == "" {
		if g.Level == review.LevelAction && len(g.Actions) > 0 {
			text := review.AdviceText(v.Body, g.Actions, g.AutoApplied)
			s.deliverReviewAdvice(ctx, task, runID, text)
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
		COALESCE(ack_status,''),COALESCE(ack_reason,''),auto_applied
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
			&x.AckStatus, &x.AckReason, &x.AutoApplied); err != nil {
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

// reviewSetTier 设任务级档位（observe|lowrisk|auto|''=跟全局）。
func (s *Server) reviewSetTier(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	var body struct{ Tier string `json:"tier"` }
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

// reviewApply 采纳动作（只允许三类）。
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
	applied := 0
	notes := []string{}
	for _, a := range body.Actions {
		if a.Kind == "set_next_stage" {
			// 引擎没有"换阶段"接口：只记账，不假称已执行。
			notes = append(notes, "换阶段需要你在任务设置里手动确认，已记录")
			continue
		}
		if ok, note := s.applyReviewAction(t, a); ok {
			applied++
			notes = append(notes, note)
		}
	}
	if runID > 0 {
		_, _ = s.m.pg.Exec(`INSERT INTO review_feedback(run_id,action,by_user) VALUES($1,'applied','ui')`, runID)
		// 人点了采纳 → 回执落库（docs/13 §5：accepted）
		_, _ = s.m.pg.Exec(`UPDATE review_runs SET ack_status='accepted', ack_reason=$2 WHERE id=$1`,
			runID, strings.Join(notes, "；"))
	}
	writeJSON(w, http.StatusOK, map[string]any{"applied": applied, "notes": notes})
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
