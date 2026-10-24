package server

import "testing"

// P0-c：AI「能换到什么」的缓存键与解析。两条都很容易被改坏，所以钉住行为：
//  1. 键必须**只跟证据有关**：换任务/换标题/换摘要/换证据任一变化都要换键，
//     否则拿旧解释冒充新结论（这是 AI 缓存最常见的坑）；
//  2. 模型输出必须能被宽容解析（Markdown 包裹、前后废话），但编出来的 finding_id 不能进缓存
//     —— 后者在 taskBriefAI 里按 byID 过滤，这里只验解析不炸。
func TestBriefAbilityHash(t *testing.T) {
	base := lootFinding{id: 1, title: "SQL 注入", summary: "search 参数可注入", evidence: "返回 MSSQL 报错"}
	h := briefAbilityHash(base)
	if h == "" || len(h) != 32 {
		t.Fatalf("hash 应为 32 位十六进制，得到 %q", h)
	}
	if briefAbilityHash(base) != h {
		t.Fatal("同一证据必须得到同一个 hash（缓存才可能命中）")
	}
	for name, mutate := range map[string]func(lootFinding) lootFinding{
		"标题变": func(f lootFinding) lootFinding { f.title += "（变）"; return f },
		"摘要变": func(f lootFinding) lootFinding { f.summary += "（变）"; return f },
		"证据变": func(f lootFinding) lootFinding { f.evidence += "（变）"; return f },
	} {
		if briefAbilityHash(mutate(base)) == h {
			t.Fatalf("%s 之后 hash 没变 —— 旧解释会被当新结论用", name)
		}
	}
	// id 不进 hash：同一段证据在别的任务/别的 id 下可以复用解释
	moved := base
	moved.id = 999
	if briefAbilityHash(moved) != h {
		t.Fatal("finding id 不该影响 hash（解释只取决于证据内容）")
	}
}

func TestParseBriefAbilities(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"裸数组", `[{"finding_id":1,"grants":"能读数据库"}]`, 1},
		{"Markdown 包裹", "```json\n[{\"finding_id\":2,\"grants\":\"能进后台\"}]\n```", 1},
		{"前后有废话", `好的，结果如下：[{"finding_id":3,"grants":"能跑命令"}] 以上。`, 1},
		{"两条", `[{"finding_id":1,"grants":"a"},{"finding_id":2,"grants":"b"}]`, 2},
		{"不是 JSON", `我觉得这个洞很严重`, 0},
		{"空数组", `[]`, 0},
	}
	for _, c := range cases {
		if got := parseBriefAbilities(c.raw); len(got) != c.want {
			t.Fatalf("%s：解析出 %d 条，期望 %d", c.name, len(got), c.want)
		}
	}
	if got := parseBriefAbilities(`[{"finding_id":7,"grants":"能读配置里的口令"}]`); got[0].FindingID != 7 || got[0].Grants == "" {
		t.Fatalf("字段解析错：%+v", got)
	}
}
