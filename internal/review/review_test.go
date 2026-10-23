package review

import "testing"

// 规则版必须：我方故障 → 建议重跑；目标拒绝 → 建议放弃；且两条都带证据锚点。
func TestRuleBasedSplitsSelfFaultAndDeadEnd(t *testing.T) {
	snap := &Snapshot{
		Goal: "拿下 example.com",
		Intents: []IntentBrief{
			{ID: 20, State: "blocked", LastError: "self_error"},
			{ID: 21, State: "blocked", LastError: "target_blocked"},
			{ID: 22, State: "open"},
		},
		Findings: []FindingBrief{{ID: 1, Severity: "high", Title: "注入"}},
	}
	v := RuleBased(snap)
	if len(v.Actions) != 2 {
		t.Fatalf("期望 2 条动作，得到 %d: %+v", len(v.Actions), v.Actions)
	}
	kinds := map[int64]string{}
	for _, a := range v.Actions {
		kinds[a.Intent] = a.Kind
	}
	if kinds[20] != "retry_intent" {
		t.Fatalf("self_error 应建议重跑，得到 %q", kinds[20])
	}
	if kinds[21] != "kill_intent" {
		t.Fatalf("target_blocked 应建议放弃，得到 %q", kinds[21])
	}
	if len(v.Cites) != 2 {
		t.Fatalf("每条动作都要有证据锚点，得到 %d", len(v.Cites))
	}
	if v.Notes == "" {
		t.Fatal("规则版也要产出笔记")
	}
}

// 输出被截断时，能从句子里抢救出 body。
func TestParseLenientSalvagesTruncatedBody(t *testing.T) {
	raw := `{"verdict":"continue","body":"手里已经有用户密码和文件读取能力，还差命令执行。`
	v, ok := ParseLenient(raw)
	if !ok {
		t.Fatal("应能抢救出 body")
	}
	if len([]rune(v.Body)) < 10 {
		t.Fatalf("body 太短: %q", v.Body)
	}
}

// 完整 JSON 时 Parse 走正常路径，且只保留允许的动作。
func TestParseKeepsOnlyAllowedActions(t *testing.T) {
	raw := `{"verdict":"pivot","body":"换成写文件那条路","actions":[{"kind":"kill_intent","intent":21,"why":"已证死"},{"kind":"change_goal","why":"不该被保留"}],"confidence":"high"}`
	v, err := Parse(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(v.Actions) != 1 || v.Actions[0].Kind != "kill_intent" {
		t.Fatalf("非白名单动作必须被丢弃，得到 %+v", v.Actions)
	}
	if v.Verdict != "pivot" {
		t.Fatalf("verdict 应保留 pivot，得到 %q", v.Verdict)
	}
}
