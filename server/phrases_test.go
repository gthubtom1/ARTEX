package server

import (
	"testing"

	"github.com/Autumn-27/artex/internal/impact"
)

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
		st   impact.Stats
	}{
		"全空":      {nil, nil, impact.Stats{}},
		"只有漏洞没战果": {[]lootFinding{{id: 1, title: "SQL 注入"}}, nil, impact.Stats{Findings: 1}},
		"有 shell": {nil, []lootRow{mkRow("rce", "done")}, impact.Stats{Shells: 1}},
		"全都有": {nil, []lootRow{
			mkRow("rce", "done"), mkRow("admin", "done"), mkRow("db", "done"), mkRow("cred", "done"), mkRow("filewrite", "done"),
		}, impact.Stats{Shells: 1, OwnedApp: 1, DataSamples: 1, Creds: 1}},
	}
	for name, c := range cases {
		got, _ := buildTaskPhrases(c.fs, c.rows, c.st)
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
	got, has := buildTaskPhrases(nil, []lootRow{mkRow("rce", "done"), mkRow("admin", "done")}, impact.Stats{Shells: 1, OwnedApp: 1})
	if got[0].Key != "rce_config" {
		t.Fatalf("有命令执行时第一条应是 rce_config，得到 %q", got[0].Key)
	}
	if len(has) < 2 {
		t.Fatalf("has 应至少含 shell/admin，得到 %v", has)
	}

	// 没有任何战果：不能出现"进后台看数据"这种现在做不到的话。
	empty, _ := buildTaskPhrases(nil, nil, impact.Stats{})
	for _, p := range empty {
		if p.Key == "admin_data" || p.Key == "db_sample" || p.Key == "rce_config" {
			t.Fatalf("没有战果时不该出现 %q", p.Key)
		}
	}

	// 有洞但没收益：要催"从确认推到达成"。
	// 用不命中任何专项规则的洞（XSS 目前没有专门短语），验证兜底的"催收益"
	push, _ := buildTaskPhrases([]lootFinding{{id: 7, title: "反射型 XSS"}}, nil, impact.Stats{Findings: 1})
	if push[0].Key != "push_gain" {
		t.Fatalf("只有漏洞时第一条应是 push_gain，得到 %q", push[0].Key)
	}
}

// P0-06（真机 task 17 复现的反例）：战果行写着 done、文字里有"凭据"，
// 但能力统计里 Creds=0 —— 短语**不许**让用户"用这条凭据登录一次"。
func TestPhrasesFollowCapabilityNotKeywords(t *testing.T) {
	fs := []lootFinding{{id: 1, title: "接口未授权泄露用户名与口令哈希", text: "未授权 泄露 用户名 口令 哈希"}}
	rows := []lootRow{mkRow("cred", "done")}
	got, has := buildTaskPhrases(fs, rows, impact.Stats{Findings: 1}) // Creds=0
	for _, p := range got {
		if p.Key == "cred_verify" {
			t.Fatalf("Creds=0 时不该出 cred_verify：%+v", p)
		}
	}
	if len(has) != 0 {
		t.Fatalf("能力为空时 has 必须为空（战况卡与短语同源），得到 %v", has)
	}
	found := false
	for _, p := range got {
		if p.Key == "cred_record" {
			found = true
		}
	}
	if !found {
		t.Fatalf("有凭据线索但没落库时，应给 cred_record（先落库），得到 %+v", got)
	}
	// 反过来：真的拿到凭据 → 才允许验证
	got2, has2 := buildTaskPhrases(fs, rows, impact.Stats{Findings: 1, Creds: 1})
	if len(has2) == 0 || has2[0] != "cred" {
		t.Fatalf("Creds>0 时 has 应含 cred，得到 %v", has2)
	}
	found = false
	for _, p := range got2 {
		if p.Key == "cred_verify" {
			found = true
		}
	}
	if !found {
		t.Fatalf("真的拿到凭据时应给 cred_verify，得到 %+v", got2)
	}
}

// 有 SQL 注入但还没读到数据 → 要"3 行最小样本"，而不是"再打一个洞"。
func TestPhrasesSqlWithoutDataAsksMinimalSample(t *testing.T) {
	fs := []lootFinding{{id: 2, title: "SQL 注入", text: "sql 注入 报错回显"}}
	got, has := buildTaskPhrases(fs, nil, impact.Stats{Findings: 1})
	if len(has) != 0 {
		t.Fatalf("DataSamples=0 时 has 必须为空，得到 %v", has)
	}
	found := false
	for _, p := range got {
		if p.Key == "db_sample_min" {
			found = true
		}
		if p.Key == "db_sample" {
			t.Fatalf("还没读到数据时不该出现 db_sample（像是已经能读库）：%+v", p)
		}
	}
	if !found {
		t.Fatalf("应给 db_sample_min，得到 %+v", got)
	}
}
