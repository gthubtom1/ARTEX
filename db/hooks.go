package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// 生产端自动写入钩子（CARD-D01）。
//
// 背景（docs/00 §7）：实测 45 分钟里 evidence_timeline / assumptions 各 0 行——
// 因为这些机制被做成了"要模型记得调的工具"，而模型在真实任务里 556 次 Bash、
// 0 次 record_capability / record_assumption。所以改由引擎在既有事件点直接写。
//
// 原则：钩子失败只吞掉（不影响主流程），因为它们只增加可追溯性，不参与判定。

var negativeMarkers = []string{
	"无差异", "未成立", "不可利用", "没有回显", "不支持", "不存在", "未发现", "未启用", "已关闭", "拒绝", "失败",
}

// NoteEvidence 在事实/漏洞落库后追加一条证据时间线（幂等：同 content_ref 只写一次）。
func (s *ExplorationStore) NoteEvidence(ctx context.Context, taskID int64, kind, contentRef, actor string, nodeID, findingID int64) error {
	if s == nil || s.db == nil || taskID <= 0 {
		return nil
	}
	sum := sha256.Sum256([]byte(contentRef))
	sha := hex.EncodeToString(sum[:8])
	_, err := s.db.ExecContext(ctx, `
INSERT INTO evidence_timeline (task_id, finding_id, node_id, kind, content_ref, sha256, actor, meta)
SELECT $1,$2,$3,$4,$5,$6,$7,'{}'::jsonb
WHERE NOT EXISTS (
  SELECT 1 FROM evidence_timeline WHERE task_id=$1 AND sha256=$6 AND kind=$4
)`, taskID, findingID, nodeID, kind, contentRef, sha, actor)
	return err
}

// NoteAssumptionIfNegative 把"否定结论"沉成假设（下次同类任务可直接跳过）。
func (s *ExplorationStore) NoteAssumptionIfNegative(ctx context.Context, taskID int64, summary string) error {
	if s == nil || s.db == nil || taskID <= 0 {
		return nil
	}
	txt := strings.TrimSpace(summary)
	if txt == "" {
		return nil
	}
	hit := false
	for _, m := range negativeMarkers {
		if strings.Contains(txt, m) {
			hit = true
			break
		}
	}
	if !hit {
		return nil
	}
	if len([]rune(txt)) > 200 {
		txt = string([]rune(txt)[:200])
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO assumptions (task_id, text, status)
SELECT $1,$2,'open'
WHERE NOT EXISTS (SELECT 1 FROM assumptions WHERE task_id=$1 AND text=$2)`, taskID, txt)
	return err
}
