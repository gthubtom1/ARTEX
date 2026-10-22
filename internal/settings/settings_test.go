package settings

import (
	"context"
	"testing"
	"time"
)

func TestDefaultsAndOverrides(t *testing.T) {
	s := New(nil)
	if err := s.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 2026-10-23：原来断言的 tool.scan_mode / tool.auto_install / tool.cmd_timeout_s
	// 属于"有默认值但全仓无读者"的假旋钮，已从 Defaults 删除（见 docs/03 §5.8）。
	// 这里改为断言**真有消费者**的键。
	if got := s.String("inject.mode", ""); got != "hybrid" {
		t.Fatalf("default inject.mode=%q", got)
	}
	if s.Bool("adviser.enabled", true) {
		t.Fatal("adviser.enabled 默认必须是 false（零额外 LLM 调用）")
	}
	if got := s.Int("workers", 0); got != 3 {
		t.Fatalf("workers=%d", got)
	}
	if got := s.Int("asset.recheck_minutes", 0); got != 60 {
		t.Fatalf("recheck_minutes=%d", got)
	}
	if got := s.Duration("net.verify_every_s", 0); got != 300*time.Second {
		t.Fatalf("verify_every=%v", got)
	}

	// 覆盖：无 DB 时只写内存
	if err := s.Set(context.Background(), "workers", "8"); err != nil {
		t.Fatal(err)
	}
	if got := s.Int("workers", 0); got != 8 {
		t.Fatalf("override failed: %d", got)
	}
}

func TestListAndUnknownKey(t *testing.T) {
	s := New(nil)
	_ = s.Load(context.Background())
	urls := s.List("net.verify_urls")
	if len(urls) != 2 {
		t.Fatalf("verify urls=%v", urls)
	}
	if got := s.String("no.such.key", "fallback"); got != "fallback" {
		t.Fatalf("unknown key=%q", got)
	}
	if got := s.Int("no.such.key", 42); got != 42 {
		t.Fatalf("unknown int=%d", got)
	}
}

func TestNoPermissionFlags(t *testing.T) {
	// 纪律：settings 里不得出现权限开关/风险分级（能力默认全开）。
	banned := []string{"allow.", "deny.", "dangerous.", "tier.", "permission."}
	for k := range Defaults {
		for _, b := range banned {
			if len(k) >= len(b) && k[:len(b)] == b {
				t.Fatalf("settings must not carry permission switches: %s", k)
			}
		}
	}
}
