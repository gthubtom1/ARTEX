package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// P8-4：一条链入口 —— 贴一个网址就开跑。
//
// 零基础用户当前要过六道闸（PostgreSQL / 密码 / 模型 / 代理 / 描述 / goal），
// 其中"自己写 goal"和"代理没起导致任务一建就 paused"是最容易流失的两步。
// 这个接口把六步压成一步：给 URL + 选档位 → 出口自检 → 自动生成 goal → 直接开跑。
//
// 设计取舍：
//   - **出口不通不报错**：用户刚点了"开始"，此时甩红字等于劝退。返回 warnings，任务照建，
//     前端显示⚠️并给三个一键选项（起 Clash / 改代理 / 只打靶场）。
//   - **不复制 createTask 的校验**：quickstart 的入参是受控的（无 schedule / 无来源任务 /
//     无企业 / 无拦截规则），直接调 manager 的底层方法，避免两条建任务路径慢慢漂移。

// quickstartTier 是目标档位。
type quickstartTier string

const (
	tierLab    quickstartTier = "lab"    // 本地靶场：允许直连
	tierLegacy quickstartTier = "legacy" // 有已知漏洞族的老站点
	tierPublic quickstartTier = "public" // 公网站点：只做低风险只读面
)

func parseTier(s string) (quickstartTier, error) {
	switch quickstartTier(strings.ToLower(strings.TrimSpace(s))) {
	case tierLab, "":
		return tierLab, nil
	case tierLegacy:
		return tierLegacy, nil
	case tierPublic:
		return tierPublic, nil
	}
	return "", fmt.Errorf("tier 只接受 lab|legacy|public")
}

// normalizeTarget 归一化用户贴进来的网址：
//   - 没有 scheme 时补 http://
//   - 只接受 http/https
//   - 返回 host（不含端口）与完整 URL
func normalizeTarget(raw string) (full, host string, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", fmt.Errorf("请先贴一个网址")
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, perr := url.Parse(s)
	if perr != nil {
		return "", "", fmt.Errorf("网址解析失败：%v", perr)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", fmt.Errorf("只接受 http/https 网址（收到 %s）", u.Scheme)
	}
	if u.Host == "" {
		return "", "", fmt.Errorf("网址缺少主机名")
	}
	if u.Path == "" {
		u.Path = "/"
	}
	// host 统一小写：DNS 不区分大小写，task_scope / netguard 比对时也不该区分。
	return u.String(), strings.ToLower(u.Hostname()), nil
}

// goalTemplate 按档位生成一句"零基础用户不用改"的 goal。
func goalTemplate(tier quickstartTier, full string, minutes int) string {
	switch tier {
	case tierLegacy:
		return fmt.Sprintf("目标 %s ，识别指纹与已知漏洞族后做最小验证，先出能力菜单再决定深入；只测这个地址，超时 %d 分钟。", full, minutes)
	case tierPublic:
		return fmt.Sprintf("目标 %s ，只做被动侦察与低风险验证（GET/HEAD、版本指纹、公开路径、默认页），输出能力菜单；只测这个域名，超时 %d 分钟。", full, minutes)
	default:
		return fmt.Sprintf("目标 %s ，先做只读侦察并输出能力菜单；只测这个地址，超时 %d 分钟。", full, minutes)
	}
}

// registerQuickstartRoutes 挂 P8-4 的一条链入口。
func (s *Server) registerQuickstartRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/tasks/quickstart", s.quickstartTask)
	mux.HandleFunc("GET /api/egress/check", s.egressCheck)
}

// egressCheck 供首页卡片做出口预检（60s 缓存，失败也缓存 30s）。
func (s *Server) egressCheck(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"ok": true, "detail": "", "proxy": s.m.ProxyAddr()}
	if s.netguard == nil {
		writeJSON(w, 200, out)
		return
	}
	st, err := s.netguard.VerifyCached(r.Context(), 60e9)
	out["exit_ip"] = st.EgressIP
	out["node"] = st.Node
	if err != nil {
		out["ok"] = false
		out["detail"] = err.Error()
	}
	writeJSON(w, 200, out)
}

func (s *Server) quickstartTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL          string `json:"url"`
		Tier         string `json:"tier"`
		TimeoutMin   int    `json:"timeout_min"`
		Workers      int    `json:"workers"`
		LLMProfileID *int64 `json:"llm_profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	full, host, err := normalizeTarget(req.URL)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	tier, err := parseTier(req.Tier)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	minutes := req.TimeoutMin
	if minutes <= 0 {
		minutes = 30
	}
	if minutes > 24*60 {
		minutes = 24 * 60
	}
	warnings := make([]string, 0, 2)

	// 1) 出口自检：不通只告警，不拦住建任务。
	egress := map[string]any{"ok": true, "detail": ""}
	allowDirect := tier == tierLab && isLoopbackHost(host)
	if s.netguard != nil && !allowDirect {
		if st, verr := s.netguard.VerifyCached(r.Context(), 60e9); verr != nil {
			egress["ok"] = false
			egress["detail"] = verr.Error()
			warnings = append(warnings, "出口未就绪（"+verr.Error()+"）：任务能建，但发不出包。请起代理或到「系统 → 网络」改代理；只打本地靶场改用「本地靶场」档位。")
		} else {
			egress["exit_ip"] = st.EgressIP
		}
	} else if allowDirect {
		egress["detail"] = "本地靶场档位：直连，不做出口校验"
		egress["ok"] = true
	}

	// 2) 组装任务
	goal := goalTemplate(tier, full, minutes)
	name := "快速任务 " + host
	desc := host + " 快速任务"
	var profileIDs []int64
	if req.LLMProfileID != nil && *req.LLMProfileID > 0 {
		if err := s.validateTaskProfileIDs([]int64{*req.LLMProfileID}); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		profileIDs = []int64{*req.LLMProfileID}
	}
	coverage := true
	t, err := s.m.CreateTaskWithOptions(desc, goal, db.TaskCreateOptions{
		Name:            name,
		LLMProfileIDs:   profileIDs,
		TimeoutSeconds:  minutes * 60,
		CoverageEnabled: &coverage,
	})
	if err != nil {
		writeErr(w, 500, "建任务失败："+err.Error())
		return
	}

	// 3) 把目标写进 task_scope（netguard / 资产拦截都读它）。失败只告警。
	if as := s.m.Assets(); as != nil {
		taskID, _ := parseInt64(t.ID)
		kind := "domain"
		if net.ParseIP(host) != nil {
			kind = "ip"
		}
		if _, serr := as.AddAgentScope(taskID, kind, host, "quickstart 自动登记", "manual"); serr != nil {
			log.Printf("[quickstart] 登记 task_scope 失败(忽略): %v", serr)
			warnings = append(warnings, "目标范围自动登记失败（"+serr.Error()+"），可以在任务页手动补：系统会按「未登记」处理。")
		}
	}

	// 4) 开跑：seed 首条意图，用户几秒内就能在活动流看到动静。
	s.launchTask(t, desc+" "+goal, true)
	log.Printf("[quickstart] 任务 #%s tier=%s target=%s", t.ID, tier, full)

	if s.credKeyFP == "" {
		warnings = append(warnings, "凭据库用的是临时密钥（未找到 data/cred.key）：重启后已存凭据将无法解密。")
	}
	writeJSON(w, 201, map[string]any{
		"task_id":  t.ID,
		"goal":     goal,
		"tier":     string(tier),
		"url":      full,
		"host":     host,
		"egress":   egress,
		"warnings": warnings,
		"redirect": "/function/capabilities?task=" + t.ID,
	})
}

// isLoopbackHost 判断是否本机/内网地址（本地靶场档位据此决定是否跳过出口校验）。
func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" || h == "::1" {
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}
