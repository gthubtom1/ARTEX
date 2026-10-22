package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
)

// 快照（写前留档）的只读列表 + 一键回滚。
//
// D1/D2 审查结论：internal/snapshot 的 Create 一直在用（每个写文件工具写前留档），
// 但 Restore 全仓零调用 —— 也就是说 docs/00 的 D02 决定里"用可回滚兜底"这半句
// 一直是空头承诺。这里把 Restore/List/Get 接出来，让它变成真能力。
//
// 纪律：回滚是**破坏性**动作（把快照里的旧内容写回原路径），所以：
//   - 只有 POST 且带 {"confirm": true} 才执行；
//   - 每条成功/失败都进日志（log.Printf），便于事后追。
// 快照缺失的文件（快照时文件不存在，Missing=true）不会被"回滚删除"，
// 因为 Store.Restore 的实现只写回存在的那些文件 —— 这是保守选择，不制造新删除。

// listSnapshots GET /api/snapshots?task_id=N&limit=100
func (s *Server) listSnapshots(w http.ResponseWriter, r *http.Request) {
	if s.snapshots == nil {
		writeJSON(w, http.StatusOK, map[string]any{"snapshots": []any{}})
		return
	}
	var taskID int64
	if v := r.URL.Query().Get("task_id"); v != "" {
		taskID, _ = strconv.ParseInt(v, 10, 64)
	}
	list, err := s.snapshots.List(taskID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if len(list) > limit {
		list = list[:limit]
	}
	// 大文件清单没必要整页传（一个快照可能几十个文件），列表只带文件数与前几个路径。
	out := make([]snapshotRow, 0, len(list))
	for _, sn := range list {
		paths := make([]string, 0, len(sn.Files))
		for i, f := range sn.Files {
			if i >= 5 {
				break
			}
			paths = append(paths, f.OrigPath)
		}
		out = append(out, snapshotRow{
			ID: sn.ID, TaskID: sn.TaskID, Reason: sn.Reason,
			CreatedAt: sn.CreatedAt, Files: len(sn.Files), Bytes: sn.Bytes,
			SamplePaths: paths,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": out})
}

type snapshotRow struct {
	ID          string   `json:"id"`
	TaskID      int64    `json:"task_id"`
	Reason      string   `json:"reason"`
	CreatedAt   any      `json:"created_at"`
	Files       int      `json:"files"`
	Bytes       int64    `json:"bytes"`
	SamplePaths []string `json:"sample_paths"`
}

// getSnapshot GET /api/snapshots/{id} —— 完整清单（含每个文件的 sha256/大小）
func (s *Server) getSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.snapshots == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "snapshot store 未初始化"})
		return
	}
	sn, err := s.snapshots.Get(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sn)
}

// restoreSnapshot POST /api/snapshots/{id}/restore  body: {"confirm": true}
func (s *Server) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.snapshots == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "snapshot store 未初始化"})
		return
	}
	id := r.PathValue("id")
	var body struct {
		Confirm bool `json:"confirm"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if !body.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "回滚会覆盖当前文件内容，需要 body 带 {\"confirm\": true} 才执行",
		})
		return
	}
	sn, err := s.snapshots.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	// 回滚本身也是"写文件"，所以先给当前状态再留一份档（失败也不阻断回滚）。
	paths := make([]string, 0, len(sn.Files))
	for _, f := range sn.Files {
		paths = append(paths, f.OrigPath)
	}
	if cur, cerr := s.snapshots.Create(sn.TaskID, "restore-before:"+id, paths); cerr == nil {
		log.Printf("[snapshot] 回滚前留档 %s", cur.ID)
	}
	if err := s.snapshots.Restore(id); err != nil {
		log.Printf("[snapshot] 回滚失败 %s: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	log.Printf("[snapshot] 已回滚 %s（task=%d, %d 个文件, reason=%s）", id, sn.TaskID, len(sn.Files), sn.Reason)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restored": len(sn.Files), "id": id})
}
