package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/capability"
)

// p2PGDB 打开真 PostgreSQL；没有 DSN / 连不上时跳过。
func p2PGDB(t *testing.T) *db.DB {
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

// p2PGTask 建一个一次性 task（capabilities 的 ON CONFLICT (task_id, grp, item)
// 需要非 NULL task_id 才能命中唯一索引），并登记级联清理。
func p2PGTask(t *testing.T, d *db.DB) int64 {
	t.Helper()
	uniq := time.Now().UnixNano()
	desc := fmt.Sprintf("pg-p2-capabilities-%d", uniq)
	goal := "capabilities pg regression"
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
		// capabilities / evidence_timeline 的 task_id 是 ON DELETE SET NULL，
		// 必须显式清掉，否则会留下 task_id=NULL 的孤儿行。
		_, _ = d.Exec(`DELETE FROM capabilities WHERE task_id = $1`, taskID)
		_, _ = d.Exec(`DELETE FROM evidence_timeline WHERE task_id = $1`, taskID)
		_, _ = d.Exec(`DELETE FROM tasks WHERE id = $1`, taskID)
		_, _ = d.Exec(`DELETE FROM explorations WHERE id = $1`, expID)
	})
	return taskID
}

func p2CapabilityByItem(t *testing.T, cards []capability.Card, item string) capability.Card {
	t.Helper()
	for _, c := range cards {
		if c.Item == item {
			return c
		}
	}
	t.Fatalf("capability item %q not found in %d cards", item, len(cards))
	return capability.Card{}
}

func p2EqualIDs(got []int64, want ...int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestSaveCapabilitiesPGMerge 锁住 capabilities 的写入/读回：
//   - source_node_ids bigint[] 用数组字面量写入、用 array_to_json(...)::text 读回；
//   - proof_level 合并规则：低级别不降级已存在的高级别，高级别可升级；
//   - confidence 合并：observed 不被 inferred 覆盖；
//   - 同一 (task_id, grp, item) 走 ON CONFLICT 更新而不是插重复行。
func TestSaveCapabilitiesPGMerge(t *testing.T) {
	d := p2PGDB(t)
	taskID := p2PGTask(t, d)

	p2, err := NewP2(d, "pg-test-only-cred-key")
	if err != nil {
		t.Fatalf("NewP2: %v", err)
	}
	ts := NewToolSet(nil, "pg-test")
	ts.SetP2(p2)
	ts.SetTaskID(taskID)
	ctx := context.Background()

	// 1) 初次写入：P2 + source_node_ids=[101,202]。
	first := []capability.Card{{
		Group: 1, Item: "1.2", Can: "订单越权（IDOR）", Surface: "/api/order?id=",
		Evidence: "ev1: GET /api/order?id=1002 → 200 返回他人手机号", ProofLevel: "P2",
		Confidence: "observed", SourceNodeIDs: []int64{101, 202},
	}}
	n, err := ts.saveCapabilities(ctx, first)
	if err != nil {
		t.Fatalf("saveCapabilities #1: %v", err)
	}
	if n != 1 {
		t.Fatalf("saveCapabilities #1 saved=%d, want 1", n)
	}

	loaded, err := ts.loadCapabilities(ctx)
	if err != nil {
		t.Fatalf("loadCapabilities #1: %v", err)
	}
	c := p2CapabilityByItem(t, loaded, "1.2")
	if c.TaskID != taskID || c.Group != 1 {
		t.Fatalf("card scope: %+v", c)
	}
	if !p2EqualIDs(c.SourceNodeIDs, 101, 202) {
		t.Fatalf("source_node_ids readback = %#v, want [101 202]", c.SourceNodeIDs)
	}
	if c.ProofLevel != "P2" || c.Confidence != "observed" {
		t.Fatalf("proof/confidence = %q/%q, want P2/observed", c.ProofLevel, c.Confidence)
	}

	// 2) 同 item 再写：低级别 P1 + inferred + 新节点 → 高不降级，证据/节点合并。
	second := []capability.Card{{
		Group: 1, Item: "1.2", Can: "订单越权（IDOR）", Surface: "/api/order?id=",
		Evidence: "ev2: 换 id=1003 同样返回 200", ProofLevel: "P1",
		Confidence: "inferred", SourceNodeIDs: []int64{303},
	}}
	if _, err := ts.saveCapabilities(ctx, second); err != nil {
		t.Fatalf("saveCapabilities #2: %v", err)
	}
	loaded, err = ts.loadCapabilities(ctx)
	if err != nil {
		t.Fatalf("loadCapabilities #2: %v", err)
	}
	rows := 0
	for _, card := range loaded {
		if card.Item == "1.2" {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("item 1.2 rows=%d, want 1 (upsert must not insert duplicates)", rows)
	}
	c = p2CapabilityByItem(t, loaded, "1.2")
	if c.ProofLevel != "P2" {
		t.Fatalf("proof_level downgraded to %q by lower P1, want P2", c.ProofLevel)
	}
	if c.Confidence != "observed" {
		t.Fatalf("confidence downgraded to %q by inferred, want observed", c.Confidence)
	}
	if !p2EqualIDs(c.SourceNodeIDs, 101, 202, 303) {
		t.Fatalf("source_node_ids after merge = %#v, want [101 202 303]", c.SourceNodeIDs)
	}
	if !strings.Contains(c.Evidence, "ev1") || !strings.Contains(c.Evidence, "ev2") {
		t.Fatalf("evidence not appended on merge: %q", c.Evidence)
	}

	// 3) 更高级别 P4 → 允许升级（高不降级 ≠ 不能升）。
	third := []capability.Card{{
		Group: 1, Item: "1.2", Can: "订单越权（IDOR）", Surface: "/api/order?id=",
		Evidence: "ev3: 独立路径复现", ProofLevel: "P4",
		Confidence: "inferred", SourceNodeIDs: []int64{404},
	}}
	if _, err := ts.saveCapabilities(ctx, third); err != nil {
		t.Fatalf("saveCapabilities #3: %v", err)
	}
	loaded, err = ts.loadCapabilities(ctx)
	if err != nil {
		t.Fatalf("loadCapabilities #3: %v", err)
	}
	c = p2CapabilityByItem(t, loaded, "1.2")
	if c.ProofLevel != "P4" {
		t.Fatalf("proof_level=%q, want P4 after upgrade", c.ProofLevel)
	}
	if !p2EqualIDs(c.SourceNodeIDs, 101, 202, 303, 404) {
		t.Fatalf("source_node_ids after upgrade = %#v, want [101 202 303 404]", c.SourceNodeIDs)
	}

	// 4) 空 source_node_ids 写成 '{}' 后读回为空（不能变成 [null] 或报错）。
	empty := []capability.Card{{
		Group: 1, Item: "1.3", Can: "未授权访问", Surface: "/api/list",
		Evidence: "ev-empty", ProofLevel: "P1", Confidence: "inferred",
	}}
	if _, err := ts.saveCapabilities(ctx, empty); err != nil {
		t.Fatalf("saveCapabilities(empty ids): %v", err)
	}
	loaded, err = ts.loadCapabilities(ctx)
	if err != nil {
		t.Fatalf("loadCapabilities(empty ids): %v", err)
	}
	c = p2CapabilityByItem(t, loaded, "1.3")
	if len(c.SourceNodeIDs) != 0 {
		t.Fatalf("empty source_node_ids readback = %#v, want empty", c.SourceNodeIDs)
	}
}
