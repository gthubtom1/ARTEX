package ledger

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// ledgerPGDB 打开真 PostgreSQL；没有 DSN / 连不上时跳过。
func ledgerPGDB(t *testing.T) *db.DB {
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

// TestLedgerRecordCheckUPSERTPG 锁住 asset_checks 的部分唯一索引：
//   - Own 登记；
//   - RecordCheck(ok=true) 写一条；
//   - 同小时同 kind 再写一次 → ON CONFLICT (owned_id, kind, check_key) WHERE check_key <> ” 更新而不是报错/插重复；
//   - Get 断言 verified / next_check_at / last_verified_at 写回正确。
//
// 用 kind="port" + 127.0.0.1:9（discard 端口，不发包）：RecordCheck 本身不执行命令。
func TestLedgerRecordCheckUPSERTPG(t *testing.T) {
	d := ledgerPGDB(t)
	s := NewStore(d, 24, 7)
	ctx := context.Background()

	// 唯一名字，避免与残留数据/其他测试冲突。
	host := fmt.Sprintf("pg-ledger-%d.test", time.Now().UnixNano())
	id, created, err := s.Own(ctx, Owned{Host: host, Port: 9, AccessType: "shell", Privilege: "user"})
	if err != nil {
		t.Fatalf("Own: %v", err)
	}
	if !created || id <= 0 {
		t.Fatalf("Own: id=%d created=%v, want new positive id", id, created)
	}
	key := AssetKey(host, "", 9, "shell")
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM asset_checks WHERE owned_id = $1`, id)
		_, _ = d.Exec(`DELETE FROM assets_owned WHERE id = $1 OR asset_key = $2`, id, key)
	})

	// 同一个小时内的两个时间点 → 同一 check_key。
	hour := time.Now().UTC().Truncate(time.Hour)
	ts1 := hour.Add(10 * time.Minute)
	ts2 := hour.Add(25 * time.Minute)

	if err := s.RecordCheck(ctx, Check{OwnedID: id, Kind: "port", OK: true, LatencyMS: 12, Detail: "open", TS: ts1}); err != nil {
		t.Fatalf("RecordCheck #1: %v", err)
	}
	o, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after first check: %v", err)
	}
	if o.Status != "verified" {
		t.Fatalf("status=%q, want verified", o.Status)
	}
	if !within(o.LastVerifiedAt, ts1) {
		t.Fatalf("last_verified_at=%v, want %v", o.LastVerifiedAt, ts1)
	}
	if !within(o.NextCheckAt, ts1.Add(24*time.Hour)) {
		t.Fatalf("next_check_at=%v, want %v", o.NextCheckAt, ts1.Add(24*time.Hour))
	}

	// 同小时再写一次：必须走 ON CONFLICT UPDATE（部分索引用 WHERE check_key <> '' 推断）。
	if err := s.RecordCheck(ctx, Check{OwnedID: id, Kind: "port", OK: true, LatencyMS: 99, Detail: "open-again", TS: ts2}); err != nil {
		t.Fatalf("RecordCheck #2 (same hour) must upsert, got: %v", err)
	}

	var rows int
	if err := d.QueryRow(`SELECT count(*) FROM asset_checks WHERE owned_id = $1 AND kind = 'port'`, id).Scan(&rows); err != nil {
		t.Fatalf("count checks: %v", err)
	}
	if rows != 1 {
		t.Fatalf("asset_checks rows=%d, want 1 (same-hour check must update in place)", rows)
	}
	var latency int
	var detail string
	var ok bool
	if err := d.QueryRow(
		`SELECT ok, COALESCE(latency_ms,0), detail FROM asset_checks WHERE owned_id = $1 AND kind = 'port'`,
		id).Scan(&ok, &latency, &detail); err != nil {
		t.Fatalf("read check row: %v", err)
	}
	if !ok || latency != 99 || detail != "open-again" {
		t.Fatalf("check row not updated: ok=%v latency=%d detail=%q", ok, latency, detail)
	}

	o, err = s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after second check: %v", err)
	}
	if o.Status != "verified" {
		t.Fatalf("status after upsert=%q, want verified", o.Status)
	}
	if !within(o.LastVerifiedAt, ts2) {
		t.Fatalf("last_verified_at=%v, want %v", o.LastVerifiedAt, ts2)
	}
	if !within(o.NextCheckAt, ts2.Add(24*time.Hour)) {
		t.Fatalf("next_check_at=%v, want %v", o.NextCheckAt, ts2.Add(24*time.Hour))
	}
}

// within 判断两个时间是否在 1 分钟容差内（PG 往返不改变时间精度，容差只为规避时钟抖动）。
func within(got, want time.Time) bool {
	if got.IsZero() {
		return false
	}
	d := got.Sub(want)
	if d < 0 {
		d = -d
	}
	return d < time.Minute
}
