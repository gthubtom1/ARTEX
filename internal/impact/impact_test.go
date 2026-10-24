package impact

import (
	"strings"
	"testing"
)

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
		steps := Propose(lv, Stats{Findings: 1, Creds: 1}, []string{"SQL 注入"})
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
	for _, s := range Propose(E0, Stats{}, nil) {
		if s.Class == ClassFileWrite {
			t.Fatal("没有漏洞时不该提 file_write")
		}
	}
}

// 2026-10-24 用户指出的逻辑错：E0（只有漏洞、没权限）不该建议"上传 webshell"。
// 那是拿到上传/写文件能力之后的事。这里钉住三条：
//  1. E0 + 普通漏洞（SQL/IDOR/XSS）→ 只能是只读验证，文本里不许出现 webshell/上传；
//  2. E0 + 已确认上传类入口 → 才允许提"落最小验证文件"；
//  3. 每条提案都必须带前置说明和 Ready 判定（界面靠它区分"现在能做/还差前置"）。
func TestProposeE0DoesNotSuggestWebshell(t *testing.T) {
	readOnly := Propose(E0, Stats{Findings: 3}, []string{"SQL 注入", "IDOR", "反射型 XSS"})
	if len(readOnly) == 0 {
		t.Fatal("有漏洞时 E0 应给一条只读验证步骤")
	}
	blob := ""
	for _, st := range readOnly {
		blob += st.Title + st.Plan
	}
	if strings.Contains(strings.ToLower(blob), "webshell") || strings.Contains(blob, "上传") {
		t.Fatalf("E0 不该提 webshell/上传：%s", blob)
	}
	if !readOnly[0].Ready || readOnly[0].Precondition == "" {
		t.Fatalf("只读验证步骤应标为就绪且带前置：%+v", readOnly[0])
	}

	withUpload := Propose(E0, Stats{Findings: 1}, []string{"任意文件上传"})
	if len(withUpload) == 0 || withUpload[0].Class != ClassFileWrite {
		t.Fatalf("确认上传入口后 E0 应提落文件验证：%+v", withUpload)
	}
	for _, lv := range []Level{E0, E1, E2, E3, E4} {
		for _, st := range Propose(lv, Stats{Findings: 1, Creds: 1, Shells: 1, DataSamples: 1, OwnedApp: 1}, nil) {
			if st.Precondition == "" {
				t.Fatalf("%v 的提案缺前置说明：%+v", lv, st)
			}
		}
	}
}
