// Package env 实现"本地工具五层发现 + 指纹 + 缓存"。
//
// 目标：让 ARTEX 的 AI 在 Kali VM 里先知道"这台机器上有什么工具"，
// 再决定后续攻击路径，避免凭空假设工具存在。
//
// 设计纪律：
//   - 任何外部命令都必须带 context 超时，绝不无界执行；
//   - find 一律 -xdev，且 prune 掉 /proc /sys /dev /run 与 node_modules；
//   - 五层按"便宜 → 昂贵"顺序执行，任何一层失败/超时都不影响其它层；
//   - 只读：不修改系统状态，缓存只写 Options.CacheDir。
package env

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// DefaultScanTimeout 是单层扫描的默认超时。
	DefaultScanTimeout = 60 * time.Second

	// cacheFileName 是缓存文件名（位于 Options.CacheDir 下）。
	cacheFileName = "tools_cache.json"

	// cacheTTL 缓存有效期，超过则视为过期并重新扫描。
	//
	// 2026-10-24：7 天 → 1 天。实测：用户在 VM 里 apt 装了 conntrack/chisel/sshpass，
	// 环境页仍然把 chisel/sshpass 列为缺失（缓存是 7 天前扫的），人以为装失败了。
	cacheTTL = 24 * time.Hour

	// hardKillGrace 是外部强制兜底超时的宽限期：
	// 若注入的 Runner 不守 ctx 约定，runCommand 在 timeout+grace 后直接放弃等待。
	hardKillGrace = time.Second
)

// Tool 是发现到的一个本地工具（可执行文件 / 系统包 / 语言包 / 容器镜像 / 桌面启动项）。
//
// 去重键：Path（同名的多个路径都会保留）；Path 为空（包清单类条目）时退化为 Kind+Name。
type Tool struct {
	Name       string   `json:"name"`
	Path       string   `json:"path,omitempty"`
	Kind       string   `json:"kind"` // binary / package / python / pipx / snap / image / go / desktop / glob
	Version    string   `json:"version,omitempty"`
	SHA256     string   `json:"sha256,omitempty"`
	ScanSource string   `json:"scan_source"` // path / common-dir / dpkg / pipx / pip / go / snap / docker / desktop / plocate / find / fingerprint
	Caps       []string `json:"caps,omitempty"`
	Healthy    bool     `json:"healthy"`
}

// Runner 抽象"执行一个外部命令"，便于测试注入。
//
// 约定：实现方必须在 ctx 结束后尽快返回；超时被杀时允许把已读到的部分 stdout
// 和 ctx 的错误一起返回（execRunner 就是这么做的），这样上层能保留部分结果。
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout string, err error)
}

// Options 是 Discoverer 的构造参数。
type Options struct {
	// CacheDir 缓存 JSON 落盘目录（空 = 不落盘，Load 直接 Scan）。
	CacheDir string
	// ScanTimeout 单层扫描超时（默认 DefaultScanTimeout）。
	ScanTimeout time.Duration
	// FullScan 是否做 L5 全盘。Go 零值无法区分"未设置"与 false，
	// 因此 New 的规则是：opts.FullScan || !opts.DisableFullScan，即默认开启。
	FullScan bool
	// ExtraDirs 额外搜索目录（会并入 L1 PATH 目录与 L2 常见目录）。
	ExtraDirs []string
	// Runner 可注入（默认 exec 实现），便于测试。
	Runner Runner

	// DisableFullScan 显式关闭 L5（优先级高于 FullScan）。
	DisableFullScan bool

	// 以下字段是"系统视图覆盖"，零值(nil)表示使用平台默认值，仅供测试注入：
	// 传空切片表示"这一层没有目录"，与 nil 语义不同。
	PathDirs    []string                     // 覆盖 L1 的 PATH 目录
	BinDirs     []string                     // 覆盖 L2 常见目录（支持 glob 与 ~）
	DesktopDirs []string                     // 覆盖 L4 .desktop 目录
	ScanRoots   []string                     // 覆盖 L5 全盘根
	LookPath    func(string) (string, error) // 覆盖 exec.LookPath
}

// Discoverer 执行五层发现。零值不可用，必须用 New 构造。
type Discoverer struct {
	cacheDir    string
	scanTimeout time.Duration
	fullScan    bool
	extraDirs   []string
	runner      Runner

	pathDirs    []string
	binDirs     []string
	desktopDirs []string
	scanRoots   []string
	lookPath    func(string) (string, error)
}

// New 用 Options 构造 Discoverer，所有零值都有安全默认。
func New(opts Options) *Discoverer {
	d := &Discoverer{
		cacheDir:    strings.TrimSpace(opts.CacheDir),
		scanTimeout: opts.ScanTimeout,
		// FullScan 零值 = false，但语义默认是"做 L5"，因此以 DisableFullScan 为准。
		fullScan: opts.FullScan || !opts.DisableFullScan,
		runner:   opts.Runner,
		lookPath: opts.LookPath,
	}
	if d.scanTimeout <= 0 {
		d.scanTimeout = DefaultScanTimeout
	}
	if d.runner == nil {
		d.runner = execRunner{}
	}
	if d.lookPath == nil {
		d.lookPath = lookPathDefault
	}

	d.extraDirs = expandDirs(opts.ExtraDirs)

	if opts.PathDirs != nil {
		d.pathDirs = cleanDirs(opts.PathDirs)
	} else {
		d.pathDirs = cleanDirs(filepath.SplitList(os.Getenv("PATH")))
	}
	d.pathDirs = append(d.pathDirs, d.extraDirs...)

	if opts.BinDirs != nil {
		d.binDirs = cleanDirs(opts.BinDirs)
	} else {
		d.binDirs = expandDirs(defaultBinDirs)
	}
	d.binDirs = append(d.binDirs, d.extraDirs...)

	if opts.DesktopDirs != nil {
		d.desktopDirs = cleanDirs(opts.DesktopDirs)
	} else {
		d.desktopDirs = expandDirs(defaultDesktopDirs)
	}

	if opts.ScanRoots != nil {
		d.scanRoots = cleanDirs(opts.ScanRoots)
	} else {
		d.scanRoots = []string{"/"}
	}
	return d
}

// Scan 执行五层全扫并返回去重后的工具清单。
//
// 单层失败/超时都不会让 Scan 返回错误（结果里保留已完成部分）；
// 只有在调用前 ctx 已取消时才会返回错误。
func (d *Discoverer) Scan(ctx context.Context) ([]Tool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	layers := []func(context.Context) []Tool{
		d.scanPath,       // L1
		d.scanCommonDirs, // L2
		d.scanPackages,   // L3
		d.scanDesktop,    // L4
	}
	if d.fullScan {
		layers = append(layers, d.scanGlobal) // L5
	}

	tools := make([]Tool, 0, 64)
	for _, layer := range layers {
		lctx, cancel := context.WithTimeout(ctx, d.scanTimeout)
		tools = append(tools, runLayer(lctx, layer)...)
		cancel()
	}

	// 指纹环节也给一个硬预算：任何一步都不能无界执行。
	fctx, fcancel := context.WithTimeout(ctx, d.scanTimeout)
	tools = d.fingerprint(fctx, tools)
	fcancel()
	return d.finalize(tools), nil
}

// runLayer 隔离单层：panic 也当作"这层失败"，不影响其它层。
func runLayer(ctx context.Context, fn func(context.Context) []Tool) (out []Tool) {
	defer func() {
		if r := recover(); r != nil {
			out = nil
		}
	}()
	if ctx.Err() != nil {
		return nil
	}
	return fn(ctx)
}

// finalize 负责：丢弃已不存在的路径条目、按 path 去重、补 Caps、稳定排序。
func (d *Discoverer) finalize(tools []Tool) []Tool {
	seen := make(map[string]bool, len(tools))
	out := make([]Tool, 0, len(tools))
	for _, t := range tools {
		t.Name = strings.TrimSpace(t.Name)
		if t.Name == "" {
			continue
		}
		if t.Path != "" && !pathExists(t.Path) {
			continue
		}
		key := dedupKey(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(t.Caps) == 0 {
			t.Caps = capsFor(t.Name)
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// dedupKey 返回去重键：优先 path，其次 Kind+Name。
func dedupKey(t Tool) string {
	if t.Path != "" {
		return "path:" + strings.ToLower(filepath.Clean(t.Path))
	}
	return "pkg:" + strings.ToLower(t.Kind) + "|" + strings.ToLower(t.Name)
}

// Find 按名查找（大小写不敏感）。同一名字有多条时，优先返回健康的、带路径的。
func (d *Discoverer) Find(tools []Tool, name string) (Tool, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" {
		return Tool{}, false
	}
	var fallback Tool
	hasFallback := false
	for _, t := range tools {
		if strings.ToLower(strings.TrimSpace(t.Name)) != want {
			continue
		}
		if t.Healthy && t.Path != "" {
			return t, true
		}
		if !hasFallback {
			fallback = t
			hasFallback = true
		}
	}
	if hasFallback {
		return fallback, true
	}
	return Tool{}, false
}

// Missing 返回关键工具清单里缺失的项（按清单顺序；大小写不敏感，支持别名单）。
func (d *Discoverer) Missing(tools []Tool) []string {
	present := make(map[string]bool, len(tools)*2)
	for _, t := range tools {
		present[strings.ToLower(strings.TrimSpace(t.Name))] = true
	}
	var missing []string
	for _, want := range keyTools {
		if present[want] {
			continue
		}
		found := false
		for _, alias := range keyToolAliases[want] {
			if present[alias] {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, want)
		}
	}
	return missing
}

// keyTools 是 AI 常用、缺失时需要显式告警的工具清单。
var keyTools = []string{
	"nmap", "masscan", "ffuf", "gobuster", "feroxbuster", "nuclei", "sqlmap",
	"hydra", "john", "chisel", "proxychains4", "sshpass", "smbclient",
	"ldapsearch", "netexec", "impacket-psexec", "curl", "jq", "python3", "go",
}

// keyToolAliases 处理"同一工具不同包名/脚本名"的等价关系。
var keyToolAliases = map[string][]string{
	"netexec":         {"nxc"},
	"python3":         {"python"},
	"impacket-psexec": {"psexec.py"},
}

// capsFor 返回某工具的能力标签（粗略映射，供 AI 参考）。
func capsFor(name string) []string {
	caps, ok := capTable[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil
	}
	out := make([]string, len(caps))
	copy(out, caps)
	return out
}

var capTable = map[string][]string{
	"nmap":              {"port-scan", "service-detect", "nse"},
	"masscan":           {"port-scan", "fast"},
	"ffuf":              {"web-fuzz"},
	"gobuster":          {"web-enum", "dns-enum"},
	"feroxbuster":       {"web-enum"},
	"nuclei":            {"vuln-scan", "templates"},
	"sqlmap":            {"sqli"},
	"hydra":             {"brute-force"},
	"john":              {"password-crack"},
	"chisel":            {"tunnel"},
	"proxychains4":      {"proxy"},
	"sshpass":           {"ssh-auth"},
	"smbclient":         {"smb"},
	"ldapsearch":        {"ldap"},
	"netexec":           {"smb", "winrm", "ldap"},
	"impacket-psexec":   {"smb", "remote-exec"},
	"curl":              {"http"},
	"jq":                {"json"},
	"python3":           {"script"},
	"go":                {"build"},
	"docker":            {"container"},
	"python":            {"script"},
	"gcc":               {"build"},
	"openssl":           {"crypto"},
	"responder":         {"llmnr", "ntlm-relay"},
	"certipy":           {"adcs"},
	"bloodhound-python": {"ad-enum"},
}
