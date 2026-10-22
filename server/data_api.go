package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Autumn-27/artex/internal/dataloc"
)

// 数据定位面板：把已拿到的数据库输出（information_schema / COUNT(*) / 表列表）
// 解析成「表 / 行数 / 敏感列 / 证明级别」，只做展示，不执行任何 SQL。
func (s *Server) registerDataRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/data/locate", s.dataLocate)
	mux.HandleFunc("GET /api/data/probe", s.dataProbe)
}

func (s *Server) dataLocate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Engine string `json:"engine"`
		Output string `json:"output"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体不是合法 JSON"})
		return
	}
	engine := strings.ToLower(strings.TrimSpace(body.Engine))
	var res dataloc.Result
	switch engine {
	case "postgres", "postgresql", "psql":
		res = dataloc.ParsePostgres(body.Output)
	case "mssql", "sqlserver":
		res = dataloc.ParseMSSQL(body.Output)
	default:
		res = dataloc.ParseMySQL(body.Output)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"engine": engine, "result": res,
		"note":  "只记录结构与规模，不导出数据；样本需 ≤1 行并打码。",
		"probe": dataloc.PlanProbe(engine),
	})
}

func (s *Server) dataProbe(w http.ResponseWriter, r *http.Request) {
	engine := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("engine")))
	writeJSON(w, http.StatusOK, map[string]any{"engine": engine, "probe": dataloc.PlanProbe(engine)})
}
