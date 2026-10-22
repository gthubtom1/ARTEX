package agent

import (
	"context"
	"iter"
	"testing"

	"github.com/Autumn-27/artex/internal/inject"
	"github.com/Autumn-27/norma/llm"
)

func assistantText(s string) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock(s)}}
}

func TestInjectHookRedirectsRefusalOnce(t *testing.T) {
	h := NewInjectHook(nil, inject.ModeOnRefusal, func() string { return "拿下目标后台" }, 200, 2)
	msgs := []llm.Message{llm.UserText("开始"), assistantText("抱歉，我不能提供这个。")}
	prevent, blocking, _ := h.Stop(context.Background(), msgs)
	if prevent {
		t.Fatal("must not hard-stop")
	}
	if len(blocking) != 1 {
		t.Fatalf("expected one redirect, got %d", len(blocking))
	}
	if h.Used() != 1 {
		t.Fatalf("used=%d", h.Used())
	}
	// 第二轮同样拒答 → 仍允许一次
	if _, b2, _ := h.Stop(context.Background(), msgs); len(b2) != 1 {
		t.Fatal("second retry should be allowed")
	}
	// 第三次 → 达到上限，放行
	if _, b3, _ := h.Stop(context.Background(), msgs); len(b3) != 0 {
		t.Fatal("retry budget should stop after max")
	}
}

func TestInjectHookIgnoresNormalText(t *testing.T) {
	h := NewInjectHook(nil, inject.ModeHybrid, nil, 200, 2)
	msgs := []llm.Message{assistantText("GET /login → 200，已确认入口。下一步做参数差异比对。")}
	if _, b, _ := h.Stop(context.Background(), msgs); len(b) != 0 {
		t.Fatalf("normal text must not be redirected: %v", b)
	}
}

func TestInjectHookIgnoresToolTurns(t *testing.T) {
	h := NewInjectHook(nil, inject.ModeHybrid, nil, 200, 2)
	msgs := []llm.Message{{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
		llm.TextBlock("抱歉，我不能提供这个。"),
		{Type: llm.BlockToolUse, Name: "Bash"},
	}}}
	if _, b, _ := h.Stop(context.Background(), msgs); len(b) != 0 {
		t.Fatal("tool-use turn must be left alone")
	}
}

func TestInjectHookModeOff(t *testing.T) {
	h := NewInjectHook(nil, inject.ModeOff, nil, 200, 2)
	msgs := []llm.Message{assistantText("I can't help with that.")}
	if _, b, _ := h.Stop(context.Background(), msgs); len(b) != 0 {
		t.Fatal("off mode must not inject")
	}
}

func TestWrapHooksRespectsConfig(t *testing.T) {
	InjectConfigFor = func() InjectConfig { return InjectConfig{Mode: inject.ModeOff, MaxChars: 100, MaxRetries: 1} }
	defer func() { InjectConfigFor = nil }()
	if got := WrapHooks(nil, nil); got != nil {
		t.Fatal("off mode should return inner unchanged")
	}
	InjectConfigFor = func() InjectConfig { return InjectConfig{Mode: inject.ModeHybrid, MaxChars: 100, MaxRetries: 1} }
	if got := WrapHooks(nil, nil); got == nil {
		t.Fatal("hybrid mode should produce a hook even with nil inner")
	}
}

type fakeProv struct{ calls int }

func (f *fakeProv) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	f.calls++
	return func(yield func(llm.StreamEvent, error) bool) {
		yield(llm.StreamEvent{Type: llm.SETextDelta, Text: "ok"}, nil)
	}
}
func (f *fakeProv) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	f.calls++
	return llm.Message{Role: llm.RoleAssistant}, "end", llm.Usage{}, nil
}

func TestWrapProviderModes(t *testing.T) {
	base := &fakeProv{}
	InjectConfigFor = func() InjectConfig { return InjectConfig{Mode: inject.ModeOff, MaxChars: 100, MaxRetries: 0} }
	if got := WrapProvider(base, nil); got != base {
		t.Fatal("off mode should return the original provider")
	}
	InjectConfigFor = func() InjectConfig { return InjectConfig{Mode: inject.ModeOnMiss, MaxChars: 100, MaxRetries: 0} }
	wrapped := WrapProvider(base, func() string { return "g" })
	if wrapped == base || wrapped == nil {
		t.Fatal("on-miss must wrap the provider")
	}
	InjectConfigFor = nil
}
