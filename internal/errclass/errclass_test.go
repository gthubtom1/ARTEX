package errclass

import (
	"context"
	"fmt"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		out  string
		want Class
	}{
		{"waf", nil, "HTTP/1.1 403 Forbidden — cloudflare", TargetBlocked},
		{"rate", nil, "429 Too Many Requests", TargetBlocked},
		{"auth", nil, "401 Unauthorized: login required", AuthRequired},
		{"missing tool", fmt.Errorf("exec: \"sqlmap\": executable file not found in $PATH"), "", EnvError},
		{"proxy", nil, "curl: (7) Failed to connect to 127.0.0.1 port 7897: connection refused", EnvError},
		{"timeout", context.DeadlineExceeded, "", Timeout},
		{"selferr", nil, "unknown flag: --foo\nusage: nmap ...", SelfError},
		{"empty", nil, "", Unknown},
	}
	for _, c := range cases {
		if got := Classify(c.err, c.out); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestHintHasNoNegativeWording(t *testing.T) {
	// 纪律：提示文案不得出现"禁止/不允许/违规"等否定表达，避免模型误读为被拒绝。
	banned := []string{"禁止", "不允许", "违规", "无权", "denied by policy"}
	for _, c := range []Class{TargetBlocked, AuthRequired, EnvError, Timeout, SelfError, Unknown} {
		h := Hint(c)
		for _, b := range banned {
			if len(h) > 0 && contains(h, b) {
				t.Errorf("hint for %s contains banned wording %q: %s", c, b, h)
			}
		}
	}
}

func TestRetryableAndPivot(t *testing.T) {
	if !Retryable(EnvError) || !Retryable(Timeout) || Retryable(TargetBlocked) {
		t.Fatal("retryable classification wrong")
	}
	if !ShouldPivot(TargetBlocked) || !ShouldPivot(AuthRequired) || ShouldPivot(Timeout) {
		t.Fatal("pivot classification wrong")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
