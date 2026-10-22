package netguard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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

func TestRequireProxyWithoutProxyFailsClosed(t *testing.T) {
	_, err := New(fixedCfg([]string{"http://127.0.0.1:1"}, func(c *Config) {
		c.RequireProxy = true
	}), nil).Verify(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fail-closed") {
		t.Fatalf("want fail-closed error, got %v", err)
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

func TestClashVersionSwitchAndDelay(t *testing.T) {
	var switched string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "v1.19.0", "meta": true})
		case strings.HasPrefix(r.URL.Path, "/proxies/") && strings.HasSuffix(r.URL.Path, "/delay"):
			_ = json.NewEncoder(w).Encode(map[string]any{"delay": 120})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/proxies/"):
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			switched = body["name"]
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(r.URL.Path, "/proxies/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"now": "HK-01", "type": "Selector", "all": []string{"HK-01", "JP-02"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	g := New(fixedCfg(nil, func(c *Config) { c.Controller = srv.URL; c.Secret = "s" }), nil)
	ctx := context.Background()

	if v, err := g.ClashVersion(ctx); err != nil || v != "v1.19.0" {
		t.Fatalf("version=%q err=%v", v, err)
	}
	if node, err := g.CurrentNode(ctx, "GLOBAL"); err != nil || node != "HK-01" {
		t.Fatalf("node=%q err=%v", node, err)
	}
	if d, err := g.Delay(ctx, "HK-01"); err != nil || d != 120 {
		t.Fatalf("delay=%d err=%v", d, err)
	}
	if err := g.Switch(ctx, "GLOBAL", "JP-02"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if switched != "JP-02" {
		t.Fatalf("switch body not received: %q", switched)
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
