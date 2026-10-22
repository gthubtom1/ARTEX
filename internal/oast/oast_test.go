package oast

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func startTestServer(t *testing.T) *Server {
	t.Helper()
	s := New(Options{HTTPAddr: "127.0.0.1:0", DNSAddr: "127.0.0.1:0", AnswerIP: "10.9.8.7"})
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestHTTPHit(t *testing.T) {
	s := startTestServer(t)
	tok := s.NewToken()
	if tok.ID == "" || tok.HTTPURL == "" {
		t.Fatalf("bad token: %+v", tok)
	}
	resp, err := http.Get(tok.HTTPURL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hits := s.Hits(tok.ID); len(hits) > 0 {
			if hits[0].Proto != "http" {
				t.Fatalf("want http hit, got %+v", hits[0])
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no http hit recorded")
}

func TestDNSHit(t *testing.T) {
	s := startTestServer(t)
	tok := s.NewToken()
	if tok.DNSHost == "" {
		t.Fatal("empty DNSHost")
	}
	// DNSHost 形如 "<id>.<host>[:port]"
	name, server, err := splitDNSHost(tok.DNSHost)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	c := &dns.Client{Timeout: 2 * time.Second}
	resp, _, err := c.Exchange(m, server)
	if err != nil {
		t.Fatalf("dns exchange: %v", err)
	}
	if len(resp.Answer) == 0 {
		t.Fatal("no A answer")
	}
	if !strings.Contains(resp.Answer[0].String(), "10.9.8.7") {
		t.Fatalf("answer ip mismatch: %s", resp.Answer[0].String())
	}
	hits := s.Hits(tok.ID)
	if len(hits) == 0 || hits[0].Proto != "dns" {
		t.Fatalf("want dns hit, got %+v", hits)
	}
}

func splitDNSHost(h string) (name, server string, err error) {
	host, port, e := net.SplitHostPort(h)
	if e != nil {
		// 无端口：用 53
		return h, net.JoinHostPort(h[strings.Index(h, ".")+1:], "53"), nil
	}
	// host = "<id>.<ip>"
	idx := strings.Index(host, ".")
	if idx < 0 {
		return "", "", &net.AddrError{Err: "bad dns host", Addr: h}
	}
	return host, net.JoinHostPort(host[idx+1:], port), nil
}

func TestUnknownTokenNoHit(t *testing.T) {
	s := startTestServer(t)
	resp, err := http.Get("http://" + s.HTTPAddr() + "/deadbeef")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if hits := s.AllHits(); len(hits) != 0 {
		t.Fatalf("unknown token must not record, got %+v", hits)
	}
}

func TestCloseTwice(t *testing.T) {
	s := New(Options{HTTPAddr: "127.0.0.1:0"})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	_ = context.Background()
}
