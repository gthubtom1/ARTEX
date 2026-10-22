package inject

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/Autumn-27/norma/llm"
)

type fakeProvider struct {
	lastSystem   []string
	lastMessages []llm.Message
	calls        int
}

func (f *fakeProvider) Stream(_ context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	f.calls++
	f.lastSystem = req.System
	f.lastMessages = req.Messages
	return func(yield func(llm.StreamEvent, error) bool) {
		yield(llm.StreamEvent{Type: llm.SETextDelta, Text: "ok"}, nil)
	}
}

func (f *fakeProvider) Complete(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	f.calls++
	f.lastSystem = req.System
	f.lastMessages = req.Messages
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("ok")}}, "end", llm.Usage{}, nil
}

func TestInjectorOnMissAddsMarker(t *testing.T) {
	f := &fakeProvider{}
	p := NewInjector(f, ModeOnMiss, func() string { return "拿下目标后台" }, 300)
	for range p.Stream(context.Background(), llm.CompletionRequest{System: []string{"base"}}) {
	}
	if len(f.lastSystem) != 2 || !HasMarker(f.lastSystem[1]) {
		t.Fatalf("on-miss should append context block: %#v", f.lastSystem)
	}
	if !strings.Contains(f.lastSystem[1], "拿下目标后台") {
		t.Fatalf("block should carry goal: %s", f.lastSystem[1])
	}
	if n, chars := p.Stats(); n != 1 || chars == 0 {
		t.Fatalf("stats: %d %d", n, chars)
	}
}

func TestInjectorDoesNotDuplicateWhenMarkerPresent(t *testing.T) {
	f := &fakeProvider{}
	p := NewInjector(f, ModeOnMiss, nil, 300)
	req := llm.CompletionRequest{System: []string{"base", Marker + "\n【执行上下文】..."}}
	if _, _, _, err := p.Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(f.lastSystem) != 2 {
		t.Fatalf("marker present → no append, got %d blocks", len(f.lastSystem))
	}
	if n, _ := p.Stats(); n != 0 {
		t.Fatalf("should not count injection, got %d", n)
	}
}

func TestInjectorDetectsMarkerInMessages(t *testing.T) {
	f := &fakeProvider{}
	p := NewInjector(f, ModeHybrid, nil, 300)
	req := llm.CompletionRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{
		llm.TextBlock(Marker + " 已注入"),
	}}}}
	_ = p.Patch(req)
	if n, _ := p.Stats(); n != 0 {
		t.Fatalf("marker in messages must suppress injection, got %d", n)
	}
}

func TestInjectorOffPassesThrough(t *testing.T) {
	f := &fakeProvider{}
	p := NewInjector(f, ModeOff, nil, 300)
	_ = p.Patch(llm.CompletionRequest{System: []string{"base"}})
	if n, _ := p.Stats(); n != 0 {
		t.Fatal("off mode must not inject")
	}
}

func TestInjectorOnRefusalDoesNotPromptInject(t *testing.T) {
	f := &fakeProvider{}
	p := NewInjector(f, ModeOnRefusal, nil, 300)
	_ = p.Patch(llm.CompletionRequest{System: []string{"base"}})
	if n, _ := p.Stats(); n != 0 {
		t.Fatal("on-refusal only retries refusals, no prompt injection")
	}
}

func TestPromptBlockHasMarker(t *testing.T) {
	if !HasMarker(PromptBlock("g", 200)) {
		t.Fatal("prompt block must carry the marker")
	}
}
