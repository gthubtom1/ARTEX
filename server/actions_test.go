package server

import (
	"testing"

	"github.com/Autumn-27/artex/internal/impact"
)

// P2 的三条硬约束，都钉在纯函数上：
//  1. 对话解析：只有**明确要做这件事**的说法才登记工单，否定句和"看看"不算；
//  2. 收口：必须出现 base_ref 之后的**新证据**才算做成，不能拿历史战果冒充；
//  3. 定义：7 类动作齐全且每类都写了留证要求（没留证要求的动作等于没做完）。
func TestActionClassFromChat(t *testing.T) {
	cases := []struct{ msg, want string }{
		{"帮我把它数据库里的订单表数据拉出来，先各取几条看看", "data_export"},
		{"把后台的用户列表和密码（或哈希）给我看看", "account_read"},
		{"用管理员权限帮我开一个后台账号，我登录看看", "account_create"},
		{"给这个账号加 100，把改前改后都记下来", "money_adjust"},
		{"在后台把这笔提款审批通过", "money_approve"},
		{"把它首页的收款地址换成我的，先备份原文件", "deface"},
		{"留一个可用的后台账号，任务结束前删掉", "persist"},
		// 否定句一律不登记（血案：把"不要留后门"当成要留后门）
		{"不要留后门，也别改首页", ""},
		{"先不导出数据", ""},
		// 纯提问不该变成工单
		{"现在到哪一步了？", ""},
		{"这个站的入口在哪", ""},
	}
	for _, c := range cases {
		if got := actionClassFromChat(c.msg); got != c.want {
			t.Fatalf("%q → %q，期望 %q", c.msg, got, c.want)
		}
	}
}

func TestActionClosureNeedsNewEvidence(t *testing.T) {
	def, ok := actionDefByClass("data_export")
	if !ok {
		t.Fatal("data_export 未定义")
	}
	rows := []lootRow{
		{Key: "db", Label: "数据库查询", Status: "done", Evidence: []candoEvidence{{ID: 10}, {ID: 12}}},
		{Key: "admin", Label: "管理员 / 后台登录", Status: "done", Evidence: []candoEvidence{{ID: 99}}},
		{Key: "data", Label: "数据拉取", Status: "run"},
	}
	// 登记时最大证据是 12：没有更新的证据 → 不能算做成。
	if done, _, _ := actionClosure(def, rows, 12); done {
		t.Fatal("证据没有越过 base_ref，不该收口")
	}
	// 拿到新证据 15 → 收口，且只认这条动作映射的行（admin 的 #99 不参与）。
	rows[0].Evidence = append(rows[0].Evidence, candoEvidence{ID: 15})
	done, evidence, ref := actionClosure(def, rows, 12)
	if !done || ref != 15 {
		t.Fatalf("应在新证据 15 上收口，得到 done=%v ref=%d", done, ref)
	}
	if evidence == "" {
		t.Fatal("收口必须带证据文案")
	}
	// 只有 run/tried 状态的行不算战果。
	onlyRunning := []lootRow{{Key: "db", Label: "数据库查询", Status: "run", Evidence: []candoEvidence{{ID: 20}}}}
	if done, _, _ := actionClosure(def, onlyRunning, 0); done {
		t.Fatal("没拿到的行不能收口")
	}
}

func TestActionDefsComplete(t *testing.T) {
	if len(actionDefs) != 7 {
		t.Fatalf("业务动作应为 7 类（与 P0 判据一致），得到 %d", len(actionDefs))
	}
	seen := map[string]bool{}
	for _, d := range actionDefs {
		if d.Class == "" || d.Title == "" || d.Need == "" || d.SayIt == "" || d.Evidence == "" || len(d.Rows) == 0 {
			t.Fatalf("动作定义缺字段（前置/说法/留证/映射行都要有）：%+v", d)
		}
		if seen[d.Class] {
			t.Fatalf("动作 class 重复：%s", d.Class)
		}
		seen[d.Class] = true
	}
}

// 「能做到」必须跟等级同源（impact.Stats），不能跟"战果行有证据"混：
// 用户 2026-10-24 截图抓到 E0（无任何访问权）却高亮「看后台用户名/密码」——
// 因为某个洞的摘要里有"密码"字样。这里钉住：没拿到权限，一个后果都不许点亮。
func TestActionDoableFollowsCapability(t *testing.T) {
	empty := impact.Stats{Findings: 3} // 只有洞，没有凭据/后台/命令/数据
	for _, d := range actionDefs {
		if actionDoable(d, empty) {
			t.Fatalf("E0（无任何能力）时 %s 不该算能做到", d.Class)
		}
	}

	withApp := impact.Stats{Findings: 3, OwnedApp: 1} // 进了后台
	for _, cls := range []string{"account_read", "account_create", "money_adjust", "money_approve", "deface"} {
		def, _ := actionDefByClass(cls)
		if !actionDoable(def, withApp) {
			t.Fatalf("进后台后 %s 应算能做到", cls)
		}
	}
	dataDef, _ := actionDefByClass("data_export")
	if actionDoable(dataDef, withApp) {
		t.Fatal("只进后台（还没读到数据）不该算能导出数据")
	}
	if !actionDoable(dataDef, impact.Stats{DataSamples: 1}) {
		t.Fatal("读到数据样本后 data_export 应算能做到")
	}
	persistDef, _ := actionDefByClass("persist")
	if actionDoable(persistDef, withApp) {
		t.Fatal("没命令执行不该算能持久化")
	}
	if !actionDoable(persistDef, impact.Stats{Shells: 1}) {
		t.Fatal("有命令执行后 persist 应算能做到")
	}
}
