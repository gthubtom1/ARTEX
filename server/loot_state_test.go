package server

import (
	"strings"
	"testing"
)

// TestLootApplyState 锁定「还没试 / 不适用」两态（B-07）：
//   - 没标记 → 不显示（返回 false），保持"没证据的行不显示"的原语义；
//   - todo / na → 显示，并且 why 里写明是用户标的，不能伪装成机器判定；
//   - 证据优先由调用侧保证（只有 status=blank 的行才会套标记）。
func TestLootApplyState(t *testing.T) {
	// 没标记：行不显示，字段不能被改动。
	row := lootRow{Key: "rce", Label: "服务器命令执行", Status: "blank"}
	if lootApplyState(&row, "") {
		t.Fatalf("空标记不应让行显示")
	}
	if row.Status != "blank" || row.Why != "" {
		t.Fatalf("空标记不应改行: %+v", row)
	}

	// todo
	row = lootRow{Key: "db", Label: "数据库查询", Status: "blank"}
	if !lootApplyState(&row, "todo") {
		t.Fatalf("todo 应让行显示")
	}
	if row.Status != "todo" || !strings.Contains(row.Why, "还没试") || !strings.Contains(row.Why, "你标的") {
		t.Fatalf("todo 状态/说明不对: %+v", row)
	}

	// na
	row = lootRow{Key: "lateral", Label: "内网横向", Status: "blank"}
	if !lootApplyState(&row, "na") {
		t.Fatalf("na 应让行显示")
	}
	if row.Status != "na" || !strings.Contains(row.Why, "不适用") || !strings.Contains(row.Why, "你标的") {
		t.Fatalf("na 状态/说明不对: %+v", row)
	}

	// 未知值（历史脏数据）当作没标记，不允许显示一行来历不明的状态。
	row = lootRow{Key: "cred", Label: "凭据", Status: "blank"}
	if lootApplyState(&row, "weird") {
		t.Fatalf("未知标记不应让行显示")
	}
}

// TestLootStateRowKey 锁定写接口的 row_key 白名单：
// 内置行是小写 key，AI 推断行是 slug；大写/空格/路径片段一律拒（防脏数据与注入面）。
func TestLootStateRowKey(t *testing.T) {
	ok := []string{"rce", "fileread", "lateral", "ai_special_1", "x"}
	bad := []string{"", "RCE", "rce ", "rce/../x", "内部横向", strings.Repeat("a", 65)}
	for _, k := range ok {
		if !lootStateRe.MatchString(k) {
			t.Fatalf("应接受 row_key=%q", k)
		}
	}
	for _, k := range bad {
		if lootStateRe.MatchString(k) {
			t.Fatalf("应拒绝 row_key=%q", k)
		}
	}
}

// TestLootTryPrompt 锁定「还没试」行的下一步指令：
// 与 lootNextPrompt 的区别是不假设已有入口，必须能从"先找入口"开始。
func TestLootTryPrompt(t *testing.T) {
	goal := "https://shop.example.com/login 测一下这个站"
	cases := []struct {
		key  string
		want []string
	}{
		{"admin", []string{"先找", "只打 shop.example.com", "20 分钟内"}},
		{"db", []string{"注入点", "只打 shop.example.com"}},
		{"data", []string{"接口", "3 条样本", "只打 shop.example.com"}},
		{"fileread", []string{"文件读取", "不要写文件", "只打 shop.example.com"}},
		{"rce", []string{"命令", "不要留后门", "只打 shop.example.com"}},
		{"cred", []string{"凭据", "只打 shop.example.com"}},
		{"lateral", []string{"只做只读探测", "不要横向登录"}},
		{"unknown", []string{"最小验证", "判死"}},
	}
	for _, c := range cases {
		got := lootTryPrompt(c.key, "某一项", goal)
		if strings.TrimSpace(got) == "" {
			t.Fatalf("%s: 生成空指令", c.key)
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Fatalf("%s: 指令缺少 %q，实际: %s", c.key, w, got)
			}
		}
	}
}

// TestLootPlainState 锁定两态的人话渲染：不能把用户标记说成机器结论。
func TestLootPlainState(t *testing.T) {
	if got := lootPlain("db", "数据库查询", "还没试（你标的）", nil, "todo"); !strings.Contains(got, "还没试") || !strings.Contains(got, "数据库查询") {
		t.Fatalf("todo 人话不对: %s", got)
	}
	if got := lootPlain("db", "数据库查询", "这个站不适用（你标的）", nil, "na"); !strings.Contains(got, "不适用") {
		t.Fatalf("na 人话不对: %s", got)
	}
	// na / todo 没有证据 → 证据强度必须为空，不能显示成 real/reachable。
	if got := lootProofState("na", nil); got != "" {
		t.Fatalf("na 的 proof_state 应为空，实际 %q", got)
	}
	if got := lootProofState("todo", nil); got != "" {
		t.Fatalf("todo 的 proof_state 应为空，实际 %q", got)
	}
}
