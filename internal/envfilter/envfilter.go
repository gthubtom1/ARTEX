// Package envfilter 负责把凭据类环境变量从子进程环境里剥掉。
//
// 背景（docs/02-P8执行手册.md P8-7 坑 3）：启动时为了让已有的
// agent.NewP2 路径复用密钥，cmd 层会用 os.Setenv(ARTEX_CRED_KEY, ...)。
// 这个变量会被所有子进程继承，包括 worker 的 Bash/PTY。构造 BashEnv 时
// 必须先过这一层，避免凭据主密钥出现在工具进程环境里。
package envfilter

import "strings"

// DefaultDeny 是默认要剥掉的环境变量名（精确匹配，大小写不敏感）。
// 含 ARTEX_CRED_KEY 及常见密钥类；另有后缀兜底规则，见 IsDenied。
var DefaultDeny = []string{
	"ARTEX_CRED_KEY",
	"__ARTEX_CRED_KEY",
	"ARTEX_PG_DSN", // 含数据库口令
	"AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN",
	"OPENAI_API_KEY",
	"ANTHROPIC_API_KEY",
}

// sensitiveSuffixes 是兜底规则：任何以这些后缀结尾的 key 都视为敏感。
var sensitiveSuffixes = []string{
	"_KEY",
	"_SECRET",
	"_TOKEN",
	"_PASSWORD",
	"_PASSWD",
}

// allowlist 是明确不剥的例外（即使命中后缀规则）。大小写不敏感。
var allowlist = []string{
	"GPG_KEY",
	"SSH_AUTH_SOCK",
	"LS_COLORS",
}

// Strip 从 env（形如 "K=V" 的切片）里移除 Deny 名单中的项与形如 K 前缀匹配的项。
// deny 为空时用 DefaultDeny。返回新切片，不修改入参。
func Strip(env []string, deny ...string) []string {
	rules := normalizeDeny(deny)
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, ok := keyOf(kv)
		if !ok || !isDeniedKey(key, rules) {
			out = append(out, kv)
		}
	}
	return out
}

// Redact 把 env 里敏感项的 value 换成 "***"，用于日志/审计（不删项，保持键存在）。
func Redact(env []string, deny ...string) []string {
	rules := normalizeDeny(deny)
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, ok := keyOf(kv)
		if ok && isDeniedKey(key, rules) {
			out = append(out, key+"=***")
			continue
		}
		out = append(out, kv)
	}
	return out
}

// IsDenied 判断单个 "K=V" 是否属于敏感项。没有 "=" 的项视为不合法，返回 false
// （Strip/Redact 会原样保留）。
func IsDenied(kv string, deny ...string) bool {
	key, ok := keyOf(kv)
	if !ok {
		return false
	}
	return isDeniedKey(key, normalizeDeny(deny))
}

// BlankInherited 在 Strip 的基础上，为所有被剥掉的 key（以及 deny 名单里的
// 每个 key）追加显式的空值 "K="。
//
// 为什么需要它：SDK 的 Bash 会把 BashEnv 追加在 os.Environ() 之后，单纯
// 从 BashEnv 里删掉 ARTEX_CRED_KEY 并不会影响父进程继承下来的值。显式的
// "ARTEX_CRED_KEY=" 会覆盖继承值，子进程里 echo $ARTEX_CRED_KEY 才是空。
//
// 建议直接这样接线：BashEnv: envfilter.BlankInherited(proxyEnv(...))。
// 如果希望覆盖所有从父进程继承的敏感项（包括 *_PASSWORD 等），传入完整环境：
// envfilter.BlankInherited(append(os.Environ(), proxyEnv(...)...))。
func BlankInherited(env []string, deny ...string) []string {
	rules := normalizeDeny(deny)

	// 先记下入参里实际命中的 key（若传的是完整 os.Environ，这能覆盖后缀兜底项）。
	blank := make(map[string]string, len(rules))
	for _, kv := range env {
		if key, ok := keyOf(kv); ok && isDeniedKey(key, rules) {
			blank[strings.ToUpper(key)] = key
		}
	}
	out := Strip(env, rules...)
	for _, name := range rules {
		if _, ok := blank[strings.ToUpper(name)]; !ok {
			blank[strings.ToUpper(name)] = name
		}
	}
	for _, name := range blank {
		out = append(out, name+"=")
	}
	return out
}

// normalizeDeny 返回本次调用的规则集；deny 为空时回落到 DefaultDeny。
func normalizeDeny(deny []string) []string {
	if len(deny) == 0 {
		return DefaultDeny
	}
	return deny
}

// keyOf 取出 "K=V" 的 key。没有 "=" 视为不合法项。
func keyOf(kv string) (key string, ok bool) {
	i := strings.IndexByte(kv, '=')
	if i < 0 {
		return "", false
	}
	return kv[:i], true
}

func isDeniedKey(key string, rules []string) bool {
	for _, name := range allowlist {
		if strings.EqualFold(key, name) {
			return false
		}
	}
	for _, name := range rules {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	upper := strings.ToUpper(key)
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(upper, suffix) {
			return true
		}
	}
	return false
}
