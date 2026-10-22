// Package relay 实现"三段式接力便条"（MEMORY.md）：
// 每次一段 run 结束，把「已完成原语 / 已证死路 / 下一步」压成一块，插到文件顶部；
// 下一次 run（可能是另一个 worker、另一天）先读它，从"下一步"接着干，不重复侦察。
//
// 与 transcript 的分工：transcript 负责同一 intent 的续跑；MEMORY 负责跨 worker/跨会话交接。
package relay

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	FileName    = "MEMORY.md"
	maxFileSize = 32 * 1024 // 单任务最多保留 32KB 便条
	sep         = "\n---\n"
)

// Path 返回某任务目录下的 MEMORY.md 路径。
func Path(taskDir string) string { return filepath.Join(taskDir, FileName) }

// Block 生成一块接力便条。next 为空时给一句通用下一步提示。
func Block(intentID int64, summary, state, next string) string {
	if strings.TrimSpace(next) == "" {
		next = "看当前 frontier 与 facts 决定下一步"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## 第 %d 段 · %s · intent#%d\n", time.Now().Unix(), state, intentID)
	fmt.Fprintf(&b, "- 意图：%s\n", oneLine(summary, 200))
	fmt.Fprintf(&b, "- 结果：%s\n", state)
	fmt.Fprintf(&b, "- 下一步：%s\n", oneLine(next, 300))
	return b.String()
}

// Append 把一块便条插到文件顶部（标题之后），并把整体裁剪到 maxFileSize。
func Append(taskDir, block string) error {
	if strings.TrimSpace(block) == "" {
		return nil
	}
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return err
	}
	p := Path(taskDir)
	old, _ := os.ReadFile(p)
	content := strings.TrimSpace(block) + "\n" + sep + "\n" + strings.TrimSpace(string(old))
	trimmed := trimHead(content, maxFileSize)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(trimmed+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Read 返回最近一次写入的便条（最多 maxBytes 字节）；不存在时返回空串。
func Read(taskDir string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 8000
	}
	b, err := os.ReadFile(Path(taskDir))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	if len(s) > maxBytes {
		s = s[:maxBytes]
		if i := strings.LastIndex(s, "\n- "); i > 0 {
			s = s[:i] // 尽量在块边界截断
		}
	}
	return s
}

// Reset 清空某任务的便条（清理/赛后）。
func Reset(taskDir string) error {
	err := os.Remove(Path(taskDir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func trimHead(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
