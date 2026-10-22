package capability

import (
	"fmt"
	"sort"
	"strings"
)

// RenderPlain 把能力卡渲染成零基础用户能直接读的白话清单。
// negatives 是"我做不到什么"的负结论（同样重要：下次省时间）。
func RenderPlain(cards []Card, negatives []string) string {
	var b strings.Builder
	if len(cards) == 0 {
		b.WriteString("■ 这个站我暂时没拿到可确证的能力\n")
	} else {
		fmt.Fprintf(&b, "■ 这个站我能做 %d 件事（按价值排序）\n", len(cards))
	}
	for i, c := range cards {
		val := c.Value
		if val == "" {
			val = "—"
		}
		group := GroupName(c.Group)
		if c.Group == 0 {
			group = "未分类"
		}
		fmt.Fprintf(&b, "%d) 【%s】%s %s —— %s\n", i+1, group, val, c.Can, c.Plain)
		if c.Surface != "" {
			fmt.Fprintf(&b, "   入口：%s\n", c.Surface)
		}
		if c.Evidence != "" {
			fmt.Fprintf(&b, "   证据（%s/%s）：%s\n", c.ProofLevel, c.Confidence, c.Evidence)
		}
		if c.Impact != "" {
			fmt.Fprintf(&b, "   影响：%s\n", c.Impact)
		}
		if c.Risk != "" {
			fmt.Fprintf(&b, "   代价：%s\n", c.Risk)
		}
		if c.NextIfApproved != "" {
			fmt.Fprintf(&b, "   要继续的话：%s\n", c.NextIfApproved)
		}
	}
	if len(negatives) > 0 {
		fmt.Fprintf(&b, "■ 我做不到什么（负结论，下次省时间）\n")
		for _, n := range negatives {
			if strings.TrimSpace(n) != "" {
				fmt.Fprintf(&b, " - %s\n", n)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// CoverageRow 是一行覆盖度（哪组测过、结论如何）。
type CoverageRow struct {
	Group   int    `json:"group"`
	Name    string `json:"name"`
	Tested  bool   `json:"tested"`
	Count   int    `json:"count"`   // 该组已确证的能力数
	Verdict string `json:"verdict"` // 结论摘要
}

// Coverage 生成 13 组覆盖度自检表——这是"全面"的定义：
// 不是找到几个洞，而是哪些组测过、结论是什么。
func Coverage(cards []Card) []CoverageRow {
	counts := map[int]int{}
	for _, c := range cards {
		counts[c.Group]++
	}
	rows := make([]CoverageRow, 0, 13)
	for g := 1; g <= 13; g++ {
		row := CoverageRow{Group: g, Name: GroupName(g), Count: counts[g]}
		if counts[g] > 0 {
			row.Tested = true
			row.Verdict = fmt.Sprintf("%d 项可做", counts[g])
		} else {
			row.Verdict = "未测/未成立"
		}
		rows = append(rows, row)
	}
	return rows
}

// SortByGroup 按组号稳定排序（前端/报告用）。
func SortByGroup(cards []Card) {
	sort.SliceStable(cards, func(i, j int) bool {
		gi, gj := cards[i].Group, cards[j].Group
		if gi == 0 {
			gi = 99
		}
		if gj == 0 {
			gj = 99
		}
		if gi != gj {
			return gi < gj
		}
		return ProofRank(cards[i].ProofLevel) > ProofRank(cards[j].ProofLevel)
	})
}
