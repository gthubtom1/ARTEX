package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/capability"
	"github.com/Autumn-27/artex/internal/verifier"
)

// 本文件实现「verifier 专判」：用独立角色（agentKey=verifier，可绑不同/更强模型）
// 对已上报的 finding 做对抗式复核，结论写 evidence_timeline(kind='verifier')。
//
// 与三门校验的分工：三门是零 token 的确定性门槛；这里是"第二个视角"，
// 只判断证据是否支持结论，并明确写出"还缺什么"。每任务 5 分钟最多一次。
//
// D2 决策：本角色默认关（settings "verifier.enabled"=false，真靶场 25 分钟 0 次调用）。
// 自动路径（verifierPass）读开关；手动路径（verifierPassNow）不受开关影响。

type verifierGate struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (g *verifierGate) allow(taskID string, every time.Duration) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.last == nil {
		g.last = map[string]time.Time{}
	}
	if t, ok := g.last[taskID]; ok && time.Since(t) < every {
		return false
	}
	g.last[taskID] = time.Now()
	return true
}

// findingView 从探索节点 payload 里取复核所需字段。
type findingForReview struct {
	ID       int64
	Title    string
	Severity string
	Surface  string
	Evidence string
}

func findingsForVerifier(nodes []*db.Node) []findingForReview {
	out := make([]findingForReview, 0, len(nodes))
	for _, n := range nodes {
		var p map[string]any
		if json.Unmarshal(n.Payload, &p) != nil {
			continue
		}
		s := func(k string) string {
			if v, ok := p[k].(string); ok {
				return strings.TrimSpace(v)
			}
			return ""
		}
		ev := s("evidence")
		if ev == "" {
			if m, ok := p["evidence"].(map[string]any); ok {
				if v, ok := m["poc"].(string); ok {
					ev = v
				}
			}
		}
		title := s("name")
		if title == "" {
			title = s("summary")
		}
		if title == "" {
			title = s("vulnclass")
		}
		out = append(out, findingForReview{ID: n.ID, Title: title, Severity: s("severity"),
			Surface: s("surface"), Evidence: ev})
	}
	return out
}

// verifierLLMEnabled 读 D2 的 verifier.enabled 开关。默认 false；
// settings 为 nil 时按关处理。
// verifierLLMEnabled 读 ``verifier.enabled`` 开关。
// 2026-10-24 改：默认 false → true（理由同 adviserLLMEnabled：默认关+藏在深处 = 人以为在用，实际没用）。
func (s *Server) verifierLLMEnabled() bool {
	return s.settings != nil && s.settings.Bool("verifier.enabled", true)
}

// verifierPass 是「自动复核」入口：受 verifier.enabled 开关控制（D2：默认关）。
// 关着时直接返回 0，连冷却门禁与 DB 查询都不做 —— 零 LLM、零查询。
// 需要人工强制复核时调 verifierPassNow（人点了就要跑，不受开关影响）。
func (s *Server) verifierPass(ctx context.Context, t *Task) int {
	if !s.verifierLLMEnabled() {
		return 0
	}
	return s.verifierPassNow(ctx, t)
}

// verifierPassNow 无视 verifier.enabled 跑一轮复核（手动触发用）。
// 返回本轮新增的复核条数。
func (s *Server) verifierPassNow(ctx context.Context, t *Task) int {
	if t == nil {
		return 0
	}
	if !s.verifierCooldown.allow(t.ID, 5*time.Minute) {
		return 0
	}
	if s.p2 == nil || s.p2.DB == nil || t.Store == nil {
		return 0
	}
	nodes, err := t.Store.ListByKind(db.KindFinding, 50)
	if err != nil || len(nodes) == 0 {
		return 0
	}
	// 已复核过的 finding（同一 finding 只判一次）。
	reviewed := map[int64]bool{}
	taskID, _ := parseInt64(t.ID)
	rows, qerr := s.p2.DB.QueryContext(ctx,
		`SELECT COALESCE(finding_id,0) FROM evidence_timeline WHERE kind = 'verifier' AND ($1 = 0 OR task_id = $1)`, taskID)
	if qerr == nil {
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil && id > 0 {
				reviewed[id] = true
			}
		}
		rows.Close()
	}

	rt := &taskLLMRuntime{s: s, taskID: t.ID, agentKey: "verifier"}
	added := 0
	for _, f := range findingsForVerifier(nodes) {
		if reviewed[f.ID] || strings.TrimSpace(f.Title) == "" {
			continue
		}
		c, cancel := context.WithTimeout(ctx, 60*time.Second)
		// 把 retester 的复测结论喂给复核员（两条线合流：复测给事实，复核做判断）。
		retest, retestNote := s.latestRetest(ctx, f.ID)
		raw, cerr := streamCollectText(c, rt, s.agentPromptOrConst("verifier", verifier.SystemPrompt),
			verifier.Prompt(t.Goal, verifier.Input{FindingID: f.ID, Title: f.Title, Severity: f.Severity,
				Surface: f.Surface, Evidence: f.Evidence, Retest: retest, RetestNote: retestNote}))
		cancel()
		if cerr != nil {
			log.Printf("[verifier] task %s finding %d: %v (skip)", t.ID, f.ID, cerr)
			continue
		}
		rev, perr := verifier.ParseJSON(raw)
		if perr != nil {
			log.Printf("[verifier] task %s finding %d: %v (skip)", t.ID, f.ID, perr)
			continue
		}
		rev.FindingID = f.ID
		// 复核结论联动能力卡降档：rejected → P1，inferred → 不高于 P2。
		downgraded := s.applyVerifierDowngrade(ctx, taskID, f, rev)
		meta, _ := json.Marshal(rev)
		_, ierr := s.p2.DB.ExecContext(ctx,
			`INSERT INTO evidence_timeline (task_id, finding_id, ts, kind, content_ref, sha256, actor, meta)
			 VALUES (NULLIF($1,0), NULLIF($2,0), now(), 'verifier', $3, '', 'verifier', $4::jsonb)
			 ON CONFLICT (COALESCE(task_id,0), kind, content_ref) WHERE content_ref <> '' DO NOTHING`,
			taskID, f.ID, fmt.Sprintf("finding:%d", f.ID), string(meta))
		if ierr != nil {
			log.Printf("[verifier] task %s persist finding %d: %v", t.ID, f.ID, ierr)
			continue
		}
		added++
		summary := fmt.Sprintf("复核 finding#%d「%s」→ %s（%s）", f.ID, clipLine(f.Title, 40), rev.Verdict, clipLine(rev.Reason, 100))
		if downgraded > 0 {
			summary += fmt.Sprintf(" [已降档 %d 张能力卡]", downgraded)
		}
		s.engine.emitActivity(t, db.Activity{Worker: "verifier", Kind: "text", Summary: summary})
	}
	return added
}

// downgradeLevel 按复核结论把证明级别降档（纯函数，便于单测）：
// rejected → P1；inferred → 不高于 P2（P3/P4/D3/D4 降到 P2）；其他结论原样返回。
func downgradeLevel(current, verdict string) string {
	cur := strings.ToUpper(strings.TrimSpace(current))
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "rejected":
		return "P1"
	case "inferred":
		switch cur {
		case "", "P3", "P4", "D3", "D4":
			return "P2"
		}
		return cur
	}
	return cur
}

// applyVerifierDowngrade 把复核结论落到能力卡上：只动 task_id 匹配且 item 匹配的卡，
// 证据用追加方式写入（不覆盖原证据）。返回被降档的卡数。
func (s *Server) applyVerifierDowngrade(ctx context.Context, taskID int64, f findingForReview, rev verifier.Review) int {
	if s.p2 == nil || s.p2.DB == nil || taskID == 0 {
		return 0
	}
	verdict := strings.ToLower(strings.TrimSpace(string(rev.Verdict)))
	if verdict != "rejected" && verdict != "inferred" {
		return 0
	}
	item, ok := capability.Classify(f.Title + " " + f.Evidence)
	if !ok || strings.TrimSpace(item) == "" {
		return 0
	}
	rows, err := s.p2.DB.QueryContext(ctx,
		`SELECT id, proof_level FROM capabilities WHERE task_id = $1 AND item = $2`, taskID, item)
	if err != nil {
		log.Printf("[verifier] task %d downgrade lookup item %s: %v", taskID, item, err)
		return 0
	}
	type cardLevel struct {
		id    int64
		level string
	}
	matched := []cardLevel{}
	for rows.Next() {
		var c cardLevel
		if rows.Scan(&c.id, &c.level) == nil {
			matched = append(matched, c)
		}
	}
	rows.Close()
	if len(matched) == 0 {
		return 0
	}
	reason := strings.Join(strings.Fields(rev.Reason), " ")
	note := fmt.Sprintf("[verifier:%s] %s", verdict, clipLine(reason, 200))
	changed := 0
	for _, c := range matched {
		if _, uerr := s.p2.DB.ExecContext(ctx,
			`UPDATE capabilities SET proof_level = $2, confidence = 'inferred',
			 evidence = CASE WHEN btrim(evidence) = '' THEN $3 ELSE evidence || E'\n' || $3 END,
			 updated_at = now() WHERE id = $1`,
			c.id, downgradeLevel(c.level, verdict), note); uerr != nil {
			log.Printf("[verifier] task %d downgrade cap %d: %v", taskID, c.id, uerr)
			continue
		}
		changed++
	}
	return changed
}

func parseInt64(s string) (int64, error) {
	var n int64
	var err error
	_, err = fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	return n, err
}

// latestRetest 返回某 finding 最近一次已完成复测的 verdict 与摘要。
func (s *Server) latestRetest(ctx context.Context, findingID int64) (string, string) {
	if s.p2 == nil || s.p2.DB == nil || findingID <= 0 {
		return "", ""
	}
	var verdict, summary, evidence string
	err := s.p2.DB.QueryRowContext(ctx,
		`SELECT verdict, COALESCE(summary,''), COALESCE(evidence,'') FROM finding_retests
		 WHERE finding_id = $1 AND status = 'completed' ORDER BY id DESC LIMIT 1`, findingID).
		Scan(&verdict, &summary, &evidence)
	if err != nil {
		return "", ""
	}
	if summary == "" {
		summary = evidence
	}
	return verdict, summary
}
