// Package oast 提供带外（Out-of-Band）回调通道：判断"无回显"的漏洞是否真的触发。
//
// 两种模式：
//   - HTTP：起一个本地监听，每个 token 生成唯一 URL；目标回连即记一条 Hit。
//   - DNS：用 miekg/dns 起一个 UDP/TCP 应答器，任何查询都记一条 Hit 并回 A 记录。
//
// 用法（内网/靶场）：
//
//	srv := oast.New(oast.Options{HTTPAddr: "0.0.0.0:8888", DNSAddr: "0.0.0.0:5353", AnswerIP: "10.0.0.5"})
//	_ = srv.Start(ctx)
//	tok, urls := srv.NewToken()
//	// 把 urls 塞进 payload；稍后：
//	hits := srv.Hits(tok.ID)
//
// 公网场景把 AnswerIP 设成你的公网 IP，并在防火墙上放行端口；或用内网跳板机监听。
package oast

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Options 配置监听地址。端口 0 = 随机端口（测试用）。
type Options struct {
	HTTPAddr string       // 例：0.0.0.0:8888；空 = 不起 HTTP
	DNSAddr  string       // 例：0.0.0.0:5353；空 = 不起 DNS
	AnswerIP string       // DNS 应答的 A 记录地址（空则用 127.0.0.1）
	MaxHits  int          // 每 token 最多保留多少条（默认 200）
	TTL      time.Duration // token 有效期（默认 24h）
}

// Token 是一次带外探测的句柄。
type Token struct {
	ID        string    `json:"id"`
	HTTPURL   string    `json:"http_url,omitempty"`
	DNSHost   string    `json:"dns_host,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Hit 是一次回调记录。
type Hit struct {
	TokenID string    `json:"token_id"`
	Proto   string    `json:"proto"` // http|dns
	Source  string    `json:"source"`
	Detail  string    `json:"detail"`
	At      time.Time `json:"at"`
}

type tokenState struct {
	tok  Token
	hits []Hit
}

// Server 是带外通道服务。
type Server struct {
	opts Options

	mu     sync.RWMutex
	tokens map[string]*tokenState

	httpSrv  *http.Server
	httpAddr string
	dnsSrv   *dns.Server
	dnsTCP   *dns.Server
	dnsAddr  string
	closed   bool
}

// New 创建（未启动）。
func New(opts Options) *Server {
	if opts.MaxHits <= 0 {
		opts.MaxHits = 200
	}
	if opts.TTL <= 0 {
		opts.TTL = 24 * time.Hour
	}
	if strings.TrimSpace(opts.AnswerIP) == "" {
		opts.AnswerIP = "127.0.0.1"
	}
	return &Server{opts: opts, tokens: map[string]*tokenState{}}
}

// Start 启动监听（HTTP / DNS 可分别关闭）。返回后 HTTPAddr()/DNSAddr() 可用。
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("oast: server already closed")
	}
	if s.opts.HTTPAddr != "" {
		ln, err := net.Listen("tcp", s.opts.HTTPAddr)
		if err != nil {
			return fmt.Errorf("oast: http listen: %w", err)
		}
		s.httpAddr = ln.Addr().String()
		mux := http.NewServeMux()
		mux.HandleFunc("/", s.handleHTTP)
		s.httpSrv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = s.httpSrv.Serve(ln) }()
	}
	if s.opts.DNSAddr != "" {
		pc, err := net.ListenPacket("udp", s.opts.DNSAddr)
		if err != nil {
			return fmt.Errorf("oast: dns listen: %w", err)
		}
		s.dnsAddr = pc.LocalAddr().String()
		h := dns.HandlerFunc(s.handleDNS)
		s.dnsSrv = &dns.Server{PacketConn: pc, Handler: h}
		go func() { _ = s.dnsSrv.ActivateAndServe() }()
	}
	return nil
}

// HTTPAddr / DNSAddr 返回实际监听地址（含随机端口）。
func (s *Server) HTTPAddr() string { s.mu.RLock(); defer s.mu.RUnlock(); return s.httpAddr }
func (s *Server) DNSAddr() string  { s.mu.RLock(); defer s.mu.RUnlock(); return s.dnsAddr }

// NewToken 生成一个新 token 及其可用 URL / 域名。
func (s *Server) NewToken() Token {
	id := randHex(8)
	t := Token{ID: id, CreatedAt: time.Now()}
	s.mu.Lock()
	if s.httpAddr != "" {
		t.HTTPURL = fmt.Sprintf("http://%s/%s", s.httpAddr, id)
	}
	if s.dnsAddr != "" {
		host, port, err := net.SplitHostPort(s.dnsAddr)
		if err == nil {
			if host == "0.0.0.0" || host == "::" || host == "" {
				host = "127.0.0.1"
			}
			t.DNSHost = fmt.Sprintf("%s.%s", id, host)
			if port != "53" {
				t.DNSHost = fmt.Sprintf("%s:%s", t.DNSHost, port)
			}
		}
	}
	s.tokens[id] = &tokenState{tok: t}
	s.mu.Unlock()
	return t
}

// Hits 返回某 token 的回调记录（副本）。
func (s *Server) Hits(id string) []Hit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.tokens[id]
	if st == nil {
		return nil
	}
	out := make([]Hit, len(st.hits))
	copy(out, st.hits)
	return out
}

// AllHits 返回所有 token 的回调（按时间升序）。
func (s *Server) AllHits() []Hit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Hit
	for _, st := range s.tokens {
		out = append(out, st.hits...)
	}
	return out
}

// Close 停止监听。
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	hs, ds := s.httpSrv, s.dnsSrv
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if hs != nil {
		_ = hs.Shutdown(ctx)
	}
	if ds != nil {
		_ = ds.Shutdown()
	}
	return nil
}

// ───────────────────────── handlers ─────────────────────────

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	id := strings.SplitN(path, "/", 2)[0]
	s.record(id, Hit{Proto: "http", Source: clientIP(r), Detail: r.Method + " " + r.URL.String()})
	// 回一个无害 200，避免目标侧报错中断流程
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleDNS(w dns.ResponseWriter, req *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(req)
	if len(req.Question) > 0 {
		q := req.Question[0]
		first := strings.SplitN(strings.ToLower(q.Name), ".", 2)[0]
		s.record(first, Hit{Proto: "dns", Source: w.RemoteAddr().String(), Detail: q.Name + " " + dns.TypeToString[q.Qtype]})
		if q.Qtype == dns.TypeA {
			rr, err := dns.NewRR(fmt.Sprintf("%s 60 IN A %s", q.Name, s.opts.AnswerIP))
			if err == nil {
				m.Answer = append(m.Answer, rr)
			}
		}
	}
	_ = w.WriteMsg(m)
}

func (s *Server) record(id string, h Hit) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.tokens[id]
	if st == nil {
		return
	}
	if time.Since(st.tok.CreatedAt) > s.opts.TTL {
		return
	}
	if len(st.hits) >= s.opts.MaxHits {
		return
	}
	h.TokenID = id
	h.At = time.Now()
	st.hits = append(st.hits, h)
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
