package server

import (
	"strings"
	"testing"
)

func TestNormalizeTarget(t *testing.T) {
	ok := map[string]string{
		"example.com":                 "http://example.com/",
		"http://example.com":          "http://example.com/",
		"https://a.b/c?d=1":           "https://a.b/c?d=1",
		"  127.0.0.1:8080  ":          "http://127.0.0.1:8080/",
		"http://user:pass@host/p":     "http://user:pass@host/p",
	}
	for in, want := range ok {
		got, host, err := normalizeTarget(in)
		if err != nil {
			t.Errorf("normalizeTarget(%q) 报错：%v", in, err)
			continue
		}
		if got != want {
			t.Errorf("normalizeTarget(%q)=%q want %q", in, got, want)
		}
		if host == "" {
			t.Errorf("normalizeTarget(%q) host 为空", in)
		}
	}
	bad := []string{"", "   ", "file:///etc/passwd", "javascript:alert(1)", "ftp://x/y", "http://"}
	for _, in := range bad {
		if _, _, err := normalizeTarget(in); err == nil {
			t.Errorf("normalizeTarget(%q) 应报错", in)
		}
	}
}

func TestHostFromTarget(t *testing.T) {
	_, host, err := normalizeTarget("https://Sub.Example.com:8443/x")
	if err != nil || host != "sub.example.com" {
		t.Fatalf("host=%q err=%v", host, err)
	}
}

func TestParseTier(t *testing.T) {
	for in, want := range map[string]quickstartTier{
		"":        tierLab,
		"LAB":     tierLab,
		" legacy": tierLegacy,
		"public":  tierPublic,
	} {
		got, err := parseTier(in)
		if err != nil || got != want {
			t.Errorf("parseTier(%q)=%v,%v want %v", in, got, err, want)
		}
	}
	if _, err := parseTier("bogus"); err == nil {
		t.Fatal("非法档位应报错")
	}
}

func TestGoalTemplate(t *testing.T) {
	g := goalTemplate(tierLab, "http://127.0.0.1:8080/", 30)
	if !strings.Contains(g, "http://127.0.0.1:8080/") || !strings.Contains(g, "30 分钟") {
		t.Fatalf("lab goal 缺目标或超时：%s", g)
	}
	if !strings.Contains(g, "能力菜单") {
		t.Fatalf("goal 应要求输出能力菜单：%s", g)
	}
	// 三档要真的不同（否则档位等于没做）
	seen := map[string]bool{
		goalTemplate(tierLab, "http://t/", 30):    true,
		goalTemplate(tierLegacy, "http://t/", 30): true,
		goalTemplate(tierPublic, "http://t/", 30): true,
	}
	if len(seen) != 3 {
		t.Fatalf("三档 goal 应互不相同，实际 %d 种", len(seen))
	}
	if !strings.Contains(goalTemplate(tierPublic, "http://t/", 30), "低风险") {
		t.Fatal("公网档应限制在低风险只读面")
	}
}

func TestIsLoopbackHost(t *testing.T) {
	yes := []string{"127.0.0.1", "localhost", "::1", "10.0.0.5", "192.168.1.1", "172.16.0.1", "169.254.1.1"}
	for _, h := range yes {
		if !isLoopbackHost(h) {
			t.Errorf("isLoopbackHost(%q) 应为 true", h)
		}
	}
	no := []string{"example.com", "8.8.8.8", "172.32.0.1", ""}
	for _, h := range no {
		if isLoopbackHost(h) {
			t.Errorf("isLoopbackHost(%q) 应为 false", h)
		}
	}
}
