// Package errclass 把"执行失败"分成可行动的几类，让上层知道该换打法、重试还是修环境。
//
// 纪律（docs/最终方案-v1.md §5）：这里的文案**一律正向**，
// 绝不出现「禁止 / 不允许 / 违规」之类字样——避免模型把平台提示误读成"被拒绝"后放弃。
package errclass

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

// Class 是失败类别。
type Class string

const (
	// TargetBlocked 目标侧拦了：WAF / 403 / 429 / 挑战页。行动：换载体或换入口。
	TargetBlocked Class = "target_blocked"
	// EnvError 本机环境问题：命令不存在、权限不足、代理不通、磁盘满。行动：修环境或降级。
	EnvError Class = "env_error"
	// Timeout 超时。行动：缩小范围或提高超时。
	Timeout Class = "timeout"
	// SelfError 我们自己的错：参数拼错、解析失败、工具用法不对。行动：改参数重试。
	SelfError Class = "self_error"
	// AuthRequired 需要认证（401/登录页）。行动：先拿凭据或换身份。
	AuthRequired Class = "auth_required"
	// Unknown 未识别。行动：看原始输出。
	Unknown Class = "unknown"
)

var (
	reTarget = regexp.MustCompile(`(?i)\b(403|406|429|503|forbidden|waf|cloudflare|rate.?limit|captcha|access denied|blocked by|challenge)\b`)
	reAuth   = regexp.MustCompile(`(?i)\b(401|unauthorized|login required|not logged in|authenticate|invalid token|token expired)\b`)
	reEnv    = regexp.MustCompile(`(?i)(command not found|executable file not found|not found in \$PATH|no such file or directory|permission denied|operation not permitted|cannot connect|connection refused|no route to host|proxy|dial tcp|network is unreachable|disk ?full|no space left|cannot allocate memory|out of memory)`)
	reSelf   = regexp.MustCompile(`(?i)(invalid argument|unknown flag|usage:|unexpected token|parse error|json: cannot unmarshal|template: |syntax error)`)
)

// Classify 依据 error 与输出文本判定类别。输出优先（更具体），其次 error 文本。
func Classify(err error, output string) Class {
	text := output
	if err != nil {
		text = err.Error() + "\n" + output
	}
	switch {
	case text == "":
		return Unknown
	case isTimeout(err, text):
		return Timeout
	case reTarget.MatchString(text):
		return TargetBlocked
	case reAuth.MatchString(text):
		return AuthRequired
	case reEnv.MatchString(text):
		return EnvError
	case reSelf.MatchString(text):
		return SelfError
	}
	return Unknown
}

func isTimeout(err error, text string) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	l := strings.ToLower(text)
	return strings.Contains(l, "context deadline exceeded") ||
		strings.Contains(l, "timeout") ||
		strings.Contains(l, "timed out")
}

// Hint 返回**正向**的下一步建议（给 agent 看的话术）。不含任何否定/禁止表达。
func Hint(c Class) string {
	switch c {
	case TargetBlocked:
		return "响应像是由目标侧的防护返回的。换一个载体再试一次：换编码、换参数位置、换入口路径，或换一种同类手法。"
	case AuthRequired:
		return "这一步需要有效身份。先确认有没有可用凭据/会话，或换一个已授权身份的入口再试。"
	case EnvError:
		return "本机环境这边有问题（工具或网络）。先跑一次环境自检：工具在不在、代理通不通、磁盘够不够；必要时改用等价工具继续。"
	case Timeout:
		return "这次超时了。缩小范围（更少端口/更小字典/单目标）后重跑，或把这步拆成两步。"
	case SelfError:
		return "命令/参数本身有问题。对照工具用法改一处参数再试；连续两次同样的错就换等价工具。"
	}
	return "看一下原始输出再决定下一步。"
}

// Retryable 表示"同类重试有意义"（环境/超时/自身错误），目标侧拦截则应换打法。
func Retryable(c Class) bool {
	switch c {
	case EnvError, Timeout, SelfError:
		return true
	}
	return false
}

// ShouldPivot 表示"应当换打法/换入口"，而不是重试。
func ShouldPivot(c Class) bool {
	return c == TargetBlocked || c == AuthRequired
}
