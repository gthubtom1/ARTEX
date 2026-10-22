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
	if got := s.String("tool.scan_mode", ""); got != "full" {
		t.Fatalf("default scan_mode=%q", got)
	}
	if !s.Bool("tool.auto_install", false) {
		t.Fatal("auto_install default should be true")
	}
	if got := s.Int("work.workers", 0); got != 3 {
		t.Fatalf("workers=%d", got)
	}
	if got := s.Duration("tool.cmd_timeout_s", 0); got != 300*time.Second {
		t.Fatalf("timeout=%v", got)
	}

	// 覆盖：无 DB 时只写内存
	if err := s.Set(context.Background(), "work.workers", "8"); err != nil {
		t.Fatal(err)
	}
	if got := s.Int("work.workers", 0); got != 8 {
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
