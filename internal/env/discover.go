package env

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// 五层扫描的实现。每层都是"只读 + 带超时"，失败即返回空，不向外抛错。

var (
	// defaultBinDirs 是 L2 的常见目录（支持 glob 与 ~ 前缀）。
	defaultBinDirs = []string{
		"/usr/bin", "/usr/local/bin", "/usr/sbin", "/sbin", "/snap/bin",
		"/opt/*/bin", "~/.local/bin", "~/bin", "~/tools", "~/go/bin",
		"~/.cargo/bin", "/root/tools",
	}

	// defaultDesktopDirs 是 L4 的 .desktop 目录。
	defaultDesktopDirs = []string{
		"/usr/share/applications",
		"/usr/local/share/applications",
		"~/.local/share/applications",
	}

	// prunePatterns 是 L5 必须 prune 掉的伪文件系统（配合 -xdev 使用）。
	prunePatterns = []string{"/proc", "/sys", "/dev", "/run"}
)

const (
	// maxGlobalResults 限制 L5 最多收多少条，避免内存爆炸。
	maxGlobalResults = 20000
	// maxPackageLines 限制单条包清单命令的解析行数。
	maxPackageLines = 50000
	// l3CommandTimeout 是 L3 每条命令的独立超时上限。
	l3CommandTimeout = 20 * time.Second
	// probeTimeout 是 --version 探测的独立超时上限。
	probeTimeout = 10 * time.Second
)

// versionOutput 用于从 --version 输出里抠出版本号。
var versionOutput = regexp.MustCompile(`(?i)v?(\d+\.\d+(?:\.\d+)?(?:[-+~][0-9A-Za-z.\-]+)?)`)

// versionProbes 是允许跑 "--version" 的白名单，避免个别二进制无参崩溃。
var versionProbes = map[string][]string{
	"nmap":              {"--version"},
	"masscan":           {"--version"},
	"ffuf":              {"-V"},
	"gobuster":          {"version"},
	"feroxbuster":       {"--version"},
	"nuclei":            {"-version"},
	"sqlmap":            {"--version"},
	"hydra":             {"-h"},
	"john":              {"--list=build-info"},
	"chisel":            {"--version"},
	"sshpass":           {"-V"},
	"smbclient":         {"--version"},
	"ldapsearch":        {"-VV"},
	"netexec":           {"--version"},
	"nxc":               {"--version"},
	"impacket-psexec":   {"-h"},
	"curl":              {"--version"},
	"jq":                {"--version"},
	"python3":           {"--version"},
	"python":            {"--version"},
	"go":                {"version"},
	"docker":            {"--version"},
	"git":               {"--version"},
	"openssl":           {"version"},
	"proxychains4":      {"-h"},
	"certipy":           {"--version"},
	"bloodhound-python": {"--version"},
}

// execRunner 是默认 Runner：用 exec.CommandContext 保证超时可 kill。
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	// ctx 结束后等待 I/O 关闭的最长时间，避免管道被孙进程占住导致挂死。
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	// 超时被 kill 时也把已读到的部分输出返回，供上层保留部分结果。
	return string(out), err
}

func lookPathDefault(name string) (string, error) { return exec.LookPath(name) }

// runCommand 在独立超时内执行一条外部命令。
//
// 纪律：绝不无界等待。即使注入的 Runner 不守 ctx 约定，
// 也会在 timeout+hardKillGrace 后返回 DeadlineExceeded。
func (d *Discoverer) runCommand(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	if timeout <= 0 {
		timeout = d.scanTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		out string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := d.runner.Run(cctx, name, args...)
		ch <- result{out: out, err: err}
	}()

	hard := time.NewTimer(timeout + hardKillGrace)
	defer hard.Stop()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-hard.C:
		return "", context.DeadlineExceeded
	}
}

// ---- L1：PATH ----

// scanPath 遍历 PATH 目录收集可执行文件，并对白名单工具跑 --version。
func (d *Discoverer) scanPath(ctx context.Context) []Tool {
	out := make([]Tool, 0, 64)
	add := func(path, source string) {
		info, ok := statExecutable(path)
		if !ok {
			return
		}
		out = append(out, Tool{
			Name:       displayName(path),
			Path:       path,
			Kind:       "binary",
			ScanSource: source,
			Healthy:    info.Size() > 0,
		})
	}

	for _, dir := range d.pathDirs {
		if ctx.Err() != nil {
			break
		}
		for _, path := range listExecutables(ctx, dir) {
			add(path, "path")
		}
	}

	// 白名单兜底：PATH 目录枚举不到时（例如目录不可读）用 LookPath 再试一次。
	for _, name := range probedNames() {
		if ctx.Err() != nil {
			break
		}
		if p, err := d.lookPath(name); err == nil && p != "" {
			add(p, "path")
		}
	}

	// 版本探测：只对白名单工具，且每个路径只探测一次。
	probed := make(map[string]bool, len(out))
	for i := range out {
		if ctx.Err() != nil {
			break
		}
		name := strings.ToLower(out[i].Name)
		args, ok := versionProbes[name]
		if !ok || out[i].Path == "" || probed[out[i].Path] {
			continue
		}
		probed[out[i].Path] = true
		if v := d.probeVersion(ctx, out[i].Path, args); v != "" {
			out[i].Version = v
		}
	}
	return out
}

// probeVersion 跑 `path args...` 并解析版本号；失败返回空串（不算不健康）。
func (d *Discoverer) probeVersion(ctx context.Context, path string, args []string) string {
	// 很多工具 --version 会写 stderr 并以非 0 退出，因此不把 err 当失败，
	// 只看有没有输出。
	out, _ := d.runCommand(ctx, d.probeTimeout(), path, args...)
	if ctx.Err() != nil || out == "" {
		return ""
	}
	return parseVersion(out)
}

// probedNames 返回白名单工具名（排序，保证行为稳定）。
func probedNames() []string {
	names := make([]string, 0, len(versionProbes))
	for name := range versionProbes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// parseVersion 从命令输出里抠版本号；抠不到就退回首行摘要。
func parseVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		if m := versionOutput.FindStringSubmatch(line); len(m) == 2 {
			return m[1]
		}
		if len(line) > 120 {
			line = line[:120]
		}
		return line
	}
	return ""
}

// ---- L2：常见目录 ----

// scanCommonDirs 扫描常见安装目录（glob 展开，不存在的直接跳过）。
func (d *Discoverer) scanCommonDirs(ctx context.Context) []Tool {
	out := make([]Tool, 0, 64)
	for _, dir := range d.binDirs {
		if ctx.Err() != nil {
			break
		}
		for _, path := range listExecutables(ctx, dir) {
			info, ok := statExecutable(path)
			if !ok {
				continue
			}
			out = append(out, Tool{
				Name:       displayName(path),
				Path:       path,
				Kind:       "binary",
				ScanSource: "common-dir",
				Healthy:    info.Size() > 0,
			})
		}
	}
	return out
}

// ---- L3：包管理器清单 ----

// scanPackages 汇总各包管理器的清单；每条命令独立超时，失败即跳过。
func (d *Discoverer) scanPackages(ctx context.Context) []Tool {
	out := make([]Tool, 0, 64)
	timeout := l3CommandTimeout
	if d.scanTimeout < timeout {
		timeout = d.scanTimeout
	}

	if s, err := d.runCommand(ctx, timeout, "dpkg", "-l"); err == nil || s != "" {
		out = append(out, parseDpkg(s)...)
	}
	if s, err := d.runCommand(ctx, timeout, "pipx", "list", "--short"); err == nil || s != "" {
		out = append(out, parsePipx(s)...)
	}
	if s, err := d.runCommand(ctx, timeout, "pip", "list", "--format=freeze"); err == nil || s != "" {
		out = append(out, parsePipFreeze(s)...)
	}
	if s, err := d.runCommand(ctx, timeout, "go", "env", "GOPATH"); err == nil || s != "" {
		out = append(out, d.parseGoBin(strings.TrimSpace(s))...)
	}
	if s, err := d.runCommand(ctx, timeout, "snap", "list"); err == nil || s != "" {
		out = append(out, parseSnap(s)...)
	}
	if s, err := d.runCommand(ctx, timeout, "docker", "images", "--format", "{{.Repository}}:{{.Tag}}"); err == nil || s != "" {
		out = append(out, parseDockerImages(s)...)
	}
	if ctx.Err() != nil {
		// 层超时：保留已拿到的部分。
		return out
	}
	return out
}

// parseDpkg 解析 `dpkg -l` 的 "ii  name  version ..." 行。
func parseDpkg(s string) []Tool {
	out := make([]Tool, 0, 32)
	for i, line := range splitLines(s) {
		if i >= maxPackageLines {
			break
		}
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "ii" {
			continue
		}
		out = append(out, Tool{
			Name:       f[1],
			Kind:       "package",
			Version:    f[2],
			ScanSource: "dpkg",
			Healthy:    true,
		})
	}
	return out
}

// parsePipx 解析 `pipx list --short` 的 "name version" 行。
func parsePipx(s string) []Tool {
	out := make([]Tool, 0, 16)
	for i, line := range splitLines(s) {
		if i >= maxPackageLines {
			break
		}
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "-") {
			continue
		}
		t := Tool{Name: f[0], Kind: "pipx", ScanSource: "pipx", Healthy: true}
		if len(f) > 1 {
			t.Version = f[1]
		}
		out = append(out, t)
	}
	return out
}

// parsePipFreeze 解析 `pip list --format=freeze` 的 "name==version" 行。
func parsePipFreeze(s string) []Tool {
	out := make([]Tool, 0, 32)
	for i, line := range splitLines(s) {
		if i >= maxPackageLines {
			break
		}
		name, version, found := strings.Cut(line, "==")
		name = strings.TrimSpace(name)
		if name == "" || strings.HasPrefix(name, "#") {
			continue
		}
		t := Tool{Name: name, Kind: "python", ScanSource: "pip", Healthy: true}
		if found {
			t.Version = strings.TrimSpace(version)
		}
		out = append(out, t)
	}
	return out
}

// parseSnap 解析 `snap list`（跳过表头）。
func parseSnap(s string) []Tool {
	out := make([]Tool, 0, 16)
	for i, line := range splitLines(s) {
		if i >= maxPackageLines {
			break
		}
		f := strings.Fields(line)
		if len(f) < 2 || f[0] == "Name" {
			continue
		}
		out = append(out, Tool{
			Name:       f[0],
			Kind:       "snap",
			Version:    f[1],
			ScanSource: "snap",
			Healthy:    true,
		})
	}
	return out
}

// parseDockerImages 解析 `docker images --format '{{.Repository}}:{{.Tag}}'`。
func parseDockerImages(s string) []Tool {
	out := make([]Tool, 0, 16)
	for i, line := range splitLines(s) {
		if i >= maxPackageLines {
			break
		}
		if !strings.Contains(line, ":") {
			continue
		}
		name, version, _ := strings.Cut(line, ":")
		out = append(out, Tool{
			Name:       strings.TrimSpace(name),
			Kind:       "image",
			Version:    strings.TrimSpace(version),
			ScanSource: "docker",
			Healthy:    true,
		})
	}
	return out
}

// parseGoBin 读取 `go env GOPATH` 下的 bin 目录（只读，不执行 shell）。
func (d *Discoverer) parseGoBin(gopath string) []Tool {
	if gopath == "" || strings.ContainsAny(gopath, "\r\n") {
		return nil
	}
	dir := filepath.Join(gopath, "bin")
	out := make([]Tool, 0, 8)
	for _, path := range listExecutables(context.Background(), dir) {
		if _, ok := statExecutable(path); !ok {
			continue
		}
		out = append(out, Tool{
			Name:       displayName(path),
			Path:       path,
			Kind:       "go",
			ScanSource: "go",
			Healthy:    true,
		})
	}
	return out
}

// ---- L4：桌面启动器 ----

// scanDesktop 读 *.desktop 的 Exec= 行，取第一个 token 的绝对路径。
func (d *Discoverer) scanDesktop(ctx context.Context) []Tool {
	out := make([]Tool, 0, 32)
	for _, dir := range d.desktopDirs {
		if ctx.Err() != nil {
			break
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if ctx.Err() != nil {
				break
			}
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".desktop") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			out = append(out, parseDesktop(string(data))...)
		}
	}
	return out
}

// parseDesktop 从 .desktop 内容里提取 Exec 的第一个 token（必须是绝对路径）。
func parseDesktop(content string) []Tool {
	out := make([]Tool, 0, 2)
	seen := make(map[string]bool, 2)
	for _, line := range splitLines(content) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Exec=") {
			continue
		}
		cmd := strings.TrimSpace(strings.TrimPrefix(line, "Exec="))
		if cmd == "" {
			continue
		}
		fields := strings.Fields(cmd)
		if len(fields) == 0 {
			continue
		}
		bin := strings.Trim(fields[0], `"'`)
		if !filepath.IsAbs(bin) || seen[bin] {
			// 非绝对路径（或依赖 PATH）的启动项没有可靠路径，交回 L1/L2。
			continue
		}
		if _, ok := statExecutable(bin); !ok {
			continue
		}
		seen[bin] = true
		out = append(out, Tool{
			Name:       displayName(bin),
			Path:       bin,
			Kind:       "desktop",
			ScanSource: "desktop",
			Healthy:    true,
		})
	}
	return out
}

// ---- L5：全盘 ----

// scanGlobal 先试 plocate，失败则退化为 find（-xdev + prune）。
// 整体受 ScanTimeout 控制：超时只保留已解析的部分结果，不返回错误。
func (d *Discoverer) scanGlobal(ctx context.Context) []Tool {
	paths := d.globalViaPlocate(ctx)
	if len(paths) == 0 {
		paths = d.globalViaFind(ctx)
	}
	out := make([]Tool, 0, len(paths))
	for _, p := range paths {
		if _, ok := statExecutable(p); !ok {
			continue
		}
		out = append(out, Tool{
			Name:       displayName(p),
			Path:       p,
			Kind:       "binary",
			ScanSource: "global",
			Healthy:    true,
		})
	}
	return out
}

// globalViaPlocate 用 plocate 的 regex 模式做索引查询（最快路径）。
func (d *Discoverer) globalViaPlocate(ctx context.Context) []string {
	if ctx.Err() != nil {
		return nil
	}
	// 不可用 / 超时 / 无结果都交给调用方用 find 兜底；这里只保留已拿到的行。
	out, _ := d.runCommand(ctx, d.scanTimeout, "plocate", "-r", "/bin/|/tools/")
	return parsePathList(out)
}

// globalViaFind 用 find 做兜底全盘扫描，严格 -xdev + prune，结果有上限。
func (d *Discoverer) globalViaFind(ctx context.Context) []string {
	if ctx.Err() != nil {
		return nil
	}
	roots := make([]string, 0, len(d.scanRoots))
	for _, r := range d.scanRoots {
		if info, err := os.Stat(r); err == nil && info.IsDir() {
			roots = append(roots, r)
		}
	}
	if len(roots) == 0 {
		return nil
	}

	args := make([]string, 0, len(roots)+24)
	args = append(args, roots...)
	args = append(args, "-xdev", "(")
	for i, p := range prunePatterns {
		if i > 0 {
			args = append(args, "-o")
		}
		args = append(args, "-path", p)
	}
	args = append(args, "-o", "-name", "node_modules", ")",
		"-prune", "-o", "-type", "f", "-perm", "-u+x", "-print")

	out, _ := d.runCommand(ctx, d.scanTimeout, "find", args...)
	// 无论是正常结束、权限报错还是超时被杀，都按已拿到的行解析（保留部分结果）。
	return parsePathList(out)
}

// parsePathList 把"一行一个路径"的输出解析成绝对路径切片（去重 + 上限）。
func parsePathList(s string) []string {
	if s == "" {
		return nil
	}
	seen := make(map[string]bool, 64)
	out := make([]string, 0, 64)
	for _, line := range splitLines(s) {
		if len(out) >= maxGlobalResults {
			break
		}
		p := strings.TrimSpace(line)
		if p == "" || !filepath.IsAbs(p) || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// ---- 通用小工具 ----

// splitLines 按 \n 切分并去掉 \r。
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// listExecutables 列出目录下的可执行文件绝对路径（不递归，ctx 可中断）。
func listExecutables(ctx context.Context, dir string) []string {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if _, ok := statExecutable(path); !ok {
			continue
		}
		out = append(out, path)
	}
	return out
}

// statExecutable 判断是否"可执行文件"，并返回 FileInfo。
func statExecutable(path string) (os.FileInfo, bool) {
	if path == "" {
		return nil, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, false
	}
	return info, isExecutable(info, path)
}

// isExecutable 判断文件是否可执行（Windows 按扩展名，其它按权限位）。
func isExecutable(info os.FileInfo, path string) bool {
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".exe", ".com", ".bat", ".cmd", ".ps1":
			return true
		}
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// displayName 取文件名作为工具名（Windows 下剥掉可执行扩展名）。
func displayName(path string) string {
	base := filepath.Base(path)
	if runtime.GOOS == "windows" {
		ext := strings.ToLower(filepath.Ext(base))
		switch ext {
		case ".exe", ".com", ".bat", ".cmd", ".ps1":
			base = strings.TrimSuffix(base, filepath.Ext(base))
		}
	}
	return base
}

func pathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// cleanDirs 去空白、去空项、去重（保序）。
func cleanDirs(dirs []string) []string {
	out := make([]string, 0, len(dirs))
	seen := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// expandDirs 展开 ~ 与 glob，跳过不存在的目录（保序去重）。
func expandDirs(dirs []string) []string {
	out := make([]string, 0, len(dirs))
	seen := make(map[string]bool, len(dirs))
	add := func(p string) {
		p = filepath.Clean(p)
		if p == "" || p == "." || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, d := range dirs {
		d = expandHome(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if strings.ContainsAny(d, "*?[") {
			matches, err := filepath.Glob(d)
			if err != nil {
				continue
			}
			for _, m := range matches {
				if info, err := os.Stat(m); err == nil && info.IsDir() {
					add(m)
				}
			}
			continue
		}
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			add(d)
		}
	}
	return out
}

// expandHome 把 "~/x" 展开成 $HOME/x。
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		if p == "~" {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	return p
}
