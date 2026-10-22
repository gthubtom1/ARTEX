package llmmodel

import "testing"

func sampleProfiles() []Profile {
	return []Profile{
		{ID: 2, Name: "Opus", Model: "claude-opus-4.5"},   // high（Tier 留空，走 Classify）
		{ID: 3, Name: "GPT-4.1", Model: "gpt-4.1"},        // mid
		{ID: 4, Name: "Flash", Model: "gemini-2.5-flash"}, // low
	}
}

func TestResolvePrecedence(t *testing.T) {
	profiles := sampleProfiles()
	tierMap := map[string]int64{"planner": 2}

	// 1. 任务级显式指定覆盖 tier
	if got := Resolve("planner", []int64{4}, tierMap, 3, profiles); got.ProfileID != 4 || got.Source != "task" {
		t.Fatalf("task 优先级失败: %+v", got)
	}
	// 2. tier 覆盖 active
	if got := Resolve("planner", nil, tierMap, 3, profiles); got.ProfileID != 2 || got.Source != "tier" {
		t.Fatalf("tier 优先级失败: %+v", got)
	}
	// 3. active 兜底
	if got := Resolve("worker", nil, tierMap, 3, profiles); got.ProfileID != 3 || got.Source != "active" {
		t.Fatalf("active 兜底失败: %+v", got)
	}
	// 4. 全空 → none
	got := Resolve("planner", nil, nil, 0, nil)
	if got.ProfileID != 0 || got.Source != "none" || got.Note == "" {
		t.Fatalf("none 分支失败: %+v", got)
	}
}

func TestResolveUnknownAgentKeyFallsBackToActive(t *testing.T) {
	profiles := sampleProfiles()
	got := Resolve("retester", nil, map[string]int64{"planner": 2, "worker": 4}, 3, profiles)
	if got.ProfileID != 3 || got.Source != "active" {
		t.Fatalf("未知 agentKey 应走 active，得到 %+v", got)
	}
}

func TestResolveTierMissFallsBackToActive(t *testing.T) {
	profiles := sampleProfiles()

	// 指向不存在的 id
	got := Resolve("planner", nil, map[string]int64{"planner": 99}, 3, profiles)
	if got.ProfileID != 3 || got.Source != "active" {
		t.Fatalf("tier 指向不存在的 id 应落回 active，得到 %+v", got)
	}
	// 0 视为未命中
	got = Resolve("planner", nil, map[string]int64{"planner": 0}, 3, profiles)
	if got.ProfileID != 3 || got.Source != "active" {
		t.Fatalf("tier 为 0 应落回 active，得到 %+v", got)
	}
}

func TestSuggestBalanced(t *testing.T) {
	profiles := sampleProfiles()
	got := Suggest([]string{"planner", "worker", "verifier", "adviser"}, nil, 3, profiles)

	want := map[string]int64{"planner": 2, "worker": 4, "verifier": 3, "adviser": 2}
	for key, id := range want {
		if got[key] != id {
			t.Errorf("Suggest[%s] = %d, want %d (all=%v)", key, got[key], id, got)
		}
	}
}

func TestSuggestOnlyLowStillAssignsPlanner(t *testing.T) {
	profiles := []Profile{{ID: 7, Name: "Flash", Model: "gemini-2.5-flash"}}
	got := Suggest([]string{"planner", "worker", "verifier"}, nil, 0, profiles)
	if got["planner"] != 7 {
		t.Fatalf("只有 low 时 planner 也应拿到 low，得到 %v", got)
	}
	if got["worker"] != 7 {
		t.Fatalf("worker 应拿到 low，得到 %v", got)
	}
	if got["verifier"] != 7 {
		t.Fatalf("verifier 无 mid/high 时应回落到 low，得到 %v", got)
	}
}

func TestSuggestEmptyProfiles(t *testing.T) {
	got := Suggest([]string{"planner", "worker"}, nil, 0, nil)
	if len(got) != 0 {
		t.Fatalf("profiles 为空应返回空 map，得到 %v", got)
	}
}

func TestSuggestPicksSmallestIDWithinTier(t *testing.T) {
	profiles := []Profile{
		{ID: 5, Model: "gpt-5"},
		{ID: 2, Model: "claude-opus-4.5"},
		{ID: 9, Model: "gemini-2.5-flash"},
		{ID: 4, Model: "gpt-4o-mini"},
		{ID: 3, Model: "gpt-4.1"},
	}
	got := Suggest([]string{"planner", "worker", "verifier"}, nil, 3, profiles)
	if got["planner"] != 2 {
		t.Errorf("planner 应取 high 里 id 最小的 2，得到 %v", got)
	}
	if got["worker"] != 4 {
		t.Errorf("worker 应取 low 里 id 最小的 4，得到 %v", got)
	}
	if got["verifier"] != 3 {
		t.Errorf("verifier 应取 mid 的 3，得到 %v", got)
	}
}

func TestSuggestKeepsExplicitTierMap(t *testing.T) {
	profiles := sampleProfiles()
	got := Suggest([]string{"planner", "worker"}, map[string]int64{"planner": 3}, 2, profiles)
	if got["planner"] != 3 {
		t.Fatalf("已设置的 tierMap 不应被推荐覆盖，得到 %v", got)
	}
	if got["worker"] != 4 {
		t.Fatalf("未设置的 worker 应继续按规则推荐 low，得到 %v", got)
	}
}

func TestReasonNonEmpty(t *testing.T) {
	profiles := sampleProfiles()
	sug := Suggest([]string{"planner", "worker", "verifier"}, nil, 3, profiles)
	if reason := Reason(sug, profiles); reason == "" {
		t.Fatal("Reason 不应为空")
	}
	if reason := Reason(map[string]int64{}, profiles); reason == "" {
		t.Fatal("空建议的 Reason 也不应为空")
	}
}
