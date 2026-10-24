package server

import (
	"context"
	"log"
	"strconv"
	"time"
)

// 投入产出闸门（docs/23 §五 第 2 道闸门，docs/25 §八 闸门2）。
//
// 要解决的问题（用户原话）："如果这个站根本打不进去，它跑十几个小时，那我就很浪费 token。"
// 反向风险也一样真实："别到时候明明有漏洞，它给放弃了。"
//
// 所以判据只用**确定性信号**：这个任务"上次有新证据"是多久以前。
// 不看模型自述、不看它说"还在努力"：
//   >= 30 分钟无新证据 → warn：必须在会话里说清"继续 / 换路线 / 收工"，并给理由
//   >= 60 分钟无新证据 → stop：建议收工，UI 直接显示「建议出报告并停」
//   >= 120 分钟无新证据 且 60 分钟内用户没说话 → 自动暂停（可一句话恢复）
//
// 为什么要"人 60 分钟没说话"这个附加条件：用户正在盯着看、刚给过新指令时，
// 自动暂停会打断他。只有"既没进展、也没人管"才替他省钱。

const (
	stallWarnAfter   = 30 * time.Minute
	stallStopAfter   = 60 * time.Minute
	stallPauseAfter  = 120 * time.Minute
	stallQuietWindow = 60 * time.Minute
)

// stallState 是给 /brief 用的一份"进度健康度"。
type stallState struct {
	Level   string `json:"level"`   // ok|warn|stop
	Minutes int    `json:"minutes"` // 距上次新证据多少分钟（-1 = 不知道）
	Advice  string `json:"advice"`  // 一句话：现在该干什么
}

// stallDecision 是纯函数：给定"上次进展时间 / 上次人说话时间 / 现在"，给结论。
// 全确定性，可单测；不看任何模型输出。
func stallDecision(lastProgress, lastHuman, now time.Time) stallState {
	if lastProgress.IsZero() {
		return stallState{Level: "ok", Minutes: -1, Advice: "还没有证据可判断"}
	}
	idle := now.Sub(lastProgress)
	mins := int(idle.Minutes())
	switch {
	case idle >= stallStopAfter:
		return stallState{Level: "stop", Minutes: mins,
			Advice: "已经 " + strconv.Itoa(mins) + " 分钟没有新证据：建议换成当前结论出报告并停，别继续烧"}
	case idle >= stallWarnAfter:
		return stallState{Level: "warn", Minutes: mins,
			Advice: "已经 " + strconv.Itoa(mins) + " 分钟没有新证据：让它在会话里给一句「继续 / 换路线 / 收工」和理由"}
	default:
		return stallState{Level: "ok", Minutes: mins, Advice: ""}
	}
}

// shouldAutoPause：闸门 3 的自动收工判据（还没进展 + 人也没管）。
func shouldAutoPause(lastProgress, lastHuman, now time.Time) bool {
	if lastProgress.IsZero() {
		return false
	}
	if now.Sub(lastProgress) < stallPauseAfter {
		return false
	}
	if !lastHuman.IsZero() && now.Sub(lastHuman) < stallQuietWindow {
		return false
	}
	return true
}

// taskLastProgress：这个任务"上次有新证据/新事实"的时间（没有就零值）。
func (s *Server) taskLastProgress(ctx context.Context, taskID int64) time.Time {
	var t time.Time
	if s.m == nil || s.m.pg == nil || taskID <= 0 {
		return t
	}
	_ = s.m.pg.QueryRowContext(ctx, `SELECT COALESCE(max(n.created_at), to_timestamp(0))
		FROM exploration_nodes n JOIN tasks t ON t.exploration_id=n.exploration_id
		WHERE t.id=$1 AND n.kind IN ('finding','fact','capability')`, taskID).Scan(&t)
	return t
}

// taskLastHuman：这个人上次在这个任务里说话的时间（会话消息）。
func (s *Server) taskLastHuman(ctx context.Context, taskID int64) time.Time {
	var t time.Time
	if s.m == nil || s.m.pg == nil || taskID <= 0 {
		return t
	}
	_ = s.m.pg.QueryRowContext(ctx, `SELECT COALESCE(max(created_at), to_timestamp(0))
		FROM conversation_messages m JOIN conversations c ON c.id=m.conversation_id
		WHERE c.task_id=$1 AND m.role='human'`, taskID).Scan(&t)
	return t
}

// startStallGate 起一个轻量巡检：每 5 分钟看一遍在跑的任务，该暂停的暂停。
// 只在置位 settings 标记前跑一次，避免重复暂停；用户 resume 后可再次触发。
func (s *Server) startStallGate(ctx context.Context) {
	if s.m == nil || s.m.pg == nil {
		return
	}
	go func() {
		tk := time.NewTicker(5 * time.Minute)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				s.runStallGate(ctx)
			}
		}
	}()
}

func (s *Server) runStallGate(ctx context.Context) {
	tasks := s.m.List()
	for _, t := range tasks {
		if t == nil || t.Paused {
			continue
		}
		if !taskIsRunning(t.Status) {
			continue
		}
		taskID, err := strconv.ParseInt(t.ID, 10, 64)
		if err != nil || taskID <= 0 {
			continue
		}
		lastP := s.taskLastProgress(ctx, taskID)
		lastH := s.taskLastHuman(ctx, taskID)
		if !shouldAutoPause(lastP, lastH, time.Now()) {
			continue
		}
		// 只暂停一次：用 settings 标记去重（resume 后由人决定，不再自动停第二次）
		key := "stall_paused_" + t.ID
		if v, _, _ := s.m.pg.GetSetting(key); v == "done" {
			continue
		}
		if _, err := s.applyTaskControl(t, "pause"); err != nil {
			log.Printf("[stall] auto-pause task %s failed: %v", t.ID, err)
			continue
		}
		_ = s.m.pg.SetSetting(key, "done")
		s.appendReviewActivity(ctx, taskID,
			"已经 2 小时没有新证据、也没有人给新指令，我先把任务暂停了（省钱）。"+
				"要继续就在会话里说一句「继续」，或者直接说「换路线：…」。")
		log.Printf("[stall] auto-paused task %s (no progress %v, last human %v)", t.ID, lastP, lastH)
	}
}

// taskIsRunning：只有真正在跑的才受闸门管（终态与排队不碰）。
func taskIsRunning(status string) bool {
	switch status {
	case "running", "exploring":
		return true
	}
	return false
}
