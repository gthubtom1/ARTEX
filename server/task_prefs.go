package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

// 任务级偏好（0018）：用户在页面上选的业务目标 + token 上限。
//
// 为什么单独做接口（docs/PLANS/2026-10-25-artex-产品审查.md P1-06 / P1-02）：
//
//	① 战况卡上的「我的目标」原来只是浏览器内存里的 state，刷新就回到默认值 ——
//	   用户会以为自己选丢了，然后又问一遍"我该干什么"；
//	② 用户明确要求"不要烧我太多 token"，但整套闸门只有时间（30/60/120 分钟），
//	   没有"花到多少就停"。
//
// 纪律：这两个值**只由人改**。任何自动化流程都不写它们（不自动调高预算、不自动换目标）。
func (s *Server) registerTaskPrefsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/tasks/{id}/prefs", s.taskPrefs)
}

func (s *Server) taskPrefs(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	var req struct {
		GoalSelected *string `json:"goal_selected"`
		TokenBudget  *int64  `json:"token_budget"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.GoalSelected == nil && req.TokenBudget == nil {
		writeErr(w, http.StatusBadRequest, "至少提供 goal_selected 或 token_budget")
		return
	}
	goal := t.GoalSelected
	if req.GoalSelected != nil {
		g := strings.TrimSpace(*req.GoalSelected)
		if g != "" {
			if _, ok := goalDefByKey(g); !ok {
				writeErr(w, http.StatusBadRequest, "goal_selected 不是已知目标（用 /api/tasks/{id}/fit 里的 key）")
				return
			}
		}
		goal = g
	}
	budget := t.TokenBudget
	if req.TokenBudget != nil {
		if *req.TokenBudget < 0 {
			writeErr(w, http.StatusBadRequest, "token_budget 不能为负（0 = 不限）")
			return
		}
		budget = *req.TokenBudget
	}
	taskID, err := strconv.ParseInt(t.ID, 10, 64)
	if err != nil || taskID <= 0 {
		writeErr(w, http.StatusBadRequest, "bad task id")
		return
	}
	if _, err := s.m.pg.Exec(`UPDATE tasks SET goal_selected=$2, token_budget=$3 WHERE id=$1`,
		taskID, goal, budget); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	// 内存里的任务同步一份，避免前端立刻读到旧值（列表页/战况卡都读它）。
	t.GoalSelected = goal
	t.TokenBudget = budget
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "goal_selected": goal, "token_budget": budget})
}

// taskSpent 汇总该任务已花多少 token（input/output/缓存读）。
// 读不到（没库/表不存在）就返回 0，不阻塞战况卡。
func (s *Server) taskSpent(ctx context.Context, taskID string) (in, out, cached int64) {
	if s == nil || s.m == nil || s.m.pg == nil || taskID == "" {
		return 0, 0, 0
	}
	rows, err := s.m.pg.TokenByModel(taskID)
	if err != nil {
		return 0, 0, 0
	}
	for _, r := range rows {
		in += int64(r.InputTokens)
		out += int64(r.OutputTokens)
		cached += int64(r.CacheReadTokens)
	}
	return in, out, cached
}
