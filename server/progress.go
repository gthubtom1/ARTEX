package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/adviser"
	"github.com/Autumn-27/artex/internal/review"
	"github.com/Autumn-27/artex/internal/stall"
)

// shouldConsultLLMAdviser 决定本轮的停顿是否需要额外 LLM 顾问。
// 两个条件：确定性检测命中（rep.Hit）且用户在设置里开了 adviser.enabled。
// 默认关：关掉时返回 false，调用点完全不碰 LLM；检测与规则 hint 照常。
func (s *Server) shouldConsultLLMAdviser(rep stall.Report) bool {
	return rep.Hit() && s.adviserLLMEnabled()
}

// adviserLLMEnabled 读 D2 的 adviser.enabled 开关。默认 false，
// settings 为 nil 时也按关处理（宁可不调，也不能凭空花 token）。
func (s *Server) adviserLLMEnabled() bool {
	return s.settings != nil && s.settings.Bool("adviser.enabled", false)
}

// reviewCondEnabled 读审查官「条件触发」开关（docs/13：条件默认开）。
// 关着时 progressHook 完全不碰 review 路径（零 LLM、零 DB 写入）。
func (s *Server) reviewCondEnabled() bool {
	return s.reviewSettings().CondOn
}

// progressHook 是每轮规划之后的反跑偏闭环（best-effort）：
//  1. 确定性停顿检测 → 顾问诊断（LLM 顾问可用时优先，失败回落规则）→ 可执行 hint 写图并唤醒 planner；
//  2. 持有台账到期 → 写一条只读复查提醒；
//  3. 把本任务已登记的 finding 派生为能力卡（幂等，只补新 item）。
//
// 设计纪律：这里不派意图、不做权限判断、不打断任务；只做"发现问题 + 给建议 + 记账"。
func (s *Server) progressHook(m *Manager) func(context.Context, *Task) error {
	return func(ctx context.Context, t *Task) error {
		if t == nil || t.Store == nil {
			return nil
		}
		taskID, _ := strconv.ParseInt(t.ID, 10, 64)
		tsx := agent.NewToolSet(t.Store, "adviser")
		tsx.SetTaskID(taskID)
		tsx.SetGoal(t.Goal)
		if m != nil {
			if assets := m.Assets(); assets != nil {
				tsx.SetAssetStore(assets, assets.Companies())
			}
		}

		rep, led, neg, err := tsx.StallBundle(ctx)
		if err != nil {
			return err
		}
		d := adviser.Diagnose(rep, led, neg)
		d.Source = "rule"
		// D2：adviser 默认关。关着时**完全不调 LLM**（省 token），
		// 但上面的 stall 检测与下面的 hint 写入照常跑 —— 那部分不花 token。
		// 开关由 shouldConsultLLMAdviser 读 settings（默认为 false）。
		if s.shouldConsultLLMAdviser(rep) {
			if llmD, ok := s.tryLLMAdviser(ctx, t, rep, led, neg); ok {
				d = llmD
			}
		}
		// 审查官「条件触发」（docs/13 §0/§4）：命中条件就让它自己看一次，结论写 review_runs（A 级），
		// 有动作时按档位自动执行或给主链路建议（B 级）。
		// 条件：连续 3 轮无新 fact / 同类我方故障反复 / 预算过半没新洞 / 某路反复失败。
		// 冷却/配额/去重/熔断由 review.Decide 兜（trigger=cond），这里只判"要不要跑一次"。
		if s.reviewCondEnabled() && reviewCondHit(rep) && s.reviewCondGate.allow(t.ID, s.reviewSettings().Cooldown) {
			go s.reviewCondKick(t, taskID, review.TriggerCond)
		}

		// 一行诊断：把本轮两个实验角色的开关状态写进日志。D2 默认关，
		// 出问题时能直接看出是“没检测到停顿”还是“开关没开”，不必猜。
		log.Printf("[progress] task %s 停顿=%v adviser_llm=%v verifier_llm=%v",
			t.ID, rep.Stuck, s.adviserLLMEnabled(), s.verifierLLMEnabled())
		wrote := 0
		for _, h := range d.Hints {
			if ok, _ := tsx.AddHintOnce(ctx, h); ok {
				wrote++
			}
		}
		if wrote > 0 {
			src := "规则"
			if d.Source == "llm" {
				src = "LLM"
			}
			s.engine.emitActivity(t, db.Activity{Worker: "adviser", Kind: "text",
				Summary: fmt.Sprintf("审查官(%s)：注入 %d 条纠正提示（%s）", src, wrote, strings.Join(rep.Stuck, ","))})
			t.Notify()
		}

		// 持有台账：到期复查提醒（只读确认 + owned_check 写回；不自动执行任何命令）。
		if s.p2 != nil {
			if due, derr := s.p2.Ledger.Due(ctx, time.Now().UTC(), 20); derr == nil && len(due) > 0 {
				var names []string
				for i, o := range due {
					if i >= 3 {
						break
					}
					host := o.Host
					if host == "" {
						host = o.IP
					}
					names = append(names, host)
				}
				text := fmt.Sprintf("持有台账：%d 台到期该复查（%s）。用只读命令（nc/curl）确认存活后用 owned_check 写回；改密或关站会自动标 stale。",
					len(due), strings.Join(names, "、"))
				if wrote, herr := tsx.AddHintOnce(ctx, text); herr == nil && wrote {
					s.engine.emitActivity(t, db.Activity{Worker: "system", Kind: "text", Summary: text})
					t.Notify()
				}
			}
		}

		// 决策预算硬熔断：continue 到点 → 卡标 done、写 hint、杀掉匹配的在跑意图。
		if n := s.enforceBudgets(ctx, t, tsx); n > 0 {
			s.engine.emitActivity(t, db.Activity{Worker: "system", Kind: "text",
				Summary: fmt.Sprintf("决策预算到点：停止 %d 个方向（已杀死匹配的运行中意图）", n)})
			t.Notify()
		}

		// 独立角色复核：证据不够就降级（每任务 5 分钟最多一次）。
		// D2：verifier 默认关，关着时 verifierPass 内部直接返回 0（零 LLM、零 DB 查询）。
		// 人工手动触发走 verifierPassNow，不受该开关影响。
		if n := s.verifierPass(ctx, t); n > 0 {
			log.Printf("[progress] task %s verifier 复核 %d 条 finding", t.ID, n)
		}

		if caps, cerr := tsx.SyncCapabilities(ctx); cerr != nil {
			log.Printf("[progress] task %s 能力卡派生失败(忽略): %v", t.ID, cerr)
		} else if caps > 0 {
			s.engine.emitActivity(t, db.Activity{Worker: "system", Kind: "text",
				Summary: fmt.Sprintf("能力卡：从 finding 派生 %d 张（list_capabilities 可看白话菜单）", caps)})
		}
		return nil
	}
}

// enforceBudgets 扫描到期的 continue 裁决：标 done + hint + kill 匹配的意图。
// 匹配口径：意图 summary 含卡片的 surface 或 can（best-effort；没匹配到就只停方向）。
func (s *Server) enforceBudgets(ctx context.Context, t *Task, tsx *agent.ToolSet) int {
	if s.p2 == nil || s.p2.DB == nil || t.Store == nil {
		return 0
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	rows, err := s.p2.DB.QueryContext(ctx,
		`SELECT id, can, surface, COALESCE(budget_rounds,0), budget_started_at FROM capabilities
		 WHERE task_id = $1 AND status = 'approved' AND decision_action = 'continue'
		   AND budget_started_at IS NOT NULL`,
		taskID)
	if err != nil {
		return 0
	}
	type expired struct {
		id              int64
		can, surface    string
		budgetRounds    int
		budgetStartedAt time.Time
	}
	var list []expired
	now := time.Now()
	for rows.Next() {
		var e expired
		if err := rows.Scan(&e.id, &e.can, &e.surface, &e.budgetRounds, &e.budgetStartedAt); err != nil {
			continue // 单行扫描失败不拖垮整轮（下一轮会重试）
		}
		if budgetExpired(now, e.budgetStartedAt, 15*time.Minute, 0, e.budgetRounds) {
			list = append(list, e)
		}
	}
	rows.Close()
	if len(list) == 0 {
		return 0
	}
	intents, _ := t.Store.ListByKind(db.KindIntent, 200)
	// 第二轮：轮数预算（按 start 之后匹配方向的意图数计）。
	final := list[:0]
	for _, e := range list {
		if !budgetExpired(time.Now(), e.budgetStartedAt, 15*time.Minute, countRounds(intents, e.budgetStartedAt, e.can, e.surface), e.budgetRounds) {
			continue
		}
		final = append(final, e)
	}
	list = final
	if len(list) == 0 {
		return 0
	}
	for _, e := range list {
		_, _ = s.p2.DB.ExecContext(ctx, `UPDATE capabilities SET status = 'done', updated_at = now() WHERE id = $1`, e.id)
		label := strings.TrimSpace(firstNonEmptyStr(e.can, e.surface, "该方向"))
		text := fmt.Sprintf("预算到点：停止「%s」（continue 的 15 分钟已用尽）。把 worker 换到未测资产或另一层；需要继续就再拿新证据重新裁决。", clipLine(label, 60))
		if tsx != nil {
			_, _ = tsx.AddHintOnce(ctx, text)
		}
		for _, n := range intents {
			if n.State != "running" && n.State != "open" {
				continue
			}
			var p map[string]any
			if json.Unmarshal(n.Payload, &p) != nil {
				continue
			}
			sum, _ := p["summary"].(string)
			if matchesBudgetTarget(sum, e.can, e.surface) {
				_ = s.engine.KillWork(n.ID)
			}
		}
	}
	return len(list)
}

func matchesIntentSummary(n *db.Node, can, surface string) bool {
	var p map[string]any
	if json.Unmarshal(n.Payload, &p) != nil {
		return false
	}
	sum, _ := p["summary"].(string)
	return matchesBudgetTarget(sum, can, surface)
}

func matchesBudgetTarget(summary, can, surface string) bool {
	sum := strings.ToLower(strings.TrimSpace(summary))
	if sum == "" {
		return false
	}
	for _, kw := range []string{surface, can} {
		k := strings.ToLower(strings.TrimSpace(kw))
		if k != "" && strings.Contains(sum, k) {
			return true
		}
	}
	return false
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// budgetExpired 判定一个 continue 预算是否到点：分钟或轮数任一用尽即到期。
func budgetExpired(now, startedAt time.Time, budget time.Duration, roundsUsed, budgetRounds int) bool {
	if startedAt.IsZero() {
		return false
	}
	if budget > 0 && !startedAt.Add(budget).After(now) {
		return true
	}
	return budgetRounds > 0 && roundsUsed >= budgetRounds
}

// countRounds 统计 start 之后、与卡片方向匹配的意图数（近似"轮数"）。
func countRounds(intents []*db.Node, startedAt time.Time, can, surface string) int {
	n := 0
	for _, it := range intents {
		if !it.CreatedAt.After(startedAt) {
			continue
		}
		if matchesIntentSummary(it, can, surface) {
			n++
		}
	}
	return n
}
