package review

import (
	"strings"
	"testing"
	"time"
)

// mkSnap 构造一个最小快照：两条意图（一条我方故障、一条目标拒绝 4 次）。
func mkSnap() Snapshot {
	return Snapshot{
		Goal:    "拿下后台",
		RoundNo: 8,
		Intents: []IntentBrief{
			{ID: 70, State: "blocked", LastError: "self_error", Attempts: 1},
			{ID: 71, State: "blocked", LastError: "target_blocked", Attempts: 4},
			{ID: 72, State: "open", Attempts: 0},
		},
		Facts: []FactBrief{{ID: 1, Summary: "a"}, {ID: 2, Summary: "b"}},
	}
}

func gatedVerdict(kind string, intent int64) Verdict {
	return Verdict{
		Verdict: "continue",
		Body:    "手里有一个后台入口，另有一条路是我方接口断的，没真正试过。",
		Cites:   []Cite{{Kind: "intent", ID: intent}},
		Actions: []Action{{Kind: kind, Intent: intent, Why: "test"}},
	}
}

func baseIn(v Verdict) GateIn {
	return GateIn{
		Snap:        mkSnap(),
		Verdict:     v,
		Trigger:     TriggerCond,
		DefaultTier: TierLowRisk,
		Cooldown:    5 * time.Minute,
		FactCount:   2,
		Now:         time.Now(),
	}
}

// R5：低风险档下，"我方故障"的重开动作由它自己执行；"目标拒绝≥3次"的标死同理。
func TestDecideAutoAppliesLowRiskActions(t *testing.T) {
	out := Decide(baseIn(gatedVerdict("retry_intent", 70)))
	if out.Drop != "" {
		t.Fatalf("不该丢弃：%s（%v）", out.Drop, out.Notes)
	}
	if out.Level != LevelAction {
		t.Fatalf("level=%s，want action", out.Level)
	}
	if len(out.AutoApplied) != 1 || out.AutoApplied[0].Intent != 70 {
		t.Fatalf("应当自动重开 #70，得到 %+v", out.AutoApplied)
	}
	if out.ReasonClass != ClassRetrySelfFault {
		t.Fatalf("reason_class=%q", out.ReasonClass)
	}
}

// 只看档：动作只展示，不执行。
func TestDecideObserveNeverAutoApplies(t *testing.T) {
	in := baseIn(gatedVerdict("kill_intent", 71))
	in.DefaultTier = TierObserve
	out := Decide(in)
	if len(out.AutoApplied) != 0 {
		t.Fatalf("只看档不得自动执行：%+v", out.AutoApplied)
	}
	if out.Level != LevelAction {
		t.Fatalf("结论仍应投递归，level=%s", out.Level)
	}
}

// 目标拒绝但只试了 1 次 → 不许标死（证据门槛）。
func TestDecideKillRequiresThreeAttempts(t *testing.T) {
	snap := mkSnap()
	snap.Intents[1].Attempts = 1
	in := baseIn(gatedVerdict("kill_intent", 71))
	in.Snap = snap
	out := Decide(in)
	if len(out.Actions) != 0 {
		t.Fatalf("只试 1 次不该允许标死：%+v", out.Actions)
	}
	if len(out.AutoApplied) != 0 {
		t.Fatalf("更不该自动执行：%+v", out.AutoApplied)
	}
}

// Q3 无依据：动作没被 cites 引用 → 剔除。
func TestDecideDropsUncitedAction(t *testing.T) {
	v := gatedVerdict("retry_intent", 70)
	v.Cites = []Cite{{Kind: "intent", ID: 71}}
	out := Decide(baseIn(v))
	if len(out.Actions) != 0 {
		t.Fatalf("无依据动作必须剔除：%+v", out.Actions)
	}
}

// Q2 过期：动作指向快照里不存在的意图 → 剔除。
func TestDecideDropsStaleAction(t *testing.T) {
	v := gatedVerdict("retry_intent", 999)
	v.Cites = []Cite{{Kind: "intent", ID: 999}}
	out := Decide(baseIn(v))
	if len(out.Actions) != 0 {
		t.Fatalf("过期动作必须剔除：%+v", out.Actions)
	}
	if len(out.AutoApplied) != 0 {
		t.Fatal("过期动作不得自动执行")
	}
}

// R1 只对变化说话：同一 dedupe_key 且回执未变 → 丢弃。
func TestDecideDedupeSameKey(t *testing.T) {
	in := baseIn(gatedVerdict("retry_intent", 70))
	first := Decide(in)
	if first.DedupeKey == "" {
		t.Fatal("动作非空时必须产出 dedupe_key")
	}
	in.Recent = []RunBrief{{DedupeKey: first.DedupeKey, Level: LevelAction, ReasonClass: first.ReasonClass}}
	out := Decide(in)
	if out.Drop == "" {
		t.Fatal("同一动作+同一状态说了第二次必须被去重")
	}
	// 状态变了（意图重新出别的错）→ 应当允许再说一次。
	snap := mkSnap()
	snap.Intents[0].State = "open"
	snap.Intents[0].LastError = "env_error"
	in.Snap = snap
	out2 := Decide(in)
	if out2.Drop != "" {
		t.Fatalf("状态变化后应允许再次投递，得到 drop=%s", out2.Drop)
	}
}

// R4 降权：同类建议被拒 2 次 → 不再推动作。
func TestDecideDownweightAfterTwoRejections(t *testing.T) {
	in := baseIn(gatedVerdict("retry_intent", 70))
	cls := ClassRetrySelfFault
	in.Recent = []RunBrief{
		{ReasonClass: cls, AckStatus: "rejected"},
		{ReasonClass: cls, AckStatus: "rejected"},
	}
	out := Decide(in)
	if len(out.Actions) != 0 || len(out.AutoApplied) != 0 {
		t.Fatalf("被拒 2 次后应降为只读：%+v %+v", out.Actions, out.AutoApplied)
	}
	if out.Level != LevelRead {
		t.Fatalf("level=%s，want read", out.Level)
	}
}

// R2 冷却：5 分钟内再触发 → 丢弃；但手动不受限。
func TestDecideCooldown(t *testing.T) {
	in := baseIn(gatedVerdict("retry_intent", 70))
	in.State.LastSentAt = in.Now.Add(-1 * time.Minute)
	if out := Decide(in); out.Drop == "" {
		t.Fatal("冷却期内必须丢弃")
	}
	in.Trigger = TriggerManual
	if out := Decide(in); out.Drop != "" {
		t.Fatalf("手动触发不受冷却限制，得到 %s", out.Drop)
	}
}

// R3 配额：介入上限 10 次。
func TestDecideQuota(t *testing.T) {
	in := baseIn(gatedVerdict("retry_intent", 70))
	in.State.Interventions = 10
	if out := Decide(in); out.Drop == "" {
		t.Fatal("配额用尽必须丢弃")
	}
}

// R3 自动动作上限：到 5 次后只提建议，不再自己动手。
func TestDecideAutoActionCap(t *testing.T) {
	in := baseIn(gatedVerdict("retry_intent", 70))
	in.State.AutoActions = 5
	out := Decide(in)
	if len(out.AutoApplied) != 0 {
		t.Fatalf("自动动作已达上限仍执行：%+v", out.AutoApplied)
	}
	if out.Level != LevelAction {
		t.Fatal("仍应作为建议投递")
	}
}

// R6 熔断：自动做了 3 次且 fact 没增加 → 转只看。
func TestDecideTripOnNoImprovement(t *testing.T) {
	in := baseIn(gatedVerdict("retry_intent", 70))
	in.State.AutoActions = 3
	in.State.LastFacts = 2 // 与 FactCount 相同 → 没变好
	out := Decide(in)
	if !out.Trip {
		t.Fatal("应触发熔断")
	}
	if len(out.AutoApplied) != 0 {
		t.Fatalf("熔断当次就不得再自动执行：%+v", out.AutoApplied)
	}
}

// 收口/转向必须人拍板：不能自动执行，也不因为是 wrapup 就吞掉结论。
func TestDecideWrapupNeedsHuman(t *testing.T) {
	v := gatedVerdict("retry_intent", 70)
	v.Verdict = "wrapup"
	out := Decide(baseIn(v))
	if !out.NeedHuman {
		t.Fatal("收口必须标记 NeedHuman")
	}
	if out.Drop != "" {
		t.Fatal("收口结论不该被丢弃")
	}
}

// AdviceText 必须写清"建议不是命令"并带核对协议，避免主 Agent 直接照做。
func TestAdviceTextCarriesProtocol(t *testing.T) {
	txt := AdviceText("手里有入口", []Action{{Kind: "retry_intent", Intent: 70, Why: "我方故障"}}, nil)
	for _, want := range []string{"建议不是命令", "graph_overview", "回执"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("建议正文缺少 %q：%s", want, txt)
		}
	}
}
