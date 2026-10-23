// Package review 是复盘官的数据层：结构化快照 + 评审笔记。
//
// 硬规则（docs/09-详设-C01-复盘官.md §7）：
// 快照里只放结构化事实——不放对话文本、不放 planner 的思考、不放 worker 的推理。
// 这是"有全局观但不继承隧道视野"的物理保证。
package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	pgdb "github.com/Autumn-27/artex/db"
)

// IntentBrief 是一条意图的对外摘要（不含执行细节）。
type IntentBrief struct {
	ID         int64  `json:"id"`
	Summary    string `json:"summary"`
	State      string `json:"state"`
	Priority   int    `json:"priority"`
	LastError  string `json:"last_error,omitempty"` // self_error/env_error/target_blocked
	Attempts   int    `json:"attempts"`
	UpdatedAgo string `json:"updated_ago"`
}

type FindingBrief struct {
	ID       int64  `json:"id"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Proof    string `json:"proof"`
}

type FactBrief struct {
	ID      int64  `json:"id"`
	Summary string `json:"summary"`
}

type CapBrief struct {
	Item  string `json:"item"`
	Can   string `json:"can"`
	Proof string `json:"proof"`
}

// Snapshot 是喂给复盘官的全部内容。
type Snapshot struct {
	Goal       string         `json:"goal"`
	RoundNo    int64          `json:"round_no"`
	BudgetLeft string         `json:"budget_left"`
	Intents    []IntentBrief  `json:"intents"`
	Findings   []FindingBrief `json:"findings"`
	Facts      []FactBrief    `json:"facts"`
	Errors     map[string]int `json:"errors_by_class"`
	Negatives  []string       `json:"negatives"`
	Owned      []string       `json:"owned"`
	Caps       []CapBrief     `json:"capabilities"`
	Notes      string         `json:"notes"`
}

// Hash 是快照指纹（落地对账用；不含时间，否则缓存永远失效）。
func (s Snapshot) Hash() string {
	raw, _ := json.Marshal(struct {
		Goal    string
		Round   int64
		Intents []IntentBrief
		Finds   []FindingBrief
		Facts   []FactBrief
	}{s.Goal, s.RoundNo, s.Intents, s.Findings, s.Facts})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

// Prompt 把快照渲染成模型输入（紧凑 JSON，省 token）。
func (s Snapshot) Prompt() string {
	raw, _ := json.MarshalIndent(s, "", " ")
	return string(raw)
}

// Build 组装快照。notes 由调用方从 review_notes 读好传入。
func Build(ctx context.Context, d *pgdb.DB, taskID int64, notes string) (*Snapshot, error) {
	if d == nil {
		return nil, fmt.Errorf("db unavailable")
	}
	s := &Snapshot{Errors: map[string]int{}, Notes: notes}

	// 目标 + 预算
	var timeout int
	var firstRun *interface{}
	_ = d.QueryRowContext(ctx, `SELECT COALESCE(goal,''), COALESCE(timeout_seconds,0) FROM tasks WHERE id=$1`, taskID).
		Scan(&s.Goal, &timeout)
	_ = firstRun
	_ = d.QueryRowContext(ctx, `SELECT COALESCE(round_no,0) FROM explorations WHERE task_id=$1 ORDER BY id DESC LIMIT 1`, taskID).
		Scan(&s.RoundNo)
	if timeout > 0 {
		var leftSec float64
		_ = d.QueryRowContext(ctx, `SELECT EXTRACT(EPOCH FROM (COALESCE(first_run_at,created_at) + ($2||' seconds')::interval - now()))
			FROM tasks WHERE id=$1`, taskID, timeout).Scan(&leftSec)
		if leftSec < 0 {
			s.BudgetLeft = "已超时"
		} else {
			s.BudgetLeft = fmt.Sprintf("%d 分钟", int(leftSec/60))
		}
	}

	// 先取最近的错误事件（用于给意图标注错误类；避免在 SQL 里做大数字拼接）
	type errEv struct{ class, detail string }
	var errs []errEv
	if erows, eerr := d.QueryContext(ctx, `SELECT COALESCE(class,''), COALESCE(detail,'')
		FROM task_error_events WHERE task_id=$1 AND ts > now() - interval '6 hours'
		ORDER BY ts DESC LIMIT 200`, taskID); eerr == nil {
		defer erows.Close()
		for erows.Next() {
			var e errEv
			if erows.Scan(&e.class, &e.detail) == nil {
				errs = append(errs, e)
			}
		}
	}

	// 意图：只取摘要/状态/优先级/错误类，不含执行细节
	rows, err := d.QueryContext(ctx, `SELECT n.id, COALESCE(n.payload->>'summary',''), COALESCE(n.state,''),
			COALESCE(n.priority,0),
			EXTRACT(EPOCH FROM (now() - COALESCE(n.updated_at, now())))::int
		FROM exploration_nodes n
		WHERE n.kind='intent' AND n.exploration_id IN (SELECT id FROM explorations WHERE task_id=$1)
		ORDER BY n.id DESC LIMIT 40`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it IntentBrief
		var ago int
		if err := rows.Scan(&it.ID, &it.Summary, &it.State, &it.Priority, &ago); err != nil {
			continue
		}
		it.UpdatedAgo = fmt.Sprintf("%d 分钟前", ago/60)
		if len([]rune(it.Summary)) > 80 {
			it.Summary = string([]rune(it.Summary)[:80]) + "…"
		}
		// 错误类：detail 里出现该意图 id 即认为相关
		idStr := fmt.Sprintf("%d", it.ID)
		for _, e := range errs {
			if strings.Contains(e.detail, idStr) {
				it.LastError = e.class
				break
			}
		}
		s.Intents = append(s.Intents, it)
	}

	// 漏洞
	frows, err := d.QueryContext(ctx, `SELECT id, COALESCE(severity,''), COALESCE(NULLIF(name,''), vulnclass, '未命名'), COALESCE(status,'')
		FROM findings WHERE task_id=$1 ORDER BY id DESC LIMIT 30`, taskID)
	if err == nil {
		defer frows.Close()
		for frows.Next() {
			var f FindingBrief
			if err := frows.Scan(&f.ID, &f.Severity, &f.Title, &f.Proof); err == nil {
				s.Findings = append(s.Findings, f)
			}
		}
	}

	// 最近事实
	xrows, err := d.QueryContext(ctx, `SELECT n.id, COALESCE(n.payload->>'summary','')
		FROM exploration_nodes n JOIN explorations x ON x.id=n.exploration_id
		WHERE x.task_id=$1 AND n.kind='fact' ORDER BY n.id DESC LIMIT 10`, taskID)
	if err == nil {
		defer xrows.Close()
		for xrows.Next() {
			var f FactBrief
			var sum string
			if err := xrows.Scan(&f.ID, &sum); err == nil {
				if len([]rune(sum)) > 70 {
					sum = string([]rune(sum)[:70]) + "…"
				}
				f.Summary = sum
				s.Facts = append(s.Facts, f)
			}
		}
	}

	// 错误分类计数
	erows, err := d.QueryContext(ctx, `SELECT class, count(*) FROM task_error_events
		WHERE task_id=$1 AND ts > now() - interval '24 hours' GROUP BY class`, taskID)
	if err == nil {
		defer erows.Close()
		for erows.Next() {
			var c string
			var n int
			if err := erows.Scan(&c, &n); err == nil {
				s.Errors[c] = n
			}
		}
	}

	// 能力卡（只取三项，不做渲染）
	crows, err := d.QueryContext(ctx, `SELECT item, COALESCE(can,''), COALESCE(proof_level,'')
		FROM capabilities WHERE task_id=$1 ORDER BY id DESC LIMIT 20`, taskID)
	if err == nil {
		defer crows.Close()
		for crows.Next() {
			var c CapBrief
			if err := crows.Scan(&c.Item, &c.Can, &c.Proof); err == nil {
				s.Caps = append(s.Caps, c)
			}
		}
	}

	// 立足点
	orows, err := d.QueryContext(ctx, `SELECT COALESCE(asset_key,'') FROM assets_owned WHERE first_task_id=$1 LIMIT 20`, taskID)
	if err == nil {
		defer orows.Close()
		for orows.Next() {
			var k string
			if err := orows.Scan(&k); err == nil && strings.TrimSpace(k) != "" {
				s.Owned = append(s.Owned, k)
			}
		}
	}

	// 负结论：从 fact 里挑带否定的（与 report.negativesFromTask 同一套标记词）
	markers := []string{"无差异", "未成立", "不可利用", "没有回显", "失败", "不存在", "未发现", "不支持"}
	nrows, err := d.QueryContext(ctx, `SELECT COALESCE(n.payload->>'summary','')
		FROM exploration_nodes n JOIN explorations x ON x.id=n.exploration_id
		WHERE x.task_id=$1 AND n.kind='fact' ORDER BY n.id DESC LIMIT 200`, taskID)
	if err == nil {
		defer nrows.Close()
		seen := map[string]bool{}
		for nrows.Next() {
			var sum string
			if err := nrows.Scan(&sum); err != nil || sum == "" {
				continue
			}
			for _, m := range markers {
				if strings.Contains(sum, m) {
					if len([]rune(sum)) > 90 {
						sum = string([]rune(sum)[:90]) + "…"
					}
					if !seen[sum] {
						seen[sum] = true
						s.Negatives = append(s.Negatives, sum)
					}
					break
				}
			}
			if len(s.Negatives) >= 8 {
				break
			}
		}
	}
	sort.SliceStable(s.Negatives, func(i, j int) bool { return s.Negatives[i] < s.Negatives[j] })
	return s, nil
}
