package server

import (
	"strings"
	"testing"
)

// 横评里的典型事故：配了模型、实际跑的是另一个模型。
// 这里守的是"绑定存在但不可用"时**必须报错**，不许悄悄改用全局模型。
func TestDecideBinding(t *testing.T) {
	cases := []struct {
		name      string
		bound     bool
		id        int64
		buildable bool
		wantOK    bool
		wantMsg   string
	}{
		{"没绑定 → 不参与决策", false, 0, false, true, ""},
		{"绑定了且能构建 → 用它", true, 7, true, true, ""},
		{"绑定了但缺 Key → 报错", true, 7, false, false, "#7"},
		{"绑定了但配置被删 → 报错", true, 3, false, false, "#3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, msg := decideBinding(c.bound, c.id, c.buildable)
			if ok != c.wantOK {
				t.Fatalf("ok=%v want %v", ok, c.wantOK)
			}
			if c.wantMsg != "" && !strings.Contains(msg, c.wantMsg) {
				t.Fatalf("错误信息里应带上配置 id %q，实际 %q", c.wantMsg, msg)
			}
			if ok && msg != "" {
				t.Fatalf("允许使用时不该有错误信息：%q", msg)
			}
			if !ok && !strings.Contains(msg, "不会自动改用其它模型") {
				t.Fatalf("报错必须说明不会静默降级：%q", msg)
			}
		})
	}
}
