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

	"github.com/Autumn-27/artex/internal/review"
)

// 复盘官（CARD-C01）。
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
}

// notImplemented 保留给 CARD-000 未接线的桩（cando.go 里也有一个同名函数，
// 这里不再重复定义，避免重复声明）。

var reviewMu sync.Map // taskID -> *sync.Mutex

func reviewLock(taskID int64) *sync.Mutex {
	v, _ := reviewMu.LoadOrStore(taskID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

type reviewRunRow struct {
	ID         int64           `json:"id"`
	TaskID     int64           `json:"task_id"`
	Trigger    string          `json:"trigger"`
	Status     string          `json:"status"`
	Verdict    string          `json:"verdict"`
	Body       string          `json:"body"`
	Cites      []review.Cite   `json:"cites"`
	Actions    []review.Action `json:"actions"`
	Confidence string          `json:"confidence"`
	Model      string          `json:"model"`
	CreatedAt  string          `json:"created_at"`
	FinishedAt string          `json:"finished_at,omitempty"`
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

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	notes, ver, _ := review.LoadNotes(ctx, s.m.PG(), taskID)
	snap, err := review.Build(ctx, s.m.PG(), taskID, notes)
	if err != nil {
		log.Printf("[review] snapshot build failed (task=%d): %v", taskID, err)
		writeErr(w, http.StatusInternalServerError, "快照生成失败")
		return
	}
	runID := s.insertReviewRun(taskID, trigger, snap.Hash(), snap.RoundNo, len(snap.Facts), ver)

	user := snap.Prompt()
	if rebuttal != "" {
		user = fmt.Sprintf("用户不同意你上次的判断，理由是：%s\n请重新判一遍；如果你改口，必须写明「旧结论 + 被哪条证据推翻」。\n\n%s", rebuttal, user)
	}
	sys := s.agentPromptOrConst("adviser", review.SystemPrompt)

	var v review.Verdict
	modelName := "rule"
	rt := &taskLLMRuntime{s: s, taskID: t.ID, agentKey: "adviser"}
	raw, lerr := streamCollectText(ctx, rt, sys, user)
	if lerr == nil {
		if parsed, perr := review.Parse(raw); perr == nil {
			v = parsed
			modelName = "adviser"
		}
	}
	if modelName == "rule" {
		v = review.RuleBased(snap)
	}

	citesJSON, _ := json.Marshal(v.Cites)
	actionsJSON, _ := json.Marshal(v.Actions)
	_, _ = s.m.pg.Exec(`UPDATE review_runs SET status='done', verdict=$2, body=$3, cites=$4, actions=$5,
		model=$6, notes_version=$7, finished_at=now() WHERE id=$1`,
		runID, v.Verdict, v.Body, string(citesJSON), string(actionsJSON), modelName, ver)

	if strings.TrimSpace(v.Notes) != "" {
		_, _ = review.SaveNotes(ctx, s.m.PG(), taskID, v.Notes, ver)
	}

	row := s.latestReviewRun(taskID)
	writeJSON(w, http.StatusOK, map[string]any{"run_id": runID, "cached": false, "run": row})
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
		COALESCE(model,''),to_char(created_at,'YYYY-MM-DD HH24:MI'),COALESCE(to_char(finished_at,'YYYY-MM-DD HH24:MI'),'')
		FROM review_runs WHERE task_id=$1 ORDER BY id DESC LIMIT $2`, taskID, limit)
	if err != nil {
		return nil, err, false
	}
	defer rows.Close()
	out := []reviewRunRow{}
	for rows.Next() {
		var x reviewRunRow
		var cites, actions, conf string
		if err := rows.Scan(&x.ID, &x.TaskID, &x.Trigger, &x.Status, &x.Verdict, &x.Body, &cites, &actions, &conf, &x.Model, &x.CreatedAt, &x.FinishedAt); err != nil {
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
	writeJSON(w, http.StatusOK, map[string]any{"run": s.latestReviewRun(taskID), "notes_version": ver, "notes": notes})
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
		switch a.Kind {
		case "kill_intent":
			done := false
			for _, from := range []string{"open", "running", "active"} {
				if err := transitionIntentState(t.Store, a.Intent, from, "stopped"); err == nil {
					done = true
					break
				}
			}
			if done {
				applied++
				notes = append(notes, fmt.Sprintf("已放弃一条方向（依据：%s）", a.Why))
			}
		case "retry_intent":
			done := false
			for _, from := range []string{"stopped", "blocked", "open"} {
				if err := transitionIntentState(t.Store, a.Intent, from, "open"); err == nil {
					done = true
					break
				}
			}
			if done {
				applied++
				notes = append(notes, "已重跑一条被自己接口打断的方向")
			}
		case "set_next_stage":
			// 引擎没有"换阶段"接口：只记账，不假称已执行。
			notes = append(notes, "换阶段需要你在任务设置里手动确认，已记录")
		}
	}
	if runID > 0 {
		_, _ = s.m.pg.Exec(`INSERT INTO review_feedback(run_id,action,by_user) VALUES($1,'applied','ui')`, runID)
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
