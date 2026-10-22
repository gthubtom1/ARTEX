package server

import (
	"context"
	"log"

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
		return
	}
	db := s.p2.DB
	errev.Sink = func(ctx context.Context, taskID int64, class, tool, detail string) {
		if taskID < 0 {
			taskID = 0
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO task_error_events (task_id, class, tool, detail) VALUES ($1, $2, $3, $4)`,
			taskID, class, tool, detail,
		); err != nil {
			// 审计类写入失败不能影响主流程，只打日志。
			log.Printf("[errev] 写入任务错误事件失败(task=%d class=%s): %v", taskID, class, err)
		}
	}
}
