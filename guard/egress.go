package guard

import (
	"context"
	"strings"
)

// P8-6：出口闸覆盖 Bash / WebFetch。
//
// 为什么：netguard 原本只拦 execx/remote/recheck/replay/adapter —— worker 最核心的
// Bash 完全不受它管，模型一句 `curl --noproxy '*' http://target` 就绕过整个出口体系，
// 而"不泄露本机 IP"是这个项目的底线之一。
//
// 纪律：拦截文案是**环境语气**（"出口未就绪"），不是权限语气（不写"禁止"）。
// 模型看到"禁止"会开始脑补规则并尝试绕过（ADR D01）。

// EgressCheck 由 server 注入：返回 error 即认为出口不可用。
var EgressCheck func(context.Context) error

// EgressToolFilter 判断一次工具调用是否"要发网络包"。
// 为 nil 时用内置词表 egressIntent（保守：宁可多问一次）。
var EgressToolFilter func(tool, cmd string) bool

// netTools 是"一执行就会发包"的命令词表。
// 保守起见把 python/node/git/pip 也放进来 —— 它们都能发网络请求。
var netTools = map[string]bool{
	"curl": true, "wget": true, "nc": true, "ncat": true, "netcat": true, "socat": true,
	"ssh": true, "scp": true, "sftp": true, "ftp": true, "telnet": true, "rsync": true,
	"mysql": true, "psql": true, "mssql-cli": true, "sqlcmd": true, "redis-cli": true,
	"mongosh": true, "mongo": true,
	"nmap": true, "masscan": true, "ffuf": true, "gobuster": true, "feroxbuster": true,
	"dirb": true, "dirsearch": true, "wfuzz": true, "nuclei": true, "httpx": true,
	"subfinder": true, "amass": true, "assetfinder": true, "dnsx": true,
	"dig": true, "host": true, "nslookup": true, "whois": true, "ping": true, "traceroute": true,
	"openssl": true, "sqlmap": true, "hydra": true, "medusa": true, "netexec": true,
	"crackmapexec": true, "smbclient": true, "ldapsearch": true, "rpcclient": true,
	"git": true, "pip": true, "pip3": true, "npm": true, "node": true, "python": true, "python3": true,
	"chisel": true, "ligolo": true, "proxychains": true, "proxychains4": true,
}

// EgressExemptTools 是永不触发出口校验的工具（纯本地计算）。
var EgressExemptTools = map[string]bool{
	"Read": true, "Write": true, "Edit": true, "MultiEdit": true,
	"LS": true, "Glob": true, "Grep": true, "Sleep": true, "TodoWrite": true,
}

// egressIntent 判断这次调用是否需要先校验出口。
func egressIntent(tool, cmd string) bool {
	if EgressExemptTools[tool] {
		return false
	}
	switch tool {
	case "WebFetch", "web_fetch", "WebSearch", "web_search":
		return true
	}
	if strings.TrimSpace(cmd) == "" {
		return false
	}
	// 命令里出现显式 URL：一定要校验（不管首词是什么）
	low := strings.ToLower(cmd)
	if strings.Contains(low, "http://") || strings.Contains(low, "https://") {
		return true
	}
	first := strings.ToLower(firstCommand(cmd))
	if first == "" {
		return false
	}
	return netTools[first]
}

// egressNeeded 是给 preToolUse 用的判定入口（允许外部覆盖词表）。
func egressNeeded(tool, cmd string) bool {
	if EgressToolFilter != nil {
		return EgressToolFilter(tool, cmd)
	}
	return egressIntent(tool, cmd)
}

// egressBlockMessage 生成环境语气的拦截文案。
// 刻意不出现"禁止/不允许"——那是权限口吻，会诱导模型尝试绕过。
func egressBlockMessage(reason string) string {
	msg := "出口未就绪：环境不通。"
	if reason != "" {
		msg += "（" + reason + "）"
	}
	return msg + "这不是目标侧防御。请起代理，或到「系统 → 网络」改代理；" +
		"只打本地靶场可在启动时设 ARTEX_ALLOW_DIRECT=1。纯本地计算请改用 Read/Write/Grep 这类工具。"
}
