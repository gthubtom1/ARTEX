package llmmodel

import (
	"fmt"
	"sort"
	"strings"
)

// Profile 是可选模型配置的最小视图（主代理会从 db 里映射成这个结构）。
type Profile struct {
	ID    int64
	Name  string
	Model string
	Tier  Tier // 可空，空则用 Classify(Model) 补
}

// Choice 是一次选择的结果。
type Choice struct {
	ProfileID int64  `json:"profile_id"`
	Source    string `json:"source"` // "task" | "tier" | "active" | "none"
	Note      string `json:"note"`
}

// Resolve 按固定优先级为某个 agentKey 选 profile：
//  1. taskChain 非空 → 用 taskChain[0]（任务级显式指定，最高优先级，tier 不得覆盖）
//  2. tierMap[agentKey] 命中且该 id 在 profiles 里存在 → 用它（source="tier"）
//  3. activeID 在 profiles 里存在 → 用它（source="active"）
//  4. 都没有 → Choice{ProfileID:0, Source:"none"}，Note 说明"未配置可用模型"
//
// 未知 agentKey（不在 tierMap 里）走 3。
// taskChain / tierMap 里的 0 或指向不存在的 profile，都视为"未指定/未命中"，
// 继续往下一级。
func Resolve(agentKey string, taskChain []int64, tierMap map[string]int64, activeID int64, profiles []Profile) Choice {
	if len(taskChain) > 0 && taskChain[0] > 0 {
		id := taskChain[0]
		if p, ok := profileByID(profiles, id); ok {
			return Choice{
				ProfileID: id,
				Source:    "task",
				Note:      fmt.Sprintf("任务显式指定用 %s，档位映射不覆盖", profileLabel(p)),
			}
		}
		return Choice{
			ProfileID: id,
			Source:    "task",
			Note:      fmt.Sprintf("任务显式指定用配置 #%d，档位映射不覆盖", id),
		}
	}

	if id := tierMap[agentKey]; id > 0 {
		if p, ok := profileByID(profiles, id); ok {
			return Choice{
				ProfileID: id,
				Source:    "tier",
				Note: fmt.Sprintf("%s 的档位映射指向 %s，%s",
					agentKey, profileLabel(p), p.effectiveTier().Describe()),
			}
		}
	}

	if p, ok := profileByID(profiles, activeID); ok {
		return Choice{
			ProfileID: activeID,
			Source:    "active",
			Note:      fmt.Sprintf("没有可用的任务指定或档位映射，沿用全局激活配置 %s", profileLabel(p)),
		}
	}

	return Choice{
		ProfileID: 0,
		Source:    "none",
		Note:      "未配置可用模型：任务没有显式指定，档位映射未命中，全局激活配置也不可用",
	}
}

// Suggest 根据已配置的 profile 列表，为一组 agentKey 给出推荐（agentKey 顺序固定）：
//
//	planner   → tier 最高、id 最小的那个；没有 high 就用 mid；再没有用 low
//	worker    → tier 最低、id 最小的那个
//	verifier  → mid 优先，没有就 high；再没有用 low
//	adviser   → 同 planner
//	其余      → activeID
//
// tierMap 里已有且有效的显式档位优先保留（不覆盖用户已做的选择），其余按上面的
// 规则推荐。返回 map[agentKey]profileID（找不到可用 profile 时该 key 不出现）。
func Suggest(agentKeys []string, tierMap map[string]int64, activeID int64, profiles []Profile) map[string]int64 {
	sug := make(map[string]int64, len(agentKeys))
	if len(profiles) == 0 {
		return sug
	}

	for _, key := range agentKeys {
		if id, ok := tierMap[key]; ok && id > 0 {
			if _, found := profileByID(profiles, id); found {
				sug[key] = id
			}
		}
	}

	for _, key := range agentKeys {
		if _, done := sug[key]; done {
			continue
		}
		var (
			id int64
			ok bool
		)
		switch key {
		case "planner", "adviser":
			id, ok = pickByTier(profiles, TierHigh, TierMid, TierLow)
		case "worker":
			id, ok = pickByTier(profiles, TierLow, TierMid, TierHigh)
		case "verifier":
			id, ok = pickByTier(profiles, TierMid, TierHigh, TierLow)
		default:
			if p, found := profileByID(profiles, activeID); found {
				id, ok = p.ID, true
			}
		}
		if ok {
			sug[key] = id
		}
	}
	return sug
}

// Reason 返回一段给用户看的中文理由（说明为什么这么分）。
func Reason(sug map[string]int64, profiles []Profile) string {
	if len(sug) == 0 {
		return "还没有可用的模型配置：先在「系统 → 模型」里添加至少一个 profile，再回来分配 planner / worker / verifier。"
	}

	keys := orderedKeys(sug)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		p, ok := profileByID(profiles, sug[key])
		if !ok {
			parts = append(parts, fmt.Sprintf("%s → 配置 #%d", key, sug[key]))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s → %s（%s）：%s",
			key, profileLabel(p), shortTier(p.effectiveTier()), roleReason(key)))
	}
	return strings.Join(parts, "；") + "。"
}

func (p Profile) effectiveTier() Tier {
	if strings.TrimSpace(string(p.Tier)) == "" {
		return Classify(p.Model)
	}
	return ParseTier(string(p.Tier))
}

func profileByID(profiles []Profile, id int64) (Profile, bool) {
	if id <= 0 {
		return Profile{}, false
	}
	for _, p := range profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

func profileLabel(p Profile) string {
	name := strings.TrimSpace(p.Name)
	model := strings.TrimSpace(p.Model)
	switch {
	case name != "" && model != "":
		return fmt.Sprintf("「%s」（%s）", name, model)
	case name != "":
		return fmt.Sprintf("「%s」", name)
	case model != "":
		return fmt.Sprintf("「%s」", model)
	default:
		return fmt.Sprintf("配置 #%d", p.ID)
	}
}

// pickByTier 按给定档位顺序挑第一个有货的档，同档里取 id 最小的，保证结果稳定。
func pickByTier(profiles []Profile, order ...Tier) (int64, bool) {
	for _, want := range order {
		var (
			best  int64
			found bool
		)
		for _, p := range profiles {
			if p.effectiveTier() != want {
				continue
			}
			if !found || p.ID < best {
				best, found = p.ID, true
			}
		}
		if found {
			return best, true
		}
	}
	return 0, false
}

func orderedKeys(sug map[string]int64) []string {
	fixed := []string{"planner", "worker", "verifier", "adviser"}
	keys := make([]string, 0, len(sug))
	seen := make(map[string]bool, len(sug))
	for _, k := range fixed {
		if _, ok := sug[k]; ok {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	rest := make([]string, 0, len(sug))
	for k := range sug {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

func shortTier(t Tier) string {
	switch t {
	case TierHigh:
		return "强"
	case TierLow:
		return "便宜"
	default:
		return "中"
	}
}

func roleReason(key string) string {
	switch key {
	case "planner", "adviser":
		return "决策质量决定全局，别省"
	case "worker":
		return "调用量最大，够用就行"
	case "verifier":
		return "只判证据，中档够用"
	default:
		return "沿用当前激活配置"
	}
}
