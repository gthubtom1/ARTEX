// Package kb 是知识库的只读检索层：索引 kb/ 下的 md/json，按文件名或正文搜索，
// 并支持按相对路径读取（带路径穿越防护）。
//
// 供三处复用：面板 API（/api/knowledge）、MCP 端点（/mcp/kb）、agent 的提示注入。
package kb

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Hit 是一条命中。
type Hit struct {
	Path    string `json:"path"`
	Title   string `json:"title,omitempty"`
	Snippet string `json:"snippet"`
	Score   int    `json:"score,omitempty"`
}

// Options 控制扫描规模（防止把一台机器拖死）。
type Options struct {
	MaxFiles int // 最多扫多少个文件（默认 500）
	MaxBytes int // 单文件最多读多少字节（默认 512KB）
	Limit    int // 最多返回多少命中（默认 20）
}

func (o *Options) norm() {
	if o.MaxFiles <= 0 {
		o.MaxFiles = 500
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = 512 * 1024
	}
	if o.Limit <= 0 {
		o.Limit = 20
	}
}

// Search 在 root 下按文件名/正文搜索。空 query 返回文件列表（按路径排序）。
func Search(root, query string, opt Options) ([]Hit, error) {
	opt.norm()
	q := strings.ToLower(strings.TrimSpace(query))
	hits := make([]Hit, 0, opt.Limit)
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "inbox":
				return filepath.SkipDir
			}
			return nil
		}
		low := strings.ToLower(d.Name())
		if !strings.HasSuffix(low, ".md") && !strings.HasSuffix(low, ".json") {
			return nil
		}
		if files >= opt.MaxFiles || len(hits) >= opt.Limit {
			return fs.SkipAll
		}
		files++
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		h := Hit{Path: rel}
		score := 0
		if q == "" {
			h.Snippet = "文件"
			score = 1
		} else if strings.Contains(strings.ToLower(rel), q) {
			h.Snippet = "文件名匹配"
			score = 10
		}
		if q != "" && len(hits) < opt.Limit {
			if b, rerr := os.ReadFile(path); rerr == nil && len(b) <= opt.MaxBytes {
				text := string(b)
				lowText := strings.ToLower(text)
				if i := strings.Index(lowText, q); i >= 0 {
					h.Title = frontMatterTitle(text)
					h.Snippet = snippet(text, i)
					score += 5
					if h.Title == "" {
						h.Title = firstHeading(text)
					}
				}
			}
		}
		if h.Snippet != "" {
			h.Score = score
			hits = append(hits, h)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Path < hits[j].Path
	})
	return hits, nil
}

// Read 读取 root 下的一个相对路径（拒绝 .. 与绝对路径）。
func Read(root, rel string, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
	clean := filepath.Clean(strings.TrimSpace(rel))
	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("kb: 非法路径 %q", rel)
	}
	full := filepath.Join(root, clean)
	// 再校验一次：Join 后必须仍在 root 内。
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absFull, absRoot+string(filepath.Separator)) && absFull != absRoot {
		return "", fmt.Errorf("kb: 路径越界 %q", rel)
	}
	b, err := os.ReadFile(absFull)
	if err != nil {
		return "", err
	}
	if len(b) > maxBytes {
		b = b[:maxBytes]
	}
	return string(b), nil
}

func snippet(text string, at int) string {
	start := at - 60
	if start < 0 {
		start = 0
	}
	end := at + 120
	if end > len(text) {
		end = len(text)
	}
	return strings.Join(strings.Fields(text[start:end]), " ")
}

func frontMatterTitle(text string) string {
	if !strings.HasPrefix(text, "---") {
		return ""
	}
	end := strings.Index(text[3:], "\n---")
	if end < 0 {
		return ""
	}
	for _, line := range strings.Split(text[3:3+end], "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "title:") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "title:")), `"'`)
		}
	}
	return ""
}

func firstHeading(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}
