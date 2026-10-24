package server

import (
	"strings"
	"testing"

	"github.com/Autumn-27/artex/internal/impact"
)

// 目标对齐是给零基础用户看的那句话："我的目标（拖库/进后台）现在能不能做到？"
// 判据必须确定性、且不能吹：能力没齐就不许说"能"。
func TestGoalFitStatus(t *testing.T) {
	rows := []lootRow{
		{Key: "admin", Label: "管理员 / 后台登录", Status: "done"},
		{Key: "db", Label: "数据库查询", Status: "done"},
	}
	// 进后台：有 admin → yes
	adminDef, _ := goalDefByKey("admin_access")
	if f := fitForGoal(adminDef, impact.Stats{OwnedApp: 1}, rows); f.Status != "yes" {
		t.Fatalf("有后台会话时应 yes，得到 %+v", f)
	}
	// 拖库：只有后台、没读到数据 → 不能（后台 ≠ 能读库），且缺口写清
	dumpDef, _ := goalDefByKey("data_dump")
	if f := fitForGoal(dumpDef, impact.Stats{OwnedApp: 1}, rows); f.Status != "no" {
		t.Fatalf("只有后台没读到数据时应 no（后台不等于能读库），得到 %+v", f)
	}
	// 改钱：需要 后台+库；只有后台（有 DataSamples 才能算"能读库"）→ 差一步
	moneyDef, _ := goalDefByKey("money")
	f := fitForGoal(moneyDef, impact.Stats{OwnedApp: 1, DataSamples: 0}, rows)
	if f.Status != "one_step" {
		t.Fatalf("只有后台（缺库）时改钱应 one_step，得到 %+v", f)
	}
	if len(f.Missing) == 0 || !strings.Contains(strings.Join(f.Missing, "/"), "数据库") {
		t.Fatalf("缺口里应指出数据库能力，得到 %v", f.Missing)
	}
	if f.SayIt == "" || len(f.Steps) == 0 {
		t.Fatalf("差一步时必须给出说法和路径：%+v", f)
	}
	// 什么都没有 → no
	if f := fitForGoal(dumpDef, impact.Stats{}, nil); f.Status != "no" {
		t.Fatalf("什么能力都没有时应 no，得到 %+v", f)
	}
	// 战果行 done 但能力不成立（例如只因为漏洞摘要里出现"数据库"）→ 不算能做到
	if f := fitForGoal(dumpDef, impact.Stats{}, []lootRow{{Key: "db", Status: "done"}, {Key: "data", Status: "done"}}); f.Status == "yes" {
		t.Fatalf("战果行 done 但没有对应能力时不许判 yes：%+v", f)
	}
}

func TestGuessGoalKey(t *testing.T) {
	cases := map[string]string{
		"拿到订单表并各取几条样本":    "data_dump",
		"拿到后台管理权限":        "admin_access",
		"给账号加 100 看能不能改成": "money",
		"控制服务器，拿到命令执行":    "server",
		"随便看看":            "sample",
	}
	for goal, want := range cases {
		if got := guessGoalKey(goal); got != want {
			t.Fatalf("%q → %q，期望 %q", goal, got, want)
		}
	}
}

func TestBlockerAdvice(t *testing.T) {
	for _, class := range []string{"target_blocked", "auth_required", "env_error", "self_error", "unknown"} {
		if blockerAdvice(class) == "" {
			t.Fatalf("%s 没有给出建议", class)
		}
	}
	if !strings.Contains(blockerAdvice("target_blocked"), "WAF") {
		t.Fatal("target_blocked 应提到 WAF/拦截")
	}
}
