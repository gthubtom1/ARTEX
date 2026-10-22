package verify

import (
	"regexp"
	"strings"
)

// 「证据里到底有没有真的发过请求 / 跑过命令」的判定。
//
// 为什么需要这一层：原来的 grounding 门只看「非空 + ≥24 字符 + 没有猜测词」，
// 于是 "ThinkPHP 5.0.23 存在 CVE-2018-20062" 这种只有版本号的推断会被判成
// verified —— 三道门形同虚设。真正的 grounded 证据必须留下"发过包 / 跑过命令"的痕迹。
//
// 判定思路：只要求命中**任意一类**痕迹，不做组合校验（组合会误杀大量合法证据）。
//
//	A. HTTP 请求行 / 响应状态行
//	B. shell 提示符 + 命令，或命令执行回显的强特征（uid=、root:x:、目录列表）
//	C. 已知工具的固定输出头（nmap report / nuclei / httpx / sqlmap）
//	D. 带 --data/-d/-X 的 curl/wget 命令行，且后面跟了响应内容
var (
	reRequestLine = regexp.MustCompile(`(?m)^\s*(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|CONNECT|TRACE)\s+\S+`)
	reStatusLine  = regexp.MustCompile(`(?mi)^\s*HTTP/\d(\.\d)?\s+\d{3}`)
	reShellPrompt = regexp.MustCompile(`(?m)^\s*(\$|#|PS [A-Z]:\\[^>]*>|C:\\[^>]*>)\s*\S`)
	reCmdEcho     = regexp.MustCompile(`(?i)(uid=\d+\(|gid=\d+\(|root:x:0:0|total \d+|drwxr-xr-x|-rw-r--r--|volume serial number|nt authority\\)`)
	reToolHeader  = regexp.MustCompile(`(?i)(nmap scan report|host is up|nuclei|sqlmap|interactsh|dnslog|burp|ffuf|gobuster|dirsearch|masscan|httpx|parameter: .*\(|injection point|payload: |<title>|set-cookie:|content-length:|server: )`)
	reHTTPCommand = regexp.MustCompile(`(?i)\b(curl|wget|nc|ncat|socat)\b[^\n]{0,400}?(https?://|--data|-d |--cookie|-H )`)
	reCVEOnly     = regexp.MustCompile(`(?i)(cve-\d{4}-\d{4,7}|存在.{0,6}漏洞|version.{0,20}v?\d+\.\d+)`)
)

// LooksLikeTrace 判断一段证据里有没有"真的发过请求 / 跑过命令"的痕迹。
// 返回 (命中类别列表, 是否至少命中一类)。
func LooksLikeTrace(evidence string) ([]string, bool) {
	ev := strings.TrimSpace(evidence)
	if ev == "" {
		return nil, false
	}
	var hits []string
	if reRequestLine.MatchString(ev) || reStatusLine.MatchString(ev) {
		hits = append(hits, "http")
	}
	if reShellPrompt.MatchString(ev) || reCmdEcho.MatchString(ev) {
		hits = append(hits, "shell")
	}
	if reToolHeader.MatchString(ev) {
		hits = append(hits, "tool")
	}
	if reHTTPCommand.MatchString(ev) {
		hits = append(hits, "command")
	}
	return hits, len(hits) > 0
}

// CVEClaimWithoutTrace 判断这段证据是不是"只有版本号/CVE 断言、没有任何触发记录"。
func CVEClaimWithoutTrace(evidence string) bool {
	ev := strings.TrimSpace(evidence)
	if ev == "" {
		return false
	}
	if _, ok := LooksLikeTrace(ev); ok {
		return false
	}
	return reCVEOnly.MatchString(ev)
}
