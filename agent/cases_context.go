package agent

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Autumn-27/artex/internal/cases"
)

// renderRelevantCases 在 planner 每轮开新方向之前，把知识库里同关键词的正/负案例
// 摘成一小段（≤3 条）塞进本轮输入，避免重复踩已经证死的路（L5 的"经验机器"）。
func renderRelevantCases(dir, goal string, limit int) string {
	dir = strings.TrimSpace(dir)
	if dir == "" || limit <= 0 {
		return ""
	}
	seen := map[string]bool{}
	var lines []string
	for _, kw := range goalKeywords(goal) {
		hits, err := cases.Search(dir, kw, limit)
		if err != nil {
			continue
		}
		for _, h := range hits {
			if seen[h.Path] {
				continue
			}
			seen[h.Path] = true
			title := h.Title
			if title == "" {
				title = h.Path
			}
			lines = append(lines, fmt.Sprintf("- %s：%s（%s）", title, clipText(h.Snippet, 160), filepath.Base(h.Path)))
			if len(lines) >= limit {
				break
			}
		}
		if len(lines) >= limit {
			break
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\n【知识库相关案例（正/负结论，先看再派）】\n" + strings.Join(lines, "\n")
}

// goalKeywords 从目标里抽 2–6 个关键词（空白/标点切分，保留 ≥3 字符的片段）。
func goalKeywords(goal string) []string {
	fields := strings.FieldsFunc(goal, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '，', '。', '、', '；', '：', ',', '.', ';', ':', '/', '(', ')', '（', '）':
			return true
		}
		return false
	})
	out := make([]string, 0, 6)
	seen := map[string]bool{}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if len([]rune(f)) < 3 || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
		if len(out) >= 6 {
			break
		}
	}
	return out
}
