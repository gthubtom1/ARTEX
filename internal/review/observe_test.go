package review

import (
	"strings"
	"testing"
)

// E1：有动作（level=action）但没写可观测预期 → 门直接丢弃，且不得计划自动执行。
func TestDecideDropsActionWithoutExpectation(t *testing.T) {
	v := gatedVerdict("retry_intent", 70)
	v.Expectation = ""
	out := Decide(baseIn(v))
	if out.Drop == "" {
		t.Fatal("expectation 为空的有动作结论必须被丢弃（E1）")
	}
	if len(out.AutoApplied) != 0 {
		t.Fatalf("被 E1 丢弃的结论不得有计划中的自动动作：%+v", out.AutoApplied)
	}
}

// 只读结论（没有动作）不要求 expectation。
func TestDecideReadLevelNeedsNoExpectation(t *testing.T) {
	v := Verdict{
		Verdict: "continue",
		Body:    "手里有一个入口，还差命令执行。",
		Cites:   []Cite{{Kind: "intent", ID: 70}},
	}
	out := Decide(baseIn(v))
	if out.Level != LevelRead {
		t.Fatalf("没有动作应为 read，得到 %s", out.Level)
	}
	if out.Drop != "" {
		t.Fatalf("read 级不应因 expectation 为空被丢弃：%s", out.Drop)
	}
}

// 关键词提取：保留具体技术词，剔掉“出现/涉及/新事实/轮内”这类空话。
func TestExpectationKeywordsKeepConcreteTerms(t *testing.T) {
	kws := expectationKeywords("3 轮内出现涉及 IIS 配置的新事实")
	joined := strings.Join(kws, ",")
	if !strings.Contains(joined, "iis") || !strings.Contains(joined, "配置") {
		t.Fatalf("应保留具体词，得到 %v", kws)
	}
	for _, bad := range []string{"出现", "涉及", "事实", "轮内", "新事"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("空话 %q 不该成为关键词：%v", bad, kws)
		}
	}
}

// 新增 fact≥1 且关键词命中 → effective。
func TestEvaluateObservationEffective(t *testing.T) {
	facts := []FactBrief{{ID: 7, Summary: "确认 IIS 配置目录可写"}}
	obs := EvaluateObservation("3 轮内出现涉及 IIS 配置的新事实", facts, nil)
	if obs.Outcome != OutcomeEffective || obs.FactID != 7 {
		t.Fatalf("应判 effective 并指向 fact#7，得到 %+v", obs)
	}
	if obs.Keyword == "" {
		t.Fatalf("effective 应带命中的关键词：%+v", obs)
	}
}

// 没有新增 fact，或新增 fact 里关键词没出现 → ineffective。
func TestEvaluateObservationIneffective(t *testing.T) {
	exp := "3 轮内出现涉及 IIS 配置的新事实"
	if obs := EvaluateObservation(exp, nil, nil); obs.Outcome != OutcomeIneffective {
		t.Fatalf("没有新增 fact 应判 ineffective，得到 %+v", obs)
	}
	facts := []FactBrief{{ID: 8, Summary: "目标返回 404，未发现目录"}}
	if obs := EvaluateObservation(exp, facts, nil); obs.Outcome != OutcomeIneffective {
		t.Fatalf("关键词未命中应判 ineffective，得到 %+v", obs)
	}
	// 无法机检的预期（全是空话）也按 ineffective 处理，不假报达成。
	if obs := EvaluateObservation("3 轮内出现新事实", []FactBrief{{ID: 9, Summary: "anything"}}, nil); obs.Outcome != OutcomeIneffective {
		t.Fatalf("不可机检的预期应判 ineffective，得到 %+v", obs)
	}
}

// 期间出现直接推翻建议的新事实 → voided，优先于关键词判定。
func TestEvaluateObservationVoidedOnConflict(t *testing.T) {
	facts := []FactBrief{{ID: 11, Summary: "IIS 配置目录可写（被建议放弃的方向又出新事实）"}}
	obs := EvaluateObservation("3 轮内出现涉及 IIS 配置的新事实", facts, &FactBrief{ID: 11, Summary: "IIS 配置目录可写"})
	if obs.Outcome != OutcomeVoided || obs.FactID != 11 {
		t.Fatalf("冲突应判 voided 并指向 fact#11，得到 %+v", obs)
	}
}

// 规则版给动作时必须同时产出可机检的 expectation（否则会被自己的门丢弃）。
func TestRuleBasedSetsExpectationWhenActionExists(t *testing.T) {
	v := RuleBased(&Snapshot{
		Goal: "拿下后台",
		Intents: []IntentBrief{
			{ID: 20, State: "blocked", Summary: "探测 IIS 配置", LastError: "self_error"},
		},
	})
	if len(v.Actions) == 0 {
		t.Fatal("规则版应给出重开动作")
	}
	if strings.TrimSpace(v.Expectation) == "" {
		t.Fatal("规则版给动作时必须产出 expectation（E1）")
	}
	if kws := expectationKeywords(v.Expectation); len(kws) == 0 {
		t.Fatalf("规则版 expectation 必须可机检，得到 %q", v.Expectation)
	}
}
