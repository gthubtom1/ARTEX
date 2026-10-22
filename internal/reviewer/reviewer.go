// Package reviewer implements the zero-token intercept auto-decision rules.
//
// The reviewer is deliberately deterministic: it never calls a model. It is used
// before an "ask" approval is escalated so routine read-only reconnaissance can
// proceed automatically, destructive actions are blocked immediately, and only
// genuinely ambiguous actions still require a human.
package reviewer

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Decision is the reviewer's verdict.
type Decision string

const (
	Allow Decision = "allow"
	Deny  Decision = "deny"
	Ask   Decision = "ask"
)

// Request is the normalized tool call handed to the reviewer.
type Request struct {
	Tool            string
	Target          string
	Command         string
	Args            string
	TaskConstraints []string
}

// Result is one reviewer verdict plus a human-readable reason.
type Result struct {
	Decision   Decision
	Reason     string
	Confidence string // high | medium | low
}

// Uncertain reports whether the reviewer could not make a terminal decision.
func Uncertain(r Result) bool { return r.Decision == Ask }

var (
	reDeleteFrom = regexp.MustCompile(`(?i)\bdelete\s+from\b`)
	reWhere      = regexp.MustCompile(`(?i)\bwhere\b`)

	reDestructive = []struct {
		label string
		re    *regexp.Regexp
	}{
		{"rm -rf /", regexp.MustCompile(`(?i)\brm\s+(?:-[a-z]*r[a-z]*f[a-z]*|-[a-z]*f[a-z]*r[a-z]*|-[a-z]*r[a-z]*\s+-[a-z]*f[a-z]*|-[a-z]*f[a-z]*\s+-[a-z]*r[a-z]*)\s+/(?:\*)?(?:\s|$|[;&|])`)}, {"mkfs", regexp.MustCompile(`(?i)\bmkfs(?:\.[a-z0-9]+)?\b`)},
		{"dd of=/dev/", regexp.MustCompile(`(?i)\bdd\b[^\n;&|]*\bof\s*=\s*/dev/`)},
		{"shutdown", regexp.MustCompile(`(?i)\b(shutdown|poweroff|halt|reboot)\b`)},
		{"DROP DATABASE", regexp.MustCompile(`(?i)\bdrop\s+database\b`)},
		{"mysqldump --all-databases", regexp.MustCompile(`(?i)\bmysqldump\b[^\n;&|]*--all-databases\b`)},
		{"sqlmap --dump", regexp.MustCompile(`(?i)\bsqlmap\b[^\n;&|]*(--dump-all|--dump)\b`)},
	}

	reURLHost = regexp.MustCompile(`(?i)https?://[^\s'"<>]+`)
	reIPv4    = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reCIDR    = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}/\d{1,2}\b`)
	reDomain  = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+(?:com|net|org|io|cn|co|info|biz|me|dev|app|xyz|top|site|online|tech|cloud|ai|local|internal|test|example|edu|gov|mil)\b`)

	reDenyMarker  = regexp.MustCompile(`(?i)(禁止|不允许|不得|严禁|不能|不可以|不要|\b(?:deny|forbid|forbidden|prohibit|prohibited|disallow|not\s+allowed|never)\b)`)
	reAllowMarker = regexp.MustCompile(`(?i)(允许|只测|只能|只允许|仅限|限定|范围|白名单|限于|\b(?:allow|allowed|only|scope|in-scope|target)\b)`)

	reCurlW = regexp.MustCompile(`(?i)(?:^|\s)(?:-x|--request)\s+(?:post|put|patch|delete)\b|(?:^|\s)(?:-d|--data|--data-raw|--data-binary|-f|--form|-t|--upload-file|-o|--output|-O)(?:\s|=|$)`)
	reNCZ   = regexp.MustCompile(`(?i)\s-z(?:\s|$)`)
)

// Review evaluates one tool call using only local rules.
//
// Priority:
//  1. task constraints / scope boundary → deny
//  2. destructive command → deny
//  3. clearly read-only command/tool → allow
//  4. everything else (writes, uploads, brute force, exploitation) → ask
func Review(req Request) Result {
	if res, ok := reviewConstraints(req); ok {
		return res
	}
	if label, ok := destructiveHit(req.Command); ok {
		return Result{Decision: Deny, Reason: fmt.Sprintf("破坏性命令命中 %q", label), Confidence: "high"}
	}
	if readOnly(req) {
		return Result{Decision: Allow, Reason: "明确的只读侦察/查看操作，无写入或破坏风险", Confidence: "high"}
	}
	return Result{
		Decision:   Ask,
		Reason:     "需要人工确认：写操作、上传、爆破或提权利用等非只读动作无法由本地规则安全放行",
		Confidence: "low",
	}
}

func reviewConstraints(req Request) (Result, bool) {
	denyLines, scopeLines := splitConstraints(req.TaskConstraints)
	if len(denyLines) == 0 && len(scopeLines) == 0 {
		return Result{}, false
	}

	hosts := extractHosts(req.Target)
	hosts = append(hosts, extractHosts(req.Command)...)
	hosts = uniqueStrings(hosts)

	for _, line := range denyLines {
		if lineHitsHosts(line, hosts) || lineMentionsTarget(line, req) {
			return Result{
				Decision:   Deny,
				Reason:     fmt.Sprintf("目标命中任务约束 deny 列表：%s", strings.TrimSpace(line)),
				Confidence: "high",
			}, true
		}
	}

	// Only enforce an out-of-scope rule when the constraints actually define an
	// allow/scope list. A task with only deny constraints must not have every
	// unrelated target treated as out of scope.
	if len(scopeLines) > 0 && len(hosts) > 0 {
		for _, h := range hosts {
			if !hostInScope(h, scopeLines) {
				return Result{
					Decision:   Deny,
					Reason:     fmt.Sprintf("目标 %q 不在任务约束允许范围内", h),
					Confidence: "high",
				}, true
			}
		}
	}
	return Result{}, false
}

func splitConstraints(list []string) (denyLines, scopeLines []string) {
	for _, raw := range list {
		c := strings.TrimSpace(raw)
		if c == "" {
			continue
		}
		switch {
		case reDenyMarker.MatchString(c):
			denyLines = append(denyLines, c)
		case reAllowMarker.MatchString(c):
			scopeLines = append(scopeLines, c)
		case len(extractHosts(c)) > 0:
			scopeLines = append(scopeLines, c)
		}
	}
	return denyLines, scopeLines
}

func destructiveHit(cmd string) (string, bool) {
	if strings.TrimSpace(cmd) == "" {
		return "", false
	}
	for _, stmt := range splitSegments(cmd) {
		if reDeleteFrom.MatchString(stmt) && !reWhere.MatchString(stmt) {
			return "DELETE FROM 无 WHERE", true
		}
	}
	for _, rule := range reDestructive {
		if rule.re.MatchString(cmd) {
			return rule.label, true
		}
	}
	return "", false
}

func readOnly(req Request) bool {
	switch req.Tool {
	case "WebFetch", "web_search", "Read", "Grep", "Glob", "LS":
		if strings.TrimSpace(req.Command) == "" {
			return true
		}
	}
	cmd := strings.TrimSpace(req.Command)
	if cmd == "" {
		return false
	}
	segments := splitSegments(cmd)
	if len(segments) == 0 {
		return false
	}
	for _, seg := range segments {
		if !readOnlySegment(seg) {
			return false
		}
	}
	return true
}

func readOnlySegment(seg string) bool {
	seg = strings.TrimSpace(seg)
	if seg == "" {
		return false
	}
	// A redirection writes a local file; keep those on the ask path.
	if strings.Contains(seg, ">") {
		return false
	}
	name, rest := commandName(seg)
	if name == "" {
		return false
	}
	switch name {
	case "ls", "cat", "head", "tail", "less", "more", "file", "stat", "pwd", "id", "whoami", "wc", "grep", "egrep", "fgrep", "cut", "sort", "uniq", "tr", "echo", "which", "type":
		return true
	case "dig", "whois", "host", "nslookup":
		return true
	case "nmap":
		if strings.Contains(seg, "--script") {
			return false
		}
		return strings.Contains(rest, "-sT") || strings.Contains(rest, "-sV")
	case "curl":
		return !reCurlW.MatchString(seg)
	case "nc", "netcat", "ncat":
		return reNCZ.MatchString(seg)
	}
	return false
}

func commandName(seg string) (string, string) {
	fields := strings.Fields(seg)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if strings.Contains(f, "=") && !strings.HasPrefix(f, "/") && !strings.HasPrefix(f, "./") {
			continue
		}
		switch f {
		case "sudo", "env", "nohup", "nice", "stdbuf", "proxychains", "proxychains4":
			continue
		case "timeout":
			if i+1 < len(fields) {
				i++
			}
			continue
		}
		if strings.HasPrefix(f, "-") {
			continue
		}
		if idx := strings.LastIndex(f, "/"); idx >= 0 {
			f = f[idx+1:]
		}
		return f, strings.Join(fields[i+1:], " ")
	}
	return "", ""
}

func splitSegments(cmd string) []string {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n", "\r", "\n")
	parts := strings.Split(replacer.Replace(cmd), "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func extractHosts(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, raw := range reURLHost.FindAllString(s, -1) {
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			out = append(out, normalizeHost(u.Hostname()))
		}
	}
	for _, raw := range reIPv4.FindAllString(s, -1) {
		if net.ParseIP(raw) != nil {
			out = append(out, raw)
		}
	}
	for _, raw := range reDomain.FindAllString(s, -1) {
		out = append(out, normalizeHost(raw))
	}
	return uniqueStrings(out)
}

func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.TrimSuffix(h, ".")
	if i := strings.Index(h, ":"); i > 0 && !strings.Contains(h, "]") {
		// Strip a port from host:port (but leave bare IPv6 alone).
		if net.ParseIP(h) == nil {
			h = h[:i]
		}
	}
	return h
}

func lineHitsHosts(line string, hosts []string) bool {
	if len(hosts) == 0 {
		return false
	}
	for _, cidr := range reCIDR.FindAllString(line, -1) {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		for _, h := range hosts {
			if ip := net.ParseIP(h); ip != nil && ipnet.Contains(ip) {
				return true
			}
		}
	}
	scopeHosts := extractHosts(line)
	for _, h := range hosts {
		for _, sh := range scopeHosts {
			if hostMatches(h, sh) {
				return true
			}
		}
	}
	low := strings.ToLower(line)
	for _, h := range hosts {
		if strings.Contains(low, h) {
			return true
		}
	}
	return false
}

func lineMentionsTarget(line string, req Request) bool {
	for _, t := range []string{req.Target, req.Command} {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && strings.Contains(strings.ToLower(line), t) {
			return true
		}
	}
	return false
}

func hostInScope(host string, scopeLines []string) bool {
	for _, line := range scopeLines {
		for _, cidr := range reCIDR.FindAllString(line, -1) {
			_, ipnet, err := net.ParseCIDR(cidr)
			if err != nil {
				continue
			}
			if ip := net.ParseIP(host); ip != nil && ipnet.Contains(ip) {
				return true
			}
		}
		for _, sh := range extractHosts(line) {
			if hostMatches(host, sh) {
				return true
			}
		}
		if strings.Contains(strings.ToLower(line), host) {
			return true
		}
	}
	return false
}

func hostMatches(host, scope string) bool {
	host, scope = normalizeHost(host), normalizeHost(scope)
	if host == "" || scope == "" {
		return false
	}
	return host == scope || strings.HasSuffix(host, "."+scope)
}
