// Package execx 是「执行面」的安全壳：提权/横移/清理命令的统一执行与策略校验。
//
// 三层护栏：
//   - 通用黑名单：删根、格式化、关机、fork bomb、覆盖关键文件 → 一律拒绝；
//   - 清理白名单：只允许删文件/删账号/清 crontab/停服务，且路径不能是系统根；
//   - 超时 + 输出上限：单条默认 120s、输出 512KB，绝不把机器拖死。
package execx

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Autumn-27/artex/internal/egress"
)

// Result 是一条命令的执行结果。
type Result struct {
	Command  string `json:"command"`
	OK       bool   `json:"ok"`
	Output   string `json:"output"`
	Err      string `json:"err,omitempty"`
	Duration string `json:"duration"`
}

// Executor 抽象执行器（测试可注入）。
type Executor interface {
	Run(ctx context.Context, command string, timeout time.Duration) (string, error)
}

// Shell 用 sh -c 执行。
type Shell struct{}

func (Shell) Run(ctx context.Context, command string, timeout time.Duration) (string, error) {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(c, "sh", "-c", command)
	if len(egress.ProxyEnv) > 0 {
		cmd.Env = append(os.Environ(), egress.ProxyEnv...)
	}
	out, err := cmd.CombinedOutput()
	if len(out) > 512*1024 {
		out = out[:512*1024]
	}
	return string(out), err
}

var (
	denyAnywhere = regexp.MustCompile(`(?i)(rm\s+-[a-z]*r[a-z]*f?[a-z]*\s+/(?:\*|\s|$)|mkfs(\.|\s)|dd\s+if=.*of=/dev/|` +
		`shutdown|reboot|halt|init\s+0|:\(\)\s*\{|chmod\s+-R\s+777\s+/\s*$|>\s*/etc/(passwd|shadow)|` +
		`DROP\s+DATABASE|truncate\s+-s\s+0\s+/dev/)`)
	cleanupPrefixes = []string{
		"rm -f ", "rm -rf ", "rm -r ", "unlink ", "userdel ", "crontab -r", "crontab -l",
		"docker rm ", "systemctl disable ", "systemctl stop ", "kill ", "pkill ",
		"truncate -s 0 ", "mv ",
	}
	systemRoots = map[string]bool{
		"/": true, "/etc": true, "/usr": true, "/var": true, "/bin": true, "/sbin": true,
		"/boot": true, "/dev": true, "/proc": true, "/sys": true, "/root": true, "/home": true,
		"/opt": true, "/srv": true, "/lib": true, "/lib64": true,
	}
)

// Validate 按用途校验命令。kind ∈ privesc|lateral|cleanup。
func Validate(command, kind string) error {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return fmt.Errorf("命令为空")
	}
	if len(cmd) > 8192 {
		return fmt.Errorf("命令过长")
	}
	if denyAnywhere.MatchString(cmd) {
		return fmt.Errorf("命中通用禁止模式（删根/格式化/关机/覆盖关键文件）")
	}
	if kind == "cleanup" {
		// 清理命令必须是单条：拒绝链式/管道/重定向/命令替换，否则白名单前缀可被绕过。
		if strings.ContainsAny(cmd, ";|&<>`") || strings.Contains(cmd, "$(") || strings.Contains(cmd, "${") || strings.Contains(cmd, "\n") {
			return fmt.Errorf("清理命令不允许多条串联/管道/重定向/命令替换")
		}
		low := strings.ToLower(cmd)
		allowed := false
		for _, p := range cleanupPrefixes {
			if strings.HasPrefix(low, p) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("清理只允许：删除文件/账号、清 crontab、停服务（收到：%s）", clip(cmd, 80))
		}
		for _, f := range strings.Fields(cmd) {
			// 去掉通配符/尾部斜杠后再判：/*、//、/etc/* 都要拦住。
			norm := strings.TrimRight(f, "/")
			norm = strings.TrimRight(norm, "*")
			norm = strings.TrimRight(norm, "/")
			if norm == "" {
				norm = "/"
			}
			if systemRoots[norm] {
				return fmt.Errorf("清理路径指向系统根：%s", f)
			}
		}
	}
	return nil
}

// Run 校验后执行一条命令。
func Run(ctx context.Context, ex Executor, command, kind string, timeout time.Duration) Result {
	start := time.Now()
	res := Result{Command: command}
	if err := Validate(command, kind); err != nil {
		res.Err = err.Error()
		res.Duration = time.Since(start).String()
		return res
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	if err := egress.Guard(ctx); err != nil {
		res.Err = "出口未就绪，拒绝执行：" + err.Error()
		res.Duration = time.Since(start).String()
		return res
	}
	if ex == nil {
		ex = Shell{}
	}
	out, err := ex.Run(ctx, command, timeout)
	res.Output = clip(out, 4000)
	res.OK = err == nil
	if err != nil {
		res.Err = err.Error()
	}
	res.Duration = time.Since(start).String()
	return res
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
