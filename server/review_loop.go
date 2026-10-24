package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
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
		// 只从「真的停过」的状态重开：已 open/running 的意图再“重开”是空转，
		// 也会让 ack 超时路径把已经做过的动作再做一遍。
		for _, from := range []string{"stopped", "blocked"} {
			if err := transitionIntentState(t.Store, a.Intent, from, "open"); err == nil {
				return true, fmt.Sprintf("已重开 #%d（依据：%s）", a.Intent, a.Why)
			}
		}
	}
	return false, ""
}

// writeReviewHint 是审查官写 hint 的唯一落库入口：由 deliverReviewHintOnce 调用（B 级建议
// 与 propose_intent 共用那一条投递路径）。
// AddHintOnce 会去重（同样的话不重复占提示位），并留一条 adviser 的 activity。
// 它写的永远只是「建议」，不执行任何动作。
func (s *Server) writeReviewHint(ctx context.Context, t *Task, text, summary string) (bool, error) {
	if t == nil || t.Store == nil || strings.TrimSpace(text) == "" {
		return false, nil
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	tsx := agent.NewToolSet(t.Store, "adviser")
	tsx.SetTaskID(taskID)
	tsx.SetGoal(t.Goal)
	ok, err := tsx.AddHintOnce(ctx, text)
	if err != nil {
		return false, err
	}
	if ok && s.engine != nil {
		s.engine.emitActivity(t, db.Activity{Worker: "adviser", Kind: "text", Summary: summary})
		t.Notify()
	}
	return ok, nil
}

// reviewAdviceHeadline 取建议正文的第一句实话（跳过「建议不是命令」表头、空行与
// 拟动作清单），用作「会话」tab 里那条主 Agent 活动的一行摘要。
func reviewAdviceHeadline(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "审查官建议（") || strings.HasPrefix(line, "拟动作：") {
			continue
		}
		return firstLine(line, 120)
	}
	return firstLine(text, 120)
}

// emitMainAgentAdvice 把「审查官建议」写进主 Agent 会话流（worker="mainagent"）。
// 「会话」tab 只渲染 worker='mainagent' 的活动，只写 hint + activity(adviser) 人看不见，
// 所以这里补一条主 Agent 可见的记录。它只是文本：不改目标、不收工、不派活。
func (s *Server) emitMainAgentAdvice(t *Task, runID int64, text string) {
	if s.engine == nil || t == nil || t.Store == nil {
		return
	}
	// 与主 Agent 的其它回合写进同一个会话段：MainSeg=nil 会落到「原始会话」，
	// 新建过会话之后当前 tab 里就看不到（sessions-tab 按 main:<seg> 路由）。
	var segPtr *int
	if seg, err := t.Store.CurrentMainSeg(); err == nil {
		segPtr = &seg
	}
	detail := text
	if runID > 0 {
		detail += "\n\n依据：review run #" + strconv.FormatInt(runID, 10) +
			"（结论卡里的 cites/actions 带真实 id 与状态）。"
	}
	detail += "\n请先核对现状再决定；不要把它当命令直接执行（审查官没有执行权：没改目标、没收工、没派活）。"
	s.engine.emitActivity(t, db.Activity{
		Worker:  "mainagent",
		Kind:    "text",
		MainSeg: segPtr,
		Summary: "审查官建议（只读，不是命令）：" + reviewAdviceHeadline(text),
		Detail:  detail,
	})
	t.Notify()
}

// claimReviewDelivery 是「同一条 run 只投递一次」的哨兵：ack_at 为空说明这条结论
// 还没投递过，只有抢到（RowsAffected==1）的那一方才有权写主 Agent 活动。
// 并发的定时评审 / ack 超时路径 / 多实例因此不会把同一句话说两遍。
//
// 为什么用 ack_at 而不是写一个 ack_status='delivered'：ack_status 的取值只有
// 空串 / held / accepted / rejected / escalated（db/migrations/0006、0008），没有 delivered；
// 而且 reviewAckSweep 只扫 ack_status 为空的行（partial index 也是按它建的），
// 真写一个非空值会让「90 秒没人回 → 按档位处理」整体失效。所以投递只打 ack_at=now()
// 表示「已投递」，ack_status 保持为空，升级流程照旧，但不会再投递第二次。
func (s *Server) claimReviewDelivery(ctx context.Context, runID int64) bool {
	if s.m == nil || s.m.pg == nil {
		return false
	}
	res, err := s.m.pg.ExecContext(ctx, `UPDATE review_runs SET ack_at=now()
		WHERE id=$1 AND ack_at IS NULL`, runID)
	if err != nil {
		log.Printf("[review] claim delivery run=%d: %v", runID, err)
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

// deliverReviewHintOnce 是审查官向主链路投递的唯一实现（B 级建议与 propose_intent 共用）：
//  1. 写 hint（planner 侧看得到，AddHintOnce 会去重）；
//  2. 抢哨兵，抢到才写一条 worker="mainagent" 的活动——「会话」tab 因此看得见。
//
// 顺序是有意的：hint 先落库，主 Agent 侧记录后写；任一失败都只是少一条活动，不会重复说两遍。
// 返回 hint 是否真的写进去了（用于给界面回执文案）。
func (s *Server) deliverReviewHintOnce(ctx context.Context, t *Task, runID int64, text, hintSummary string) bool {
	if t == nil || strings.TrimSpace(text) == "" {
		return false
	}
	ok, err := s.writeReviewHint(ctx, t, text, hintSummary)
	if err != nil {
		log.Printf("[review] deliver hint task=%s: %v", t.ID, err)
	}
	if runID <= 0 {
		// 没有 run 可做哨兵（例如手工触发的建议）：照常投一次，调用方保证只调一次。
		s.emitMainAgentAdvice(t, runID, text)
		return ok
	}
	if !s.claimReviewDelivery(ctx, runID) {
		return ok // 这条 run 已经投递过：hint 照写（去重），但不再占会话位置
	}
	s.emitMainAgentAdvice(t, runID, text)
	return ok
}

// deliverReviewAdvice 把 B 级建议投给主链路：写一条 hint（planner 会看到并把
// "先核对现状再决定"当硬约束），并在「会话」tab 留一条主 Agent 可见的活动。
// 它**不**直接执行任何动作。
func (s *Server) deliverReviewAdvice(ctx context.Context, t *Task, runID int64, text string) {
	s.deliverReviewHintOnce(ctx, t, runID, text,
		"审查官：给主链路的建议已投递（先核对现状再决定，不是命令）")
}

// finishReviewRun 把一次评审的最终形态写回 review_runs。
func (s *Server) finishReviewRun(ctx context.Context, runID int64, v review.Verdict, model string,
	g review.GateOut, ver, autoApplied int, ackStatus, ackReason string) {
	cites := v.Cites
	if cites == nil {
		cites = []review.Cite{} // 落 null 会让前端分不清“没引证据”和“字段缺失”
	}
	citesJSON, _ := json.Marshal(cites)
	// 只落门后真正允许下发的动作；被剔除/降权的动作不该进入观察窗冲突判定。
	actions := g.Actions
	if actions == nil {
		actions = []review.Action{}
	}
	actionsJSON, _ := json.Marshal(actions)
	status := "done"
	if g.Drop != "" {
		status = "superseded" // 自检没过：只留内部日志，不进主会话、不亮红点
	}
	_, err := s.m.pg.ExecContext(ctx, `UPDATE review_runs SET status=$2, verdict=$3, body=$4, cites=$5, actions=$6,
		model=$7, notes_version=$8, dedupe_key=$9, level=$10, reason_class=$11,
		auto_applied=$12, ack_status=$13, ack_reason=$14, error=$15, expectation=$16, finished_at=now() WHERE id=$1`,
		runID, status, v.Verdict, v.Body, string(citesJSON), string(actionsJSON),
		model, ver, g.DedupeKey, g.Level, g.ReasonClass,
		autoApplied > 0, ackStatus, ackReason, g.Drop, strings.TrimSpace(v.Expectation))
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

// reviewTicker 是「定时触发 + ack 扫描」：
//
//	每 20 秒：先扫 ack 窗口（到点没人回 → escalated → 按档位自动/只建议）
//	每 60 秒：扫一遍在跑的任务，距上次评审超过间隔就跑一次
//
// ack 扫描必须不被长评审拖住，所以定时评审放后台 goroutine（reviewCondKick 自己带任务锁）。
func (s *Server) reviewTicker(ctx context.Context) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	tick := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.reviewAckSweep(ctx)     // 20 秒粒度（docs/14 §4）
			s.reviewObserveSweep(ctx) // 20 秒粒度回访观察窗（docs/15 §3）
			tick++
			if tick%3 != 0 {
				continue
			}
			if !reviewTickMu.TryLock() {
				continue // 上一轮还没跑完，不堆叠
			}
			go func() {
				defer reviewTickMu.Unlock()
				s.reviewTickOnce(ctx)
			}()
		}
	}
}

// reviewTickMu 保证定时评审不会因为上一轮超时而堆叠（ack 扫描不受它限制）。
var reviewTickMu sync.Mutex

// reviewAckSweep 扫「到点且没人回执」（ack_status 为空）的结论：先原子抢 escalated，
// 抢到的那一方才有权执行（避免与人点击/多实例重复处理）。
func (s *Server) reviewAckSweep(ctx context.Context) {
	if s.m == nil || s.m.PG() == nil {
		return
	}
	rows, err := s.m.pg.QueryContext(ctx, `SELECT id, task_id, COALESCE(actions::text,'[]'), COALESCE(reason_class,'')
		FROM review_runs
		WHERE status='done' AND COALESCE(ack_status,'')=''
		  AND ack_deadline IS NOT NULL AND ack_deadline < now()
		ORDER BY id LIMIT 20`)
	if err != nil {
		return
	}
	type due struct {
		runID, taskID int64
		actions       string
		reasonClass   string
	}
	var list []due
	for rows.Next() {
		var x due
		if rows.Scan(&x.runID, &x.taskID, &x.actions, &x.reasonClass) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	for _, x := range list {
		res, err := s.m.pg.ExecContext(ctx, `UPDATE review_runs SET ack_status='escalated', ack_at=now()
			WHERE id=$1 AND COALESCE(ack_status,'')='' AND ack_deadline IS NOT NULL AND ack_deadline < now()`, x.runID)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n != 1 {
			continue // 人刚好点了 / 另一个实例抢到了
		}
		log.Printf("[review] run=%d ack 超时（无人回应）→ 按档位处理", x.runID)
		s.reviewEscalate(ctx, x.taskID, x.runID, x.actions)
	}
}

// ───────── 观察窗回访（docs/15 §3/§5）─────────

// reviewObserveDue 是一条到点待回访的介入。
type reviewObserveDue struct {
	runID, taskID int64
	expectation   string
	actionsRaw    string
	startedAt     time.Time
}

// reviewObserveSweep 扫「观察窗到点且还没有结论」的介入：
//
//	outcome='' AND observe_until < now()
//
// 比对介入后新增 fact 与 expectation 关键词，写 effective/ineffective/voided。
// 用 outcome=” 的原子更新认领，多实例/重复扫描不会把同一句话发两遍。
func (s *Server) reviewObserveSweep(ctx context.Context) {
	if s.m == nil || s.m.PG() == nil {
		return
	}
	rows, err := s.m.pg.QueryContext(ctx, `SELECT id, task_id, COALESCE(expectation,''),
		COALESCE(actions::text,'[]'), COALESCE(finished_at, created_at)
		FROM review_runs
		WHERE status='done' AND COALESCE(outcome,'')=''
		  AND observe_until IS NOT NULL AND observe_until < now()
		ORDER BY id LIMIT 20`)
	if err != nil {
		return
	}
	var list []reviewObserveDue
	for rows.Next() {
		var x reviewObserveDue
		if rows.Scan(&x.runID, &x.taskID, &x.expectation, &x.actionsRaw, &x.startedAt) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	for _, x := range list {
		s.reviewObserveOne(ctx, x)
	}
}

// reviewObserveOne 判定单条观察窗：
//
//	voided      —— 建议放弃的方向又产出了新 fact（exploration_edges 直连，见 reviewObservationConflict）；
//	effective   —— 新增 fact ≥1 且 expectation 里至少一个具体关键词出现在新 fact 摘要里；
//	ineffective —— 其余；有动作时再发一条回访建议给主 Agent。
func (s *Server) reviewObserveOne(ctx context.Context, x reviewObserveDue) {
	newFacts, total := s.reviewFactsSince(ctx, x.taskID, x.startedAt)
	var conflict *review.FactBrief
	var conflictIntent int64
	if c := s.reviewObservationConflict(ctx, x); c != nil {
		conflict = &c.Fact
		conflictIntent = c.IntentID
	}
	obs := review.EvaluateObservation(x.expectation, newFacts, conflict)

	detail := ""
	switch obs.Outcome {
	case review.OutcomeVoided:
		summary := ""
		if conflict != nil {
			summary = firstLine(conflict.Summary, 80)
		}
		if conflictIntent > 0 {
			detail = fmt.Sprintf("观察窗：建议放弃的 #%d 方向又产出了新事实（fact#%d：%s），原建议被推翻，已撤回",
				conflictIntent, obs.FactID, summary)
		} else {
			detail = fmt.Sprintf("观察窗：原建议被新事实推翻（fact#%d：%s），已撤回", obs.FactID, summary)
		}
	case review.OutcomeEffective:
		detail = fmt.Sprintf("观察窗：预期「%s」已出现（命中 fact#%d 关键词「%s」，新增 fact %d 条）",
			x.expectation, obs.FactID, obs.Keyword, total)
	default:
		detail = fmt.Sprintf("观察窗：预期「%s」未出现（新增 fact %d 条，关键词未命中）", x.expectation, total)
	}

	res, err := s.m.pg.ExecContext(ctx, `UPDATE review_runs
		SET outcome=$2, observe_until=NULL,
		    ack_reason = CASE WHEN COALESCE(ack_reason,'')='' THEN $3 ELSE ack_reason || '；' || $3 END
		WHERE id=$1 AND COALESCE(outcome,'')=''`, x.runID, obs.Outcome, detail)
	if err != nil {
		log.Printf("[review] observe run=%d: %v", x.runID, err)
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return // 另一个实例/上一次扫描已经判过
	}
	log.Printf("[review] run=%d 观察窗结论=%s：%s", x.runID, obs.Outcome, detail)

	// 全部写 activity（worker='adviser'），便于排查；任务已不在运行就只落库。
	t, ok := s.m.Task(strconv.FormatInt(x.taskID, 10))
	if ok && t != nil && s.engine != nil {
		s.engine.emitActivity(t, db.Activity{
			Worker: "adviser", Kind: "text",
			Summary: "观察窗：" + obs.Outcome + "（" + firstLine(x.expectation, 40) + "）",
			Detail:  detail,
		})
		t.Notify()
	}

	// 没起效且有动作：给主 Agent 一条回访建议（复用唯一投递入口）。
	if obs.Outcome == review.OutcomeIneffective {
		var actions []review.Action
		_ = json.Unmarshal([]byte(x.actionsRaw), &actions)
		if len(actions) > 0 && ok && t != nil {
			text := fmt.Sprintf("审查官回访：上次建议没起效（预期「%s」未出现），考虑换方向/判死。\n依据：review run #%d；观察窗 10 分钟内新增 fact %d 条，但关键词没有命中。",
				x.expectation, x.runID, total)
			// runID=0：这是一条「新的回访消息」，不能因为原 run 已投递过就被哨兵吞掉；
			// 不会重复——outcome 的原子更新已经保证每条 run 只回访一次。
			s.deliverReviewHintOnce(ctx, t, 0, text, "审查官回访：上次建议没起效，建议换方向/判死")
		}
	}
}

// reviewFactsSince 取「介入后新增」的 fact（按 id 倒序，最多 100 条），并返回总数。
// 计数用 count(*) OVER ()，一次查询拿到总数与摘要，避免扫描时再查一遍。
func (s *Server) reviewFactsSince(ctx context.Context, taskID int64, since time.Time) ([]review.FactBrief, int) {
	rows, err := s.m.pg.QueryContext(ctx, `SELECT n.id,
			COALESCE(NULLIF(n.payload->>'summary',''), NULLIF(n.payload->>'text',''), ''),
			count(*) OVER () AS total
		FROM exploration_nodes n
		WHERE n.exploration_id=(SELECT exploration_id FROM tasks WHERE id=$1)
		  AND n.kind='fact' AND n.created_at > $2
		ORDER BY n.id DESC LIMIT 100`, taskID, since)
	if err != nil {
		return nil, 0
	}
	defer rows.Close()
	var out []review.FactBrief
	total := 0
	for rows.Next() {
		var f review.FactBrief
		if rows.Scan(&f.ID, &f.Summary, &total) == nil {
			out = append(out, f)
		}
	}
	return out, total
}

// reviewObservationConflict 判定「建议被新事实直接推翻」的情形：
// 如果这条 run 建议 kill 某条意图，而观察窗内该意图又产出了新 fact（
// exploration_edges 直连，rel 不限），就是最典型的 voided（docs/15 §3：
// “它建议放弃的方向产出了新 fact”）。
type reviewObserveConflict struct {
	IntentID int64
	Fact     review.FactBrief
}

func (s *Server) reviewObservationConflict(ctx context.Context, x reviewObserveDue) *reviewObserveConflict {
	var actions []review.Action
	_ = json.Unmarshal([]byte(x.actionsRaw), &actions)
	for _, a := range actions {
		if a.Kind != "kill_intent" || a.Intent <= 0 {
			continue
		}
		var f review.FactBrief
		err := s.m.pg.QueryRowContext(ctx, `SELECT n.id,
				COALESCE(NULLIF(n.payload->>'summary',''), NULLIF(n.payload->>'text',''), '')
			FROM exploration_nodes n
			JOIN exploration_edges e ON e.exploration_id=n.exploration_id
			  AND ((e.src_id=$2 AND e.dst_id=n.id) OR (e.dst_id=$2 AND e.src_id=n.id))
			WHERE n.exploration_id=(SELECT exploration_id FROM tasks WHERE id=$1)
			  AND n.kind='fact' AND n.created_at > $3
			ORDER BY n.id DESC LIMIT 1`, x.taskID, a.Intent, x.startedAt).Scan(&f.ID, &f.Summary)
		if err == nil {
			return &reviewObserveConflict{IntentID: a.Intent, Fact: f}
		}
	}
	return nil
}

// reviewEscalate 是「90 秒没人点」之后的处理（docs/14 §4）：
//
//	低风险减浪费动作（kill_intent / retry_intent）→ 复用自动路径 apply；
//	需人确认的（propose_intent / set_next_stage）→ 只写一条 B 级建议给主链路。
//
// 绝不改目标、绝不收工；observe 档下一个也不动，全部降为建议。
func (s *Server) reviewEscalate(ctx context.Context, taskID, runID int64, actionsRaw string) {
	setReason := func(reason string) {
		_, _ = s.m.pg.ExecContext(ctx, `UPDATE review_runs SET ack_reason=$2 WHERE id=$1`, runID, reason)
	}
	var actions []review.Action
	_ = json.Unmarshal([]byte(actionsRaw), &actions)
	if len(actions) == 0 {
		setReason("窗口超时（90s）：没有要执行的动作")
		return
	}
	t, ok := s.m.Task(strconv.FormatInt(taskID, 10))
	if !ok || t == nil || t.Store == nil {
		setReason("窗口超时（90s）：任务已不在运行，未执行任何动作")
		return
	}
	rs := s.reviewSettings()
	st := s.loadReviewState(ctx, taskID)
	tier := s.reviewTierFor(taskID, st)
	auto, suggest := review.EscalatePlan(actions)
	if tier == review.TierObserve {
		auto, suggest = nil, actions // 只看档：一个也不动
	}

	var done []string
	var applied []review.Action
	for _, a := range auto {
		if reviewActionAlreadyDone(t, a) {
			continue // 自动路径/人已经处理过，不重复说第二次
		}
		if st.AutoActions+len(applied) >= rs.MaxAuto {
			suggest = append(suggest, a) // 配额用尽 → 降为建议
			continue
		}
		if ok, note := s.applyReviewAction(t, a); ok {
			applied = append(applied, a)
			done = append(done, note)
		} else {
			suggest = append(suggest, a) // 改不动就让人看
		}
	}
	if len(applied) > 0 {
		st.AutoActions += len(applied)
		s.saveReviewState(ctx, taskID, st, st.LastFacts)
		_, _ = s.m.pg.ExecContext(ctx, `INSERT INTO review_feedback(run_id,action,by_user,outcome) VALUES($1,'applied','auto','')`, runID)
		_, _ = s.m.pg.ExecContext(ctx, `UPDATE review_runs SET auto_applied=true WHERE id=$1`, runID)
		if s.engine != nil {
			s.engine.emitActivity(t, db.Activity{Worker: "adviser", Kind: "text",
				Summary: "审查官等不到回应，按档位自行处理（只减浪费）：" + strings.Join(done, "；")})
		}
	}
	if len(suggest) > 0 {
		text := review.AdviceText("90 秒窗口内没人回应，审查官按档位处理：下面这些需要你核对后再决定，它没有动目标、没有收工。", suggest, nil)
		s.deliverReviewAdvice(ctx, t, runID, text)
	}

	reason := "窗口超时（90s）："
	if len(applied) > 0 {
		reason += "自动处理 " + strings.Join(done, "；")
	} else {
		reason += "没有可自动执行的动作"
	}
	if len(suggest) > 0 {
		reason += fmt.Sprintf("；%d 条只写成建议（未改目标/未收工）", len(suggest))
	}
	setReason(reason)
	t.Notify()
}

// reviewActionAlreadyDone 判断这条动作的效果是否已经生效（避免超时路径重复处理）。
func reviewActionAlreadyDone(t *Task, a review.Action) bool {
	if t == nil || t.Store == nil || a.Intent <= 0 {
		return false
	}
	n, err := t.Store.GetNode(a.Intent)
	if err != nil || n == nil {
		return false
	}
	switch a.Kind {
	case "kill_intent":
		return n.State == "stopped" || n.State == "deleted"
	case "retry_intent":
		return n.State == "open" || n.State == "running" || n.State == "active"
	}
	return false
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
