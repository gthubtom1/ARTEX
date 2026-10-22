package remote

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBuildSSHUsesEnvNotArgs(t *testing.T) {
	name, args, env, stdin, err := build(Target{Proto: "ssh", Host: "10.0.0.5", Port: 22, User: "root", Secret: "s3cr3t"}, "id")
	if err != nil {
		t.Fatal(err)
	}
	if name != "sshpass" || len(env) != 1 || !strings.HasPrefix(env[0], "SSHPASS=") || stdin != "" {
		t.Fatalf("name/env/stdin: %s %v %q", name, env, stdin)
	}
	for _, a := range args {
		if strings.Contains(a, "s3cr3t") {
			t.Fatalf("secret leaked into args: %v", args)
		}
	}
	if args[len(args)-1] != "id" {
		t.Fatalf("command missing: %v", args)
	}
}

func TestBuildDBs(t *testing.T) {
	name, _, env, _, err := build(Target{Proto: "mysql", Host: "db", User: "u", Secret: "p"}, "SELECT 1")
	if err != nil || name != "mysql" || env[0] != "MYSQL_PWD=p" {
		t.Fatalf("mysql: %s %v %v", name, env, err)
	}
	name, _, env, _, _ = build(Target{Proto: "postgres", Host: "db", User: "u", Secret: "p"}, "SELECT 1")
	if name != "psql" || env[0] != "PGPASSWORD=p" {
		t.Fatalf("psql: %s %v", name, env)
	}
	name, _, env, _, _ = build(Target{Proto: "redis", Host: "r", Secret: "p"}, "PING")
	if name != "redis-cli" || env[0] != "REDISCLI_AUTH=p" {
		t.Fatalf("redis: %s %v", name, env)
	}
}

func TestBuildWebshellURLEncodes(t *testing.T) {
	name, args, _, _, err := build(Target{Proto: "webshell", Endpoint: "http://x/shell.php"}, "id; whoami")
	if err != nil || name != "curl" {
		t.Fatalf("curl: %s %v", name, err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--data-urlencode") || !strings.Contains(joined, "cmd=id; whoami") {
		t.Fatalf("args: %v", args)
	}
}

func TestBuildShellUsesStdin(t *testing.T) {
	name, args, env, stdin, err := build(Target{Proto: "shell", Host: "10.0.0.7", Port: 4444, Secret: "ignored"}, "id")
	if err != nil || name != "nc" || env != nil {
		t.Fatalf("shell: %s %v %v", name, env, err)
	}
	if strings.Contains(strings.Join(args, " "), "id") {
		t.Fatalf("command must not be in args: %v", args)
	}
	if !strings.Contains(stdin, "id") || !strings.Contains(stdin, shellEnd) {
		t.Fatalf("stdin: %q", stdin)
	}
}

func TestBuildHTTPBasicKeepsSecretInStdin(t *testing.T) {
	name, args, _, stdin, err := build(Target{Proto: "httpbasic", Host: "app.example.com", Port: 443, User: "admin", Secret: "p@ss"}, "/")
	if err != nil || name != "curl" {
		t.Fatalf("httpbasic: %s %v", name, err)
	}
	for _, a := range args {
		if strings.Contains(a, "p@ss") {
			t.Fatalf("secret in args: %v", args)
		}
	}
	if !strings.Contains(stdin, `user = "admin:p@ss"`) || !strings.Contains(stdin, "https://app.example.com:443/") {
		t.Fatalf("stdin config: %q", stdin)
	}
}

func TestUnsupportedProto(t *testing.T) {
	if Supports("vpn") || Supports("jump") {
		t.Fatal("vpn/jump must not be marked supported")
	}
	if !Supports("shell") {
		t.Fatal("raw shell must now be supported over nc")
	}
	_, err := Exec(context.Background(), Target{Proto: "vpn", Host: "x"}, "id", time.Second)
	if err == nil || !strings.Contains(err.Error(), "不支持远程执行") {
		t.Fatalf("want unsupported error, got %v", err)
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("sshpass -p s3cr3t ssh x", "s3cr3t"); strings.Contains(got, "s3cr3t") {
		t.Fatalf("leak: %s", got)
	}
}
