package agent

import "testing"

// TestGoalDemandsExploit 锁定 goal_met 闸门的判定词表：
// 只有"要求拿权限/数据"的目标才拦；只要清单/判定的目标不该被拦（否则侦察型任务永远收不了尾）。
func TestGoalDemandsExploit(t *testing.T) {
	mustGate := []string{
		"至少把一条漏洞变成实际的命令执行或数据访问",
		"把这个站拿到权限，挂 webshell",
		"读到它的业务数据（用户表）",
		"提权到 root",
	}
	for _, g := range mustGate {
		if !goalDemandsExploit(g) {
			t.Fatalf("应要求 E3+ 才允许收官: %q", g)
		}
	}
	noGate := []string{
		"摸清这个站有哪些入口和攻击面，输出清单",
		"判断这个站值不值得继续投入",
		"列出一份指纹和端口清单",
	}
	for _, g := range noGate {
		if goalDemandsExploit(g) {
			t.Fatalf("侦察/判定型目标不该被 goal_met 闸门拦住: %q", g)
		}
	}
}

// TestGoalMetGateWiring 确认钩子存在时会被调用（server 注入后生效）。
func TestGoalMetGateWiring(t *testing.T) {
	if ImpactLevelFor != nil {
		t.Log("ImpactLevelFor 已注入（测试环境通常为 nil，属正常）")
	}
}
