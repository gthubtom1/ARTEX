package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/verifier"
)

// verifierPGDB 打开真 PostgreSQL；没有 DSN / 连不上时跳过。
func verifierPGDB(t *testing.T) *db.DB {
	t.Helper()
	dsn, _, err := db.DSN()
	if err != nil {
		t.Skipf("no database config (%v) — skipping", err)
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// verifierPGTask 建一个一次性 task，并登记级联清理（capabilities / evidence_timeline 先删）。
func verifierPGTask(t *testing.T, d *db.DB) int64 {
	t.Helper()
	uniq := time.Now().UnixNano()
	desc := fmt.Sprintf("pg-verifier-%d", uniq)
	goal := "verifier pg regression"
	var expID int64
	if err := d.QueryRow(`INSERT INTO explorations (description, goal) VALUES ($1, $2) RETURNING id`, desc, goal).
		Scan(&expID); err != nil {
		t.Fatalf("insert exploration: %v", err)
	}
	var taskID int64
	if err := d.QueryRow(
		`INSERT INTO tasks (name, description, goal, exploration_id) VALUES ($1, $2, $3, $4) RETURNING id`,
		desc, desc, goal, expID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM capabilities WHERE task_id = $1`, taskID)
		_, _ = d.Exec(`DELETE FROM evidence_timeline WHERE task_id = $1`, taskID)
		_, _ = d.Exec(`DELETE FROM tasks WHERE id = $1`, taskID)
		_, _ = d.Exec(`DELETE FROM explorations WHERE id = $1`, expID)
	})
	return taskID
}

// verifierSeedCap 预置一张能力卡（模拟 record_capability/派生已写入的状态）。
func verifierSeedCap(t *testing.T, d *db.DB, taskID int64, grp int, item, proof, confidence, evidence string) int64 {
	t.Helper()
	var id int64
	if err := d.QueryRow(
		`INSERT INTO capabilities (task_id, grp, item, can, surface, evidence, proof_level, confidence, status)
		 VALUES ($1, $2, $3, $4, '/pg-test', $5, $6, $7, 'open') RETURNING id`,
		taskID, grp, item, "pg card "+item, evidence, proof, confidence).Scan(&id); err != nil {
		t.Fatalf("seed capability %s: %v", item, err)
	}
	return id
}

func verifierCapState(t *testing.T, d *db.DB, id int64) (proof, confidence, evidence string) {
	t.Helper()
	if err := d.QueryRow(`SELECT proof_level, confidence, evidence FROM capabilities WHERE id = $1`, id).
		Scan(&proof, &confidence, &evidence); err != nil {
		t.Fatalf("read capability %d: %v", id, err)
	}
	return proof, confidence, evidence
}

// TestVerifierDowngradePG 锁住 applyVerifierDowngrade 的落库行为：
// rejected → P1、inferred → 不高于 P2，confidence 变为 inferred，证据追加而非覆盖；
// verified 结论不应改动能力卡。
func TestVerifierDowngradePG(t *testing.T) {
	d := verifierPGDB(t)
	taskID := verifierPGTask(t, d)
	p2, err := agent.NewP2(d, "pg-test-only-cred-key")
	if err != nil {
		t.Fatalf("NewP2: %v", err)
	}
	s := &Server{p2: p2}
	ctx := context.Background()

	t.Run("rejected", func(t *testing.T) {
		original := "原始证据：GET /api/order?id=1002 → 200 返回他人手机号"
		id := verifierSeedCap(t, d, taskID, 1, "1.2", "P3", "observed", original)
		f := findingForReview{ID: 9001, Title: "订单越权", Severity: "high",
			Surface: "/api/order?id=", Evidence: "GET /api/order?id=1002 → 200"}
		changed := s.applyVerifierDowngrade(ctx, taskID, f, verifier.Review{
			Verdict: verifier.VerdictRejected,
			Reason:  "只有状态码 200，缺少响应体差异对照",
		})
		if changed != 1 {
			t.Fatalf("applyVerifierDowngrade(rejected) changed=%d, want 1", changed)
		}
		proof, confidence, evidence := verifierCapState(t, d, id)
		if proof != "P1" {
			t.Fatalf("proof_level=%q, want P1 after rejected", proof)
		}
		if confidence != "inferred" {
			t.Fatalf("confidence=%q, want inferred after rejected", confidence)
		}
		if !strings.Contains(evidence, original) {
			t.Fatalf("original evidence lost: %q", evidence)
		}
		if !strings.Contains(evidence, "[verifier:rejected]") {
			t.Fatalf("verifier note not appended: %q", evidence)
		}
	})

	t.Run("inferred", func(t *testing.T) {
		original := "原始证据：id 命令输出 uid=0(root)"
		id := verifierSeedCap(t, d, taskID, 4, "4.1", "P3", "observed", original)
		f := findingForReview{ID: 9002, Title: "命令执行", Severity: "critical",
			Surface: "/cgi-bin/x", Evidence: "id 命令输出 uid=0(root)"}
		changed := s.applyVerifierDowngrade(ctx, taskID, f, verifier.Review{
			Verdict: verifier.VerdictInferred,
			Reason:  "只有单次命令输出，未证明可控输入",
		})
		if changed != 1 {
			t.Fatalf("applyVerifierDowngrade(inferred) changed=%d, want 1", changed)
		}
		proof, confidence, evidence := verifierCapState(t, d, id)
		if proof != "P2" {
			t.Fatalf("proof_level=%q, want P2 after inferred", proof)
		}
		if confidence != "inferred" {
			t.Fatalf("confidence=%q, want inferred after inferred", confidence)
		}
		if !strings.Contains(evidence, original) || !strings.Contains(evidence, "[verifier:inferred]") {
			t.Fatalf("evidence merge wrong: %q", evidence)
		}
	})

	t.Run("verified-nochange", func(t *testing.T) {
		id := verifierSeedCap(t, d, taskID, 1, "1.5", "P3", "observed", "原始证据：/.git/config 可读")
		f := findingForReview{ID: 9003, Title: "源码泄露", Evidence: "/.git/config 200"}
		changed := s.applyVerifierDowngrade(ctx, taskID, f, verifier.Review{
			Verdict: verifier.VerdictVerified, Reason: "证据充分",
		})
		if changed != 0 {
			t.Fatalf("verified verdict must not change cards, changed=%d", changed)
		}
		proof, confidence, evidence := verifierCapState(t, d, id)
		if proof != "P3" || confidence != "observed" || evidence != "原始证据：/.git/config 可读" {
			t.Fatalf("verified verdict mutated card: proof=%q conf=%q evidence=%q", proof, confidence, evidence)
		}
	})
}

// verifierEvidenceSQL 必须与 server/verifier.go 的 verifierPass 写入语句保持一致：
// 依赖部分唯一索引 uq_evidence_ref (COALESCE(task_id,0), kind, content_ref) WHERE content_ref <> ”，
// 因此 ON CONFLICT 必须带同样的谓词，否则 PG 会报 42P10 / 无法幂等。
const verifierEvidenceSQL = `INSERT INTO evidence_timeline (task_id, finding_id, ts, kind, content_ref, sha256, actor, meta)
 VALUES (NULLIF($1,0), NULLIF($2,0), now(), 'verifier', $3, '', 'verifier', $4::jsonb)
 ON CONFLICT (COALESCE(task_id,0), kind, content_ref) WHERE content_ref <> '' DO NOTHING`

// TestVerifierEvidenceTimelineIdempotentPG 锁住 verifier 证据写入幂等：
// 同一 task + kind='verifier' + content_ref 写两次只能留一条，且 meta 是合法 jsonb。
func TestVerifierEvidenceTimelineIdempotentPG(t *testing.T) {
	d := verifierPGDB(t)
	taskID := verifierPGTask(t, d)
	ctx := context.Background()

	contentRef := fmt.Sprintf("pg-finding:%d", time.Now().UnixNano())
	first := `{"verdict":"rejected","reason":"缺少对照"}`
	second := `{"verdict":"verified","reason":"第二次不应覆盖"}`

	if _, err := d.ExecContext(ctx, verifierEvidenceSQL, taskID, 0, contentRef, first); err != nil {
		t.Fatalf("insert verifier evidence #1: %v", err)
	}
	// 第二次同 content_ref：必须被 DO NOTHING 吃掉，而不是报唯一冲突。
	if _, err := d.ExecContext(ctx, verifierEvidenceSQL, taskID, 0, contentRef, second); err != nil {
		t.Fatalf("insert verifier evidence #2 (same content_ref) must be idempotent, got: %v", err)
	}

	var count int
	if err := d.QueryRowContext(ctx,
		`SELECT count(*) FROM evidence_timeline WHERE task_id = $1 AND kind = 'verifier' AND content_ref = $2`,
		taskID, contentRef).Scan(&count); err != nil {
		t.Fatalf("count verifier evidence: %v", err)
	}
	if count != 1 {
		t.Fatalf("verifier evidence rows=%d, want 1 (content_ref unique)", count)
	}

	var metaText, metaType, verdict string
	if err := d.QueryRowContext(ctx,
		`SELECT meta::text, jsonb_typeof(meta), meta->>'verdict'
		 FROM evidence_timeline WHERE task_id = $1 AND kind = 'verifier' AND content_ref = $2`,
		taskID, contentRef).Scan(&metaText, &metaType, &verdict); err != nil {
		t.Fatalf("read verifier evidence meta: %v", err)
	}
	if metaType != "object" {
		t.Fatalf("jsonb_typeof(meta)=%q, want object (raw=%s)", metaType, metaText)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(metaText), &m); err != nil {
		t.Fatalf("verifier meta is not valid JSON: %v (raw=%s)", err, metaText)
	}
	if verdict != "rejected" || m["reason"] != "缺少对照" {
		t.Fatalf("first write must win on conflict: verdict=%q meta=%#v", verdict, m)
	}

	// 不同 content_ref 应当能正常新增（谓词只约束非空 content_ref 的同键）。
	otherRef := contentRef + "-b"
	if _, err := d.ExecContext(ctx, verifierEvidenceSQL, taskID, 0, otherRef, first); err != nil {
		t.Fatalf("insert verifier evidence with different content_ref: %v", err)
	}
	if err := d.QueryRowContext(ctx,
		`SELECT count(*) FROM evidence_timeline WHERE task_id = $1 AND kind = 'verifier'`,
		taskID).Scan(&count); err != nil {
		t.Fatalf("count verifier evidence (2): %v", err)
	}
	if count != 2 {
		t.Fatalf("verifier evidence rows=%d, want 2 for two content_refs", count)
	}
}

// TestCapabilitiesAPIPG 锁住 server /api/capabilities 的读回：
// source_node_ids 必须从 bigint[] 经 array_to_json 变成 JSON 数组（而不是 Scan 失败/空/字符串）。
func TestCapabilitiesAPIPG(t *testing.T) {
	d := verifierPGDB(t)
	taskID := verifierPGTask(t, d)
	p2, err := agent.NewP2(d, "pg-test-only-cred-key")
	if err != nil {
		t.Fatalf("NewP2: %v", err)
	}
	if _, err := d.Exec(
		`INSERT INTO capabilities (task_id, grp, item, can, surface, evidence, proof_level, confidence, source_node_ids, status)
		 VALUES ($1, 1, '1.2', '订单越权', '/api/order?id=', 'ev', 'P2', 'observed', '{7,8}'::bigint[], 'open')`,
		taskID); err != nil {
		t.Fatalf("seed capability: %v", err)
	}

	s := &Server{p2: p2}
	mux := http.NewServeMux()
	s.registerP2Routes(mux)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/capabilities?task_id=%d", taskID), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/capabilities: %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Cards []struct {
			Item          string  `json:"item"`
			ProofLevel    string  `json:"proof_level"`
			SourceNodeIDs []int64 `json:"source_node_ids"`
		} `json:"cards"`
		Plain string `json:"plain"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode capabilities response: %v (body=%s)", err, w.Body.String())
	}
	var got *struct {
		Item          string  `json:"item"`
		ProofLevel    string  `json:"proof_level"`
		SourceNodeIDs []int64 `json:"source_node_ids"`
	}
	for i := range payload.Cards {
		if payload.Cards[i].Item == "1.2" {
			got = &payload.Cards[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("item 1.2 missing from /api/capabilities (cards=%d)", len(payload.Cards))
	}
	if len(got.SourceNodeIDs) != 2 || got.SourceNodeIDs[0] != 7 || got.SourceNodeIDs[1] != 8 {
		t.Fatalf("source_node_ids via API = %#v, want [7 8]", got.SourceNodeIDs)
	}
	if got.ProofLevel != "P2" {
		t.Fatalf("proof_level via API = %q, want P2", got.ProofLevel)
	}
	if strings.TrimSpace(payload.Plain) == "" {
		t.Fatal("plain menu should be rendered for capability cards")
	}
}
