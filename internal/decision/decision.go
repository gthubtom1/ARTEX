// Package decision 是「决策队列」：能力卡是给零基础用户看的菜单，
// 用户只做三个商业决策（不是技术决策）：
//
//	evidence  只拿证据  —— 该能力再取 1 条样本就停（默认，最省风险）
//	continue  继续深挖  —— 允许升到 P3，但给硬预算（时间/轮数）
//	switch    换目标    —— 该资产冻结，把 worker 换到未测资产
//
// 本包只算**裁决效果**（状态/预算/hint 文案），不碰 DB；落库由 agent/server 做。
package decision

import (
	"fmt"
	"strings"
)

// Action 是三档裁决之一。
type Action string

const (
	ActionEvidence Action = "evidence" // 只拿证据（默认）
	ActionContinue Action = "continue" // 继续深挖（有预算）
	ActionSwitch   Action = "switch"   // 换目标（冻结该资产）
)

// Item 是待裁决的能力卡（最小字段）。
type Item struct {
	CapID    int64   `json:"cap_id"`
	TaskID   int64   `json:"task_id"`
	Group    int     `json:"group"`
	Item     string  `json:"item"`
	Can      string  `json:"can"`
	Surface  string  `json:"surface"`
	Status   string  `json:"status"`
	AssetIDs []int64 `json:"asset_ids,omitempty"`
}

// Verdict 是一次裁决的效果。
type Verdict struct {
	CapID          int64   `json:"cap_id"`
	Action         Action  `json:"action"`
	Status         string  `json:"status"`              // approved|declined|done
	ProofCap       string  `json:"proof_cap,omitempty"` // 该裁决允许的最高证明级别
	BudgetMinutes  int     `json:"budget_minutes,omitempty"`
	BudgetRounds   int     `json:"budget_rounds,omitempty"`
	FreezeAssetIDs []int64 `json:"freeze_asset_ids,omitempty"`
	Hint           string  `json:"hint"`
	Reason         string  `json:"reason,omitempty"`
}

// Parse 把前端/API 的字符串归一到 Action。
func Parse(s string) (Action, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "evidence", "只拿证据", "evidence_only", "proof":
		return ActionEvidence, nil
	case "continue", "继续", "继续深挖", "dig":
		return ActionContinue, nil
	case "switch", "换目标", "skip", "freeze":
		return ActionSwitch, nil
	}
	return "", fmt.Errorf("未知裁决 %q（只接受 evidence|continue|switch）", s)
}

// Decide 计算裁决效果。默认必须是 ActionEvidence——零基础用户不该被要求做技术决策。
func Decide(it Item, action Action, note string) (Verdict, error) {
	if it.CapID == 0 {
		return Verdict{}, fmt.Errorf("cap_id 必填")
	}
	surface := strings.TrimSpace(it.Surface)
	if surface == "" {
		surface = "该入口"
	}
	can := strings.TrimSpace(it.Can)
	if can == "" {
		can = "该能力"
	}
	v := Verdict{CapID: it.CapID, Action: action, Reason: note}
	switch action {
	case ActionEvidence:
		v.Status = "approved"
		v.ProofCap = "P2"
		v.Hint = fmt.Sprintf("用户裁决：只拿证据。对「%s」（%s）最多再取 1 条样本（≤1 行、打码），拿到就停；"+
			"不要批量导出、不要写库、不要改数据。", can, surface)
	case ActionContinue:
		v.Status = "approved"
		v.ProofCap = "P3"
		v.BudgetMinutes = 15
		v.BudgetRounds = 20
		v.Hint = fmt.Sprintf("用户裁决：继续深挖「%s」（%s）。预算 15 分钟 / 20 轮；到点无论结果如何都写回 fact，"+
			"并把新证据用 record_capability 升级证明级别。", can, surface)
	case ActionSwitch:
		v.Status = "declined"
		v.ProofCap = "P1"
		v.FreezeAssetIDs = append(v.FreezeAssetIDs, it.AssetIDs...)
		v.Hint = fmt.Sprintf("用户裁决：换目标。冻结「%s」（%s）相关资产，本轮不再投入；"+
			"转去范围内未测资产（用 list_untested_assets 挑一个）。", can, surface)
	default:
		return Verdict{}, fmt.Errorf("未知裁决 %q", action)
	}
	if strings.TrimSpace(note) != "" {
		v.Hint += " 用户备注：" + strings.TrimSpace(note)
	}
	return v, nil
}

// MaxBatch 是一次批量裁决允许的最大卡数（防止误点全选把队列刷爆）。
const MaxBatch = 200

// NormalizeBatch 校验批量裁决入参：action 必须合法，ids 去重且丢弃非正数，
// 有效条数为 0 或超过 MaxBatch 时报错。返回保持原顺序的干净 id 列表。
func NormalizeBatch(ids []int64, action Action) ([]int64, error) {
	switch action {
	case ActionEvidence, ActionContinue, ActionSwitch:
	default:
		return nil, fmt.Errorf("未知裁决 %q（只接受 evidence|continue|switch）", action)
	}
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ids 为空（至少选一张能力卡）")
	}
	if len(out) > MaxBatch {
		return nil, fmt.Errorf("一次最多裁决 %d 张能力卡（本次 %d 张）", MaxBatch, len(out))
	}
	return out, nil
}

// Pending 从一批能力卡里挑出待裁决的（status=open），按组号/证明级别排序在前。
func Pending(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if strings.EqualFold(it.Status, "open") || it.Status == "" {
			out = append(out, it)
		}
	}
	return out
}
