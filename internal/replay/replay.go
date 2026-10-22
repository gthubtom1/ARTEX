// Package replay 是「真回放」：读 transcript（norma 的 JSONL 会话日志），
// 还原每一步工具调用，并允许**只读重放**（curl GET / nmap -sT / nc -z / dig…）
// 后与历史结果做差异对比。
//
// 纪律：
//   - 只重放只读命令（白名单 + 危险词黑名单），写操作一律跳过；
//   - 单步超时、步数上限由调用方控制；
//   - 不修改 transcript，不写任务图。
package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/transcript"
)

// Step 是一次工具调用（含结果）。
type Step struct {
	Index   int             `json:"index"`
	ID      string          `json:"id,omitempty"`
	Tool    string          `json:"tool"`
	Command string          `json:"command,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	Result  string          `json:"result,omitempty"`
	IsError bool            `json:"is_error"`
	TS      string          `json:"ts,omitempty"`
}

// Doc 是一场会话的回放视图。
type Doc struct {
	SessionID string `json:"session_id"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
	Turns     int    `json:"turns"`
	TokensIn  int    `json:"tokens_in"`
	TokensOut int    `json:"tokens_out"`
	Steps     []Step `json:"steps"`
}

// Parse 把 transcript 记录重建为 Step 列表（按工具调用顺序）。
func Parse(recs []transcript.Record) Doc {
	d := Doc{}
	byID := map[string]int{} // tool_use id → step index
	for _, r := range recs {
		if r.SessionID != "" {
			d.SessionID = r.SessionID
		}
		if r.Timestamp != "" {
			if d.StartedAt == "" {
				d.StartedAt = r.Timestamp
			}
			d.EndedAt = r.Timestamp
		}
		if r.Usage != nil {
			d.TokensIn += r.Usage.InputTokens
			d.TokensOut += r.Usage.OutputTokens
		}
		if r.Message == nil {
			continue
		}
		if r.Message.Role == llm.RoleAssistant {
			d.Turns++
		}
		for _, c := range r.Message.Content {
			switch c.Type {
			case llm.BlockToolUse:
				st := Step{Index: len(d.Steps), ID: c.ID, Tool: c.Name, Input: c.Input, TS: r.Timestamp}
				st.Command = commandOf(c.Name, c.Input)
				byID[c.ID] = st.Index
				d.Steps = append(d.Steps, st)
			case llm.BlockToolResult:
				if idx, ok := byID[c.ToolUseID]; ok {
					d.Steps[idx].Result = toolResultText(c)
					d.Steps[idx].IsError = c.IsError
				}
			}
		}
	}
	return d
}

// commandOf 从工具入参里取"可重放的命令"。只有 Bash 与 WebFetch 能拿到。
func commandOf(tool string, input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	s := func(k string) string {
		if v, ok := m[k].(string); ok {
			return v
		}
		return ""
	}
	switch strings.ToLower(tool) {
	case "bash", "shell", "terminal":
		return strings.TrimSpace(s("command"))
	case "webfetch", "web_fetch":
		u := s("url")
		if u == "" {
			return ""
		}
		return "curl -s -m 15 " + u
	}
	return ""
}

func toolResultText(c llm.ContentBlock) string {
	var b strings.Builder
	for _, x := range c.Content {
		if x.Type == llm.BlockText && x.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(x.Text)
		}
	}
	return b.String()
}

// 只读白名单：前缀匹配。
// 只列“从操作机发起、对目标做只读探测”的命令。目标侧 shell 命令（ls/cat/id/ps…）
// 不能在操作机重放，否则对比的是本机输出；它们不进入白名单。
var readonlyPrefixes = []string{
	"curl -s", "curl -sS", "curl -I", "curl --head",
	"nmap -st", "nmap -sv", "nmap -pn", "nmap -ss",
	"nc -z", "dig ", "whois ", "ping -c",
}

// 危险词/重定向：出现即不重放。
var denyPattern = regexp.MustCompile(`(?i)(\brm\b|\bmv\b|\bcp\b|\bdd\b|chmod|chown|sudo|su -|mkfs|shutdown|reboot|` +
	`drop\s+database|delete\s+from|insert\s+into|update\s+\w+\s+set|truncate|` +
	`mysqldump|sqlmap|msfconsole|ffuf|nuclei|hydra|wpscan|>|>>|\|\s*(sh|bash)\b|` +
	`--data|-X\s*(POST|PUT|DELETE|PATCH)|--request\s+(post|put|delete|patch)|` +
	`(^|\s)-(f|t|o|O|d|T|u)(\s|=|$)|--form|--upload-file|--output|--data-raw|--data-binary|` +
	`--dump|--os-shell|nc -e|bash -i|/dev/tcp)`)

// ReadOnly 判断一条 Step 是否可以安全重放。
func ReadOnly(st Step) bool {
	cmd := strings.ToLower(strings.TrimSpace(st.Command))
	if cmd == "" {
		return false
	}
	if denyPattern.MatchString(cmd) {
		return false
	}
	for _, p := range readonlyPrefixes {
		if strings.HasPrefix(cmd, p) {
			return true
		}
	}
	return false
}

// Kind 给步骤分类（前端分组用）。
func Kind(st Step) string {
	cmd := strings.ToLower(st.Command)
	switch {
	case strings.HasPrefix(cmd, "curl"):
		return "http"
	case strings.HasPrefix(cmd, "nmap"), strings.HasPrefix(cmd, "nc "):
		return "scan"
	case strings.HasPrefix(cmd, "dig"), strings.HasPrefix(cmd, "whois"):
		return "dns"
	case strings.HasPrefix(cmd, "cat"), strings.HasPrefix(cmd, "ls"), strings.HasPrefix(cmd, "head"), strings.HasPrefix(cmd, "tail"):
		return "file"
	case strings.Contains(strings.ToLower(st.Tool), "fetch"):
		return "http"
	}
	return "other"
}

// Executor 执行一条只读命令。
type Executor interface {
	Exec(ctx context.Context, command string) (string, error)
}

// Comparison 是一次回放对比。
type Comparison struct {
	Index    int     `json:"index"`
	Tool     string  `json:"tool"`
	Command  string  `json:"command"`
	Same     bool    `json:"same"`
	Overlap  float64 `json:"overlap"` // 行重合度 0..1
	OldLines int     `json:"old_lines"`
	NewLines int     `json:"new_lines"`
	Diff     string  `json:"diff,omitempty"`
	Err      string  `json:"err,omitempty"`
}

// ReplayReadOnly 只读重放前 limit 条可重放步骤（跳过写操作）。
func ReplayReadOnly(ctx context.Context, d Doc, ex Executor, limit int) []Comparison {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	var out []Comparison
	for _, st := range d.Steps {
		if len(out) >= limit || ctx.Err() != nil {
			break
		}
		if !ReadOnly(st) {
			continue
		}
		now, err := ex.Exec(ctx, st.Command)
		c := Comparison{Index: st.Index, Tool: st.Tool, Command: st.Command,
			OldLines: lineCount(st.Result), NewLines: lineCount(now)}
		if err != nil {
			c.Err = err.Error()
			c.Diff = "执行失败：" + clip(err.Error(), 160)
			out = append(out, c)
			continue
		}
		c.Overlap = overlap(st.Result, now)
		// 回放对比要严：行集合重合 ≥0.9 且行数一致才算"没变"（多一行也可能意味着服务变了）。
		c.Same = c.Overlap >= 0.9 && c.OldLines == c.NewLines && !st.IsError
		c.Diff = firstDiff(st.Result, now)
		out = append(out, c)
	}
	return out
}

func lineCount(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return len(strings.Split(strings.TrimRight(s, "\n"), "\n"))
}

// overlap 用行集合 Jaccard 衡量两次输出是否一致。
func overlap(a, b string) float64 {
	sa, sb := lineSet(a), lineSet(b)
	if len(sa) == 0 && len(sb) == 0 {
		return 1
	}
	inter := 0
	for l := range sa {
		if sb[l] {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 {
		return 1
	}
	return round2(float64(inter) / float64(union))
}

func lineSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		out[l] = true
	}
	return out
}

func firstDiff(old, now string) string {
	a := strings.Split(strings.TrimSpace(old), "\n")
	b := strings.Split(strings.TrimSpace(now), "\n")
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return fmt.Sprintf("第 %d 行不同：原「%s」→ 现「%s」", i+1, clip(a[i], 80), clip(b[i], 80))
		}
	}
	if len(a) != len(b) {
		return fmt.Sprintf("行数不同：原 %d 行 → 现 %d 行", len(a), len(b))
	}
	return ""
}

// Summary 生成回放摘要（前端一行）。
func Summary(d Doc, cmps []Comparison) string {
	same := 0
	for _, c := range cmps {
		if c.Same {
			same++
		}
	}
	ro := 0
	for _, st := range d.Steps {
		if ReadOnly(st) {
			ro++
		}
	}
	return fmt.Sprintf("会话 %s：%d 轮 / %d 步（可只读重放 %d），重放 %d 步一致 %d 步",
		d.SessionID, d.Turns, len(d.Steps), ro, len(cmps), same)
}

// ToolCounts 统计各工具调用次数（按次数降序）。
func ToolCounts(d Doc) []string {
	m := map[string]int{}
	for _, st := range d.Steps {
		m[st.Tool]++
	}
	list := make([]struct {
		name string
		n    int
	}, 0, len(m))
	for k, v := range m {
		list = append(list, struct {
			name string
			n    int
		}{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	out := make([]string, 0, len(list))
	for _, x := range list {
		out = append(out, fmt.Sprintf("%s×%d", x.name, x.n))
	}
	return out
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
