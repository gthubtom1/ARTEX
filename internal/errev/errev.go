// Package errev 是「任务级错误事件」的写入通道。
//
// 为什么单独一个包：错误散在 engine / guard / recheck 三处，各自的 ctx 里拿 taskID 的
// 方式不同；统一到一个极小的注入点（Sink），调用方只写一行，落库逻辑留在 server。
//
// class 固定 5 类（docs/02-P8执行手册.md P8-8）：
//
//	env_error       环境（代理/网络/DNS）不可用
//	tool_missing    命令首词在本机不存在
//	target_blocked  目标返回 401/403/429/503 或 WAF 页
//	self_error      平台自己出错（LLM 调用失败 / 解析失败 / 代码异常）
//	policy_blocked  被拦截规则 / 出口闸 / 决策冻结拦下
//
// 纪律：detail 里不写凭据、不写完整口令；截断到 500 字符。
package errev

import (
	"context"
	"strings"
)

// 五类错误。
const (
	ClassEnvError      = "env_error"
	ClassToolMissing   = "tool_missing"
	ClassTargetBlocked = "target_blocked"
	ClassSelfError     = "self_error"
	ClassPolicyBlocked = "policy_blocked"
)

// Sink 由 server 注入（写 task_error_events）。未注入时静默丢弃（测试/无库场景）。
// intentID=0 表示归属未知（历史行 / 平台级事件 / 非 worker 调用）。
var Sink func(ctx context.Context, taskID, intentID int64, class, tool, detail string)

// IntentIDOf 由 server 注入：从 ctx 里取当前意图 id（取不到返回 0）。
// 单独一个钩子而不是直接 import agent —— guard 已经 import 本包，
// 本包再 import agent 会形成 errev → agent → guard → errev 的循环。
var IntentIDOf func(context.Context) int64

// maxDetail 是 detail 的截断长度。
const maxDetail = 500

// Emit 记录一条错误事件。taskID<=0 表示平台级事件（不属于某个任务）。
// 意图 id 由 ctx 里的 RunInfo 解析（见 IntentIDOf）；解析不到写 0。
func Emit(ctx context.Context, taskID int64, class, tool, detail string) {
	EmitIntent(ctx, taskID, intentIDFrom(ctx), class, tool, detail)
}

// EmitIntent 记录一条已知意图归属的错误事件（调用方手里有 intent.ID 时用这个，
// 不要依赖 ctx 里有没有 RunInfo）。intentID<=0 一律按 0（归属未知）落库。
func EmitIntent(ctx context.Context, taskID, intentID int64, class, tool, detail string) {
	if Sink == nil {
		return
	}
	if intentID < 0 {
		intentID = 0
	}
	class = NormalizeClass(class)
	Sink(ctx, taskID, intentID, class, clip(tool, 64), clip(detail, maxDetail))
}

// intentIDFrom 从 ctx 取意图 id；未注入钩子或取不到时返回 0（绝不 panic）。
func intentIDFrom(ctx context.Context) int64 {
	if IntentIDOf == nil {
		return 0
	}
	if id := IntentIDOf(ctx); id > 0 {
		return id
	}
	return 0
}

// NormalizeClass 把未知分类归到 self_error，保证面板只有 5 个桶。
func NormalizeClass(class string) string {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case ClassEnvError:
		return ClassEnvError
	case ClassToolMissing:
		return ClassToolMissing
	case ClassTargetBlocked:
		return ClassTargetBlocked
	case ClassPolicyBlocked:
		return ClassPolicyBlocked
	}
	return ClassSelfError
}

// ClassifyHTTP 按 HTTP 状态码给分类：401/403/429/503 → target_blocked，其余 → self_error。
// 供 guard / adapter 在拿到响应后快速分类。
func ClassifyHTTP(status int) string {
	switch status {
	case 401, 403, 429, 503:
		return ClassTargetBlocked
	}
	return ClassSelfError
}

// LooksLikeTargetBlocked 在只有错误文本时猜一下是不是目标侧拒绝（WAF/拦截页常见措辞）。
func LooksLikeTargetBlocked(s string) bool {
	t := strings.ToLower(s)
	for _, m := range []string{
		"403 forbidden", "401 unauthorized", "429 too many", "503 service",
		"access denied", "request blocked", "waf", "cloudflare", "安全狗", "拦截",
	} {
		if strings.Contains(t, m) {
			return true
		}
	}
	return false
}

// LooksLikeEnvError 判断错误文本是不是环境问题（代理/DNS/连不上）。
func LooksLikeEnvError(s string) bool {
	t := strings.ToLower(s)
	for _, m := range []string{
		"proxyconnect", "connection refused", "no such host", "i/o timeout",
		"network is unreachable", "tls handshake timeout", "dial tcp", "temporary failure in name resolution",
	} {
		if strings.Contains(t, m) {
			return true
		}
	}
	return false
}

// clip 按 rune 截断并压掉换行（detail 会被塞进一行 UI）。
func clip(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " "))
	if n <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
