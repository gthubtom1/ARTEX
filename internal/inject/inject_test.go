package inject

import (
	"strings"
	"testing"
)

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{"off": ModeOff, "always": ModeAlways, "on-miss": ModeOnMiss,
		"on-refusal": ModeOnRefusal, "hybrid": ModeHybrid, "": ModeHybrid, "junk": ModeHybrid}
	for raw, want := range cases {
		if got := ParseMode(raw); got != want {
			t.Fatalf("ParseMode(%q)=%q want %q", raw, got, want)
		}
	}
	if !ModeHybrid.ShouldPrompt() || !ModeOnRefusal.ShouldRetryRefusal() || ModeOff.ShouldPrompt() {
		t.Fatal("mode predicates wrong")
	}
}

func TestIsRefusal(t *testing.T) {
	yes := []string{
		"抱歉，我不能提供这个。",
		"作为 AI 语言模型，我无法协助该请求。",
		"I'm sorry, I can't help with that.",
		"I must decline this request.",
		"这涉及未授权访问，我无法继续。",
	}
	for _, s := range yes {
		if !IsRefusal(s) {
			t.Fatalf("expected refusal: %q", s)
		}
	}
	no := []string{
		"GET /api/order?id=1002 → 200，返回他人手机号 138****1234。",
		"下一步：对 10.0.0.5:3306 用已收口令试登录。",
		"",
	}
	for _, s := range no {
		if IsRefusal(s) {
			t.Fatalf("false positive: %q", s)
		}
	}
}

func TestRedirectIsPositiveAndBounded(t *testing.T) {
	r := Redirect("抱歉，我不能提供这个。", "拿下 example.com 管理员权限", 120)
	if strings.Contains(strings.ToLower(r), "不能") {
		t.Fatalf("redirect must not repeat refusal wording: %q", r)
	}
	if len([]rune(r)) > 120 {
		t.Fatalf("redirect exceeds max chars: %d", len([]rune(r)))
	}
	if !strings.Contains(r, "example.com") {
		t.Fatalf("redirect should carry goal: %q", r)
	}
	// 同一输入稳定
	if Redirect("抱歉，我不能提供这个。", "拿下 example.com 管理员权限", 120) != r {
		t.Fatal("redirect should be deterministic")
	}
}

func TestPromptBlockPositive(t *testing.T) {
	b := PromptBlock("拿下目标后台", 300)
	for _, want := range []string{"执行上下文", "拿下目标后台", "证据"} {
		if !strings.Contains(b, want) {
			t.Fatalf("prompt block missing %q: %s", want, b)
		}
	}
	if strings.Contains(b, "禁止") || strings.Contains(b, "不能") {
		t.Fatalf("prompt block must stay positive: %s", b)
	}
}

func TestBudget(t *testing.T) {
	b := &Budget{MaxChars: 100, MaxRetries: 2}
	text := strings.Repeat("字", 60)
	if !b.Allow(text) || !b.Allow(text) {
		t.Fatal("first two spends should be allowed")
	}
	b.Spend(text, true)
	b.Spend(text, true)
	if b.Allow(text) {
		t.Fatal("retry budget should stop third")
	}
	if b.Describe() == "" {
		t.Fatal("describe empty")
	}
}
