// Package llmmodel 提供模型档位判定与"每个 agentKey 该用哪个 profile"的纯逻辑。
// 不依赖 server，可独立单测；接线由 server 层负责。
package llmmodel

import "strings"

// Tier 是模型的"能力档"。
type Tier string

const (
	TierHigh Tier = "high" // 规划/决策用：Opus / GPT-5 / O3 级
	TierMid  Tier = "mid"  // 通用
	TierLow  Tier = "low"  // 量大、便宜的 worker 用
)

// 关键词表会过时；改模型名只改这里。规则来源见
// docs/02-P8执行手册.md 的「P8-9 模型分层引导」小节。
var (
	// 强档。先匹配这一组：像 gemini-3-pro-flash 这种"既有 pro 又有 flash"
	// 的名字按强档算，宁可多花一点，也不要把强模型误判成便宜档。
	highKeywords = []string{
		"opus", "gpt-5", "o3", "o4",
		"gemini-3-pro", "sonnet-4.5", "sonnet-4-5",
		"qwen3-max", "glm-5",
	}
	// 便宜档。
	lowKeywords = []string{
		"flash", "mini", "lite", "turbo", "haiku",
		"7b", "8b", "4b", "1.5b",
		"small", "nano", "instant",
	}
	// 通用档。
	midKeywords = []string{
		"sonnet", "gpt-4.1", "gpt-4o", "qwen-max", "glm-4.6",
		"deepseek-v3", "gemini-2.5-pro", "kimi-k2",
	}
)

// ParseTier 归一；空/非法 → TierMid。
func ParseTier(s string) Tier {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(TierHigh):
		return TierHigh
	case string(TierLow):
		return TierLow
	case string(TierMid):
		return TierMid
	default:
		return TierMid
	}
}

// Classify 按模型名猜档位（大小写不敏感，先做子串匹配）。
// 匹配顺序 high → low → mid，全不命中 → TierMid。
func Classify(model string) Tier {
	s := strings.ToLower(strings.TrimSpace(model))
	switch {
	case matchAnyKeyword(s, highKeywords):
		return TierHigh
	case matchAnyKeyword(s, lowKeywords):
		return TierLow
	case matchAnyKeyword(s, midKeywords):
		return TierMid
	default:
		return TierMid
	}
}

// Describe 给页面/日志用的一句话（中文）。
func (t Tier) Describe() string {
	switch t {
	case TierHigh:
		return "强（规划与决策：决定全局质量，别省）"
	case TierLow:
		return "便宜（执行量大，够用就行）"
	default:
		return "中（通用/复核）"
	}
}

func matchAnyKeyword(s string, keywords []string) bool {
	for _, kw := range keywords {
		if containsKeyword(s, kw) {
			return true
		}
	}
	return false
}

// containsKeyword 是大小写不敏感的（调用方已转小写）子串匹配。
// 唯一的例外是 "mini"：Google 的 "gemini" 里也含 mini，但 gemini 不是便宜档，
// 所以要求 mini 前面不是字母（如 gpt-4o-mini、-mini 仍然命中）。
func containsKeyword(s, kw string) bool {
	if kw != "mini" {
		return strings.Contains(s, kw)
	}
	for from := 0; from <= len(s)-len(kw); {
		i := strings.Index(s[from:], kw)
		if i < 0 {
			return false
		}
		i += from
		if i == 0 || !isASCIILetter(s[i-1]) {
			return true
		}
		from = i + len(kw)
	}
	return false
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
