package env

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
)

// 缺工具时“该怎么装”——把提示从“去环境页装”变成一条能直接复制的命令。
//
// 背景（2026-10-24 实测）：任务里报了 tool_missing，错误卡只说“到系统→环境安装”，
// 而环境页只列工具、不给命令，也不覆盖任务里真正缺的工具；用户进去以后不知道该干什么，
// 只能自己猜 apt 包名。这里把「工具名 → 包名 → 命令」做成确定性映射。
//
// 纪律：只写**确定**的映射；没收录的工具给通用兜底（apt search / apt-file），
// 不编造包名（装错包比装不上更糟）。

// installPkgs 把命令名映射到发行版包名（同一个包里有多个可执行文件时都指向它）。
var installPkgs = map[string]string{
	"nmap": "nmap", "masscan": "masscan",
	"ffuf": "ffuf", "gobuster": "gobuster", "feroxbuster": "feroxbuster",
	"nuclei": "nuclei", "subfinder": "subfinder", "httpx": "httpx-toolkit",
	"naabu": "naabu", "katana": "katana",
	"sqlmap": "sqlmap", "hydra": "hydra", "john": "john",
	"hashcat": "hashcat", "medusa": "medusa",
	"chisel": "chisel", "sshpass": "sshpass", "proxychains4": "proxychains4",
	"proxychains": "proxychains4",
	"smbclient": "smbclient", "ldapsearch": "ldap-utils",
	"netexec": "netexec", "nxc": "netexec",
	"impacket-psexec": "python3-impacket", "psexec.py": "python3-impacket",
	"conntrack":  "conntrack",
	"curl":       "curl",
	"jq":         "jq",
	"python3":    "python3",
	"go":         "golang-go",
	"gcc":        "gcc",
	"make":       "make",
	"git":        "git",
	"openssl":    "openssl",
	"tcpdump":    "tcpdump",
	"tshark":     "tshark",
	"responder":  "responder",
	"impacket":   "python3-impacket",
	"bloodhound": "bloodhound",
	"enum4linux": "enum4linux",
	"smbmap":     "smbmap",
	"rpcclient":  "samba-common-bin",
	"dig":        "dnsutils",
	"whois":      "whois",
	"nikto":      "nikto",
	"wpscan":     "wpscan",
	"dirb":       "dirb",
	"wfuzz":      "wfuzz",
	"arjun":      "arjun",
	"dalfox":     "dalfox",
}

// InstallHint 返回缺某工具时的安装建议：包名 + 可直接执行的命令。
// 认不出的工具返回包名为空、命令为“怎么自己找”，绝不瞎猜包名。
func InstallHint(name string) (pkg string, cmd string) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return "", ""
	}
	pkg = installPkgs[n]
	switch runtime.GOOS {
	case "linux":
		if pkg != "" {
			return pkg, "sudo apt install -y " + pkg
		}
		return "", "apt search " + n + "      # 找包名；或 apt-file search bin/" + n
	case "darwin":
		if pkg != "" {
			return pkg, "brew install " + pkg
		}
		return "", "brew search " + n
	default:
		// Windows / 其它：不假装知道包管理器。
		return pkg, ""
	}
}

// InstallHintBatch 给一批缺工具出一条命令（同一发行版的包管理器语法）。
// 认不出的工具单独列在后面，不混进命令行里（避免一条命令里塞进不存在的包名、整条失败）。
func InstallHintBatch(names []string) string {
	if len(names) == 0 {
		return ""
	}
	var pkgs, unknown []string
	seen := map[string]bool{}
	for _, n := range names {
		pkg, _ := InstallHint(n)
		if pkg == "" {
			unknown = append(unknown, n)
			continue
		}
		if seen[pkg] {
			continue
		}
		seen[pkg] = true
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	out := ""
	if len(pkgs) > 0 {
		switch runtime.GOOS {
		case "linux":
			out = "sudo apt install -y " + strings.Join(pkgs, " ")
		case "darwin":
			out = "brew install " + strings.Join(pkgs, " ")
		}
	}
	if len(unknown) > 0 {
		tail := "未收录包名的工具：" + strings.Join(unknown, " ") + "（用 apt search 找一个）"
		if out == "" {
			return tail
		}
		return out + "\n# " + tail
	}
	return out
}

// DescribeMissing 给前端用：每个缺失工具带上包名与命令。
func DescribeMissing(names []string) []MissingTool {
	out := make([]MissingTool, 0, len(names))
	for _, n := range names {
		pkg, cmd := InstallHint(n)
		out = append(out, MissingTool{Name: n, Package: pkg, Install: cmd})
	}
	return out
}

// MissingTool 是一条"缺什么 + 怎么装"。
type MissingTool struct {
	Name    string `json:"name"`
	Package string `json:"package,omitempty"`
	Install string `json:"install,omitempty"`
}

// MustFormatInstall 仅用于日志/测试里拼一句话。
func MustFormatInstall(names []string) string {
	if c := InstallHintBatch(names); c != "" {
		return c
	}
	return fmt.Sprintf("缺 %d 个工具（无已知安装命令）", len(names))
}
