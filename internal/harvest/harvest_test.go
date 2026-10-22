package harvest

import (
	"strings"
	"testing"
)

func findAll(sigs []Signal, kind string) []Signal {
	var out []Signal
	for _, s := range sigs {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	return out
}

func findSecret(sigs []Signal, kind, secret string) *Signal {
	for i, s := range sigs {
		if s.Kind == kind && s.Secret == secret {
			return &sigs[i]
		}
	}
	return nil
}

func hasSecretContaining(sigs []Signal, kind, sub string) bool {
	for _, s := range sigs {
		if s.Kind == kind && strings.Contains(s.Secret, sub) {
			return true
		}
	}
	return false
}

func assertValidSignals(t *testing.T, sigs []Signal) {
	t.Helper()
	for _, s := range sigs {
		if !validKinds[s.Kind] {
			t.Fatalf("invalid kind %q in %+v", s.Kind, s)
		}
		switch s.Confidence {
		case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
		default:
			t.Fatalf("invalid confidence %q in %+v", s.Confidence, s)
		}
		if strings.TrimSpace(s.Secret) == "" {
			t.Fatalf("empty secret in %+v", s)
		}
	}
}

const envText = `# legacy config
DB_USER=appuser
DB_PASSWORD=Sup3rS3cret!
API_TOKEN=tok_9f8e7d6c5b4a
AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
SSH_PRIVATE_KEY_PATH=/home/app/.ssh/id_rsa
LOG_LEVEL=debug
`

func TestFromTextEnv(t *testing.T) {
	sigs := FromText("config", envText)
	assertValidSignals(t, sigs)

	pw := findSecret(sigs, KindPassword, "Sup3rS3cret!")
	if pw == nil {
		t.Fatalf("password not harvested: %+v", sigs)
	}
	if pw.Username != "appuser" {
		t.Fatalf("password username = %q, want appuser", pw.Username)
	}
	if pw.Confidence != ConfidenceMedium {
		t.Fatalf("password confidence = %q, want medium", pw.Confidence)
	}

	tok := findSecret(sigs, KindToken, "tok_9f8e7d6c5b4a")
	if tok == nil {
		t.Fatalf("token not harvested: %+v", sigs)
	}

	// AWS AK 与 SK 应配成一条 key 信号。
	if !hasSecretContaining(sigs, KindKey, "AKIAIOSFODNN7EXAMPLE:wJalrXUtnFEMI") {
		t.Fatalf("aws ak/sk pair not harvested: %+v", sigs)
	}
	for _, s := range sigs {
		if strings.Contains(s.Secret, "/home/app/.ssh/id_rsa") {
			t.Fatalf("key path must not be harvested as a secret: %+v", s)
		}
	}
	if len(findAll(sigs, KindPassword)) == 0 {
		t.Fatalf("no password signals: %+v", sigs)
	}
}

func TestFromTextConnectionStrings(t *testing.T) {
	text := "MYSQL_DSN=mysql://root:Pa55w0rd@10.0.0.5:3306/app\n" +
		"pg: postgres://svc:s3cr3t@db.internal:5432/main\n" +
		"notes: mongodb://mongo:M0ngoPass@127.0.0.1:27017/admin\n" +
		"cache: redis://:r3disPass@127.0.0.1:6379/0\n"
	sigs := FromText("notes", text)
	assertValidSignals(t, sigs)

	conns := findAll(sigs, KindConnStr)
	if len(conns) != 4 {
		t.Fatalf("conn signals = %+v", conns)
	}
	root := findSecret(sigs, KindPassword, "Pa55w0rd")
	if root == nil {
		t.Fatalf("conn password not split out: %+v", sigs)
	}
	if root.Username != "root" || root.Confidence != ConfidenceHigh {
		t.Fatalf("conn password = %+v", root)
	}
	if !strings.Contains(root.AssetHint, "10.0.0.5") {
		t.Fatalf("asset hint = %q", root.AssetHint)
	}
	// redis://:pass@host 这类空用户名的连接串也要抓到。
	redis := findSecret(sigs, KindPassword, "r3disPass")
	if redis == nil {
		t.Fatalf("redis password not harvested: %+v", sigs)
	}
	for _, s := range findAll(sigs, KindConnStr) {
		if s.Confidence != ConfidenceHigh {
			t.Fatalf("conn_str confidence should be high: %+v", s)
		}
	}
}

func TestFromTextJWTAndPrivateKey(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	pem := `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBhbm90aGVyZmFrZWtleW1hdGVyaWFsaGVyZQAAAJg=
-----END OPENSSH PRIVATE KEY-----`
	sigs := FromText("dump.txt", "Authorization: Bearer "+jwt+"\n"+pem+"\n")
	assertValidSignals(t, sigs)

	if findSecret(sigs, KindToken, jwt) == nil {
		t.Fatalf("jwt not harvested: %+v", sigs)
	}
	keys := findAll(sigs, KindSSHKey)
	if len(keys) != 1 {
		t.Fatalf("ssh_key signals = %+v", keys)
	}
	if !strings.HasPrefix(keys[0].Secret, "-----BEGIN OPENSSH PRIVATE KEY-----") ||
		!strings.HasSuffix(keys[0].Secret, "-----END OPENSSH PRIVATE KEY-----") {
		t.Fatalf("private key block truncated: %q", Redact(keys[0].Secret))
	}
	if keys[0].Confidence != ConfidenceHigh {
		t.Fatalf("private key confidence = %q", keys[0].Confidence)
	}
}

func TestFromTextCLIHistory(t *testing.T) {
	hist := `sshpass -p 'hunter2' ssh root@10.0.0.9
mysql -h db.local -u app -p'S3cretPass' app
curl -u 'admin:pass123' http://10.0.0.7/admin
`
	sigs := FromText("bash_history", hist)
	assertValidSignals(t, sigs)

	ssh := findSecret(sigs, KindPassword, "hunter2")
	if ssh == nil || ssh.Username != "root" || ssh.AssetHint != "10.0.0.9" {
		t.Fatalf("sshpass signal = %+v (all=%+v)", ssh, sigs)
	}
	my := findSecret(sigs, KindPassword, "S3cretPass")
	if my == nil || my.Username != "app" || my.AssetHint != "db.local" {
		t.Fatalf("mysql -p signal = %+v (all=%+v)", my, sigs)
	}
	cu := findSecret(sigs, KindPassword, "pass123")
	if cu == nil || cu.Username != "admin" {
		t.Fatalf("curl -u signal = %+v (all=%+v)", cu, sigs)
	}
	for _, s := range findAll(sigs, KindPassword) {
		if s.Confidence != ConfidenceHigh {
			t.Fatalf("history credential confidence should be high: %+v", s)
		}
	}
}

func TestFromTextPHPDefineAndYAMLAndComment(t *testing.T) {
	text := `<?php
define('DB_PASSWORD', 'PhpPa55word');
# OLD_TOKEN=legacy_token_value
aws:
  secret_key: YamlSecretValue123
`
	sigs := FromText("web", text)
	assertValidSignals(t, sigs)

	if findSecret(sigs, KindPassword, "PhpPa55word") == nil {
		t.Fatalf("php define not harvested: %+v", sigs)
	}
	legacy := findSecret(sigs, KindToken, "legacy_token_value")
	if legacy == nil {
		t.Fatalf("commented credential should still be recorded (low): %+v", sigs)
	}
	if legacy.Confidence != ConfidenceLow {
		t.Fatalf("comment confidence = %q, want low", legacy.Confidence)
	}
	if findSecret(sigs, KindKey, "YamlSecretValue123") == nil {
		t.Fatalf("yaml secret not harvested: %+v", sigs)
	}
}

func TestFromTextHashes(t *testing.T) {
	bcrypt := "$2y$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	sigs := FromText("leak.txt", "admin:"+bcrypt+"\n")
	if findSecret(sigs, KindHash, bcrypt) == nil {
		t.Fatalf("bcrypt hash not harvested: %+v", sigs)
	}
}

func TestFromTextEmpty(t *testing.T) {
	if got := FromText("x", "  \n\t "); got != nil {
		t.Fatalf("empty text should yield nil, got %+v", got)
	}
}

func TestFromEnv(t *testing.T) {
	env := map[string]string{
		"APP_PASSWORD":          "EnvPa55!",
		"REDIS_URL":             "redis://:r3disPass@127.0.0.1:6379/0",
		"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPLE",
		"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"PATH":                  "/usr/bin",
	}
	sigs := FromEnv(env)
	assertValidSignals(t, sigs)

	if findSecret(sigs, KindPassword, "EnvPa55!") == nil {
		t.Fatalf("env password not harvested: %+v", sigs)
	}
	if findSecret(sigs, KindPassword, "r3disPass") == nil {
		t.Fatalf("env redis url not harvested: %+v", sigs)
	}
	if !hasSecretContaining(sigs, KindKey, "AKIAIOSFODNN7EXAMPLE:wJalrXUtnFEMI") {
		t.Fatalf("env ak/sk pair not harvested: %+v", sigs)
	}
	for _, s := range sigs {
		if strings.Contains(s.Secret, "/usr/bin") {
			t.Fatalf("PATH must not be harvested: %+v", s)
		}
	}
}

func TestFromEnvNil(t *testing.T) {
	if got := FromEnv(nil); got != nil {
		t.Fatalf("nil env should yield nil, got %+v", got)
	}
}

func TestFromMetadataAliyun(t *testing.T) {
	meta := `{"AccessKeyId":"LTAI5tExampleKeyId123","AccessKeySecret":"AliyunSecretValue123456","SecurityToken":"CAISgA1q6ExampleToken","RegionId":"cn-hangzhou","InstanceId":"i-abc123"}`
	sigs := FromMetadata(meta)
	assertValidSignals(t, sigs)

	key := findSecret(sigs, KindKey, "LTAI5tExampleKeyId123:AliyunSecretValue123456")
	if key == nil {
		t.Fatalf("aliyun ak/sk pair not harvested: %+v", sigs)
	}
	if key.Confidence != ConfidenceHigh {
		t.Fatalf("metadata confidence = %q", key.Confidence)
	}
	if !strings.Contains(key.AssetHint, "cn-hangzhou") || !strings.Contains(key.AssetHint, "i-abc123") {
		t.Fatalf("asset hint = %q", key.AssetHint)
	}
	if findSecret(sigs, KindToken, "CAISgA1q6ExampleToken") == nil {
		t.Fatalf("security token not harvested: %+v", sigs)
	}
}

func TestFromMetadataGCPPrivateKey(t *testing.T) {
	meta := `{"project-id":"demo-proj","private_key":"-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQ\n-----END PRIVATE KEY-----"}`
	sigs := FromMetadata(meta)
	assertValidSignals(t, sigs)

	keys := findAll(sigs, KindSSHKey)
	if len(keys) != 1 {
		t.Fatalf("gcp private key not harvested: %+v", sigs)
	}
	if !strings.Contains(keys[0].Secret, "BEGIN PRIVATE KEY") {
		t.Fatalf("private key content = %q", Redact(keys[0].Secret))
	}
}

func TestFromMetadataAWS(t *testing.T) {
	meta := `{"AccessKeyId":"AKIAIOSFODNN7EXAMPLE","SecretAccessKey":"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY","Token":"IQoJb3JpZ2luX2VjEXAMPLETOKEN","region":"us-east-1"}`
	sigs := FromMetadata(meta)
	if !hasSecretContaining(sigs, KindKey, "AKIAIOSFODNN7EXAMPLE:wJalrXUtnFEMI") {
		t.Fatalf("aws imds pair not harvested: %+v", sigs)
	}
	if findSecret(sigs, KindToken, "IQoJb3JpZ2luX2VjEXAMPLETOKEN") == nil {
		t.Fatalf("aws session token not harvested: %+v", sigs)
	}
}

func TestFromMetadataInvalid(t *testing.T) {
	if got := FromMetadata("not json"); got != nil {
		t.Fatalf("invalid json should yield nil, got %+v", got)
	}
	if got := FromMetadata(""); got != nil {
		t.Fatalf("empty json should yield nil, got %+v", got)
	}
}

func TestRedact(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"AKIAIOSFODNN7EXAMPLE", "AK****************LE"},
		{"abcdef", "ab**ef"},
		{"ab", "**"},
		{"", ""},
		{"中文密码测试", "中文**测试"},
	}
	for _, c := range cases {
		if got := Redact(c.in); got != c.want {
			t.Fatalf("Redact(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	secret := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	masked := Redact(secret)
	if masked == secret {
		t.Fatalf("redact leaked full secret")
	}
	if strings.Contains(masked, "XUtnFEMI") || strings.Contains(masked, "bPxRfi") {
		t.Fatalf("redact leaked middle: %q", masked)
	}
	if !strings.HasPrefix(masked, "wJ") || !strings.HasSuffix(masked, "EY") {
		t.Fatalf("redact should keep 2+2: %q", masked)
	}
}

// 任何信号都不得在打码提示里泄露明文中段。
func TestRedactAllSignals(t *testing.T) {
	sigs := FromText("all", envText+"\n"+`mysql://root:Pa55w0rd@10.0.0.5:3306/app`)
	assertValidSignals(t, sigs)
	for _, s := range sigs {
		masked := Redact(s.Secret)
		if len([]rune(s.Secret)) > 8 && masked == s.Secret {
			t.Fatalf("signal secret not redacted: %q", masked)
		}
		if len([]rune(s.Secret)) > 10 {
			mid := []rune(s.Secret)
			mid = mid[3 : len(mid)-3]
			if strings.Contains(masked, string(mid)) {
				t.Fatalf("masked hint leaks middle of secret: %q", masked)
			}
		}
	}
}
