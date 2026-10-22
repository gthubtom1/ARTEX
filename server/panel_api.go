package server

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/internal/env"
	artexkb "github.com/Autumn-27/artex/internal/kb"
	"github.com/Autumn-27/artex/internal/netguard"
)

// 本文件提供「环境 / 网络 / 知识」三个只读面板接口：
//   GET /api/environment  工具发现结果 + 关键运行参数
//   GET /api/network      出口自检状态 + 基准 IP + 最近变更记录（不主动发探测）
//   GET /api/knowledge?q= 知识库文件名/正文搜索（只读、限条数）

func (s *Server) registerPanelRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/environment", s.getEnvironment)
	mux.HandleFunc("GET /api/network", s.getNetwork)
	mux.HandleFunc("GET /api/knowledge", s.getKnowledge)
}

func (s *Server) getEnvironment(w http.ResponseWriter, r *http.Request) {
	type toolView struct {
		Name    string   `json:"name"`
		Path    string   `json:"path"`
		Kind    string   `json:"kind"`
		Version string   `json:"version"`
		Healthy bool     `json:"healthy"`
		Caps    []string `json:"caps,omitempty"`
		Source  string   `json:"source,omitempty"`
	}
	out := []toolView{}
	var missing []string
	if v := s.tools.Load(); v != nil {
		if tools, ok := v.([]env.Tool); ok {
			for _, t := range tools {
				out = append(out, toolView{Name: t.Name, Path: t.Path, Kind: t.Kind, Version: t.Version,
					Healthy: t.Healthy, Caps: t.Caps, Source: t.ScanSource})
			}
			if s.envDisc != nil {
				missing = s.envDisc.Missing(tools)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	settings := map[string]string{}
	if s.settings != nil {
		// 只展示**真的有消费者**的键（2026-10-23 全量审查：tool.*、work.max_turns、
		// budget.*、kb.search_limit 共 10 个键在环境页被当"生效参数"展示，实际全仓无人读，
		// 已从 Defaults 删除。展示假旋钮比不展示更糟 —— 人会以为改了有用。）
		for _, k := range []string{"asset.recheck_hours", "asset.stale_days", "asset.recheck_minutes"} {
			settings[k] = s.settings.String(k, "")
		}
		// workers 的真值在 Manager 内存里（settings 缓存不会随 SetWorkers 回刷），
		// 读缓存会永远显示旧值 —— D2 审查发现。
		if s.m != nil {
			settings["workers"] = strconv.Itoa(s.m.Workers())
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tools":    out,
		"count":    len(out),
		"missing":  missing,
		"settings": settings,
	})
}

func (s *Server) getNetwork(w http.ResponseWriter, r *http.Request) {
	// D1 之后网络面板只剩「出口自检」：代理/Clash/节点参数全部删除，
	// 唯一需要回显的是基准 IP —— 出口变了就是靠它看出来的。
	settings := map[string]string{}
	if s.settings != nil {
		for _, k := range []string{netguard.BaselineSettingKey, "net.verify_urls", "net.verify_every_s"} {
			settings[k] = s.settings.String(k, "")
		}
	}
	resp := map[string]any{
		"settings": settings,
		"current":  map[string]any{},
		"egress":   []any{},
		"events":   []any{},
	}
	if s.netguard != nil {
		last := s.netguard.Last()
		resp["current"] = map[string]any{
			"exit_ip":     last.EgressIP,
			"baseline_ip": last.BaselineIP,
			"ok":          last.OK,
			"detail":      last.Detail,
			"at":          last.At.Format("2006-01-02 15:04:05"),
		}
	}
	pg := s.m.PG()
	if pg == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	rows, err := pg.QueryContext(r.Context(),
		`SELECT ts, COALESCE(task_id,0), egress_ip, ok, detail FROM egress_log ORDER BY id DESC LIMIT 50`)
	if err == nil {
		var out []map[string]any
		for rows.Next() {
			var ts any
			var taskID int64
			var ip, detail string
			var ok bool
			if rows.Scan(&ts, &taskID, &ip, &ok, &detail) == nil {
				out = append(out, map[string]any{"ts": tsString(ts), "task_id": taskID, "egress_ip": ip,
					"ok": ok, "detail": detail})
			}
		}
		rows.Close()
		resp["egress"] = out
	}
	rows2, err := pg.QueryContext(r.Context(),
		`SELECT ts, COALESCE(task_id,0), kind, detail FROM net_events ORDER BY id DESC LIMIT 50`)
	if err == nil {
		var out []map[string]any
		for rows2.Next() {
			var ts any
			var taskID int64
			var kind, detail string
			if rows2.Scan(&ts, &taskID, &kind, &detail) == nil {
				out = append(out, map[string]any{"ts": tsString(ts), "task_id": taskID, "kind": kind, "detail": detail})
			}
		}
		rows2.Close()
		resp["events"] = out
	}
	writeJSON(w, http.StatusOK, resp)
}

// tsString 把驱动返回的时间值转成字符串（pgx 可能给 time.Time / []byte / string）。
func tsString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case time.Time:
		return x.Format("2006-01-02 15:04:05")
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func (s *Server) getKnowledge(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	hits, err := artexkb.Search(s.kbRoot(), q, artexkb.Options{Limit: 20})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": q, "root": s.kbRoot(), "count": len(hits), "hits": hits})
}
