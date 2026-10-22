package llmmodel

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		model string
		want  Tier
	}{
		// high：规划/决策级
		{"claude-opus-4.5", TierHigh},
		{"GPT-5", TierHigh},
		{"o3", TierHigh},
		{"gemini-3-pro", TierHigh},
		{"claude-sonnet-4.5", TierHigh},
		// low：量大便宜级
		{"gemini-2.5-flash", TierLow},
		{"gpt-4o-mini", TierLow},
		{"claude-haiku-4.5", TierLow},
		{"qwen3-8b", TierLow},
		// mid：通用级
		{"claude-sonnet-4", TierMid},
		{"gpt-4.1", TierMid},
		{"deepseek-v3", TierMid},
		{"gemini-2.5-pro", TierMid},
		{"qwen-max", TierMid},
		// 顺序验证：既有 pro 又有 flash，按 high 优先
		{"gemini-3-pro-flash", TierHigh},
		// 未命中 / 空 → mid
		{"", TierMid},
		{"unknown-model-xyz", TierMid},
	}
	for _, c := range cases {
		if got := Classify(c.model); got != c.want {
			t.Errorf("Classify(%q) = %s, want %s", c.model, got, c.want)
		}
	}
}

func TestParseTier(t *testing.T) {
	cases := []struct {
		in   string
		want Tier
	}{
		{"", TierMid},
		{"high", TierHigh},
		{"MID", TierMid},
		{" low ", TierLow},
		{"High", TierHigh},
		{"bogus", TierMid},
	}
	for _, c := range cases {
		if got := ParseTier(c.in); got != c.want {
			t.Errorf("ParseTier(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestTierDescribe(t *testing.T) {
	seen := map[string]Tier{}
	for _, tier := range []Tier{TierHigh, TierMid, TierLow} {
		desc := tier.Describe()
		if desc == "" {
			t.Fatalf("Tier(%s).Describe() 为空", tier)
		}
		if prev, ok := seen[desc]; ok {
			t.Fatalf("Tier(%s) 与 Tier(%s) 的 Describe 文案相同: %q", tier, prev, desc)
		}
		seen[desc] = tier
	}
}
