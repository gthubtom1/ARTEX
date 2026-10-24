package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/guard"
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
	mux.HandleFunc("POST /api/environment/rescan", s.rescanEnvironment)
	mux.HandleFunc("GET /api/network", s.getNetwork)
	mux.HandleFunc("GET /api/knowledge", s.getKnowledge)
}

// rescanEnvironment 丢掉工具缓存并重新扫描一次（后台），让刚 apt 装完的工具立刻可见。
//
// 为什么要有它：原来缓存 TTL 7 天且只在启动时扫一次，装完工具环境页还是“缺 X”，
// 用户只能重启服务——对“开箱即用”来说是反向体验。
func (s *Server) rescanEnvironment(w http.ResponseWriter, r *http.Request) {
	if s.envDisc == nil {
		writeErr(w, http.StatusServiceUnavailable, "工具发现未初始化")
		return
	}
	if err := s.envDisc.Invalidate(); err != nil {
		writeErr(w, http.StatusInternalServerError, "清理缓存失败："+err.Error())
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		tools, err := s.envDisc.Load(ctx)
		if err != nil {
			log.Printf("[env] rescan failed: %v", err)
			return
		}
		s.tools.Store(tools)
		missing := s.envDisc.Missing(tools)
		log.Printf("[env] rescan: %d tools, missing=%v", len(tools), missing)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok": true, "detail": "已在后台重新扫描，稍后刷新本页（约 5-30 秒）",
	})
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
	var missingTools []env.MissingTool
	var installHint string
	if v := s.tools.Load(); v != nil {
		if tools, ok := v.([]env.Tool); ok {
			for _, t := range tools {
				out = append(out, toolView{Name: t.Name, Path: t.Path, Kind: t.Kind, Version: t.Version,
					Healthy: t.Healthy, Caps: t.Caps, Source: t.ScanSource})
			}
			if s.envDisc != nil {
				missing = s.envDisc.Missing(tools)
				// “缺什么 + 怎么装”一并给前端：错误卡只说“去环境页装”，
				// 但页面没给命令，人还得自己查包名（2026-10-24 实测反馈）。
				missingTools = env.DescribeMissing(missing)
				installHint = env.InstallHintBatch(missing)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	// 「任务真的缺」与「环境体检建议」必须分开。
	//
	// 2026-10-24 实测反馈：任务里只缺 conntrack，可环境页把整份 keyTools 里缺的都列出来
	// （chisel/sshpass/go），人一看以为要全装——而 Go 只是构建工具，任务永远用不到。
	// 判据应该是：
	//   ① 任务里真的调用过、且现在仍然没有的 → 高优先级，带安装命令（甚至可自动装）；
	//   ② 环境清单里缺、但没有任何任务用过的 → 弱提示，装不装随意。
	var taskMissing []env.MissingTool
	var taskMissingHint string
	if s.p2 != nil && s.p2.DB != nil {
		rows, err := s.p2.DB.QueryContext(r.Context(), `
			SELECT DISTINCT tool FROM task_error_events
			WHERE class = 'tool_missing' AND ts > now() - interval '24 hours'`)
		if err == nil {
			defer rows.Close()
			var names []string
			for rows.Next() {
				var n string
				if rows.Scan(&n) == nil {
					names = append(names, n)
				}
			}
			// 已经装上 / 本来就是误报（shell 探测词）的不再催人装。
			var still []string
			for _, n := range names {
				if guard.IsShellProbe(n) {
					continue
				}
				if _, err := exec.LookPath(strings.TrimSpace(n)); err == nil {
					continue
				}
				still = append(still, n)
			}
			taskMissing = env.DescribeMissing(still)
			taskMissingHint = env.InstallHintBatch(still)
		}
	}

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
		"tools":          out,
		"count":          len(out),
		"missing":        missing,
		"missing_detail": missingTools,
		"install_hint":   installHint,
		// task_missing = 任务里真的调用过、现在仍然没有的工具（高优先级）
		"task_missing":      taskMissing,
		"task_missing_hint": taskMissingHint,
		"settings":          settings,
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
