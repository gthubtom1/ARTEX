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

// ───────────── 能力级别升级 / 立足点登记（CARD-D01 H3 / H5）─────────────

// ProofKeyword 把漏洞类别映射到能力卡上的关键词；映射不出来返回空串（不升级）。
// 只做"有证据就把未证实提到已证实"，不猜测级别高低。
func ProofKeyword(vulnClass, name string) string {
	t := vulnClass + " " + name
	switch {
	case containsAny(t, "文件读取", "路径穿越", "目录穿越", "LFI", "path_traversal", "file read"):
		return "文件"
	case containsAny(t, "哈希", "hash", "MD5", "NTLM", "口令"):
		return "口令"
	case containsAny(t, "命令执行", "代码执行", "RCE", "webshell", "后门"):
		return "命令"
	case containsAny(t, "SQL", "注入", "injection"):
		return "注入"
	case containsAny(t, "后台", "登录接管", "弱口令", "默认口令"):
		return "后台"
	}
	return ""
}

// IsFootholdSignal 判断这条 finding 是否意味着"有立足点"（可登记台账）。
func IsFootholdSignal(vulnClass, name string) bool {
	return containsAny(vulnClass+" "+name, "命令执行", "代码执行", "RCE", "webshell", "后门", "反弹", "shell")
}

func containsAny(hay string, needles ...string) bool {
	up := strings.ToUpper(hay)
	for _, n := range needles {
		if strings.Contains(up, strings.ToUpper(n)) {
			return true
		}
	}
	return false
}

// BumpCapabilityProof 把该任务里命中的能力项从未证实提到"已证实"（P2 / observed）。
// keyword 为空则不动。只升不降，且不改 severity/impact 等其它字段。
func (s *ExplorationStore) BumpCapabilityProof(ctx context.Context, taskID int64, keyword string) error {
	if s == nil || s.db == nil || taskID <= 0 || strings.TrimSpace(keyword) == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE capabilities
   SET proof_level = 'P2', confidence = 'observed', updated_at = now()
 WHERE task_id = $1
   AND (proof_level IS NULL OR proof_level IN ('', 'P1'))
   AND can ILIKE '%' || $2 || '%'`, taskID, keyword)
	return err
}

// RegisterFoothold 在有立足点信号时登记持有台账（幂等：asset_key = 'asset:<id>'）。
func (s *ExplorationStore) RegisterFoothold(ctx context.Context, taskID, assetID int64, accessType, note string) error {
	if s == nil || s.db == nil || taskID <= 0 || assetID <= 0 {
		return nil
	}
	if accessType == "" {
		accessType = "webshell"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO assets_owned (asset_key, asset_id, access_type, privilege, status, first_task_id, last_verified_at, evidence)
SELECT 'asset:' || a.id, a.id, $2, 'user', 'verified', $1, now(), $3
  FROM assets a
 WHERE a.id = $4
   AND NOT EXISTS (SELECT 1 FROM assets_owned WHERE asset_key = 'asset:' || a.id)`,
		taskID, accessType, note, assetID)
	return err
}

// ───────────── L3-lite：侦察意图有没有用结构化工具（断言→记录，不阻断）─────────────

var reconKeywords = []string{"端口", "指纹", "存活", "目录", "服务", "技术栈", "banner", "扫描", "枚举"}

// AssertReconUsedTool 检查“这条侦察事实所属的意图”有没有调用过结构化工具；
// 没有的话记一条 assumption（幂等）——这是度量与提醒，**不阻断任务**。
//
// 为什么不做阻断版（docs/06 冻结表 E01）：intent 没有 stage 字段，且模型很少主动
// 调结构化工具，硬阻断会让侦察意图永远收不了尾。先记录、观察，再决定是否升硬约束。
func (s *ExplorationStore) AssertReconUsedTool(ctx context.Context, taskID, intentID int64, summary string) error {
	if s == nil || s.db == nil || taskID <= 0 || intentID <= 0 {
		return nil
	}
	if !containsAny(summary, reconKeywords...) {
		return nil
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `
SELECT count(*) FROM tool_usage
 WHERE intent_id = $1
   AND tool_key IN ('nmap_scan','httpx_probe','nuclei_scan','ffuf','ffuf_dir')`, intentID).Scan(&n); err != nil {
		return nil
	}
	if n > 0 {
		return nil
	}
	txt := summary
	if len([]rune(txt)) > 80 {
		txt = string([]rune(txt)[:80]) + "…"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO assumptions (task_id, text, status)
SELECT $1, $2, 'open'
WHERE NOT EXISTS (SELECT 1 FROM assumptions WHERE task_id=$1 AND text=$2)`,
		taskID, "侦察未使用结构化工具："+txt)
	return err
}
