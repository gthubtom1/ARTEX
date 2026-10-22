// Package harvest 是「凭据收割信号」：从配置文本、命令历史、云元数据里
// 识别凭据（连接串 / AK-SK / JWT / 私钥 / sshpass / mysql -p / curl -u …），
// 供 agent 逐条写入加密凭据库。
//
// 纪律：明文只交给保险库；这里提供的 Redact 只用于日志/工具返回，绝不回显完整秘密。
package harvest

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Kind 取值。
const (
	KindPassword = "password"
	KindHash     = "hash"
	KindToken    = "token"
	KindKey      = "key"
	KindSSHKey   = "ssh_key"
	KindConnStr  = "conn_str"
)

// 置信度取值。
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

var validKinds = map[string]bool{
	KindPassword: true, KindHash: true, KindToken: true,
	KindKey: true, KindSSHKey: true, KindConnStr: true,
}

// Signal 是一条待入库的凭据信号。
type Signal struct {
	Kind       string `json:"kind"`
	Username   string `json:"username,omitempty"`
	Secret     string `json:"secret"`
	Source     string `json:"source,omitempty"`
	Confidence string `json:"confidence"`
	AssetHint  string `json:"asset_hint,omitempty"`
}

// Redact 只保留前2后2，用于日志与工具返回（短值整体打码）。
func Redact(secret string) string {
	if secret == "" {
		return ""
	}
	r := []rune(secret)
	n := len(r)
	if n <= 4 {
		return strings.Repeat("*", n)
	}
	return string(r[:2]) + strings.Repeat("*", n-4) + string(r[n-2:])
}

// ───────────────────────── 收集器 ─────────────────────────

type collector struct {
	source string
	seen   map[string]bool
	out    []Signal
}

func (c *collector) add(sig Signal) {
	if strings.TrimSpace(sig.Secret) == "" || !validKinds[sig.Kind] {
		return
	}
	if sig.Confidence == "" {
		sig.Confidence = ConfidenceMedium
	}
	switch {
	case c.source == "":
	case sig.Source == "":
		sig.Source = c.source
	default:
		sig.Source = c.source + ":" + sig.Source
	}
	key := sig.Kind + "\x00" + strings.ToLower(strings.TrimSpace(sig.Username)) + "\x00" + sig.Secret
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	c.out = append(c.out, sig)
}

func newCollector(source string) *collector {
	return &collector{source: source, seen: map[string]bool{}}
}

// FromText 从任意文本（.env / yaml / properties / php define / 连接串 /
// bash history / 私钥块 …）收割凭据信号。
func FromText(name, content string) []Signal {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	src := strings.TrimSpace(name)
	if src == "" {
		src = "text"
	}
	c := newCollector(src)
	c.privateKeys(content)
	c.connStrings(content)
	c.jwts(content)
	c.awsKeys(content)
	c.cliHistory(content)
	c.envPairs(content)
	c.hashes(content)
	return c.out
}

// FromEnv 从环境变量键值对收割（AWS AK/SK 自动配对）。
func FromEnv(env map[string]string) []Signal {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := strings.TrimSpace(env[k])
		if v == "" {
			continue
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	return FromText("env", b.String())
}

// FromMetadata 解析云元数据 JSON（AWS / GCP / 阿里云 IMDS）。
func FromMetadata(jsonText string) []Signal {
	if strings.TrimSpace(jsonText) == "" {
		return nil
	}
	var root any
	if err := json.Unmarshal([]byte(jsonText), &root); err != nil {
		return nil
	}
	flat := map[string]string{}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, val := range t {
				if s, ok := val.(string); ok {
					lk := strings.ToLower(strings.TrimSpace(k))
					if lk == "" {
						continue
					}
					if cur, exists := flat[lk]; !exists || strings.TrimSpace(cur) == "" {
						flat[lk] = s
					}
					continue
				}
				walk(val)
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		}
	}
	walk(root)
	if len(flat) == 0 {
		return nil
	}

	first := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := flat[k]; ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	ak := first("accesskeyid", "access_key_id", "accesskey", "ak", "aws_access_key_id")
	sk := first("secretaccesskey", "secret_access_key", "accesskeysecret", "secretkey", "sk", "aws_secret_access_key")
	token := first("token", "securitytoken", "security_token", "sessiontoken", "session_token", "tokenvalue", "x-amz-security-token")
	priv := first("private_key", "privatekey", "private_key_data", "privatekeydata", "ssh_private_key")
	hint := cloudHint(ak, first("region", "regionid", "location"),
		first("instance-id", "instanceid", "instance_id", "zone", "availability-zone", "project-id", "projectid"))

	c := newCollector("metadata")
	switch {
	case ak != "" && sk != "":
		c.add(Signal{Kind: KindKey, Username: ak, Secret: ak + ":" + sk, Confidence: ConfidenceHigh, AssetHint: hint})
	case ak != "":
		c.add(Signal{Kind: KindKey, Secret: ak, Confidence: ConfidenceHigh, AssetHint: hint})
	case sk != "":
		c.add(Signal{Kind: KindKey, Secret: sk, Confidence: ConfidenceHigh, AssetHint: hint})
	}
	if token != "" {
		c.add(Signal{Kind: KindToken, Secret: token, Confidence: ConfidenceHigh, AssetHint: hint})
	}
	if priv != "" {
		kind := KindKey
		if strings.Contains(priv, "PRIVATE KEY-----") {
			kind = KindSSHKey
		}
		c.add(Signal{Kind: kind, Secret: priv, Confidence: ConfidenceHigh, AssetHint: hint})
	}
	return c.out
}

func cloudHint(ak, region, instance string) string {
	vendor := ""
	switch {
	case strings.HasPrefix(ak, "AKIA"), strings.HasPrefix(ak, "ASIA"):
		vendor = "aws"
	case strings.HasPrefix(ak, "LTAI"):
		vendor = "aliyun"
	case ak != "":
		vendor = "cloud"
	}
	parts := []string{}
	for _, p := range []string{vendor, region, instance} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/")
}

// ───────────────────────── 文本规则 ─────────────────────────

var (
	rePrivateKey = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	reConnURL    = regexp.MustCompile(`(?i)\b(?:mysql|mariadb|postgres|postgresql|mongodb\+srv|mongodb|redis|rediss|mssql|sqlserver|amqp|amqps|ftp|ftps|ldap|ldaps|clickhouse|oracle)://[^\s"'<>` + "`" + `]+`)
	reJWT        = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b`)
	reAWSAK      = regexp.MustCompile(`\b(?:AKIA|ASIA|AGPA|AIDA|AROA|ANPA|ANVA|AIPA)[A-Z0-9]{16}\b`)
	reAWSSK      = regexp.MustCompile(`(?i)(?:aws_secret_access_key|secret_access_key|secretaccesskey|secret_key)["']?\s*[:=]\s*["']?([A-Za-z0-9/+=]{32,64})`)
	reBcrypt     = regexp.MustCompile(`\$(?:2[abxy]|argon2(?:id|i|d)|1|5|6)\$[^\s"'<>]{10,}`)
	reKVLine     = regexp.MustCompile(`^\s*(?:export\s+|set\s+)?([A-Za-z_][A-Za-z0-9_.\-]{0,64})\s*=\s*(.+)$`)
	reYAMLLine   = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_.\-]{0,64})\s*:\s*(.+)$`)
	rePHPDefine  = regexp.MustCompile(`(?i)\bdefine\s*\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]\s*,\s*['"]([^'"]+)['"]\s*\)`)
	reComment    = regexp.MustCompile(`^\s*(?:#|;|//|--|<!--)`)
	reUserAtHost = regexp.MustCompile(`\b([\w.\-]+)@([\w.\-]+)`)
)

func (c *collector) privateKeys(text string) {
	for _, m := range rePrivateKey.FindAllString(text, -1) {
		c.add(Signal{Kind: KindSSHKey, Secret: strings.TrimSpace(m), Confidence: ConfidenceHigh, Source: "pem"})
	}
}

func (c *collector) connStrings(text string) {
	for _, m := range reConnURL.FindAllString(text, -1) {
		raw := strings.TrimRight(m, `,;)]}>.`)
		u, err := url.Parse(raw)
		if err != nil || u.User == nil {
			continue
		}
		user := u.User.Username()
		pass, hasPass := u.User.Password()
		if !hasPass || strings.TrimSpace(pass) == "" {
			continue
		}
		hint := u.Host
		c.add(Signal{Kind: KindConnStr, Username: user, Secret: raw, Confidence: ConfidenceHigh, Source: "connstr", AssetHint: hint})
		// 单独再收一条口令，方便 cred_reuse_plan 直接用 ${USER}/${SECRET} 试服务。
		c.add(Signal{Kind: KindPassword, Username: user, Secret: pass, Confidence: ConfidenceHigh, Source: "connstr", AssetHint: hint})
	}
}

func (c *collector) jwts(text string) {
	for _, m := range reJWT.FindAllString(text, -1) {
		c.add(Signal{Kind: KindToken, Secret: m, Confidence: ConfidenceHigh, Source: "jwt"})
	}
}

func (c *collector) awsKeys(text string) {
	for _, loc := range reAWSAK.FindAllStringIndex(text, -1) {
		ak := text[loc[0]:loc[1]]
		end := loc[1] + 512
		if end > len(text) {
			end = len(text)
		}
		window := text[loc[1]:end]
		sk := ""
		if m := reAWSSK.FindStringSubmatch(window); m != nil {
			sk = m[1]
		}
		sig := Signal{Kind: KindKey, Username: ak, Secret: ak, Confidence: ConfidenceHigh, Source: "aws", AssetHint: "aws"}
		if sk != "" {
			sig.Secret = ak + ":" + sk
		}
		c.add(sig)
	}
}

func (c *collector) cliHistory(text string) {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "sshpass") {
			pass := quotedFlag(line, "-p")
			if pass == "" {
				pass = flagValue(line, "-p", "--password")
			}
			if pass != "" {
				user, host := userAtHost(line)
				c.add(Signal{Kind: KindPassword, Username: user, Secret: pass, Confidence: ConfidenceHigh, Source: "history", AssetHint: host})
			}
		}
		if strings.Contains(lower, "mysql") || strings.Contains(lower, "maria") {
			user := flagValue(line, "-u", "--user")
			pass := quotedFlag(line, "-p")
			if pass == "" {
				pass = flagValue(line, "-p", "--password")
			}
			if pass != "" {
				host := flagValue(line, "-h", "--host")
				c.add(Signal{Kind: KindPassword, Username: user, Secret: pass, Confidence: ConfidenceHigh, Source: "history", AssetHint: host})
			}
		}
		if strings.Contains(lower, "curl") {
			if user, pass, ok := curlAuth(line); ok {
				c.add(Signal{Kind: KindPassword, Username: user, Secret: pass, Confidence: ConfidenceHigh, Source: "history"})
			}
		}
		if strings.Contains(lower, "wget") {
			pass := flagValue(line, "", "--password")
			if pass == "" {
				pass = quotedFlag(line, "--password")
			}
			if pass != "" {
				user := flagValue(line, "", "--user")
				c.add(Signal{Kind: KindPassword, Username: user, Secret: pass, Confidence: ConfidenceHigh, Source: "history"})
			}
		}
	}
}

func (c *collector) envPairs(text string) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	ctxUser := ""
	for _, line := range lines {
		commented := reComment.MatchString(line)
		work := line
		if commented {
			// 注释里的凭据多半是遗留配置，仍然收，但降为低置信度。
			work = reComment.ReplaceAllString(work, "")
		}
		for _, m := range rePHPDefine.FindAllStringSubmatch(work, -1) {
			if kind := classifyKey(m[1]); kind != "" {
				c.addPair(kind, m[1], cleanValue(m[2]), ctxUser, commented)
			}
		}
		key, val, ok := splitKV(work)
		if !ok {
			continue
		}
		if isUserKey(key) && plausibleSecret(cleanValue(val)) {
			ctxUser = cleanValue(val)
			continue
		}
		kind := classifyKey(key)
		if kind == "" {
			continue
		}
		c.addPair(kind, key, cleanValue(val), ctxUser, commented)
	}
}

func (c *collector) addPair(kind, key, value, user string, comment bool) {
	// PEM 块交给 privateKeys 整块收割，避免把多行私钥按行截断入库。
	if !plausibleSecret(value) || looksLikeConn(value) || strings.HasPrefix(value, "-----BEGIN") {
		return
	}
	conf := ConfidenceMedium
	if comment {
		conf = ConfidenceLow
	}
	sig := Signal{Kind: kind, Secret: value, Confidence: conf}
	if kind == KindPassword || kind == KindConnStr {
		sig.Username = user
	}
	c.add(sig)
}

func (c *collector) hashes(text string) {
	for _, m := range reBcrypt.FindAllString(text, -1) {
		c.add(Signal{Kind: KindHash, Secret: m, Confidence: ConfidenceHigh, Source: "hash"})
	}
}

// ───────────────────────── 小工具 ─────────────────────────

var skipKeyParts = []string{
	"path", "file", "dir", "policy", "length", "_len", "ttl", "timeout", "expire", "expiry",
	"enabled", "enable", "count", "limit", "max_", "min_", "hint", "url", "uri", "host",
	"port", "endpoint", "db_name", "database_name", "schema", "name",
	"type", "mode", "format", "prefix", "suffix", "field", "column", "header", "param", "class",
}

func classifyKey(key string) string {
	k := strings.ToLower(strings.Trim(key, `"'`))
	if k == "" {
		return ""
	}
	for _, p := range skipKeyParts {
		if strings.Contains(k, p) {
			return ""
		}
	}
	switch {
	case strings.Contains(k, "private_key"), strings.Contains(k, "privatekey"),
		strings.Contains(k, "ssh_key"), strings.Contains(k, "id_rsa"), strings.Contains(k, "id_ed25519"):
		return KindSSHKey
	case strings.Contains(k, "hash"):
		return KindHash
	case strings.Contains(k, "password"), strings.Contains(k, "passwd"), strings.Contains(k, "pwd"),
		strings.HasSuffix(k, "_pass"), strings.HasSuffix(k, "pass"):
		return KindPassword
	case strings.Contains(k, "token"), strings.Contains(k, "jwt"), strings.Contains(k, "cookie"),
		strings.Contains(k, "session"):
		return KindToken
	case strings.Contains(k, "secret"), strings.Contains(k, "api_key"), strings.Contains(k, "apikey"),
		strings.Contains(k, "access_key"), strings.Contains(k, "accesskey"), strings.Contains(k, "secret_key"),
		strings.Contains(k, "credential"), strings.HasSuffix(k, "key"):
		return KindKey
	case strings.Contains(k, "conn"), strings.Contains(k, "dsn"), strings.Contains(k, "database_url"), strings.Contains(k, "db_url"):
		return KindConnStr
	}
	return ""
}

func isUserKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	switch k {
	case "user", "username", "user_name", "login", "db_user", "db_username", "ssh_user", "mysql_user":
		return true
	}
	return false
}

func splitKV(line string) (key, value string, ok bool) {
	if m := reKVLine.FindStringSubmatch(line); m != nil {
		return m[1], m[2], true
	}
	if m := reYAMLLine.FindStringSubmatch(line); m != nil {
		return m[1], m[2], true
	}
	return "", "", false
}

func cleanValue(v string) string {
	v = strings.TrimSpace(v)
	// 去掉行内注释（空白 + # / ; / //）。
	for _, marker := range []string{" #", "\t#", " ;", "\t;", " //", "\t//"} {
		if i := strings.Index(v, marker); i > 0 {
			v = v[:i]
		}
	}
	v = strings.TrimSpace(v)
	v = strings.Trim(v, `"'`)
	return strings.TrimSpace(v)
}

func plausibleSecret(v string) bool {
	if len(v) < 3 {
		return false
	}
	lower := strings.ToLower(v)
	if strings.HasPrefix(v, "<") && strings.HasSuffix(v, ">") {
		return false
	}
	for _, p := range []string{"${", "{{", "$(", "%(", "env:", "changeme", "placeholder", "redacted", "your_", "xxxx"} {
		if strings.Contains(lower, p) {
			return false
		}
	}
	switch lower {
	case "null", "none", "true", "false", "nil", "***", "******":
		return false
	}
	return true
}

func looksLikeConn(v string) bool {
	return strings.Contains(v, "://")
}

// flagValue 取短/长选项的值（支持 -pVALUE / -p VALUE / --password=VALUE）。
func flagValue(line, short, long string) string {
	fields := strings.Fields(line)
	for i, f := range fields {
		lf := strings.ToLower(f)
		if long != "" {
			if strings.HasPrefix(lf, strings.ToLower(long)+"=") {
				return cleanToken(f[len(long)+1:])
			}
			if lf == strings.ToLower(long) && i+1 < len(fields) {
				return cleanToken(fields[i+1])
			}
		}
		if short == "" {
			continue
		}
		if strings.HasPrefix(f, short) && len(f) > len(short) && !strings.HasPrefix(f, short+"=") {
			if f[len(short)] != '-' {
				return cleanToken(f[len(short):])
			}
		}
		if f == short && i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "-") {
			return cleanToken(strings.TrimPrefix(fields[i+1], "="))
		}
	}
	return ""
}

func quotedFlag(line, flag string) string {
	q := regexp.QuoteMeta(flag)
	if m := regexp.MustCompile(q + `\s*(?:=\s*)?'([^']*)'`).FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[1])
	}
	if m := regexp.MustCompile(q + `\s*(?:=\s*)?"([^"]*)"`).FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func cleanToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "=")
	return strings.Trim(s, `'"`)
}

func userAtHost(line string) (user, host string) {
	m := reUserAtHost.FindStringSubmatch(line)
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

func curlAuth(line string) (user, pass string, ok bool) {
	fields := strings.Fields(line)
	value := ""
	for i, f := range fields {
		lf := strings.ToLower(f)
		if lf == "-u" || lf == "--user" {
			if i+1 < len(fields) {
				value = cleanToken(fields[i+1])
			}
			break
		}
		if strings.HasPrefix(lf, "--user=") {
			value = cleanToken(f[len("--user="):])
			break
		}
		if strings.HasPrefix(f, "-u") && len(f) > 2 && f[2] != '-' {
			value = cleanToken(f[2:])
			break
		}
	}
	if value == "" {
		return "", "", false
	}
	i := strings.Index(value, ":")
	if i <= 0 || i == len(value)-1 {
		return "", "", false
	}
	return value[:i], value[i+1:], true
}
