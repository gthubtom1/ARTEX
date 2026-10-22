package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// auditPGDB 打开真 PostgreSQL；没有 DSN / 连不上时跳过。
func auditPGDB(t *testing.T) *db.DB {
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

// TestAuditLogMetaJSONBPG 锁住 audit_log.meta 的写入路径：
// database/sql 的 []byte 参数必须显式 ::jsonb（或转 string），否则 PG 会报类型错误。
// 断言：Meta map 落库后是合法 jsonb object，且键值可读回；nil Meta 落成 '{}'。
func TestAuditLogMetaJSONBPG(t *testing.T) {
	d := auditPGDB(t)
	l := New(d)
	ctx := context.Background()

	// 唯一 tool 名，避免与残留数据/其他测试冲突（audit_log 无业务幂等键）。
	uniq := fmt.Sprintf("pg-audit-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM audit_log WHERE tool = $1 OR tool = $2`, uniq, uniq+"-nil") })

	meta := map[string]any{
		"phase":  "recon",
		"ok":     true,
		"count":  5,
		"nested": map[string]any{"host": "10.0.0.5", "port": 22},
		"tags":   []any{"ssh", "open"},
	}
	// audit_log 刻意无外键，任意正数 task_id 都能落库；这里用一个大而唯一的 int32 内正数，
	// 避免依赖某个真实 task 存在。两个已知生产问题（本用例旁路暴露，未改生产代码，
	// 也不在本用例的 meta/jsonb 断言范围内）：
	//   1) Log 对 task_id=0 走 NULLIF($1,0) → NULL，触发 audit_log.task_id NOT NULL 约束，
	//      全局审计（task_id=0）静默写不进去；
	//   2) NULLIF($1,0) 让 PG 把 $1 推断成 int4，task_id > 2^31-1 时 pgx 编码失败。
	// 修复方向：直接传 $1（列本身 NOT NULL DEFAULT 0），或写 NULLIF($1::bigint,0)。
	taskID := int64(2_000_000_000 - time.Now().UnixNano()%1_000_000)
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM audit_log WHERE task_id = $1`, taskID) })
	l.Log(ctx, Entry{
		TaskID: taskID,
		Actor:  "worker:pg-test",
		Tool:   uniq,
		Target: "127.0.0.1:9",
		Action: "exec",
		Result: "ok",
		Err:    "",
		Args:   []byte(`{"cmd":"id"}`),
		Meta:   meta,
	})

	var metaText, metaType, tool, digest string
	if err := d.QueryRow(
		`SELECT meta::text, jsonb_typeof(meta), tool, args_digest
		 FROM audit_log WHERE task_id = $1 AND tool = $2 ORDER BY id DESC LIMIT 1`, taskID, uniq).
		Scan(&metaText, &metaType, &tool, &digest); err != nil {
		t.Fatalf("query audit_log.meta: %v", err)
	}
	if tool != uniq {
		t.Fatalf("tool=%q, want %q", tool, uniq)
	}
	if metaType != "object" {
		t.Fatalf("jsonb_typeof(meta)=%q, want object (raw=%s)", metaType, metaText)
	}
	// 合法 jsonb：能原样反序列化回 map。
	var back map[string]any
	if err := json.Unmarshal([]byte(metaText), &back); err != nil {
		t.Fatalf("meta is not valid JSON: %v (raw=%s)", err, metaText)
	}
	if back["phase"] != "recon" || back["ok"] != true {
		t.Fatalf("meta scalar mismatch: %#v", back)
	}
	if n, ok := back["count"].(float64); !ok || n != 5 {
		t.Fatalf("meta count=%#v, want 5", back["count"])
	}
	nested, ok := back["nested"].(map[string]any)
	if !ok || nested["host"] != "10.0.0.5" {
		t.Fatalf("meta nested=%#v", back["nested"])
	}
	tags, ok := back["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "ssh" {
		t.Fatalf("meta tags=%#v", back["tags"])
	}
	// 参数只落摘要：digest 非空且不是原文。
	if digest == "" || digest == `{"cmd":"id"}` {
		t.Fatalf("args_digest=%q, want non-empty hash", digest)
	}

	// nil Meta → '{}'（合法 jsonb object，而不是 null/空串）。
	nilTool := uniq + "-nil"
	l.Log(ctx, Entry{TaskID: taskID, Actor: "worker:pg-test", Tool: nilTool, Action: "exec", Result: "ok"})
	var nilMetaText, nilType string
	if err := d.QueryRow(
		`SELECT meta::text, jsonb_typeof(meta) FROM audit_log WHERE task_id = $1 AND tool = $2 ORDER BY id DESC LIMIT 1`, taskID, nilTool).
		Scan(&nilMetaText, &nilType); err != nil {
		t.Fatalf("query nil-meta row: %v", err)
	}
	if nilType != "object" || nilMetaText != "{}" {
		t.Fatalf("nil Meta: typeof=%q text=%q, want object/{}", nilType, nilMetaText)
	}
}
