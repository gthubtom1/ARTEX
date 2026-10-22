package env

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"time"
)

// 指纹：给"认不出来"的二进制算 sha256，并用 strings 里的关键子串猜身份。
//
// 约束：
//   - 只对 ≤ 200MB 的文件算 sha256，且总量有上限，避免全盘扫描时把机器拖死；
//   - strings 只对"文件名不在已知列表"的二进制跑，且输出截断到 2000 行；
//   - 全程检查 ctx，超时/取消立即收工（已算出的指纹保留）。

const (
	// maxHashSize 只给 ≤ 200MB 的文件算 sha256。
	maxHashSize = 200 << 20
	// maxHashFiles 限制一次扫描最多算多少个哈希。
	maxHashFiles = 2000
	// maxStringsCandidates 限制一次扫描最多跑多少个 strings。
	maxStringsCandidates = 2000
	// maxStringsSize 只对 ≤ 50MB 的文件跑 strings（大文件跑 strings 太慢）。
	maxStringsSize = 50 << 20
	// stringsLines 是 `strings | head -2000` 的行数上限。
	stringsLines = 2000
)

// fingerprintKeywords 是用于猜身份的关键子串，顺序即优先级。
var fingerprintKeywords = []string{
	"nmap",
	"nuclei",
	"sqlmap",
	"ffuf",
	"gobuster",
	"masscan",
	"hydra",
	"chisel",
	"feroxbuster",
	"john",
	"netexec",
}

// knownNames 是"文件名已可识别"的集合：命中则不再用 strings 猜。
var knownNames = func() map[string]bool {
	names := map[string]bool{
		// 常见系统工具，避免对每个 /usr/bin 项都跑 strings
		"bash": true, "sh": true, "dash": true, "zsh": true, "ls": true, "cat": true,
		"cp": true, "mv": true, "rm": true, "ln": true, "chmod": true, "chown": true,
		"grep": true, "sed": true, "awk": true, "tar": true, "gzip": true, "gunzip": true,
		"find": true, "xargs": true, "less": true, "more": true, "echo": true, "env": true,
		"perl": true, "ruby": true, "node": true, "php": true, "java": true, "openssl": true,
		"ssh": true, "scp": true, "ssh-keygen": true, "systemctl": true, "apt": true,
		"apt-get": true, "dpkg": true, "pip": true, "pip3": true, "npm": true, "gem": true,
		"git": true, "docker": true, "make": true, "gcc": true, "psql": true, "mysql": true,
		"file": true, "strings": true, "ldd": true, "readelf": true, "objdump": true,
		"nc": true, "ncat": true, "netcat": true, "socat": true, "tcpdump": true,
	}
	for _, name := range keyTools {
		names[strings.ToLower(name)] = true
	}
	for name := range versionProbes {
		names[strings.ToLower(name)] = true
	}
	for name := range capTable {
		names[strings.ToLower(name)] = true
	}
	return names
}()

// fingerprint 是扫描的最后一步：补 sha256，并对未知名字的二进制猜身份。
func (d *Discoverer) fingerprint(ctx context.Context, tools []Tool) []Tool {
	if ctx == nil {
		ctx = context.Background()
	}
	hashed := 0
	for i := range tools {
		if ctx.Err() != nil {
			return tools
		}
		if tools[i].Path == "" || tools[i].SHA256 != "" {
			continue
		}
		if hashed >= maxHashFiles {
			break
		}
		info, ok := statExecutable(tools[i].Path)
		if !ok || info.Size() > maxHashSize {
			continue
		}
		hashed++
		if sum, ok := hashFile(ctx, tools[i].Path); ok {
			tools[i].SHA256 = sum
		}
	}

	guessed := 0
	for i := range tools {
		if ctx.Err() != nil {
			return tools
		}
		t := &tools[i]
		if t.Path == "" || t.ScanSource == "fingerprint" {
			continue
		}
		if knownNames[strings.ToLower(t.Name)] {
			continue
		}
		if guessed >= maxStringsCandidates {
			break
		}
		if info, ok := statExecutable(t.Path); !ok || info.Size() > maxStringsSize {
			continue
		}
		guessed++
		name := d.guessName(ctx, t.Path)
		if name == "" {
			continue
		}
		t.Name = name
		t.ScanSource = "fingerprint"
		if caps := capsFor(name); len(caps) > 0 {
			t.Caps = caps
		}
	}
	return tools
}

// guessName 跑 `strings -n 8 <path> | head -2000` 并匹配关键子串。
func (d *Discoverer) guessName(ctx context.Context, path string) string {
	if _, ok := statExecutable(path); !ok {
		return ""
	}
	out, err := d.runCommand(ctx, d.probeTimeout(), "strings", "-n", "8", path)
	if out == "" {
		_ = err // 只关心有没有输出；strings 不存在/超时都当“识别不出”
		return ""
	}
	// 等价于 `| head -2000`：只关心前 2000 行。
	lines := splitLines(out)
	if len(lines) > stringsLines {
		lines = lines[:stringsLines]
	}
	blob := strings.ToLower(strings.Join(lines, "\n"))
	for _, kw := range fingerprintKeywords {
		if strings.Contains(blob, kw) {
			return kw
		}
	}
	return ""
}

// probeTimeout 返回版本/strings 探测的超时上限（不超过单层超时）。
func (d *Discoverer) probeTimeout() time.Duration {
	t := probeTimeout
	if d.scanTimeout > 0 && d.scanTimeout < t {
		t = d.scanTimeout
	}
	return t
}

// hashFile 分块计算 sha256，每块检查 ctx（超时立即放弃）。
func hashFile(ctx context.Context, path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		if ctx != nil && ctx.Err() != nil {
			return "", false
		}
		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := h.Write(buf[:n]); werr != nil {
				return "", false
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", false
		}
	}
	return hex.EncodeToString(h.Sum(nil)), true
}
