package server

import "testing"

func mkRow(key, status string) lootRow { return lootRow{Key: key, Status: status} }

// P1 的判据是"3–5 条 + 跟这个站当前状态匹配"。这里钉住四件事：
//  1. 数量恒在 3–5；
//  2. 拿到命令执行时，第一条就是"从机器里捞配置"，不是再去打新洞；
//  3. 什么都没有时给侦察类说法，而不是无意义寒暄；
//  4. 文本不重复（点错了会连发两句一样的话）。
func TestBuildTaskPhrasesCount(t *testing.T) {
	cases := map[string]struct {
		fs   []lootFinding
		rows []lootRow
	}{"全空": {nil, nil}, "只有漏洞没战果": {[]lootFinding{{id: 1, title: "SQL 注入"}}, nil}, "有 shell": {nil, []lootRow{mkRow("rce", "done")}}, "全都有": {nil, []lootRow{
		mkRow("rce", "done"), mkRow("admin", "done"), mkRow("db", "done"), mkRow("cred", "done"), mkRow("filewrite", "done"),
	}}}
	for name, c := range cases {
		got, _ := buildTaskPhrases(c.fs, c.rows)
		if n := len(got); n < 3 || n > 5 {
			t.Fatalf("%s：短语数 %d，期望 3–5", name, n)
		}
		seen := map[string]bool{}
		for _, p := range got {
			if p.Text == "" || p.Why == "" || p.Key == "" {
				t.Fatalf("%s：字段不能为空 %+v", name, p)
			}
			if seen[p.Text] {
				t.Fatalf("%s：文本重复 %q", name, p.Text)
			}
			seen[p.Text] = true
		}
	}
}

func TestBuildTaskPhrasesPriority(t *testing.T) {
	got, has := buildTaskPhrases(nil, []lootRow{mkRow("rce", "done"), mkRow("admin", "done")})
	if got[0].Key != "rce_config" {
		t.Fatalf("有命令执行时第一条应是 rce_config，得到 %q", got[0].Key)
	}
	if len(has) < 2 {
		t.Fatalf("has 应至少含 shell/admin，得到 %v", has)
	}

	// 没有任何战果：不能出现"进后台看数据"这种现在做不到的话。
	empty, _ := buildTaskPhrases(nil, nil)
	for _, p := range empty {
		if p.Key == "admin_data" || p.Key == "db_sample" || p.Key == "rce_config" {
			t.Fatalf("没有战果时不该出现 %q", p.Key)
		}
	}

	// 有洞但没收益：要催"从确认推到达成"。
	// 用不命中任何专项规则的洞（XSS 目前没有专门短语），验证兜底的"催收益"
	push, _ := buildTaskPhrases([]lootFinding{{id: 7, title: "反射型 XSS"}}, nil)
	if push[0].Key != "push_gain" {
		t.Fatalf("只有漏洞时第一条应是 push_gain，得到 %q", push[0].Key)
	}
}
