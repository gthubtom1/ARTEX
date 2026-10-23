package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"

	actool "github.com/Autumn-27/norma/tool"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/internal/oast"
)

// OAST（带外通道）接线。
//
// 它解决的是"看不见的漏洞"：盲 SSRF、盲命令注入、盲 XXE、Log4j 之类，目标不会回显
// 任何东西给你。唯一的判据是"目标有没有真的去访问你给的地址" —— 收到了就是漏洞成立，
// 还带着源 IP 和时间当证据。
//
// 为什么要"回填地址"：监听地址（0.0.0.0:8888）是给自己听的，目标访问不到。必须填一个
// **外部能访问到**的基地址（VPS 的公网 IP/域名）。本机 Kali 是 NAT，填了也收不到外网回调，
// 所以留空即可：留空 = 整条链不启动，零开销、零流量、不影响任何其它功能。
//
// 配置存在 settings（DB）里，两个键：
//   oast.http_addr   监听地址，例 ":8888"（空=不起 HTTP 通道）
//   oast.public_base 外部可达基址，例 "http://1.2.3.4:8888"（空=只能用本机自测地址）
// 改完 PUT /api/oast 会**热重启**通道，不用重启进程。

// oastMu 保护 oastSrv 的启停（PUT 时会 Close 旧的再起新的）。
var oastMu sync.Mutex

type oastConfig struct {
	HTTPAddr   string `json:"http_addr"`
	PublicBase string `json:"public_base"`
}

func (s *Server) oastConfig() oastConfig {
	if s.settings == nil {
		return oastConfig{}
	}
	return oastConfig{
		HTTPAddr:   strings.TrimSpace(s.settings.String("oast.http_addr", "")),
		PublicBase: strings.TrimSpace(s.settings.String("oast.public_base", "")),
	}
}

// startOAST 按当前设置启动（或重启）带外通道。空配置 = 不启动。
func (s *Server) startOAST() {
	cfg := s.oastConfig()

	oastMu.Lock()
	defer oastMu.Unlock()
	// 先停旧的（热重启用）
	if s.oastSrv != nil {
		_ = s.oastSrv.Close()
		s.oastSrv = nil
	}
	if cfg.HTTPAddr == "" {
		log.Printf("[oast] 未配置 oast.http_addr —— 带外通道关闭（盲 SSRF/盲 RCE 无法验证）")
		return
	}
	srv := oast.New(oast.Options{HTTPAddr: cfg.HTTPAddr, PublicBase: cfg.PublicBase})
	if err := srv.Start(); err != nil {
		log.Printf("[oast] 启动失败（%s）：%v", cfg.HTTPAddr, err)
		return
	}
	s.oastSrv = srv
	if cfg.PublicBase == "" {
		log.Printf("[oast] 通道已起在 %s，但**没有填 oast.public_base** —— 生成的地址只在本机可达。"+
			"要让目标机回连，必须填外部能访问到的地址（VPS 公网 IP/域名）", srv.HTTPAddr())
	} else {
		log.Printf("[oast] 通道已起在 %s，外部基址 %s（目标回连会记录到「系统 → 带外通道」）", srv.HTTPAddr(), cfg.PublicBase)
	}
}

// getOAST GET /api/oast —— 状态 + 全部回调记录
func (s *Server) getOAST(w http.ResponseWriter, r *http.Request) {
	cfg := s.oastConfig()
	oastMu.Lock()
	srv := s.oastSrv
	oastMu.Unlock()

	out := map[string]any{
		"http_addr":   cfg.HTTPAddr,
		"public_base": cfg.PublicBase,
		"enabled":     srv != nil,
		"hits":        []any{},
	}
	if srv != nil {
		out["listen_addr"] = srv.HTTPAddr()
		if hits := srv.AllHits(); hits != nil {
			out["hits"] = hits
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// updateOAST PUT /api/oast body: {"http_addr":":8888","public_base":"http://1.2.3.4:8888"}
func (s *Server) updateOAST(w http.ResponseWriter, r *http.Request) {
	var body oastConfig
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体不是合法 JSON"})
		return
	}
	body.HTTPAddr = strings.TrimSpace(body.HTTPAddr)
	body.PublicBase = strings.TrimRight(strings.TrimSpace(body.PublicBase), "/")
	if body.PublicBase != "" && !strings.HasPrefix(body.PublicBase, "http://") && !strings.HasPrefix(body.PublicBase, "https://") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "public_base 要以 http:// 或 https:// 开头"})
		return
	}
	if s.settings == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "settings 未初始化"})
		return
	}
	if err := s.settings.Set(r.Context(), "oast.http_addr", body.HTTPAddr); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if err := s.settings.Set(r.Context(), "oast.public_base", body.PublicBase); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.startOAST() // 热重启
	oastMu.Lock()
	up := s.oastSrv != nil
	oastMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": up})
}

// oastTools 返回带外工具（通道未启用时 nil，不占 worker 的 token 预算）。
func (s *Server) oastTools() []actool.CoreTool {
	oastMu.Lock()
	srv := s.oastSrv
	oastMu.Unlock()
	if srv == nil {
		return nil
	}
	return agent.NewOASTTools(srv)
}

// testOAST POST /api/oast/test —— 生成一个探测地址，方便先在本机自测通不通
func (s *Server) testOAST(w http.ResponseWriter, r *http.Request) {
	oastMu.Lock()
	srv := s.oastSrv
	oastMu.Unlock()
	if srv == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "带外通道未启动：先在下方填监听地址（如 :8888）并保存",
		})
		return
	}
	tok := srv.NewToken()
	writeJSON(w, http.StatusOK, tok)
}
