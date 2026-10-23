package server

import (
	"context"
	"log"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/internal/errev"
)

// installErrevSink 把「任务级错误事件」的落库通道接到 errev.Sink。
//
// 为什么用包级注入而不是让每个调用方自己写库：错误产生点分散在 engine / guard /
// recheck / adapter 四处，各自手里只有 ctx；统一到一个 Sink，调用方只需一行
// errev.Emit(...)，写库细节（列名、超时、失败降级）只在这里维护。
//
// 未注入时 errev.Emit 是空操作（测试与无库场景安全）。
func (s *Server) installErrevSink() {
	if s.p2 == nil || s.p2.DB == nil {
		errev.Sink = nil
		errev.IntentIDOf = nil
		return
	}
	// 意图归属：worker 的 run 在 ctx 里挂了 RunInfo（agent/worker.go），
	// 这里只读 IntentID；planner/mainagent/chat 跑没有意图 → 0（归属未知）。
	errev.IntentIDOf = func(ctx context.Context) int64 {
		if id := agent.RunInfoFrom(ctx).IntentID; id > 0 {
			return id
		}
		return 0
	}
	db := s.p2.DB
	errev.Sink = func(ctx context.Context, taskID, intentID int64, class, tool, detail string) {
		if taskID < 0 {
			taskID = 0
		}
		if intentID < 0 {
			intentID = 0
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO task_error_events (task_id, intent_id, class, tool, detail) VALUES ($1, $2, $3, $4, $5)`,
			taskID, intentID, class, tool, detail,
		); err != nil {
			// 审计类写入失败不能影响主流程，只打日志。
			log.Printf("[errev] 写入任务错误事件失败(task=%d intent=%d class=%s): %v", taskID, intentID, class, err)
		}
	}
}
