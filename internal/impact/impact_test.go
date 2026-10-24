package impact

import "testing"

// TestCompute 锁定权限阶梯的判定（纯规则，可复算）。
// 教训（2026-10-24 实测）：曾把漏洞摘要也计入判定，一个 XSS 被算成 E4 数据访问；
// 等级虚高比不显示更糟——用户会以为已经拿到数据。
func TestCompute(t *testing.T) {
	cases := []struct {
		name string
		in   Stats
		want Level
	}{
		{"什么都没有", Stats{}, E0},
		{"只有漏洞", Stats{Findings: 2}, E0},
		{"有凭据", Stats{Findings: 2, Creds: 1}, E1},
		{"有命令执行", Stats{Findings: 1, Shells: 1}, E3},
		{"有数据样本", Stats{Shells: 1, DataSamples: 1}, E4},
		{"跨主机立足点", Stats{OwnedHosts: 2}, E5},
		{"root 立足点", Stats{OwnedRoot: 1}, E5},
	}
	for _, c := range cases {
		if got, _ := Compute(c.in); got != c.want {
			t.Fatalf("%s: Compute=%v want %v", c.name, got, c.want)
		}
	}
	if got, _ := Compute(Stats{Findings: 1}); got.String() != "E0 仅有信息" {
		t.Fatalf("Level.String 不对: %s", got.String())
	}
}

// TestPropose 锁定"下一步提案"：每一级都要给出可执行、可拒绝的一步，
// 且必须写清 why/plan/risk（零基础用户靠这三段做决定）。
func TestPropose(t *testing.T) {
	for _, lv := range []Level{E0, E1, E2, E3, E4} {
		steps := Propose(lv, Stats{Findings: 1, Creds: 1})
		if len(steps) == 0 {
			continue // E1 有凭据才出提案，缺条件时为空是允许的
		}
		for _, s := range steps {
			if s.Title == "" || s.Why == "" || s.Plan == "" || s.Risk == "" {
				t.Fatalf("%v 的提案字段不全: %+v", lv, s)
			}
			if s.Next <= lv && lv != E0 {
				// 提案必须指向更高等级（E0 的 file_write 从 E0 算起是 O(1) 步，允许同或更高）
				t.Fatalf("%v 的提案 next=%v 没有升级", lv, s.Next)
			}
		}
	}
	// 没有漏洞时不提"把漏洞变成命令执行"
	for _, s := range Propose(E0, Stats{}) {
		if s.Class == ClassFileWrite {
			t.Fatal("没有漏洞时不该提 file_write")
		}
	}
}
