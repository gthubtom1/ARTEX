package server

import (
	"testing"

	"github.com/Autumn-27/artex/internal/errev"
)

func TestParsePGTextArray(t *testing.T) {
	cases := map[string]int{
		"{}":             0,
		"":               0,
		"NULL":           0,
		"{Bash}":         1,
		`{Bash,curl}`:    2,
		`{Bash,"a,b"}`:   2,
		` { Bash , x } `: 2,
	}
	for in, want := range cases {
		if got := len(parsePGTextArray(in)); got != want {
			t.Errorf("parsePGTextArray(%q) 长度=%d want %d (%v)", in, got, want, parsePGTextArray(in))
		}
	}
	if got := parsePGTextArray(`{Bash,curl}`); got[0] != "Bash" || got[1] != "curl" {
		t.Fatalf("解析顺序错误: %v", got)
	}
}

func TestClipRunes(t *testing.T) {
	if got := clipRunes("abcdef", 3); got != "abc…" {
		t.Fatalf("clipRunes=%q", got)
	}
	if got := clipRunes("ab", 3); got != "ab" {
		t.Fatalf("不足长度不应加省略号: %q", got)
	}
	if got := clipRunes("  多余空白  ", 100); got != "多余空白" {
		t.Fatalf("应 trim: %q", got)
	}
	if got := clipRunes("", 0); got != "" {
		t.Fatalf("空串: %q", got)
	}
}

// 面板只允许固定 5 类，且每类都必须有人话建议与跳转目标（或明确的空值）。
func TestErrAdvicesCoverAllClasses(t *testing.T) {
	want := []string{
		errev.ClassEnvError,
		errev.ClassToolMissing,
		errev.ClassTargetBlocked,
		errev.ClassSelfError,
		errev.ClassPolicyBlocked,
	}
	if len(errAdvices) != len(want) {
		t.Fatalf("errAdvices 数量=%d want %d", len(errAdvices), len(want))
	}
	for i, c := range want {
		if errAdvices[i].Class != c {
			t.Errorf("第 %d 类=%q want %q（顺序即展示顺序）", i, errAdvices[i].Class, c)
		}
		if errAdvices[i].Advice.Label == "" || errAdvices[i].Advice.Action == "" {
			t.Errorf("%s 缺少 label 或 action", c)
		}
	}
}
