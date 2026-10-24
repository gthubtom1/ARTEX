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
		Verdict:     "continue",
		Body:        "手里有一个后台入口，另有一条路是我方接口断的，没真正试过。",
		Cites:       []Cite{{Kind: "intent", ID: intent}},
		Actions:     []Action{{Kind: kind, Intent: intent, Why: "test"}},
		Expectation: "3 轮内出现涉及配置的新事实",
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

// P3：ask_human 是"需要人提供东西"的动作，不指向具体意图、永远不自动执行，
// 但必须写清 why（要人提供什么），否则等于没说。
func TestDecideKeepsAskHuman(t *testing.T) {
	in := GateIn{
		Verdict: Verdict{
			Verdict: "pivot",
			Cites:   []Cite{{Kind: "finding", ID: 298}},
			Actions: []Action{
				{Kind: "ask_human", Why: "需要一个普通用户账号才能验证越权"},
				{Kind: "ask_human"}, // 没写 why → 剔除
			},
			Expectation: "拿到账号后能读到别人的订单",
		},
		Snap: Snapshot{Findings: []FindingBrief{{ID: 298, Title: "IDOR"}}},
	}
	out := Decide(in)
	if !out.NeedHuman {
		t.Fatal("ask_human 必须交给人")
	}
	var kept []string
	for _, a := range out.Actions {
		kept = append(kept, a.Kind)
	}
	if len(kept) != 1 {
		t.Fatalf("应只保留 1 条（没写 why 的剔除），得到 %v / notes=%v", kept, out.Notes)
	}
}

// 战况必须进快照：审查官不能只看漏洞条数判断"有没有进展"。
func TestSnapshotPromptIncludesWar(t *testing.T) {
	s := Snapshot{
		Goal: "test",
		War: &WarBrief{
			Level: "E0 仅有信息", Has: nil,
			CanStanding: map[string]int{"got": 0, "one_step": 3},
			Doable:      []string{}, Stall: "stop（61 分钟没有新证据）",
		},
	}
	p := s.Prompt()
	for _, want := range []string{"war", "can_standing", "stall", "one_step"} {
		if !strings.Contains(p, want) {
			t.Fatalf("快照提示里缺 %q：%s", want, p)
		}
	}
}

// P3 战况兜底：模型只写"还差什么"不给动作时，审查官必须自己补一条（否则用户看完还是不知道下一步）。
// 判据纯确定性：war.got==0 且 one_step>0 且（停摆或任务已结束）→ 补 ask_human + 写 expectation。
func TestDecideWarFallbackAddsAskHuman(t *testing.T) {
	in := GateIn{
		Verdict: Verdict{Verdict: "continue", Body: "还差证据"}, // 模型没给动作、没写 expectation
		Snap: Snapshot{
			War: &WarBrief{
				CanStanding: map[string]int{"got": 0, "one_step": 2},
				Stall:       "stop（61 分钟没有新证据）",
				FirstGap:    "管理员 / 后台登录",
			},
		},
	}
	out := Decide(in)
	if len(out.Actions) != 1 || out.Actions[0].Kind != "ask_human" {
		t.Fatalf("战况停摆时应补一条 ask_human，得到 %+v", out.Actions)
	}
	if out.Level != LevelAction {
		t.Fatalf("补了动作就应该是 action 级，得到 %s", out.Level)
	}
	if !out.NeedHuman {
		t.Fatal("ask_human 必须交给人")
	}
	if out.Drop != "" {
		t.Fatalf("兜底动作自带判据，不该因 E1 被丢：%s", out.Drop)
	}

	// 已经有 got 时不该硬塞动作（有战果就别催）
	ok := GateIn{Verdict: Verdict{Verdict: "continue"}, Snap: Snapshot{War: &WarBrief{
		CanStanding: map[string]int{"got": 1, "one_step": 2}, Stall: "stop", FirstGap: "x",
	}}}
	if got := Decide(ok); len(got.Actions) != 0 {
		t.Fatalf("已经有 got 时不该补动作，得到 %+v", got.Actions)
	}
}

// 兜底动作的判据必须能被调用方存下来（真实踩过：run 85 actions 有、expectation 空）。
func TestWarFallbackExposesExpectation(t *testing.T) {
	out := Decide(GateIn{
		Verdict: Verdict{Verdict: "continue"},
		Snap: Snapshot{War: &WarBrief{
			CanStanding: map[string]int{"got": 0, "one_step": 1},
			Stall:       "stop（70 分钟没有新证据）", FirstGap: "数据库查询",
		}},
	})
	if strings.TrimSpace(out.Expectation) == "" {
		t.Fatal("战况兜底必须给出可存库的 expectation")
	}
	if !strings.Contains(out.Expectation, "数据库查询") {
		t.Fatalf("expectation 应指名缺口，得到 %q", out.Expectation)
	}
}
