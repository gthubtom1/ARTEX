package server

import "testing"

// 三段式（docs/23）：漏洞存在 ≠ 能换到现实后果。这里钉住判定边界：
//
//	got      映射行 done（有证据）
//	one_step 有映射行但没拿到（Gap 指出缺哪一步）
//	no_gain  一条映射行都不匹配（"洞多但换不到"就是这种）
func TestFindingStanding(t *testing.T) {
	sql := lootFinding{id: 1, title: "SQL 注入", text: "SQL Injection search q 参数"}
	xss := lootFinding{id: 2, title: "XSS", text: "Reflected XSS 反射型"}
	odd := lootFinding{id: 3, title: "目录列举", text: "Directory listing enabled"}

	// db 行已 done（读到数据）
	rows := []lootRow{
		{Key: "db", Label: "数据库查询", Status: "done"},
	}

	if got := findingStanding(sql, rows); got.Standing != "got" {
		t.Fatalf("SQL 注入 + db 已 done 应 got，得到 %q", got.Standing)
	}
	if got := findingStanding(sql, nil); got.Standing != "one_step" || got.Gap == "" {
		t.Fatalf("SQL 注入但没战果行应 one_step 且指出缺口，得到 %+v", got)
	}
	// XSS 映射到 admin 行（还没拿到）
	if got := findingStanding(xss, nil); got.Standing != "one_step" || got.Gap != "管理员 / 后台登录" {
		t.Fatalf("XSS 缺后台会话，期望 one_step/管理员，得到 %+v", got)
	}
	// 目录列举不映射任何战果行 → 换不到东西
	if got := findingStanding(odd, nil); got.Standing != "no_gain" || len(got.Rows) != 0 {
		t.Fatalf("目录列举应 no_gain，得到 %+v", got)
	}
	// 人标了"不适用"的行不算缺口
	if got := findingStanding(xss, []lootRow{{Key: "admin", Label: "管理员 / 后台登录", Status: "na"}}); got.Gap != "" {
		t.Fatalf("na 不该当缺口，得到 %q", got.Gap)
	}
}
