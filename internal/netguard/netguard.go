// Package netguard 是出口守卫：确保所有对外流量都经过代理，并在出击前验证出口 IP。
//
// 三条铁律（docs/全局出口与网络守卫.md）：
//  1. 默认全代理：所有子进程/HTTP 客户端继承代理；
//  2. 出击前验 IP：两个独立源互证，必须是代理 IP，不能是本机/内网 IP；
//  3. 失效即停：代理不可用就直接报错（fail-closed），绝不回退直连。
//
// 本包只做"判定与记录"，不负责改系统代理；环境变量由调用方（agent.proxyEnv）注入。
package netguard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/internal/dbiface"
)

// Config 是出口守卫的运行参数（来自 settings，每次调用实时读取）。
type Config struct {
	ProxyHTTP    string        // 例：http://127.0.0.1:7897
	ProxySOCKS   string        // 例：socks5://127.0.0.1:7897（仅记录，客户端用 ProxyHTTP）
	Controller   string        // Clash 外部控制：http://127.0.0.1:9097
	Secret       string        // Clash secret（Bearer）
	VerifyURLs   []string      // 出口 IP 查询源（至少两个，互证）
	Timeout      time.Duration // 单次请求超时（默认 8s）
	RequireProxy bool          // true（默认）= 没有代理直接判失败
	AllowEgress  []string      // 可选：出口 IP 白名单（空=不限制具体 IP）
}

// Status 是一次校验的结果。
type Status struct {
	EgressIP string    `json:"egress_ip"`
	Node     string    `json:"node,omitempty"`
	OK       bool      `json:"ok"`
	At       time.Time `json:"at"`
	Detail   string    `json:"detail,omitempty"`
}

// Guard 持有配置解析器与最近一次状态。
type Guard struct {
	cfg func() Config
	db  dbiface.DB

	mu   sync.RWMutex
	last Status

	// VerifyCached 的缓存（成功按 ttl，失败按 min(ttl,30s)），
	// 让 net.verify_every_s 真正生效 —— 否则每轮规划都要打两次外网。
	cacheAt  time.Time
	cacheSt  Status
	cacheErr error
	cacheSet bool
}

func New(cfg func() Config, db dbiface.DB) *Guard { return &Guard{cfg: cfg, db: db} }

// Last 返回最近一次校验结果。
func (g *Guard) Last() Status {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.last
}

func (g *Guard) setLast(s Status) {
	g.mu.Lock()
	g.last = s
	g.mu.Unlock()
}

func (g *Guard) config() Config {
	c := g.cfg()
	if c.Timeout <= 0 {
		c.Timeout = 8 * time.Second
	}
	c.ProxyHTTP = strings.TrimSpace(c.ProxyHTTP)
	c.Controller = strings.TrimRight(strings.TrimSpace(c.Controller), "/")
	if len(c.VerifyURLs) == 0 {
		c.VerifyURLs = []string{"https://api.ipify.org", "https://ifconfig.me"}
	}
	return c
}

// Client 返回一个走代理的 HTTP 客户端（代理未配置且 RequireProxy=false 时直连）。
func (g *Guard) Client() (*http.Client, error) {
	c := g.config()
	tr := &http.Transport{
		Proxy:               nil, // 默认直连；配了代理才用代理（避免测试/环境变量干扰）
		MaxIdleConns:        8,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: c.Timeout,
	}
	if c.ProxyHTTP != "" {
		pu, err := url.Parse(c.ProxyHTTP)
		if err != nil {
			return nil, fmt.Errorf("netguard: bad proxy url %q: %w", c.ProxyHTTP, err)
		}
		tr.Proxy = http.ProxyURL(pu)
	} else if c.RequireProxy {
		return nil, fmt.Errorf("netguard: 未配置代理，且要求必须经代理出网（fail-closed）")
	}
	return &http.Client{Transport: tr, Timeout: c.Timeout}, nil
}

// Verify 校验出口 IP：
//   - 所有 VerifyURLs 必须返回同一个 IP（否则说明分流规则坏了）；
//   - 该 IP 不能是内网/回环/链路本地；
//   - 若配置了 AllowEgress，必须命中白名单。
//
// VerifyCached 是 Verify 的带缓存版本。
//
// 为什么需要：出口校验每次要打两个独立源（互证出口 IP），一次两个外网往返。
// 规划每轮都问一次的话，一天下来是几百次无用请求。成功结果缓存 ttl；
// 失败结果缓存 min(ttl, 30s) —— 代理刚恢复时不能等满 5 分钟才放行。
func (g *Guard) VerifyCached(ctx context.Context, ttl time.Duration) (Status, error) {
	if ttl <= 0 {
		return g.Verify(ctx)
	}
	now := time.Now()
	g.mu.RLock()
	if g.cacheSet {
		age := now.Sub(g.cacheAt)
		limit := ttl
		if g.cacheErr != nil && limit > 30*time.Second {
			limit = 30 * time.Second
		}
		if age < limit {
			st, err := g.cacheSt, g.cacheErr
			g.mu.RUnlock()
			return st, err
		}
	}
	g.mu.RUnlock()

	st, err := g.Verify(ctx)

	g.mu.Lock()
	g.cacheAt, g.cacheSt, g.cacheErr, g.cacheSet = time.Now(), st, err, true
	g.mu.Unlock()
	return st, err
}

// InvalidateCache 清掉缓存（改代理后用，让下一次立即真校验）。
func (g *Guard) InvalidateCache() {
	g.mu.Lock()
	g.cacheSet = false
	g.mu.Unlock()
}

func (g *Guard) Verify(ctx context.Context) (Status, error) {
	c := g.config()
	st := Status{At: time.Now()}
	cli, err := g.Client()
	if err != nil {
		st.Detail = err.Error()
		g.setLast(st)
		g.logEgress(ctx, st, err)
		return st, err
	}

	ips := make([]string, 0, len(c.VerifyURLs))
	for _, u := range c.VerifyURLs {
		ip, ferr := fetchIP(ctx, cli, u)
		if ferr != nil {
			st.Detail = fmt.Sprintf("%s: %v", u, ferr)
			g.setLast(st)
			g.logEgress(ctx, st, ferr)
			return st, fmt.Errorf("netguard: 出口校验失败（%s）: %w", u, ferr)
		}
		ips = append(ips, ip)
	}
	for i := 1; i < len(ips); i++ {
		if ips[i] != ips[0] {
			err := fmt.Errorf("netguard: 出口 IP 不一致（%s vs %s），分流规则可能坏了", ips[0], ips[i])
			st.Detail = err.Error()
			g.setLast(st)
			g.logEgress(ctx, st, err)
			return st, err
		}
	}
	if err := checkIP(c, ips[0]); err != nil {
		st.EgressIP = ips[0]
		st.Detail = err.Error()
		g.setLast(st)
		g.logEgress(ctx, st, err)
		return st, err
	}

	st.EgressIP = ips[0]
	st.OK = true
	st.Detail = "ok"
	// 尽力补上当前节点名（不影响校验结果）
	if node, nerr := g.CurrentNode(ctx, "GLOBAL"); nerr == nil {
		st.Node = node
	}
	g.setLast(st)
	g.logEgress(ctx, st, nil)
	return st, nil
}

// checkIP 拒绝内网/回环/链路本地出口，并校验白名单。
func checkIP(c Config, raw string) error {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return fmt.Errorf("netguard: 出口 IP 无法解析: %q", raw)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
		return fmt.Errorf("netguard: 出口 IP 是内网/本机地址 %s，说明流量没有走代理（fail-closed）", raw)
	}
	if len(c.AllowEgress) > 0 {
		for _, a := range c.AllowEgress {
			if strings.TrimSpace(a) == strings.TrimSpace(raw) {
				return nil
			}
		}
		return fmt.Errorf("netguard: 出口 IP %s 不在允许列表内", raw)
	}
	return nil
}

func fetchIP(ctx context.Context, cli *http.Client, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "curl/8")
	resp, err := cli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	// ifconfig.me 可能带换行/额外文本；取第一个字段
	if f := strings.Fields(s); len(f) > 0 {
		s = f[0]
	}
	if net.ParseIP(s) == nil {
		return "", fmt.Errorf("响应不是 IP: %q", truncate(s, 60))
	}
	return s, nil
}

// ───────────────────────── Clash 外部控制 API ─────────────────────────

func (g *Guard) clashDo(ctx context.Context, method, path string, body io.Reader, out any) error {
	c := g.config()
	if c.Controller == "" {
		return fmt.Errorf("netguard: 未配置 Clash 控制地址")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Controller+path, body)
	if err != nil {
		return err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	cli := &http.Client{Timeout: c.Timeout}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("clash %s %s: status %d: %s", method, path, resp.StatusCode, truncate(string(b), 200))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// ClashVersion 探活。
func (g *Guard) ClashVersion(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
		Meta    bool   `json:"meta"`
	}
	if err := g.clashDo(ctx, http.MethodGet, "/version", nil, &v); err != nil {
		return "", err
	}
	if v.Version == "" {
		return "unknown", nil
	}
	return v.Version, nil
}

// CurrentNode 返回某策略组当前选中的节点。
func (g *Guard) CurrentNode(ctx context.Context, group string) (string, error) {
	var d struct {
		Now  string   `json:"now"`
		Type string   `json:"type"`
		All  []string `json:"all"`
	}
	if err := g.clashDo(ctx, http.MethodGet, "/proxies/"+url.PathEscape(group), nil, &d); err != nil {
		return "", err
	}
	return d.Now, nil
}

// Delay 测某节点延迟（毫秒）。失败返回 error（调用方据此换节点）。
func (g *Guard) Delay(ctx context.Context, node string) (int, error) {
	var d struct {
		Delay int `json:"delay"`
	}
	path := "/proxies/" + url.PathEscape(node) + "/delay?timeout=3000&url=" + url.QueryEscape("http://www.gstatic.com/generate_204")
	if err := g.clashDo(ctx, http.MethodGet, path, nil, &d); err != nil {
		return 0, err
	}
	return d.Delay, nil
}

// Switch 切换策略组节点。
func (g *Guard) Switch(ctx context.Context, group, node string) error {
	body, _ := json.Marshal(map[string]string{"name": node})
	return g.clashDo(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), strings.NewReader(string(body)), nil)
}

// ───────────────────────── 记录 ─────────────────────────

func (g *Guard) logEgress(ctx context.Context, st Status, cause error) {
	if g == nil || g.db == nil {
		return
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ok := cause == nil
	_, _ = g.db.ExecContext(c,
		`INSERT INTO egress_log (task_id, egress_ip, node, ok, detail) VALUES (0, $1, $2, $3, $4)`,
		st.EgressIP, st.Node, ok, truncate(st.Detail, 500))
	if !ok {
		_, _ = g.db.ExecContext(c,
			`INSERT INTO net_events (task_id, kind, detail) VALUES (0, 'verify', $1)`,
			truncate(st.Detail, 500))
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
