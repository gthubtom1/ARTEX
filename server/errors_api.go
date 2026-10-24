package server

import (
	"github.com/Autumn-27/artex/internal/env"
	"net/http"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/internal/errev"
)

// P8-8：任务级错误事件的可解释面板。
//
// 设计目标：零基础用户最常问的不是"这个洞怎么打"，而是"它为什么不动了"。
// 现在错误散在活动流/审计/工具输出里，这里把它们按固定 5 类聚合成一行，
// 每类带一句人话 + 一个"一键修"跳转目标。

// errAdvice 是一类错误的固定建议（不要从模型生成，要可预期）。
type errAdvice struct {
	Label  string `json:"label"`  // 人话类别名
	Action string `json:"action"` // 该怎么做
	Fix    string `json:"fix"`    // 前端跳转指令：open:/path 或空
}

// errAdvices 是 5 类错误的固定映射，顺序即展示顺序。
var errAdvices = []struct {
	Class  string
	Advice errAdvice
}{
	{errev.ClassEnvError, errAdvice{
		Label:  "环境（代理 / 网络 / DNS）",
		Action: "出口不通：起代理，或到「系统 → 网络」改代理；只打本地靶场可在启动时设 ARTEX_ALLOW_DIRECT=1",
		Fix:    "open:/system/network",
	}},
	{errev.ClassToolMissing, errAdvice{
		Label:  "缺工具",
		Action: "命令首词在本机不存在：到「系统 → 环境」安装，或让它用等价工具继续（平台已给降级建议）",
		Fix:    "open:/system/environment",
	}},
	{errev.ClassTargetBlocked, errAdvice{
		Label:  "目标拒绝（401 / 403 / 429 / 503 / WAF）",
		Action: "目标侧有拦截：先只做指纹与只读面，能力菜单里换一条机理不同的路线",
		Fix:    "open:/function/capabilities",
	}},
	{errev.ClassSelfError, errAdvice{
		Label:  "平台自身出错",
		Action: "模型调用失败或解析失败：看「LLM 录制」确认模型端点，必要时在「系统 → LLM」换一个配置",
		Fix:    "open:/function/llm-records",
	}},
	{errev.ClassPolicyBlocked, errAdvice{
		Label:  "被平台规则拦下",
		Action: "拦截规则 / 出口闸 / 决策冻结生效：到「系统 → 拦截规则」查看或调整",
		Fix:    "open:/system/intercept",
	}},
}

// errGroup 是聚合后的一组。
type errGroup struct {
	Class  string   `json:"class"`
	Label  string   `json:"label"`
	Count  int      `json:"count"`
	Sample string   `json:"sample"`
	Action string   `json:"action"`
	Fix    string   `json:"fix"`
	Tools  []string `json:"tools"`
	// Install 只对 tool_missing 有值：一条可直接复制的安装命令。
	// 原来卡片只说“到环境页安装”，而环境页当时既不给命令也不列任务里缺的工具。
	Install string `json:"install,omitempty"`
}

// registerErrorRoutes 挂 P8-8 的错误聚合接口。
func (s *Server) registerErrorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/errors", s.taskErrors)
}

// taskErrors 聚合某任务在窗口期内的错误事件。
//
// 查询参数：
//
//	window_min  回看多少分钟（默认 60，上限 10080=7天）
//	limit       每组最多带多少条原始记录（默认 0 = 只要聚合，不返回明细）
func (s *Server) taskErrors(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	taskID, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	if err != nil || taskID <= 0 {
		writeErr(w, 400, "任务 id 不合法")
		return
	}
	window := 60
	if v := strings.TrimSpace(r.URL.Query().Get("window_min")); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 {
			window = n
		}
	}
	if window > 7*24*60 {
		window = 7 * 24 * 60
	}

	resp := struct {
		TaskID    int64      `json:"task_id"`
		WindowMin int        `json:"window_min"`
		Total     int        `json:"total"`
		Groups    []errGroup `json:"groups"`
	}{TaskID: taskID, WindowMin: window, Groups: []errGroup{}}

	if s.p2 == nil || s.p2.DB == nil {
		writeJSON(w, 200, resp)
		return
	}

	// 聚合在 SQL 里做（GROUP BY class），不要拉全部行到 Go 里再统计。
	rows, err := s.p2.DB.QueryContext(r.Context(), `
		SELECT class, count(*)::int, max(detail) AS sample, array_agg(DISTINCT tool) AS tools
		FROM task_error_events
		WHERE task_id = $1 AND ts > now() - ($2 || ' minutes')::interval
		GROUP BY class`, taskID, strconv.Itoa(window))
	if err != nil {
		writeErr(w, 500, "读取错误事件失败："+err.Error())
		return
	}
	defer rows.Close()

	type agg struct {
		count  int
		sample string
		tools  []string
	}
	byClass := map[string]agg{}
	for rows.Next() {
		var class, sample string
		var n int
		var tools []string
		// array_agg 返回 text[]，用 pgx 的字符串形式扫再手工拆，避免引入额外依赖。
		var rawTools []byte
		if err := rows.Scan(&class, &n, &sample, &rawTools); err != nil {
			continue
		}
		tools = parsePGTextArray(string(rawTools))
		byClass[errev.NormalizeClass(class)] = agg{count: n, sample: sample, tools: tools}
	}
	if err := rows.Err(); err != nil {
		writeErr(w, 500, "读取错误事件失败："+err.Error())
		return
	}

	for _, it := range errAdvices {
		a, ok := byClass[it.Class]
		if !ok || a.count == 0 {
			continue
		}
		resp.Total += a.count
		resp.Groups = append(resp.Groups, errGroup{
			Class:  it.Class,
			Label:  it.Advice.Label,
			Count:  a.count,
			Sample: clipRunes(a.sample, 200),
			Action: it.Advice.Action,
			Fix:    it.Advice.Fix,
			Tools:  a.tools,
			Install: func() string {
				if it.Class != errev.ClassToolMissing {
					return ""
				}
				return env.InstallHintBatch(a.tools)
			}(),
		})
	}
	writeJSON(w, 200, resp)
}

// parsePGTextArray 解析 PG 的 text[] 文本形式：{a,b,"c,d","e\"f"}。
// 只用于工具名这类简单值，但引号/逗号/反斜杠转义要正确处理，
// 否则带逗号的元素会被拆成两个。
func parsePGTextArray(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "NULL" {
		return nil
	}
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	if s == "" {
		return nil
	}
	var (
		out     []string
		cur     strings.Builder
		inQuote bool
		escaped bool
		started bool // 当前元素是否已经开始（区分 {a,,b} 里的空元素）
	)
	flush := func() {
		if !started {
			return
		}
		if v := cur.String(); v != "" {
			out = append(out, v)
		}
		cur.Reset()
		started = false
	}
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
			started = true
		case inQuote && r == '\\':
			escaped = true
		case r == '"':
			inQuote = !inQuote
			started = true
		case !inQuote && r == ',':
			flush()
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	flush()
	return out
}

func clipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if n <= 0 || len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
