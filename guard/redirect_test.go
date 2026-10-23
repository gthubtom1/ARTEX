package guard

import (
	"context"
	"testing"
)

// CARD-E01 L2：同一个任务里，同一条裸跑命令只提示一次；第二次必须放行。
func TestRedirectHintOncePerTask(t *testing.T) {
	prevTask := TaskIDOf
	prevRedirect := ToolRedirect
	defer func() { TaskIDOf = prevTask; ToolRedirect = prevRedirect }()

	TaskIDOf = func(context.Context) int64 { return 9001 }
	ToolRedirect = func(name string) (string, bool) {
		if name == "nmap" {
			return "nmap_scan", true
		}
		return "", false
	}

	ctx := context.Background()
	if msg, hit := redirectHint(ctx, "Bash", "nmap -sV 127.0.0.1"); !hit || msg == "" {
		t.Fatalf("首次应提示改道，得到 hit=%v msg=%q", hit, msg)
	}
	if _, hit := redirectHint(ctx, "Bash", "nmap -sV 127.0.0.1"); hit {
		t.Fatal("第二次应放行（改道是提示不是权限）")
	}
	if _, hit := redirectHint(ctx, "Bash", "curl -s http://127.0.0.1"); hit {
		t.Fatal("没有等价物的命令不应提示")
	}
}
