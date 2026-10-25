package agent

import "testing"

// P0-04（docs/PLANS/2026-10-25-artex-找洞能力审查.md）：收官闸门要拦在**真正收官**那一步，
// 判据是权限阶梯，不看模型怎么说。之前 goal_met 有闸但默认不注册，prove_goal 一条闸都没有，
// 于是"只登录进后台"就能把所有目标标 met → 任务 done。
func TestFinishGateBlocksEarlyWrapUp(t *testing.T) {
	old := ImpactLevelFor
	defer func() { ImpactLevelFor = old }()

	ImpactLevelFor = func(int64) (int, string) { return 0, "只有侦察信息" }
	tt := &ToolSet{taskID: 19, goal: "至少把一条漏洞变成实际的命令执行或数据访问"}
	ok0, why0 := tt.finishGate()
	if ok0 {
		t.Fatalf("E0 时目标要求数据访问，必须拒绝收官，why=%q", why0)
	}
	if why0 == "" {
		t.Fatal("拒绝时必须给出人话原因")
	}

	ImpactLevelFor = func(int64) (int, string) { return 2, "已进入应用后台" }
	if ok, _ := tt.finishGate(); ok {
		t.Fatal("E2（只进后台）不够 E3/E4，必须拒绝收官")
	}

	ImpactLevelFor = func(int64) (int, string) { return 3, "已拿到命令执行" }
	if ok, why := tt.finishGate(); !ok {
		t.Fatalf("E3 应放行收官，why=%q", why)
	}

	// 回归 #20（2026-10-25 真机）：目标写"读取交易记录作为数据证据"，
	// 旧词表（只认"拿数据/业务数据/数据访问"）没覆盖 → 2.5 分钟就以 E1 收工。
	ImpactLevelFor = func(int64) (int, string) { return 1, "有凭据但未验证" }
	for _, g := range []string{
		"使用取得的后台会话读取 1 个账户的交易记录（最多 3 条）作为数据证据",
		"把订单表前 20 条拉出来",
		"读到用户表里的手机号",
	} {
		gg := &ToolSet{taskID: 21, goal: g}
		if ok, _ := gg.finishGate(); ok {
			t.Fatalf("要求具体数据的目标必须拦收官：%q", g)
		}
	}

	// 侦察型目标（只要清单/判定）不该被拦，否则这类任务永远收不了尾。
	ImpactLevelFor = func(int64) (int, string) { return 0, "只有侦察信息" }
	recon := &ToolSet{taskID: 20, goal: "摸清这个站有哪些入口和攻击面，输出清单"}
	if ok, why := recon.finishGate(); !ok {
		t.Fatalf("侦察型目标不该被收官闸拦住，why=%q", why)
	}

	// 没注入钩子（单测/离线）时不拦，避免把正常收官挡死。
	ImpactLevelFor = nil
	if ok, _ := tt.finishGate(); !ok {
		t.Fatal("未注入 ImpactLevelFor 时应放行")
	}
}
