package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// P0-02 端到端（工具层）：worker 调 kb_playbook 能拿到该类的判定标准与检查顺序，
// 且带硬上限（不许把 kb 变成上下文炸弹）。
func TestKBPlaybookTool(t *testing.T) {
	ts := &ToolSet{}
	ts.SetTaskID(1)
	tool := ts.kbPlaybook()
	if tool.Name() != "kb_playbook" {
		t.Fatalf("工具名应为 kb_playbook，得到 %q", tool.Name())
	}
	res, err := tool.Call(context.Background(), json.RawMessage(`{"shape":"POST /doLogin uid/passw"}`), nil)
	if err != nil {
		t.Fatalf("调用失败：%v", err)
	}
	out := res.Flatten()
	if !strings.Contains(out, "算命中") && !strings.Contains(out, "不算命中") {
		t.Fatalf("应返回判定标准，得到：%q", out)
	}
	if !strings.Contains(out, "检查顺序") {
		t.Fatalf("应返回检查顺序，得到：%q", out)
	}
	if len(out) > kbPlaybookMaxChars+200 {
		t.Fatalf("输出 %d 字节，超上限", len(out))
	}
	// 空 shape 必须报错（不许静默给一堆无关内容）
	if _, err := tool.Call(context.Background(), json.RawMessage(`{}`), nil); err != nil {
		if !strings.Contains(err.Error(), "shape") {
			t.Fatalf("空 shape 应提示 shape 必填，得到 %v", err)
		}
	} else if strings.Contains(res.Flatten(), "shape 必填") {
		t.Fatal("空 shape 不该返回内容")
	}
}
