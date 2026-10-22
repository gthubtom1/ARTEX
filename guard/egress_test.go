package guard

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Autumn-27/norma/hook"
)

func TestEgressIntent(t *testing.T) {
	yes := []struct{ tool, cmd string }{
		{"Bash", "curl http://example.com"},
		{"Bash", "curl -sS --noproxy '*' http://10.0.0.1"},
		{"Bash", "nmap -sT 10.0.0.0/24"},
		{"Bash", "python3 -c \"import requests\""},
		{"Bash", "ls -la && curl http://x"},
		{"shell_open", "wget http://x"},
		{"WebFetch", ""},
		{"WebSearch", ""},
	}
	for _, c := range yes {
		if !egressIntent(c.tool, c.cmd) {
			t.Errorf("egressIntent(%q,%q)=false，应为 true", c.tool, c.cmd)
		}
	}
	no := []struct{ tool, cmd string }{
		{"Bash", "ls -la"},
		{"Bash", "cat /etc/hosts"},
		{"Bash", ""},
		{"Read", ""},
		{"Grep", "curl"},
		{"Write", "http://x"},
	}
	for _, c := range no {
		if egressIntent(c.tool, c.cmd) {
			t.Errorf("egressIntent(%q,%q)=true，应为 false", c.tool, c.cmd)
		}
	}
}

func TestEgressBlockMessageHasNoPermissionTone(t *testing.T) {
	msg := egressBlockMessage("proxyconnect refused")
	for _, bad := range []string{"禁止", "不允许", "无权限", "违规"} {
		if strings.Contains(msg, bad) {
			t.Fatalf("出口闸文案不应出现权限口吻 %q：%s", bad, msg)
		}
	}
	if !strings.Contains(msg, "出口未就绪") {
		t.Fatalf("文案应说明是环境问题：%s", msg)
	}
}

// curl 被拦、ls 不拦、直连模式放行 —— 三条验收判据。
func TestPreToolUseEgressGate(t *testing.T) {
	prevE, prevF := EgressCheck, EgressToolFilter
	prevT, prevA := ToolPresence, AutoDecide
	t.Cleanup(func() {
		EgressCheck, EgressToolFilter = prevE, prevF
		ToolPresence, AutoDecide = prevT, prevA
	})
	ToolPresence = nil
	AutoDecide = nil

	calls := 0
	EgressCheck = func(context.Context) error {
		calls++
		return errors.New("proxyconnect tcp: dial tcp 127.0.0.1:7897: connect: connection refused")
	}
	EgressToolFilter = nil

	g := New()

	// 1) curl 被拦
	res := g.preToolUse(context.Background(), hook.Event{ToolName: "Bash", Input: []byte(`{"command":"curl http://example.com"}`)})
	if res.Decision != "block" {
		t.Fatalf("curl 应被出口闸拦下，实际 decision=%q msg=%q", res.Decision, res.Message)
	}
	if !strings.Contains(res.Message, "出口未就绪") {
		t.Fatalf("拦截文案应是环境语气：%s", res.Message)
	}
	if calls != 1 {
		t.Fatalf("应调用一次出口校验，实际 %d", calls)
	}

	// 2) ls 不被拦，也不触发校验
	res = g.preToolUse(context.Background(), hook.Event{ToolName: "Bash", Input: []byte(`{"command":"ls -la"}`)})
	if res.Decision == "block" {
		t.Fatalf("ls 不应被拦：%s", res.Message)
	}
	if calls != 1 {
		t.Fatalf("本地命令不应触发出口校验，实际 calls=%d", calls)
	}

	// 3) 未注入 EgressCheck（= ARTEX_ALLOW_DIRECT=1 的直连模式）→ 放行
	EgressCheck = nil
	res = g.preToolUse(context.Background(), hook.Event{ToolName: "Bash", Input: []byte(`{"command":"curl http://127.0.0.1:8080"}`)})
	if res.Decision == "block" {
		t.Fatalf("直连模式不应拦：%s", res.Message)
	}
}
