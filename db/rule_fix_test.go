package db

import (
	"regexp"
	"testing"
)

// TestDeleteSystemPathRuleNoFalsePositive 锁定 2026-10-24 实测的误拦：
// 同一条命令里 `rm -f cj2.txt && curl -o /dev/null` 被误判为“删除系统关键路径”。
func TestDeleteSystemPathRuleNoFalsePositive(t *testing.T) {
	newPat := `(?i)\brm\b[^\n;&|]{0,80}["'\s]/(?:etc|bin|usr|boot|var|lib|sys|proc|sbin|root)\b`
	re := regexp.MustCompile(newPat)

	allowed := []string{
		`cd /opt/artex/data/tasks/7/i195 && rm -f cj2.txt && curl -s -o /dev/null --max-time 20 http://x/`,
		`rm -f cj.txt`,
		`rm -rf /tmp/artex-work`,
		`rm -f ./raw/content_*.htm`,
		`rm -f 2>/dev/null; curl -o /dev/null http://x/`,
	}
	for _, c := range allowed {
		if re.MatchString(c) {
			t.Fatalf("不应拦截（误报）: %s", c)
		}
	}

	blocked := []string{
		`rm -rf /etc`,
		`rm -f /usr/bin/curl`,
		`rm -rf --no-preserve-root /var/lib`,
		`rm /root/.ssh/authorized_keys`,
		`rm -f /bin/sh`,
	}
	for _, c := range blocked {
		if !re.MatchString(c) {
			t.Fatalf("应拦截（漏报）: %s", c)
		}
	}
}
