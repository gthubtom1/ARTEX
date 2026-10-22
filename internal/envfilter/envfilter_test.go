package envfilter

import (
	"reflect"
	"testing"
)

func TestStripRemovesSecrets(t *testing.T) {
	env := []string{
		"PATH=/bin",
		"ARTEX_CRED_KEY=deadbeef",
		"AWS_SECRET_ACCESS_KEY=aws-secret",
		"MY_DB_PASSWORD=hunter2",
		"HTTP_PROXY=http://127.0.0.1:8080",
		"GPG_KEY=abc",
	}
	got := Strip(env)
	want := []string{"PATH=/bin", "HTTP_PROXY=http://127.0.0.1:8080", "GPG_KEY=abc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Strip = %#v, want %#v", got, want)
	}
}

func TestStripCaseInsensitiveAndSuffix(t *testing.T) {
	env := []string{
		"artex_cred_key=x",
		"my_api_token=x",
		"db_passwd=x",
		"client_secret=x",
		"SSH_AUTH_SOCK=/tmp/sock",
		"LS_COLORS=di=1",
		"normal=keep",
	}
	got := Strip(env)
	want := []string{"SSH_AUTH_SOCK=/tmp/sock", "LS_COLORS=di=1", "normal=keep"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Strip = %#v, want %#v", got, want)
	}
}

func TestStripDoesNotMutateInput(t *testing.T) {
	env := []string{"ARTEX_CRED_KEY=secret", "PATH=/bin"}
	orig := append([]string(nil), env...)
	_ = Strip(env)
	if !reflect.DeepEqual(env, orig) {
		t.Fatalf("input mutated: %#v, want %#v", env, orig)
	}
}

func TestStripEmptyEnv(t *testing.T) {
	for _, in := range [][]string{nil, {}} {
		got := Strip(in)
		if len(got) != 0 {
			t.Fatalf("Strip(%#v) = %#v, want empty", in, got)
		}
	}
}

func TestStripKeepsMalformedItem(t *testing.T) {
	env := []string{"NOEQUALS", "ARTEX_CRED_KEY=x"}
	got := Strip(env)
	want := []string{"NOEQUALS"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Strip = %#v, want %#v", got, want)
	}
}

func TestRedact(t *testing.T) {
	env := []string{"ARTEX_CRED_KEY=secret", "PATH=/bin", "NOEQUALS"}
	got := Redact(env)
	want := []string{"ARTEX_CRED_KEY=***", "PATH=/bin", "NOEQUALS"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Redact = %#v, want %#v", got, want)
	}
	// 不修改入参。
	if env[0] != "ARTEX_CRED_KEY=secret" {
		t.Fatalf("input mutated: %#v", env)
	}
}

func TestIsDenied(t *testing.T) {
	cases := []struct {
		kv   string
		want bool
	}{
		{"ARTEX_CRED_KEY=x", true},
		{"PATH=/bin", false},
		{"NOEQUALS", false},
		{"GPG_KEY=x", false},
		{"MY_KEY=x", true},
	}
	for _, c := range cases {
		if got := IsDenied(c.kv); got != c.want {
			t.Errorf("IsDenied(%q) = %v, want %v", c.kv, got, c.want)
		}
	}
}

func TestStripCustomDeny(t *testing.T) {
	// 自定义 deny 替换 DefaultDeny；ARTEX_PG_DSN 不再被精确命中
	// （它也不命中后缀兜底规则，所以应保留）。
	env := []string{"CUSTOM=x", "ARTEX_PG_DSN=pg", "PATH=/bin"}
	got := Strip(env, "CUSTOM")
	want := []string{"ARTEX_PG_DSN=pg", "PATH=/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Strip custom = %#v, want %#v", got, want)
	}
}

func TestBlankInherited(t *testing.T) {
	env := []string{"ARTEX_CRED_KEY=secret", "PATH=/bin"}
	got := BlankInherited(env, "ARTEX_CRED_KEY")
	want := []string{"PATH=/bin", "ARTEX_CRED_KEY="}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BlankInherited = %#v, want %#v", got, want)
	}
	// 即使入参里没有密钥，也要补空值，覆盖父进程继承值。
	got = BlankInherited(nil, "ARTEX_CRED_KEY")
	want = []string{"ARTEX_CRED_KEY="}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BlankInherited(nil) = %#v, want %#v", got, want)
	}
}
