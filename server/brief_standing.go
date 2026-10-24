package server

import (
	"fmt"
	"strings"
)

// 三段式 standing（docs/23 §二）：把"洞存在"和"能换到现实后果"彻底分开。
//
//   got      已经成立：这个洞对应的能力行已拿到（有证据）
//   one_step 差一步：洞确认了，但对应能力还没拿到 —— 明说缺哪一项
//   no_gain  换不到东西：这条洞目前不产生任何现实后果（不是误报，只是没到能用的程度）
//
// 判据全部是确定性的（洞 → 能力行 的映射与 server/loot.go 的 lootRows 同源，
// 不另写一份关键词表）；AI 只负责写措辞，绝不参与这里。
const (
	standingGot     = "got"
	standingOneStep = "one_step"
	standingNoGain  = "no_gain"
)

// lootRowKeysForFinding：这条洞会落到哪几个战果行（用 loot.go 的同一张关键词表）。
func lootRowKeysForFinding(text string) []string {
	keys := []string{}
	for _, def := range lootRows {
		if containsAnyFold(text, def.kws...) {
			keys = append(keys, def.key)
		}
	}
	return keys
}

func lootRowLabel(key string) string {
	for _, def := range lootRows {
		if def.key == key {
			return def.label
		}
	}
	return key
}

// abilityStanding 返回 (standing, gap一句话)。
func abilityStanding(f lootFinding, rows []lootRow) (string, string) {
	text := f.title + " " + f.summary + " " + f.evidence
	keys := lootRowKeysForFinding(text)
	if len(keys) == 0 {
		return standingNoGain, "这条洞暂时换不到东西"
	}
	status := map[string]string{}
	for _, r := range rows {
		status[r.Key] = r.Status
	}
	for _, k := range keys { // 任一行已拿到 = 已经成立
		if status[k] == "done" {
			return standingGot, ""
		}
	}
	for _, k := range keys { // 还没拿到 → 差一步，说清缺哪一项
		switch status[k] {
		case "run", "tried", "todo", "", "blank":
			return standingOneStep, "还差一步：拿到「" + lootRowLabel(k) + "」"
		}
	}
	return standingNoGain, "这条洞暂时换不到东西"
}

// briefHeadline：战况卡顶部那句话，回答"我现在到底能不能干活"。
// 用户要求"一眼看懂"，所以这里只许有一句，且必须给"下一步做什么"。
func briefHeadline(can []briefAbility, has []briefItem, actions []briefAction) string {
	got, oneStep := 0, 0
	for _, c := range can {
		switch c.Standing {
		case standingGot:
			got++
		case standingOneStep:
			oneStep++
		}
	}
	var firstDoable *briefAction
	for i := range actions {
		if actions[i].Doable {
			firstDoable = &actions[i]
			break
		}
	}
	switch {
	case len(can) == 0 && len(has) == 0:
		return "还没确认任何漏洞：先用只读手法摸 20 分钟，把入口和攻击面列出来"
	case len(has) == 0 && len(can) > 0:
		return fmt.Sprintf("有 %d 个洞，但还没换成任何权限（%d 个差一步）：先把最强那个打成能读到东西",
			len(can), oneStep)
	case len(has) > 0 && firstDoable != nil:
		return fmt.Sprintf("已经拿到 %d 项能力：现在可以「%s」", len(has), firstDoable.Title)
	case len(has) > 0:
		return fmt.Sprintf("已经拿到 %d 项能力，但这几件事的前置还没齐：先把权限往上推一步", len(has))
	default:
		return "还在收集信息"
	}
}

// canGroupCount 给前端做"分三组显示"用的一句话统计。
func canGroupCount(can []briefAbility) string {
	got, oneStep, noGain := 0, 0, 0
	for _, c := range can {
		switch c.Standing {
		case standingGot:
			got++
		case standingOneStep:
			oneStep++
		default:
			noGain++
		}
	}
	parts := make([]string, 0, 3)
	if got > 0 {
		parts = append(parts, fmt.Sprintf("%d 个已成立", got))
	}
	if oneStep > 0 {
		parts = append(parts, fmt.Sprintf("%d 个差一步", oneStep))
	}
	if noGain > 0 {
		parts = append(parts, fmt.Sprintf("%d 个换不到东西", noGain))
	}
	if len(parts) == 0 {
		return "还没有洞"
	}
	return strings.Join(parts, " · ")
}
