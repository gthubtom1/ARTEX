package env

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- 测试脚手架 ----

type runnerFunc func(ctx context.Context, args []string) (string, error)

// fakeRunner 是可注入的 Runner：按命令名分发，并记录调用次数。
type fakeRunner struct {
	mu       sync.Mutex
	handlers map[string]runnerFunc
	calls    []string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	fn := f.handlers[name]
	if fn == nil {
		// 版本探测直接用绝对路径执行（如 /usr/bin/nmap --version），
		// 因此再按 basename 兜底匹配一次。
		fn = f.handlers[strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))]
	}
	f.mu.Unlock()
	if fn == nil {
		return "", errors.New("fakeRunner: command not found: " + name)
	}
	return fn(ctx, args)
}

func (f *fakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// noLookPath 让测试完全不依赖真实 PATH（保证 hermetic）。
func noLookPath(string) (string, error) { return "", errors.New("not found") }

// exeName 返回当前平台上"可执行文件名"。
func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// writeExe 在 dir 下创建一个可执行文件（内容非空），返回绝对路径。
func writeExe(t *testing.T, dir, base string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, exeName(base))
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho fake "+base+"\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
	return path
}

// writeFile 写普通文件（如 .desktop）。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func byName(tools []Tool, name string) []Tool {
	out := make([]Tool, 0, 2)
	for _, t := range tools {
		if strings.EqualFold(t.Name, name) {
			out = append(out, t)
		}
	}
	return out
}

// ---- 1. 五层合并去重（同名多路径保留） ----

func TestScanMergesFiveLayersAndKeepsDuplicateNames(t *testing.T) {
	root := t.TempDir()
	dirA := filepath.Join(root, "pathA")
	dirB := filepath.Join(root, "pathB")
	dirC := filepath.Join(root, "common")
	gopath := filepath.Join(root, "gopath")
	desktopDir := filepath.Join(root, "applications")
	optDir := filepath.Join(root, "opt")
	scanRoot := filepath.Join(root, "l5")

	nmapA := writeExe(t, dirA, "nmap")                                  // L1 + 出现在 plocate 输出里（测试去重）
	nmapB := writeExe(t, dirB, "nmap")                                  // L1，同名不同路径必须保留
	ffuf := writeExe(t, dirC, "ffuf")                                   // L2
	gobuster := writeExe(t, filepath.Join(gopath, "bin"), "gobuster")   // L3 (go env GOPATH)
	burp := writeExe(t, optDir, "burpsuite")                            // L4
	custom := writeExe(t, filepath.Join(scanRoot, "bin"), "customscan") // L5

	writeFile(t, filepath.Join(desktopDir, "burp.desktop"),
		"[Desktop Entry]\nName=Burp\nExec="+burp+" --flag %U\n")

	fr := &fakeRunner{handlers: map[string]runnerFunc{
		"dpkg": func(context.Context, []string) (string, error) {
			return "Desired=Unknown/Install/Remove/Purge/Hold\n" +
				"||/ Name         Version   Architecture Description\n" +
				"+++-============-=========-============-===========\n" +
				"ii  nuclei       3.1.0     amd64        fast scanner\n" +
				"ii  bash         5.2.15    amd64        shell\n", nil
		},
		"pipx": func(context.Context, []string) (string, error) {
			return "sqlmap 1.8.2\n", nil
		},
		"pip": func(context.Context, []string) (string, error) {
			return "requests==2.31.0\npycryptodome==3.20.0\n", nil
		},
		"go": func(_ context.Context, args []string) (string, error) {
			if len(args) == 2 && args[0] == "env" && args[1] == "GOPATH" {
				return gopath + "\n", nil
			}
			return "", errors.New("unexpected go args")
		},
		"snap": func(context.Context, []string) (string, error) {
			return "Name   Version  Rev  Tracking  Publisher  Notes\n" +
				"amass  4.2.0    1234 latest/stable  cg        classic\n", nil
		},
		"docker": func(context.Context, []string) (string, error) {
			return "kalilinux/kali-rolling:latest\n", nil
		},
		"plocate": func(context.Context, []string) (string, error) {
			// 故意把 nmapA 再吐一次，验证按 path 去重。
			return custom + "\n" + nmapA + "\n", nil
		},
		"nmap": func(context.Context, []string) (string, error) {
			return "Nmap version 7.94 ( https://nmap.org )\n", nil
		},
		"strings": func(context.Context, []string) (string, error) {
			return "", errors.New("strings: not installed")
		},
	}}

	d := New(Options{
		ScanTimeout: 5 * time.Second,
		Runner:      fr,
		LookPath:    noLookPath,
		PathDirs:    []string{dirA, dirB, filepath.Join(root, "no-such-path-dir")},
		BinDirs:     []string{dirC, filepath.Join(root, "no-such-bin-dir")},
		DesktopDirs: []string{desktopDir},
		ScanRoots:   []string{scanRoot},
	})

	tools, err := d.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// 同名多路径：nmap 两条都要在，且路径不同。
	nmap := byName(tools, "nmap")
	if len(nmap) != 2 {
		t.Fatalf("nmap entries = %d, want 2 (paths: %v)", len(nmap), pathsOf(nmap))
	}
	got := []string{nmap[0].Path, nmap[1].Path}
	want := []string{nmapA, nmapB}
	if !(contains(got, want[0]) && contains(got, want[1])) {
		t.Fatalf("nmap paths = %v, want %v", got, want)
	}
	for _, tt := range nmap {
		if tt.Version != "7.94" {
			t.Fatalf("nmap version = %q, want 7.94", tt.Version)
		}
		if tt.SHA256 == "" {
			t.Fatalf("nmap sha256 为空（%s）", tt.Path)
		}
	}

	// 各层都要有贡献。
	checks := []struct {
		layer string
		name  string
		path  string
		want  string // 期望的 ScanSource
	}{
		{"L1", "nmap", nmapA, "path"},
		{"L2", "ffuf", ffuf, "common-dir"},
		{"L3-dpkg", "nuclei", "", "dpkg"},
		{"L3-pipx", "sqlmap", "", "pipx"},
		{"L3-pip", "requests", "", "pip"},
		{"L3-go", "gobuster", gobuster, "go"},
		{"L3-snap", "amass", "", "snap"},
		{"L3-docker", "kalilinux/kali-rolling", "", "docker"},
		{"L4", "burpsuite", burp, "desktop"},
		{"L5", "customscan", custom, "global"},
	}
	for _, c := range checks {
		found := byName(tools, c.name)
		if len(found) == 0 {
			t.Fatalf("%s 层没有产出 %q（已有: %v）", c.layer, c.name, namesOf(tools))
		}
		if c.path != "" && found[0].Path != c.path {
			t.Fatalf("%s %q path = %q, want %q", c.layer, c.name, found[0].Path, c.path)
		}
		if found[0].ScanSource != c.want {
			t.Fatalf("%s %q ScanSource = %q, want %q", c.layer, c.name, found[0].ScanSource, c.want)
		}
	}

	// 全局去重：任何 path 只能出现一次。
	seen := map[string]bool{}
	for _, tt := range tools {
		if tt.Path == "" {
			continue
		}
		if seen[tt.Path] {
			t.Fatalf("路径重复出现: %s", tt.Path)
		}
		seen[tt.Path] = true
	}

	// 同名的包清单条目（不同 Kind）不会被 path 去重误杀。
	if n := byName(tools, "bash"); len(n) != 1 || n[0].Kind != "package" {
		t.Fatalf("dpkg bash = %+v", n)
	}

	// 排序稳定：按 name 升序。
	for i := 1; i < len(tools); i++ {
		if strings.ToLower(tools[i-1].Name) > strings.ToLower(tools[i].Name) {
			t.Fatalf("结果未按 name 排序: %q > %q", tools[i-1].Name, tools[i].Name)
		}
	}
}

// ---- 2. strings 指纹识别改名二进制 ----

func TestFingerprintRenamesUnknownBinary(t *testing.T) {
	dir := t.TempDir()
	weird := writeExe(t, dir, "weird-bin")

	fr := &fakeRunner{handlers: map[string]runnerFunc{
		"strings": func(_ context.Context, args []string) (string, error) {
			if len(args) == 0 || args[len(args)-1] != weird {
				t.Errorf("strings 被以意外参数调用: %v", args)
			}
			return "GCC: (Debian)\nprojectdiscovery/nuclei\nnuclei v3.1.0 templates\n", nil
		},
	}}

	d := New(Options{
		ScanTimeout:     5 * time.Second,
		Runner:          fr,
		LookPath:        noLookPath,
		DisableFullScan: true,
		PathDirs:        []string{dir},
		BinDirs:         []string{},
		DesktopDirs:     []string{},
	})

	tools, err := d.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %+v, want 1 条", tools)
	}
	got := tools[0]
	if got.Name != "nuclei" {
		t.Fatalf("Name = %q, want nuclei", got.Name)
	}
	if got.ScanSource != "fingerprint" {
		t.Fatalf("ScanSource = %q, want fingerprint", got.ScanSource)
	}
	if len(got.SHA256) != 64 {
		t.Fatalf("SHA256 = %q（长度 %d），want 64 位 hex", got.SHA256, len(got.SHA256))
	}
	if got.Path != weird {
		t.Fatalf("Path = %q, want %q", got.Path, weird)
	}
	if len(got.Caps) == 0 {
		t.Fatalf("Caps 未按识别出的名字补齐: %+v", got)
	}

	// 已知名字不应该触发 strings（白名单工具直接用文件名）。
	fr2 := &fakeRunner{handlers: map[string]runnerFunc{
		"strings": func(context.Context, []string) (string, error) {
			t.Fatalf("已知名字的工具不应跑 strings")
			return "", nil
		},
	}}
	dir2 := t.TempDir()
	writeExe(t, dir2, "nmap")
	d2 := New(Options{
		ScanTimeout:     5 * time.Second,
		Runner:          fr2,
		LookPath:        noLookPath,
		DisableFullScan: true,
		PathDirs:        []string{dir2},
		BinDirs:         []string{},
		DesktopDirs:     []string{},
	})
	if _, err := d2.Scan(context.Background()); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
}

// ---- 3. L5 超时：返回部分结果且 err == nil ----

func TestGlobalScanTimeoutKeepsPartialResults(t *testing.T) {
	root := t.TempDir()
	partial := writeExe(t, filepath.Join(root, "bin"), "slowtool")

	var findCalls int
	fr := &fakeRunner{handlers: map[string]runnerFunc{
		"plocate": func(context.Context, []string) (string, error) {
			return "", errors.New("exec: \"plocate\": executable file not found in $PATH")
		},
		"find": func(ctx context.Context, _ []string) (string, error) {
			findCalls++
			// 模拟"扫到一半被超时 kill"：阻塞到 ctx 取消，再带着错误交回部分输出。
			<-ctx.Done()
			return partial + "\n", ctx.Err()
		},
	}}

	d := New(Options{
		ScanTimeout: 150 * time.Millisecond,
		Runner:      fr,
		LookPath:    noLookPath,
		PathDirs:    []string{},
		BinDirs:     []string{},
		DesktopDirs: []string{},
		ScanRoots:   []string{root},
	})

	start := time.Now()
	tools, err := d.Scan(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("L5 超时不应返回错误，得到 %v", err)
	}
	if findCalls != 1 {
		t.Fatalf("find 调用次数 = %d, want 1（plocate 不可用应回退到 find）", findCalls)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("扫描耗时 %v，超时没有生效", elapsed)
	}
	found := byName(tools, "slowtool")
	if len(found) != 1 {
		t.Fatalf("超时后应保留部分结果，tools = %v", namesOf(tools))
	}
	if found[0].ScanSource != "global" || found[0].Path != partial {
		t.Fatalf("部分结果不正确: %+v", found[0])
	}
}

// ---- 4. 缓存 Save/Load 往返 + 过期重扫 ----

func TestCacheRoundTripAndExpiry(t *testing.T) {
	cacheDir := t.TempDir()
	binDir := t.TempDir()
	writeExe(t, binDir, "nmap")
	writeExe(t, binDir, "ffuf")

	fr := &fakeRunner{handlers: map[string]runnerFunc{
		"strings": func(context.Context, []string) (string, error) {
			return "", errors.New("strings: not installed")
		},
	}}
	opts := Options{
		CacheDir:        cacheDir,
		ScanTimeout:     5 * time.Second,
		Runner:          fr,
		LookPath:        noLookPath,
		DisableFullScan: true,
		PathDirs:        []string{binDir},
		BinDirs:         []string{},
		DesktopDirs:     []string{},
	}
	d := New(opts)

	tools, err := d.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools = %v, want nmap+ffuf", namesOf(tools))
	}
	if err := d.Save(context.Background(), tools); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	cachePath := filepath.Join(cacheDir, cacheFileName)
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("读取缓存文件: %v", err)
	}
	var decoded []Tool
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("缓存不是合法 []Tool JSON: %v", err)
	}

	before := fr.callCount()
	loaded, err := d.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(tools, loaded) {
		t.Fatalf("缓存往返不一致:\n got %+v\nwant %+v", loaded, tools)
	}
	if fr.callCount() != before {
		t.Fatalf("命中缓存时不应重新扫描（runner 调用 %d → %d）", before, fr.callCount())
	}

	// 新增一个工具，但缓存没过期 → 仍然读缓存（看不到新工具）。
	writeExe(t, binDir, "masscan")
	if again, err := d.Load(context.Background()); err != nil || len(byName(again, "masscan")) != 0 {
		t.Fatalf("未过期缓存不应重扫: tools=%v err=%v", namesOf(again), err)
	}

	// 把 mtime 改成 8 天前 → 过期，Load 必须重扫。
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(cachePath, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	refreshed, err := d.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(byName(refreshed, "masscan")) != 1 {
		t.Fatalf("过期后应重扫并发现 masscan，得到 %v", namesOf(refreshed))
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("Stat cache: %v", err)
	}
	if time.Since(info.ModTime()) > time.Minute {
		t.Fatalf("重扫后应回写缓存（mtime = %v）", info.ModTime())
	}

	// 缓存损坏 → 重扫而不是报错。
	if err := os.WriteFile(cachePath, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("写坏缓存: %v", err)
	}
	recovered, err := d.Load(context.Background())
	if err != nil {
		t.Fatalf("缓存损坏时应重扫，err = %v", err)
	}
	if len(byName(recovered, "nmap")) != 1 {
		t.Fatalf("损坏后重扫结果不对: %v", namesOf(recovered))
	}

	// CacheDir 为空：Save 是 no-op，Load 直接 Scan。
	noCache := New(Options{
		ScanTimeout: 5 * time.Second, Runner: fr, LookPath: noLookPath,
		DisableFullScan: true, PathDirs: []string{binDir},
		BinDirs: []string{}, DesktopDirs: []string{},
	})
	if err := noCache.Save(context.Background(), tools); err != nil {
		t.Fatalf("无 CacheDir 时 Save 应为 no-op，err = %v", err)
	}
	if _, err := noCache.Load(context.Background()); err != nil {
		t.Fatalf("无 CacheDir 时 Load 应直接 Scan，err = %v", err)
	}
}

// ---- 5. Missing / Find ----

func TestMissingAndFind(t *testing.T) {
	d := New(Options{LookPath: noLookPath, DisableFullScan: true})

	tools := []Tool{
		{Name: "nmap", Path: "/usr/bin/nmap", Healthy: true},
		{Name: "FFUF", Path: "/usr/bin/ffuf", Healthy: true},
		{Name: "curl", Healthy: true},
		{Name: "nxc", Healthy: true}, // netexec 的别名
	}

	missing := d.Missing(tools)
	for _, name := range []string{"nmap", "ffuf", "curl", "netexec"} {
		for _, m := range missing {
			if strings.EqualFold(m, name) {
				t.Fatalf("%q 已存在，不应出现在 Missing 里: %v", name, missing)
			}
		}
	}
	wantLen := len(keyTools) - 4
	if len(missing) != wantLen {
		t.Fatalf("Missing 条数 = %d, want %d: %v", len(missing), wantLen, missing)
	}
	// 2026-10-24：go 已从 keyTools 移除（它只是构建工具，任务不会调用），
	// 所以这里不再要求它出现在 Missing 里；反过来要断言它**不再出现**。
	for _, name := range []string{"sqlmap", "proxychains4", "python3"} {
		if !contains(missing, name) {
			t.Fatalf("Missing 缺少 %q: %v", name, missing)
		}
	}
	for _, name := range missing {
		if strings.EqualFold(name, "go") {
			t.Fatalf("go 是构建工具，不该出现在环境体检清单里: %v", missing)
		}
	}
	// 顺序跟关键清单一致。
	order := map[string]int{}
	for i, name := range keyTools {
		order[name] = i
	}
	for i := 1; i < len(missing); i++ {
		if order[missing[i-1]] > order[missing[i]] {
			t.Fatalf("Missing 顺序不稳定: %v", missing)
		}
	}
	// 全都在时返回 nil。
	all := make([]Tool, 0, len(keyTools))
	for _, name := range keyTools {
		all = append(all, Tool{Name: name, Healthy: true})
	}
	if got := d.Missing(all); got != nil {
		t.Fatalf("全部存在时 Missing 应为 nil，得到 %v", got)
	}

	// Find：大小写不敏感。
	if got, ok := d.Find(tools, "ffuf"); !ok || got.Path != "/usr/bin/ffuf" {
		t.Fatalf("Find(ffuf) = %+v, %v", got, ok)
	}
	if got, ok := d.Find(tools, "NMAP"); !ok || got.Name != "nmap" {
		t.Fatalf("Find(NMAP) = %+v, %v", got, ok)
	}
	if _, ok := d.Find(tools, "sqlmap"); ok {
		t.Fatalf("Find(sqlmap) 不应命中")
	}
	if _, ok := d.Find(tools, "  "); ok {
		t.Fatalf("Find(空串) 不应命中")
	}

	// 同名多条时优先健康的、带路径的。
	dup := []Tool{
		{Name: "nmap", Path: "/broken/nmap", Healthy: false},
		{Name: "nmap", Path: "/usr/bin/nmap", Healthy: true},
	}
	if got, _ := d.Find(dup, "nmap"); got.Path != "/usr/bin/nmap" {
		t.Fatalf("Find 应优先健康条目，得到 %+v", got)
	}
}

// 去重键：path 为空时按 Kind+Name，不会互相吞掉。
func TestDedupKeyFallbacksToKindWhenPathEmpty(t *testing.T) {
	d := New(Options{LookPath: noLookPath, DisableFullScan: true})
	tools := d.finalize([]Tool{
		{Name: "nmap", Kind: "package", ScanSource: "dpkg"},
		{Name: "nmap", Kind: "package", ScanSource: "dpkg"},
		{Name: "nmap", Kind: "image", ScanSource: "docker"},
	})
	if len(tools) != 2 {
		t.Fatalf("finalize = %+v, want 2 条（package/image 各一）", tools)
	}
}

// ---- 小工具 ----

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func namesOf(tools []Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

func pathsOf(tools []Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Path)
	}
	return out
}
