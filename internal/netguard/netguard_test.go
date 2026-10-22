package netguard

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 说明（D1）：本包已删掉 Clash 外部控制（version/proxies/delay/switch）与
// 「必须配代理否则 fail-closed」的语义，相应测试一并删除 —— 应用内不再管出口。
// 现在测的是「纯自检」：互证出口 IP、拒绝内网 IP、白名单、基准比对。

func ipServer(ip string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, ip+"\n")
	}))
}

func fixedCfg(urls []string, mutate func(*Config)) func() Config {
	return func() Config {
		c := Config{VerifyURLs: urls, Timeout: 3 * time.Second}
		if mutate != nil {
			mutate(&c)
		}
		return c
	}
}

func TestVerifyOK(t *testing.T) {
	a, b := ipServer("1.2.3.4"), ipServer("1.2.3.4")
	defer a.Close()
	defer b.Close()

	g := New(fixedCfg([]string{a.URL, b.URL}, nil), nil)
	st, err := g.Verify(context.Background())
	if err != nil {
		t.Fatalf("want ok, got %v", err)
	}
	if !st.OK || st.EgressIP != "1.2.3.4" {
		t.Fatalf("unexpected status: %+v", st)
	}
	if got := g.Last(); got.EgressIP != "1.2.3.4" {
		t.Fatalf("Last() not updated: %+v", got)
	}
}

// 首次成功要自动记下基准（net.baseline_ip）。
func TestVerifyRecordsBaselineOnFirstSuccess(t *testing.T) {
	a, b := ipServer("1.2.3.4"), ipServer("1.2.3.4")
	defer a.Close()
	defer b.Close()

	var saved string
	g := New(fixedCfg([]string{a.URL, b.URL}, func(c *Config) {
		c.SetBaseline = func(_ context.Context, ip string) error { saved = ip; return nil }
	}), nil)

	st, err := g.Verify(context.Background())
	if err != nil {
		t.Fatalf("want ok, got %v", err)
	}
	if !st.OK || st.BaselineIP != "1.2.3.4" {
		t.Fatalf("baseline not adopted: %+v", st)
	}
	if saved != "1.2.3.4" {
		t.Fatalf("SetBaseline not called, got %q", saved)
	}
}

// 出口变了：OK=false（显形），Detail 写清 baseline 与现值。
func TestVerifyBaselineMismatchShowsBothIPs(t *testing.T) {
	a, b := ipServer("5.6.7.8"), ipServer("5.6.7.8")
	defer a.Close()
	defer b.Close()

	called := false
	g := New(fixedCfg([]string{a.URL, b.URL}, func(c *Config) {
		c.Baseline = "1.2.3.4"
		c.SetBaseline = func(context.Context, string) error { called = true; return nil }
	}), nil)

	st, err := g.Verify(context.Background())
	if err == nil || !strings.Contains(err.Error(), "出口 IP 变了") {
		t.Fatalf("want baseline-changed error, got %v", err)
	}
	if st.OK {
		t.Fatalf("want OK=false on baseline change, got %+v", st)
	}
	if !strings.Contains(st.Detail, "baseline=1.2.3.4") || !strings.Contains(st.Detail, "现在=5.6.7.8") {
		t.Fatalf("detail must show both IPs, got %q", st.Detail)
	}
	if called {
		t.Fatalf("baseline must not be overwritten on mismatch")
	}
}

// 出口没变：ok。
func TestVerifyBaselineMatch(t *testing.T) {
	a, b := ipServer("1.2.3.4"), ipServer("1.2.3.4")
	defer a.Close()
	defer b.Close()

	st, err := New(fixedCfg([]string{a.URL, b.URL}, func(c *Config) { c.Baseline = "1.2.3.4" }), nil).
		Verify(context.Background())
	if err != nil || !st.OK {
		t.Fatalf("want ok, got st=%+v err=%v", st, err)
	}
	if st.BaselineIP != "1.2.3.4" {
		t.Fatalf("baseline not echoed: %+v", st)
	}
}

func TestVerifySplitRouting(t *testing.T) {
	a, b := ipServer("1.2.3.4"), ipServer("5.6.7.8")
	defer a.Close()
	defer b.Close()

	_, err := New(fixedCfg([]string{a.URL, b.URL}, nil), nil).Verify(context.Background())
	if err == nil || !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("want split-routing error, got %v", err)
	}
}

func TestVerifyRejectsPrivateIP(t *testing.T) {
	a, b := ipServer("192.168.1.5"), ipServer("192.168.1.5")
	defer a.Close()
	defer b.Close()

	_, err := New(fixedCfg([]string{a.URL, b.URL}, nil), nil).Verify(context.Background())
	if err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("want private-ip rejection, got %v", err)
	}
}

// 没有配代理也要能跑（D1：不再 fail-closed）。
func TestVerifyWithoutProxyStillWorks(t *testing.T) {
	a, b := ipServer("1.2.3.4"), ipServer("1.2.3.4")
	defer a.Close()
	defer b.Close()

	st, err := New(fixedCfg([]string{a.URL, b.URL}, nil), nil).Verify(context.Background())
	if err != nil || !st.OK {
		t.Fatalf("direct egress must pass without any proxy config: st=%+v err=%v", st, err)
	}
}

func TestAllowEgressList(t *testing.T) {
	a, b := ipServer("1.2.3.4"), ipServer("1.2.3.4")
	defer a.Close()
	defer b.Close()

	_, err := New(fixedCfg([]string{a.URL, b.URL}, func(c *Config) {
		c.AllowEgress = []string{"9.9.9.9"}
	}), nil).Verify(context.Background())
	if err == nil || !strings.Contains(err.Error(), "允许列表") {
		t.Fatalf("want allowlist error, got %v", err)
	}
}

// 缓存 TTL 语义不变：成功按 ttl、失败 min(ttl, 30s)。
func TestVerifyCachedTTL(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = io.WriteString(w, "1.2.3.4")
	}))
	defer srv.Close()

	g := New(fixedCfg([]string{srv.URL}, nil), nil)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := g.VerifyCached(ctx, time.Minute); err != nil {
			t.Fatalf("verify: %v", err)
		}
	}
	if hits != 1 {
		t.Fatalf("success should be cached for ttl, hits=%d", hits)
	}
}

func TestCheckIP(t *testing.T) {
	cases := []struct {
		ip  string
		err bool
	}{
		{"1.2.3.4", false},
		{"10.0.0.1", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"0.0.0.0", true},
		{"not-an-ip", true},
	}
	for _, c := range cases {
		err := checkIP(Config{}, c.ip)
		if (err != nil) != c.err {
			t.Errorf("%s: err=%v wantErr=%v", c.ip, err, c.err)
		}
	}
}
