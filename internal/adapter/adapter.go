// Package adapter 把外部 CLI 工具（nmap / httpx / nuclei）的文本输出解析成
// 稳定的结构化 JSON，供 agent 直接消费，避免模型读整屏文本时误读或浪费 token。
//
// 设计约束：
//   - 只做「文本 → 结构化」，不执行任何命令，便于单测；
//   - 只用标准库；
//   - 字段名一旦确定即视为契约，前端 / agent 依赖它们。
package adapter

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// maxStdoutBytes 是允许解析的单次输出上限（8MB）。超过时直接报错，
// 让调用方把原始输出落盘后再按文件/分片处理，避免一次性吃爆内存。
const maxStdoutBytes = 8 << 20

// Result 是统一的适配器输出。
type Result struct {
	Tool    string `json:"tool"`
	OK      bool   `json:"ok"`
	Items   []Item `json:"items"`
	Raw_Ref string `json:"raw_ref,omitempty"` // 原始输出落盘路径（可选）
	Err     string `json:"err,omitempty"`
}

// Item 是单条结构化结果。
type Item struct {
	Kind string         `json:"kind"` // port|service|web|finding|hit|template
	Data map[string]any `json:"data"`
}

// Parser 只做"文本→结构化"，不执行命令（便于单测）。
type Parser interface {
	Name() string
	Parse(stdout, stderr string) (Result, error)
}

// All 返回全部内置解析器。
func All() []Parser {
	return []Parser{NmapXML(), HttpxJSONL(), NucleiJSONL()}
}

// checkSize 在解析前拦截超长输出。
func checkSize(tool, stdout string) error {
	if len(stdout) > maxStdoutBytes {
		return fmt.Errorf("%s: stdout 过大（%d 字节，上限 %d 字节），请先落盘再分片解析", tool, len(stdout), maxStdoutBytes)
	}
	return nil
}

// Summarize 生成 3–5 行中文人类可读摘要。即使解析失败也会给出可行动的信息。
func Summarize(r Result) string {
	lines := summarizeLines(r)
	for len(lines) < 3 {
		lines = append(lines, "（无更多细节）")
	}
	if len(lines) > 5 {
		lines = lines[:5]
	}
	return strings.Join(lines, "\n")
}

func summarizeLines(r Result) []string {
	tool := r.Tool
	if tool == "" {
		tool = "unknown"
	}
	if r.Err != "" || !r.OK {
		msg := r.Err
		if msg == "" {
			msg = "未提供原因"
		}
		lines := []string{
			fmt.Sprintf("%s: 解析失败：%s", tool, msg),
			fmt.Sprintf("已解析条目：%d", len(r.Items)),
			"建议：保留原始输出并降级重跑；若文本格式非预期，请核对工具版本与参数",
		}
		if r.Raw_Ref != "" {
			lines = append(lines, "原始输出："+r.Raw_Ref)
		}
		return lines
	}

	switch tool {
	case "nmap":
		return summarizeNmap(r)
	case "httpx":
		return summarizeHttpx(r)
	case "nuclei":
		return summarizeNuclei(r)
	default:
		kinds := map[string]int{}
		for _, it := range r.Items {
			kinds[it.Kind]++
		}
		lines := []string{
			fmt.Sprintf("%s: %d 条结果", tool, len(r.Items)),
			"类型分布：" + formatCounts(kinds),
			rawLine(r),
		}
		return lines
	}
}

func summarizeNmap(r Result) []string {
	type portEntry struct {
		num   int
		label string
	}
	var entries []portEntry
	hostSeen := map[string]bool{}
	var hostList []string
	services := map[string]int{}
	for _, it := range r.Items {
		if it.Kind != "port" {
			continue
		}
		port := asInt(it.Data["port"])
		svc := asString(it.Data["service"])
		if svc == "" {
			svc = "unknown"
		}
		entries = append(entries, portEntry{num: port, label: fmt.Sprintf("%d/%s", port, svc)})
		services[svc]++

		host := asString(it.Data["host"])
		if host == "" {
			host = asString(it.Data["ip"])
		}
		if host != "" && !hostSeen[host] {
			hostSeen[host] = true
			hostList = append(hostList, host)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].num != entries[j].num {
			return entries[i].num < entries[j].num
		}
		return entries[i].label < entries[j].label
	})

	line1 := fmt.Sprintf("nmap: %d 个开放端口", len(entries))
	if len(entries) > 0 {
		labels := make([]string, 0, 5)
		for i, e := range entries {
			if i == 5 {
				labels = append(labels, "…")
				break
			}
			labels = append(labels, e.label)
		}
		line1 += "（" + strings.Join(labels, ", ") + "）"
	}
	line2 := fmt.Sprintf("涉及 %d 台主机", len(hostList))
	if len(hostList) > 0 {
		line2 += "：" + joinLimit(hostList, 5)
	}
	lines := []string{line1, line2, "服务分布：" + formatCounts(services)}
	if r.Raw_Ref != "" {
		lines = append(lines, "原始输出："+r.Raw_Ref)
	}
	return lines
}

func summarizeHttpx(r Result) []string {
	statusBuckets := map[string]int{}
	servers := map[string]int{}
	titles := []string{}
	techs := map[string]int{}
	cdnCount := 0
	for _, it := range r.Items {
		if it.Kind != "web" {
			continue
		}
		status := asInt(it.Data["status"])
		if status > 0 {
			statusBuckets[fmt.Sprintf("%dxx", status/100)]++
		}
		if srv := asString(it.Data["webserver"]); srv != "" {
			servers[srv]++
		}
		if title := asString(it.Data["title"]); title != "" {
			titles = append(titles, title)
		}
		for _, tech := range asStrings(it.Data["tech"]) {
			if tech != "" {
				techs[tech]++
			}
		}
		if asBool(it.Data["cdn"]) {
			cdnCount++
		}
	}

	line1 := fmt.Sprintf("httpx: %d 个 Web 目标", len(r.Items))
	if len(statusBuckets) > 0 {
		line1 += "（" + formatCounts(statusBuckets) + "）"
	}
	line2 := fmt.Sprintf("标题示例：%d 条非空", len(titles))
	if len(titles) > 0 {
		line2 += "：" + joinLimit(titles, 3)
	}
	line3 := fmt.Sprintf("服务器：%s · CDN 命中 %d", formatCountsOrNone(servers), cdnCount)
	if len(techs) > 0 {
		line3 = fmt.Sprintf("技术栈：%s · CDN 命中 %d", formatCounts(techs), cdnCount)
	}
	lines := []string{line1, line2, line3}
	if r.Raw_Ref != "" {
		lines = append(lines, "原始输出："+r.Raw_Ref)
	}
	return lines
}

func summarizeNuclei(r Result) []string {
	severities := map[string]int{}
	templates := []string{}
	templateSeen := map[string]bool{}
	hosts := map[string]bool{}
	for _, it := range r.Items {
		if it.Kind != "finding" {
			continue
		}
		sev := asString(it.Data["severity"])
		if sev == "" {
			sev = "unknown"
		}
		severities[sev]++
		tid := asString(it.Data["template_id"])
		if tid != "" && !templateSeen[tid] {
			templateSeen[tid] = true
			templates = append(templates, tid)
		}
		if host := asString(it.Data["host"]); host != "" {
			hosts[host] = true
		}
	}
	lines := []string{
		fmt.Sprintf("nuclei: %d 条发现，涉及 %d 台主机", len(r.Items), len(hosts)),
		"严重性分布：" + formatCountsOrNone(severities),
		"模板：" + joinLimitOrNone(templates, 5),
	}
	if r.Raw_Ref != "" {
		lines = append(lines, "原始输出："+r.Raw_Ref)
	}
	return lines
}

func rawLine(r Result) string {
	if r.Raw_Ref == "" {
		return "原始输出：未落盘"
	}
	return "原始输出：" + r.Raw_Ref
}

// formatCounts 把计数 map 排成 "name count · name count"（按数量降序、名称升序）。
func formatCounts(m map[string]int) string {
	type kv struct {
		name  string
		count int
	}
	list := make([]kv, 0, len(m))
	for name, count := range m {
		list = append(list, kv{name, count})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].count != list[j].count {
			return list[i].count > list[j].count
		}
		return list[i].name < list[j].name
	})
	parts := make([]string, 0, len(list))
	for _, item := range list {
		parts = append(parts, fmt.Sprintf("%s %d", item.name, item.count))
	}
	return strings.Join(parts, " · ")
}

func formatCountsOrNone(m map[string]int) string {
	if len(m) == 0 {
		return "无"
	}
	return formatCounts(m)
}

func joinLimit(items []string, limit int) string {
	if len(items) == 0 {
		return "无"
	}
	if len(items) > limit {
		return strings.Join(items[:limit], ", ") + " …"
	}
	return strings.Join(items, ", ")
}

func joinLimitOrNone(items []string, limit int) string {
	if len(items) == 0 {
		return "无"
	}
	return joinLimit(items, limit)
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case nil:
		return ""
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprint(x)
	}
}

func asInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

func asStrings(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			out = append(out, asString(e))
		}
		return out
	case string:
		if x == "" {
			return nil
		}
		return []string{x}
	default:
		return nil
	}
}

func asBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s != "" && s != "false" && s != "0" && s != "no"
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		return false
	}
}
