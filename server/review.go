package server

import "net/http"

// 复盘官（CARD-C01 实现）。
//
// 本文件由 CARD-000 建立骨架：先只挂路由并返回 501，
// 真正的快照/评审/采纳逻辑在 CARD-C01 里填进同一文件的 handler 体内。
//
// 规格：docs/09-详设-C01-复盘官.md
func (s *Server) registerReviewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/tasks/{id}/review", s.reviewRun)
	mux.HandleFunc("GET /api/tasks/{id}/review/latest", s.reviewLatest)
	mux.HandleFunc("GET /api/tasks/{id}/review/runs", s.reviewListRuns)
	mux.HandleFunc("POST /api/tasks/{id}/review/{run}/apply", s.reviewApply)
	mux.HandleFunc("POST /api/tasks/{id}/review/{run}/rebut", s.reviewRebut)
	mux.HandleFunc("GET /api/tasks/{id}/review/notes", s.reviewNotes)
}

// reviewRun 立即跑一次评审（手动/条件/定时共用入口）。
func (s *Server) reviewRun(w http.ResponseWriter, _ *http.Request) { notImplemented(w) }

// reviewLatest 返回最近一次结论（含 notes 版本）。
func (s *Server) reviewLatest(w http.ResponseWriter, _ *http.Request) { notImplemented(w) }

// reviewListRuns 返回历史介入列表。
func (s *Server) reviewListRuns(w http.ResponseWriter, _ *http.Request) { notImplemented(w) }

// reviewApply 采纳部分/全部建议动作（只允许 kill_intent / retry_intent / set_next_stage）。
func (s *Server) reviewApply(w http.ResponseWriter, _ *http.Request) { notImplemented(w) }

// reviewRebut 反驳：它必须重新评审并写明「旧结论 + 推翻证据」。
func (s *Server) reviewRebut(w http.ResponseWriter, _ *http.Request) { notImplemented(w) }

// reviewNotes 返回当前评审笔记。
func (s *Server) reviewNotes(w http.ResponseWriter, _ *http.Request) { notImplemented(w) }
