package server

import (
	"context"
	"net/http"
	"strconv"
)

// 未读通知（⑦）：任务列表上的红点 + 浏览器标题数字。
//
// 判据只用确定性数据，不做第二套统计：
//   - 战果未读 = 该任务里 id 大于 loot_seen.seen_finding_id 的 finding 条数（= 新拿到的证据）；
//   - 审查官未读 = review_runs 里 status=done 且 seen_at 为空（已有字段，P0 之前就在用）。
//
// 为什么不做成"每个任务调一次 /brief"：列表页 N 个任务就是 N 次战果全量计算，
// 首屏会变慢。这里一条聚合 SQL 解决，代价可预期。

type unreadView struct {
	Total  int            `json:"total"`
	ByTask map[string]int `json:"by_task"`
}

func (s *Server) registerUnreadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/unread", s.tasksUnread)
}

func (s *Server) tasksUnread(w http.ResponseWriter, r *http.Request) {
	out := unreadView{ByTask: map[string]int{}}
	if s.m == nil || s.m.pg == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	byTask := s.unreadByTask(r.Context())
	out.ByTask = byTask
	for _, n := range byTask {
		out.Total += n
	}
	writeJSON(w, http.StatusOK, out)
}

// unreadByTask 一次算完所有任务的未读数（战果新证据 + 审查官未读）。
func (s *Server) unreadByTask(ctx context.Context) map[string]int {
	out := map[string]int{}
	add := func(id int64, n int) {
		if id <= 0 || n <= 0 {
			return
		}
		key := strconv.FormatInt(id, 10)
		out[key] += n
	}

	// 战果：findings 的 id 大于"上次看过"的游标
	if rows, err := s.m.pg.QueryContext(ctx, `
		SELECT t.id, count(n.id)
		FROM tasks t
		JOIN exploration_nodes n ON n.exploration_id = t.exploration_id AND n.kind='finding'
		LEFT JOIN loot_seen ls ON ls.task_id = t.id
		WHERE n.id > COALESCE(ls.seen_finding_id, 0)
		GROUP BY t.id`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			var n int
			if rows.Scan(&id, &n) == nil {
				add(id, n)
			}
		}
	}

	// 审查官结论：done 且没看过
	if rows, err := s.m.pg.QueryContext(ctx, `
		SELECT task_id, count(*) FROM review_runs
		WHERE status='done' AND seen_at IS NULL GROUP BY task_id`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			var n int
			if rows.Scan(&id, &n) == nil {
				add(id, n)
			}
		}
	}
	return out
}
