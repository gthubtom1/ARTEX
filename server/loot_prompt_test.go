package server

import (
	"strings"
	"testing"
)

// TestLootPrompt 锁定每类战果行的“下一步指令”模板：
// 必须全中文、可直接粘贴，且带目标 host 与时限类约束。
func TestLootPrompt(t *testing.T) {
	goal := "https://shop.example.com/login 测一下这个站"
	objs := []lootObject{{Name: "acublog.dbo.users"}, {Name: "orders"}}

	cases := []struct {
		key  string
		hint string
		want []string
	}{
		{"data", "", []string{"3 条样本", "用户表", "只打 shop.example.com"}},
		{"admin", "", []string{"登录它后台", "只打 shop.example.com"}},
		{"cred", "", []string{"验证", "只打 shop.example.com"}},
		{"rce", "改用 xp_cmdshell", []string{"判死", "xp_cmdshell", "只打 shop.example.com"}},
		{"fileread", "", []string{"任意文件读取", "只打 shop.example.com"}},
		{"filewrite", "", []string{"写入手法", "只打 shop.example.com"}},
		{"hash", "", []string{"哈希", "不要碰目标站"}},
		{"lateral", "", []string{"可达资产", "只打 shop.example.com"}},
		{"unknown-key", "", []string{"最小样本", "只打 shop.example.com"}},
	}

	for _, c := range cases {
		got := lootNextPrompt(c.key, objs, c.hint, goal)
		if strings.TrimSpace(got) == "" {
			t.Fatalf("%s: 生成了空指令", c.key)
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Fatalf("%s: 指令缺少 %q，实际: %s", c.key, w, got)
			}
		}
		t.Logf("%s: %s", c.key, got)
	}

	// 取不到 host 时兜底写“目标”，但仍然要能直接粘贴。
	if got := lootNextPrompt("data", nil, "", "随便测个站"); !strings.Contains(got, "只打 目标") {
		t.Fatalf("goal 里没有 URL 时应兜底为“目标”，实际: %s", got)
	}
	if got := lootGoalHost("目标 https://a.example.com:8443/x"); got != "a.example.com:8443" {
		t.Fatalf("host 抽取不对: %q", got)
	}
}
