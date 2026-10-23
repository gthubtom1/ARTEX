package server

import "net/http"

// 「能做到什么」地图（CARD-B01 实现）。
//
// 本文件由 CARD-000 建立骨架：先只挂路由并返回 501，
// 真正的聚合/细化逻辑在 CARD-B01 里填进同一文件的 handler 体内。
//
// 规格：docs/08-详设-B01-能做到什么.md
func (s *Server) registerCandoRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/cando", s.candoGet)
	mux.HandleFunc("POST /api/tasks/{id}/cando/annotate", s.candoAnnotate)
	mux.HandleFunc("GET /api/tasks/{id}/cando/annotate", s.candoAnnotateStatus)
}

func notImplemented(w http.ResponseWriter) {
	writeErr(w, http.StatusNotImplemented, "not implemented")
}

// candoGet 返回当前任务的战果图（lanes + loot + banner + annotations）。
func (s *Server) candoGet(w http.ResponseWriter, _ *http.Request) { notImplemented(w) }

// candoAnnotate 提交一次按需 AI 细化（后台跑，命中缓存直接回）。
func (s *Server) candoAnnotate(w http.ResponseWriter, _ *http.Request) { notImplemented(w) }

// candoAnnotateStatus 查询某个节点的细化状态。
func (s *Server) candoAnnotateStatus(w http.ResponseWriter, _ *http.Request) { notImplemented(w) }
