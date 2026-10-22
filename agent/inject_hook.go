package agent

import (
	"context"
	"strings"

	"github.com/Autumn-27/artex/internal/inject"
	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
)

// 本文件把「注入层」接进 harness：
//   - 提示级：always/hybrid 模式下在系统提示尾部附一段执行上下文（正向重述）；
//   - 拒答级：Stop hook 检测到"看起来像拒答"的回合时，注入一条技术重定向消息并继续，
//     次数受 MaxRetries 限制（默认 2），避免把一次拒答变成无限循环。
//
// 纪律：只正向重述，不写"禁止/你不能"；不改动任何工具或权限。

// InjectConfig 是注入层运行参数（由 server 从 settings 解析）。
type InjectConfig struct {
	Mode       inject.Mode `json:"mode"`
	MaxChars   int         `json:"max_chars"`
	MaxRetries int         `json:"max_retries"`
	Custom     string      `json:"custom"`
}

// InjectConfigFor 由 server 安装；nil = 默认 hybrid / 900 / 2。
var InjectConfigFor func() InjectConfig

func resolveInjectConfig() InjectConfig {
	c := InjectConfig{Mode: inject.ModeHybrid, MaxChars: 900, MaxRetries: 2}
	if InjectConfigFor != nil {
		got := InjectConfigFor()
		if got.Mode != "" {
			c.Mode = got.Mode
		}
		if got.MaxChars > 0 {
			c.MaxChars = got.MaxChars
		}
		if got.MaxRetries >= 0 {
			c.MaxRetries = got.MaxRetries
		}
		c.Custom = got.Custom
	}
	return c
}

// injectPromptBlock 返回系统提示尾部的执行上下文块（off/on-refusal 时为空）。
func injectPromptBlock(goal string) string {
	c := resolveInjectConfig()
	if !c.Mode.ShouldPrompt() {
		return ""
	}
	return "\n\n" + inject.PromptBlock(goal, c.MaxChars)
}

// InjectHook 包装 Harness 的 HookRunner：其余钩子原样透传，只在 Stop 时处理拒答。
type InjectHook struct {
	inner    harness.HookRunner
	mode     inject.Mode
	goal     func() string
	maxChars int
	retries  int

	used   int
	custom string
}

// NewInjectHook 构造；inner 可为 nil。
func NewInjectHook(inner harness.HookRunner, mode inject.Mode, goal func() string, maxChars, maxRetries int, custom string) *InjectHook {
	if maxChars <= 0 {
		maxChars = 900
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	return &InjectHook{inner: inner, mode: mode, goal: goal, maxChars: maxChars, retries: maxRetries, custom: custom}
}

func (h *InjectHook) PreToolUse(ctx context.Context, name string, input []byte) (bool, string, []byte) {
	if h.inner == nil {
		return false, "", nil
	}
	return h.inner.PreToolUse(ctx, name, input)
}

func (h *InjectHook) PostToolUse(ctx context.Context, name string, input, result []byte, isErr bool) {
	if h.inner != nil {
		h.inner.PostToolUse(ctx, name, input, result, isErr)
	}
}

// Stop 先让内层钩子裁决；内层要拦就原样返回，否则检测拒答并注入重定向。
func (h *InjectHook) Stop(ctx context.Context, messages []llm.Message) (bool, []string, string) {
	if h.inner != nil {
		prevent, blocking, msg := h.inner.Stop(ctx, messages)
		if prevent || len(blocking) > 0 {
			return prevent, blocking, msg
		}
	}
	if !h.mode.ShouldRetryRefusal() || h.used >= h.retries {
		return false, nil, ""
	}
	text := lastAssistantText(messages)
	if !inject.IsRefusal(text) {
		return false, nil, ""
	}
	goal := ""
	if h.goal != nil {
		goal = h.goal()
	}
	redirect := inject.Redirect(text, goal, h.maxChars)
	if strings.TrimSpace(h.custom) != "" {
		redirect = strings.TrimSpace(h.custom) + string(rune(10)) + redirect
		redirect = clipRunes(redirect, h.maxChars)
	}
	if strings.TrimSpace(redirect) == "" {
		return false, nil, ""
	}
	h.used++
	return false, []string{redirect}, ""
}

// Used 返回本轮已注入的拒答重定向次数（活动流/测试用）。
func (h *InjectHook) Used() int { return h.used }

// lastAssistantText 取最后一条助手消息的文本（忽略带工具调用的回合）。
func lastAssistantText(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role != llm.RoleAssistant {
			continue
		}
		if hasToolUse(m) {
			return ""
		}
		var b strings.Builder
		for _, c := range m.Content {
			if c.Type == llm.BlockText && strings.TrimSpace(c.Text) != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(c.Text)
			}
		}
		return b.String()
	}
	return ""
}

func hasToolUse(m llm.Message) bool {
	for _, c := range m.Content {
		if c.Type == llm.BlockToolUse {
			return true
		}
	}
	return false
}

// WrapHooks 按当前配置包装钩子；off 模式或 inner=nil 且不需要拒答处理时返回原值。
func WrapHooks(inner harness.HookRunner, goal func() string) harness.HookRunner {
	c := resolveInjectConfig()
	if c.Mode == inject.ModeOff || (inner == nil && !c.Mode.ShouldRetryRefusal()) {
		return inner
	}
	return NewInjectHook(inner, c.Mode, goal, c.MaxChars, c.MaxRetries, c.Custom)
}

// WrapProvider 按当前配置给 provider 挂上「按需注入」装饰器（on-miss/always/hybrid）。
// off / on-refusal 原样返回（后者只走 Stop hook 的拒答重定向）。
func WrapProvider(prov llm.Provider, goal func() string) llm.Provider {
	c := resolveInjectConfig()
	if prov == nil || !c.Mode.ShouldCheckRequest() {
		return prov
	}
	return inject.NewInjector(prov, c.Mode, goal, c.MaxChars)
}

func clipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if n <= 0 || len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
