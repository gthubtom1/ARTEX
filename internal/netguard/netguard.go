// Package netguard 是「出口 IP 自检」：查出本机当前的对外出口 IP，并与首次记录的基准比对。
//
// D1 决策（docs/03-P9强化方案.md）：**删除应用内 Clash / 代理配置**。
// 部署目标是 VPS 或 TUN 全局的 VM —— 网络在宿主层解决；应用内再管一遍必然漂移
// （已实际出过一次 bug：UI 写 global_proxy、netguard 读 net.proxy_http，
// 界面配好了任务却仍是 paused）。
//
// 所以本包现在只做三件事，且**不拦任务**（不打 fail-closed，只把事实显形）：
//  1. 两个独立源互证当前出口 IP（同一个 IP 才认）；
//  2. 拒绝内网/回环/链路本地地址（说明流量根本没出网）；
//  3. 与 net.baseline_ip 比对：首次成功后自动记基准；之后不一致 →
//     Status.OK=false + Detail 写明「出口 IP 变了：baseline=x 现在=y」。
//
// 本包不读应用内的代理/Clash 配置、不向子进程注入环境变量、不碰 Clash 控制 API；
// 唯一认可的“代理”是宿主层的 HTTP(S)_PROXY 环境变量（与子进程看到的出口一致）。
package netguard

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/internal/dbiface"
)

// BaselineSettingKey 是「基准出口 IP」的 setting 键：首次校验成功时自动写入。
const BaselineSettingKey = "net.baseline_ip"

// Config 是出口自检的运行参数（来自 settings，每次调用实时读取）。
type Config struct {
	VerifyURLs  []string      // 出口 IP 查询源（至少两个，互证）
	Timeout     time.Duration // 单次请求超时（默认 8s）
	AllowEgress []string      // 可选：出口 IP 白名单（空=不限制具体 IP）
	Baseline    string        // net.baseline_ip；空=还没建立基准
	// SetBaseline 在首次校验成功时把基准写回 settings（server 注入）。
	// 为 nil 时只在本次 Status 里体现，不持久化（测试/降级场景）。
	SetBaseline func(ctx context.Context, ip string) error
}

// Status 是一次出口自检的结果。
type Status struct {
	EgressIP   string    `json:"egress_ip"`
	BaselineIP string    `json:"baseline_ip,omitempty"`
	OK         bool      `json:"ok"`
	At         time.Time `json:"at"`
	Detail     string    `json:"detail,omitempty"`
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

// Last 返回最近一次自检结果。
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
	c.Baseline = strings.TrimSpace(c.Baseline)
	if len(c.VerifyURLs) == 0 {
		c.VerifyURLs = []string{"https://api.ipify.org", "https://ifconfig.me"}
	}
	return c
}

// Client 返回出口自检用的 HTTP 客户端。
//
// 代理来源只有一个：**宿主层**。应用内不再配代理（D1），但如果是 VPS 上用
// HTTP_PROXY / HTTPS_PROXY 环境变量统一出网（docker-compose 的 env、systemd 的
// Environment= 都是这种形态），自检也走它 —— 否则子进程经代理出网、自检却直连，
// 报出来的“出口”就是假的。没有这两个环境变量时 ProxyFromEnvironment 返回 nil，
// 走直连（TUN 全局场景）。
//
// 注意：Go 的 ProxyFromEnvironment 对 localhost/127.0.0.1 自动免代理，所以测试
// 里的 httptest 服务不会被环境变量干扰。
func (g *Guard) Client() *http.Client {
	c := g.config()
	return &http.Client{
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConns:        8,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: c.Timeout,
		},
		Timeout: c.Timeout,
	}
}

// VerifyCached 是 Verify 的带缓存版本。
//
// 为什么需要：出口自检每次要打两个独立源（互证出口 IP），一次两个外网往返。
// 规划每轮都问一次的话，一天下来是几百次无用请求。成功结果缓存 ttl；
// 失败结果缓存 min(ttl, 30s) —— 出口刚恢复时不能等满 5 分钟才生效。
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

// InvalidateCache 清掉缓存（手工「重新检测」后用，让下一次立即真校验）。
func (g *Guard) InvalidateCache() {
	g.mu.Lock()
	g.cacheSet = false
	g.mu.Unlock()
}

// Verify 校验出口 IP：
//   - 所有 VerifyURLs 必须返回同一个 IP（否则说明分流/双栈坏了）；
//   - 该 IP 不能是内网/回环/链路本地；
//   - 若配置了 AllowEgress，必须命中白名单；
//   - 与 baseline 比对：首次成功 → 记下基准；之后不一致 → OK=false。
//
// 返回的 error 只描述「这次自检本身没通过」，**调用方不应据此拦截任务**：
// 应用内不再拥有出口控制权，拦下来只会把宿主层的网络问题变成「任务一建就 paused」。
func (g *Guard) Verify(ctx context.Context) (Status, error) {
	c := g.config()
	st := Status{At: time.Now(), BaselineIP: c.Baseline}
	cli := g.Client()

	// 逐个源尝试：**单个源失败不应让整次自检失败**（R3 审查 R7）。
	// 实测环境里 api.ipify.org 常被重置，而 ifconfig.me 可用；按原来的写法（首个失败即返回）
	// 会让 exit_ip 永远为空 —— 而这块是 D1 决策后唯一保留的出口能力。
	ips := make([]string, 0, len(c.VerifyURLs))
	var firstErr error
	for _, u := range c.VerifyURLs {
		ip, ferr := fetchIP(ctx, cli, u)
		if ferr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", u, ferr)
			}
			continue
		}
		ips = append(ips, ip)
	}
	if len(ips) == 0 {
		st.Detail = fmt.Sprintf("所有出口源都不可达（first: %v）", firstErr)
		g.setLast(st)
		g.logEgress(ctx, st)
		return st, fmt.Errorf("netguard: 出口自检失败（所有源不可达）: %w", firstErr)
	}
	if len(ips) == 1 {
		// 只剩一个源可用：无法互证，但在 Detail 里写明（不当作失败）
		st.Detail = "只有一个出口源可用，无法互证（不阻断）"
	}
	for i := 1; i < len(ips); i++ {
		if ips[i] != ips[0] {
			err := fmt.Errorf("netguard: 两个源报的出口 IP 不一致（%s vs %s），分流规则可能坏了", ips[0], ips[i])
			st.Detail = err.Error()
			g.setLast(st)
			g.logEgress(ctx, st)
			return st, err
		}
	}
	st.EgressIP = ips[0]
	if err := checkIP(c, ips[0]); err != nil {
		st.Detail = err.Error()
		g.setLast(st)
		g.logEgress(ctx, st)
		return st, err
	}

	switch {
	case c.Baseline == "":
		// 第一次校验成功：记下基准。写失败不影响自检结论（下次会再记一次）。
		st.OK = true
		st.BaselineIP = st.EgressIP
		st.Detail = "ok（已记录基准出口 IP " + st.EgressIP + "）"
		if c.SetBaseline != nil {
			if err := c.SetBaseline(ctx, st.EgressIP); err != nil {
				st.Detail = "ok（基准出口 IP 写入失败：" + err.Error() + "，下次自检会重试）"
			}
		}
	case c.Baseline != st.EgressIP:
		// 出口变了：只显形，不阻塞。Detail 写清前后值，人一眼能看出换没换机房/掉没掉 TUN。
		err := fmt.Errorf("netguard: 出口 IP 变了：baseline=%s 现在=%s", c.Baseline, st.EgressIP)
		st.OK = false
		st.Detail = err.Error()
		g.setLast(st)
		g.logEgress(ctx, st)
		return st, err
	default:
		st.OK = true
		st.Detail = "ok"
	}
	g.setLast(st)
	g.logEgress(ctx, st)
	return st, nil
}

// checkIP 拒绝内网/回环/链路本地出口，并校验白名单。
func checkIP(c Config, raw string) error {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return fmt.Errorf("netguard: 出口 IP 无法解析: %q", raw)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
		return fmt.Errorf("netguard: 出口 IP 是内网/本机地址 %s，流量没有出网", raw)
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

// ───────────────────────── 记录 ─────────────────────────
//
// 只落库、不判定：egress_log 是「最近变更记录」的数据源（网络页直接读），
// net_events 只在自检没通过时补一条，方便和任务活动流对齐时间线。
func (g *Guard) logEgress(ctx context.Context, st Status) {
	if g == nil || g.db == nil {
		return
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, _ = g.db.ExecContext(c,
		`INSERT INTO egress_log (task_id, egress_ip, ok, detail) VALUES (NULL, $1, $2, $3)`,
		st.EgressIP, st.OK, truncate(st.Detail, 500))
	if !st.OK {
		_, _ = g.db.ExecContext(c,
			`INSERT INTO net_events (task_id, kind, detail) VALUES (NULL, 'verify', $1)`,
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
