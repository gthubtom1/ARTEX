package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/review"
	"github.com/Autumn-27/artex/internal/stall"
)

// 本文件是审查官（agentKey=adviser）的「闭环」层：把 internal/review.Decide 的判定
// 落到库里并执行——分级投递（A 级只读结论 / B 级建议给主 Agent）、低风险自动动作、
// 配额与熔断、未读标记、定时触发。
//
// 唯一事实源：docs/13-复盘官-闭环与防抖.md。纪律（不可越）：
//   1. 它没有执行权：只允许 kill_intent / retry_intent 两类"减浪费"动作自动执行；
//      改目标 / 改数据 / 收工一律留给人（NeedHuman）。
//   2. 每次自动动作都写 review_feedback(by_user='auto') + auto_applied=true，可审计。
//   3. 熔断只降级它自己（tier→observe），绝不动任务。

// reviewSettings 是一次评审用到的全部参数（全局设置 + 任务覆盖）。
type reviewSettings struct {
	Tier      string
	Interval  time.Duration
	Cooldown  time.Duration
	CondOn    bool
	MaxInterv int
	MaxAuto   int
}

func (s *Server) reviewSettings() reviewSettings {
	rs := reviewSettings{
		Tier:      review.TierLowRisk,
		Interval:  10 * time.Minute,
		Cooldown:  5 * time.Minute,
		CondOn:    true,
		MaxInterv: 10,
		MaxAuto:   5,
	}
	if s.settings == nil {
		return rs
	}
	switch t := strings.ToLower(strings.TrimSpace(s.settings.String("review.tier", ""))); t {
	case review.TierObserve, review.TierLowRisk, review.TierAuto:
		rs.Tier = t
	}
	if m := s.settings.Int("review.interval_min", 10); m > 0 {
		rs.Interval = time.Duration(m) * time.Minute
	}
	if m := s.settings.Int("review.cooldown_min", 5); m >= 0 {
		rs.Cooldown = time.Duration(m) * time.Minute
	}
	rs.CondOn = s.settings.Bool("review.cond", true)
	return rs
}

// reviewTierFor 返回某任务实际生效的档位：任务覆盖（review_state.tier）优先于全局。
func (s *Server) reviewTierFor(taskID int64, st review.State) string {
	if t := strings.TrimSpace(st.Tier); t != "" {
		return t
	}
	return s.reviewSettings().Tier
}

// loadReviewState 读 review_state（没有就返回零值，不写库）。
func (s *Server) loadReviewState(ctx context.Context, taskID int64) review.State {
	var st review.State
	var lastSent, tripped sql.NullTime
	var tripReason string
	err := s.m.pg.QueryRowContext(ctx, `SELECT COALESCE(tier,''), interventions, auto_actions,
		last_sent_at, last_facts, tripped_at, COALESCE(trip_reason,'')
		FROM review_state WHERE task_id=$1`, taskID).
		Scan(&st.Tier, &st.Interventions, &st.AutoActions, &lastSent, &st.LastFacts, &tripped, &tripReason)
	if err != nil {
		return review.State{}
	}
	if lastSent.Valid {
		st.LastSentAt = lastSent.Time
	}
	st.Tripped = tripped.Valid
	if tripped.Valid {
		st.TripReason = tripReason
	}
	return st
}

// saveReviewState 写回 review_state（幂等 upsert）。
func (s *Server) saveReviewState(ctx context.Context, taskID int64, st review.State, facts int) {
	var lastSent any
	if !st.LastSentAt.IsZero() {
		lastSent = st.LastSentAt
	}
	var tripped any
	if st.Tripped {
		tripped = time.Now()
	}
	_, err := s.m.pg.ExecContext(ctx, `
INSERT INTO review_state(task_id, tier, interventions, auto_actions, last_sent_at, last_facts, tripped_at, trip_reason, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,now())
ON CONFLICT (task_id) DO UPDATE SET
  tier=EXCLUDED.tier, interventions=EXCLUDED.interventions, auto_actions=EXCLUDED.auto_actions,
  last_sent_at=EXCLUDED.last_sent_at, last_facts=EXCLUDED.last_facts,
  tripped_at=EXCLUDED.tripped_at, trip_reason=EXCLUDED.trip_reason, updated_at=now()`,
		taskID, st.Tier, st.Interventions, st.AutoActions, lastSent, facts, tripped, st.TripReason)
	if err != nil {
		log.Printf("[review] save state task=%d: %v", taskID, err)
	}
}

// recentReviewRuns 取最近 N 条评审摘要（去重 / 降权判定用）。
func (s *Server) recentReviewRuns(ctx context.Context, taskID int64, limit int) []review.RunBrief {
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.m.pg.QueryContext(ctx, `SELECT COALESCE(dedupe_key,''), COALESCE(level,''), COALESCE(reason_class,''), COALESCE(ack_status,'')
		FROM review_runs WHERE task_id=$1 AND status='done' ORDER BY id DESC LIMIT $2`, taskID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []review.RunBrief{}
	for rows.Next() {
		var r review.RunBrief
		if rows.Scan(&r.DedupeKey, &r.Level, &r.ReasonClass, &r.AckStatus) == nil {
			out = append(out, r)
		}
	}
	return out
}

// applyReviewAction 执行单个已批准动作。返回 (是否真的做了, 说明)。
// 与「人点采纳」共用同一份实现，避免自动路径和手动路径行为漂移。
func (s *Server) applyReviewAction(t *Task, a review.Action) (bool, string) {
	if t == nil || t.Store == nil {
		return false, ""
	}
	switch a.Kind {
	case "kill_intent":
		for _, from := range []string{"open", "running", "active"} {
			if err := transitionIntentState(t.Store, a.Intent, from, "stopped"); err == nil {
				return true, fmt.Sprintf("已标死 #%d（依据：%s）", a.Intent, a.Why)
			}
		}
	case "retry_intent":
		for _, from := range []string{"stopped", "blocked", "open"} {
			if err := transitionIntentState(t.Store, a.Intent, from, "open"); err == nil {
				return true, fmt.Sprintf("已重开 #%d（依据：%s）", a.Intent, a.Why)
			}
		}
	}
	return false, ""
}

// deliverReviewAdvice 把 B 级建议投给主链路：写一条 hint（planner 会看到并把
// "先核对现状再决定"当硬约束），并留一条 activity 供人看。它**不**直接执行任何动作。
func (s *Server) deliverReviewAdvice(ctx context.Context, t *Task, runID int64, text string) {
	if t == nil || t.Store == nil {
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	tsx := agent.NewToolSet(t.Store, "adviser")
	tsx.SetTaskID(taskID)
	tsx.SetGoal(t.Goal)
	if ok, err := tsx.AddHintOnce(ctx, text); err != nil {
		log.Printf("[review] deliver advice task=%s: %v", t.ID, err)
		return
	} else if ok && s.engine != nil {
		s.engine.emitActivity(t, db.Activity{Worker: "adviser", Kind: "text",
			Summary: "审查官：给主链路的建议已投递（先核对现状再决定，不是命令）"})
		t.Notify()
	}
}

// finishReviewRun 把一次评审的最终形态写回 review_runs。
func (s *Server) finishReviewRun(ctx context.Context, runID int64, v review.Verdict, model string,
	g review.GateOut, ver, autoApplied int, ackStatus, ackReason string) {
	citesJSON, _ := json.Marshal(v.Cites)
	actionsJSON, _ := json.Marshal(v.Actions)
	status := "done"
	if g.Drop != "" {
		status = "superseded" // 自检没过：只留内部日志，不进主会话、不亮红点
	}
	_, err := s.m.pg.ExecContext(ctx, `UPDATE review_runs SET status=$2, verdict=$3, body=$4, cites=$5, actions=$6,
		model=$7, notes_version=$8, dedupe_key=$9, level=$10, reason_class=$11,
		auto_applied=$12, ack_status=$13, ack_reason=$14, error=$15, finished_at=now() WHERE id=$1`,
		runID, status, v.Verdict, v.Body, string(citesJSON), string(actionsJSON),
		model, ver, g.DedupeKey, g.Level, g.ReasonClass,
		autoApplied > 0, ackStatus, ackReason, g.Drop)
	if err != nil {
		log.Printf("[review] finish run=%d: %v", runID, err)
	}
}

// reviewCondHit 是「条件触发」的判据（docs/12 §一 补齐方案 3 / docs/13 §4）。
// 四条任一命中就跑，全部是确定性信号（零 token）：
//  1. 连续 3 轮没有任何新事实/新漏洞 → 它在原地打转；
//  2. 某条方向反复失败（同一意图 attempts ≥ 5）→ 该换打法；
//  3. 预算烧掉过半但一个洞都没有 → 方向大概率错了；
//  4. 停顿检测器报出≥2 个互不相同的信号 → 多个维度同时不健康。
func reviewCondHit(rep stall.Report) bool {
	if rep.NoProgressRounds >= 3 {
		return true
	}
	for _, it := range rep.Intents {
		if it.Attempts >= 5 {
			return true
		}
	}
	if rep.BudgetBurnPct >= 50 && len(rep.Stuck) > 0 {
		return true
	}
	return len(rep.Stuck) >= 2
}

// reviewTicker 是「定时触发」：每分钟扫一次在跑的任务，距上次评审超过间隔就跑一次。
// 与条件触发共用 reviewOnce（同一份快照/提示词/防抖），trigger='timer'。
func (s *Server) reviewTicker(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.reviewTickOnce(ctx)
		}
	}
}

func (s *Server) reviewTickOnce(ctx context.Context) {
	if s.m == nil || s.m.PG() == nil {
		return
	}
	rs := s.reviewSettings()
	if rs.Interval <= 0 {
		return
	}
	for _, task := range s.m.List() {
		if task == nil || task.Queued || task.Paused {
			continue
		}
		if task.CompletedAt != 0 || task.Status == "done" || task.Status == "failed" || task.Status == "timeout" {
			continue
		}
		taskID, err := strconv.ParseInt(task.ID, 10, 64)
		if err != nil || taskID <= 0 {
			continue
		}
		var last sql.NullTime
		_ = s.m.pg.QueryRowContext(ctx, `SELECT max(created_at) FROM review_runs WHERE task_id=$1`, taskID).Scan(&last)
		if last.Valid && time.Since(last.Time) < rs.Interval {
			continue
		}
		s.reviewCondKick(task, taskID, "timer")
	}
}

// reviewCondKick 在后台跑一次评审（带锁 + 超时），失败只记日志。
func (s *Server) reviewCondKick(t *Task, taskID int64, trigger string) {
	if t == nil {
		return
	}
	mu := reviewLock(taskID)
	if !mu.TryLock() {
		return
	}
	defer mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	runID, err := s.reviewOnce(ctx, t.ID, taskID, trigger, "")
	if err != nil {
		log.Printf("[review] %s 触发失败 (task=%s): %v", trigger, t.ID, err)
		return
	}
	log.Printf("[review] %s 触发完成 (task=%s, run=%d)", trigger, t.ID, runID)
	t.Notify()
}
