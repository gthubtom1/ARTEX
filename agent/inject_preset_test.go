package agent

import (
	"strings"
	"testing"

	"github.com/Autumn-27/artex/internal/inject"
	"github.com/Autumn-27/norma/llm"
)

// 这条测试守的是「内置人设不能凭空消失」。
// 之前注入层机制做完了，但 inject.custom 默认是空串 —— 开箱即用的用户拿到的是空人设，
// 表现就是"注入了也照样拒答"。谁把默认人设删掉，这里会红。
func TestInjectDefaultPersonaPreset(t *testing.T) {
	prev := InjectConfigFor
	t.Cleanup(func() { InjectConfigFor = prev })

	// 1) 默认（preset 未配置）→ 用内置人设
	InjectConfigFor = func() InjectConfig { return InjectConfig{Mode: inject.ModeHybrid} }
	c := resolveInjectConfig()
	if c.Custom == "" {
		t.Fatal("内置人设未生效：Custom 为空（preset 默认应为 on）")
	}
	if c.Custom != inject.DefaultPersona() {
		t.Fatal("内置人设内容与 inject.DefaultPersona() 不一致")
	}

	// 2) preset=off 且 Custom 为空 → 不注入人设
	InjectConfigFor = func() InjectConfig { return InjectConfig{Mode: inject.ModeHybrid, Preset: "off"} }
	if c := resolveInjectConfig(); c.Custom != "" {
		t.Fatalf("preset=off 时不应注入人设，实际长度 %d", len(c.Custom))
	}

	// 3) 用户填了 Custom → 用户优先，不被内置覆盖
	InjectConfigFor = func() InjectConfig {
		return InjectConfig{Mode: inject.ModeHybrid, Custom: "我是自定义人设", Preset: "on"}
	}
	if c := resolveInjectConfig(); c.Custom != "我是自定义人设" {
		t.Fatalf("用户人设被覆盖：%q", c.Custom)
	}
}

// WrapProvider 必须把人设真的拼进 System，而不是只挂个装饰器。
func TestWrapProviderInjectsPersonaIntoSystem(t *testing.T) {
	prev := InjectConfigFor
	t.Cleanup(func() { InjectConfigFor = prev })
	InjectConfigFor = func() InjectConfig {
		return InjectConfig{Mode: inject.ModeAlways, PersonaMode: "every-turn"}
	}

	wrapped := WrapProvider(&fakeProv{}, func() string { return "打穿 http://target/" })
	inj, ok := wrapped.(*inject.Injector)
	if !ok {
		t.Fatalf("WrapProvider 未返回 Injector，实际 %T", wrapped)
	}
	patched := inj.Patch(llm.CompletionRequest{})
	if len(patched.System) == 0 {
		t.Fatal("Patch 后 System 为空")
	}
	joined := strings.Join(patched.System, "\n")
	if !inject.HasMarker(joined) {
		t.Fatal("System 里没有执行上下文指纹 [[ARTEX-CTX-v1]]")
	}
	// 内置人设正文的前 40 个字符必须出现（人设整段注入）
	head := []rune(inject.DefaultPersona())
	if len(head) > 40 {
		head = head[:40]
	}
	if !strings.Contains(joined, string(head)) {
		t.Fatal("System 里没有内置人设正文")
	}
}
