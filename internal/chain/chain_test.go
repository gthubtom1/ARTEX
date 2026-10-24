package chain

import "testing"

// TestBuild 锁定链的派生规则（纯函数，可复算）：
// 一条漏洞一条链；授权未决定=pending、允许=done、拒绝=failed；
// 拿到 shell/数据/立足点 → 链 done；全部授权被拒且无收益 → dead。
func TestBuild(t *testing.T) {
	fs := []FindingRef{{ID: 200, Title: "反射型 XSS"}}

	// 1) 刚发现漏洞、还没授权
	cs := Build(fs, nil, nil)
	if len(cs) != 1 || cs[0].Key != "F200" || cs[0].Status != "open" {
		t.Fatalf("基础链不对: %+v", cs)
	}
	if cs[0].Steps[0].Key != "confirm" || cs[0].Steps[0].Status != "done" {
		t.Fatalf("第一步必须是已确认: %+v", cs[0].Steps[0])
	}
	if cs[0].Steps[1].Status != "pending" {
		t.Fatalf("未授权应为 pending: %+v", cs[0].Steps[1])
	}

	// 2) 拒绝全部授权且无收益 → dead
	cs = Build(fs, []GrantRef{{Class: "file_write"}, {Class: "data_dump"}}, nil)
	if cs[0].Status != "dead" {
		t.Fatalf("全部拒绝应 dead: %+v", cs[0])
	}

	// 3) 允许 + 拿到命令执行 → done，且 gain 步存在
	cs = Build(fs,
		[]GrantRef{{Class: "file_write", Granted: true}},
		[]GainRef{{Kind: "shell", Ref: 42}})
	if cs[0].Status != "done" {
		t.Fatalf("拿到 shell 应 done: %+v", cs[0])
	}
	var hasGain bool
	for _, s := range cs[0].Steps {
		if s.Kind == "gain" && s.RefID == 42 && s.Status == "done" {
			hasGain = true
		}
	}
	if !hasGain {
		t.Fatalf("缺少 gain 步: %+v", cs[0].Steps)
	}
}
