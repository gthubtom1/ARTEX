package creds

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// credsPGDB 打开真 PostgreSQL；没有 DSN / 连不上时跳过（而不是失败）。
func credsPGDB(t *testing.T) *db.DB {
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

func credsUnique(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("pg-cred-%d", time.Now().UnixNano())
}

// TestCredsAddNewPasswordPG 锁住「新凭据入库」这条 basic path：
// Add(密码) → 同指纹去重 → Get 能解出明文 → 库里的 secret_enc 不是明文。
//
// 已知生产问题（本用例暴露，按约定未修改生产代码）：
// creds.Add 的 `ON CONFLICT (fingerprint)` 无法推断**部分**唯一索引
// uq_creds_fingerprint (fingerprint) WHERE fingerprint <> ''，PG 直接报
// 42P10 "no unique or exclusion constraint matching the ON CONFLICT specification"，
// 结果新凭据永远无法入库（已有指纹的 Add 靠 fallback SELECT 回查才侥幸可用）。
// 修复方向：`ON CONFLICT (fingerprint) WHERE fingerprint <> '' DO NOTHING`
// 或 `ON CONFLICT DO NOTHING`（冲突后由现有 fallback SELECT 回查 id）。
//
// 这里刻意不对 Add 的错误做任何宽松处理——Add 失败就是失败（回归测试的意义）。
func TestCredsAddNewPasswordPG(t *testing.T) {
	d := credsPGDB(t)
	s := NewStore(d, nil)
	ctx := context.Background()

	user := credsUnique(t)
	secret := "S3cret-" + user
	source := "pg-test"
	// Add 之前就登记清理，避免半途失败留下脏行（按指纹清，唯一）。
	fp := s.Vault().Fingerprint("password|" + strings.ToLower(user) + "|" + secret)
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM creds_vault WHERE fingerprint = $1`, fp) })

	id, existing, err := s.Add(ctx, Entry{Kind: "password", Username: user, Source: source}, secret)
	if err != nil {
		t.Fatalf("Add(new password) failed: %v", err)
	}
	if existing || id <= 0 {
		t.Fatalf("Add(new password): id=%d existing=%v, want new positive id", id, existing)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM creds_vault WHERE id = $1`, id) })

	// 同秘密 + 同用户名 → 不重复入库，返回同一 id。
	id2, existing2, err := s.Add(ctx, Entry{Kind: "password", Username: user, Source: source}, secret)
	if err != nil {
		t.Fatalf("Add(duplicate) failed: %v", err)
	}
	if !existing2 || id2 != id {
		t.Fatalf("duplicate add: id=%d existing=%v, want id=%d existing=true", id2, existing2, id)
	}

	// 密文非明文；指纹也不含明文。
	var ct, nonce []byte
	var storedFP string
	if err := d.QueryRow(`SELECT secret_enc, secret_nonce, fingerprint FROM creds_vault WHERE id = $1`, id).
		Scan(&ct, &nonce, &storedFP); err != nil {
		t.Fatalf("read cipher: %v", err)
	}
	if len(ct) == 0 || len(nonce) == 0 {
		t.Fatalf("empty ciphertext/nonce (ct=%d nonce=%d)", len(ct), len(nonce))
	}
	if bytes.Contains(ct, []byte(secret)) || bytes.Contains(nonce, []byte(secret)) {
		t.Fatal("ciphertext/nonce leaks plaintext")
	}
	if storedFP == "" || strings.Contains(storedFP, secret) {
		t.Fatalf("bad fingerprint %q (empty or leaks plaintext)", storedFP)
	}
	if !strings.EqualFold(storedFP, fp) {
		t.Fatalf("fingerprint mismatch: stored %q want %q", storedFP, fp)
	}

	e, plain, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if plain != secret {
		t.Fatalf("Get plaintext mismatch: %q", plain)
	}
	if e.Fingerprint != "" {
		// Entry 里的 Fingerprint 是可选的；这里只要求不泄漏明文（Get 不返回指纹也合法）。
		if strings.Contains(e.Fingerprint, secret) {
			t.Fatal("Entry.Fingerprint leaks plaintext")
		}
	}
	if e.Kind != "password" || e.Username != user || e.Source != source {
		t.Fatalf("metadata mismatch: %+v", e)
	}
	if e.Verified || e.Status != "discovered" {
		t.Fatalf("new cred should start unverified/discovered: %+v", e)
	}
}

// TestCredsUsedOnArrayRoundTripPG 锁住 used_on text[] 的读回路径
// （历史上 []string 直接 Scan 失败，已改为 array_to_json(...)::text + json.Unmarshal）。
//
// 这里直接预置一行（绕过 Add，以便与上面的 Add bug 解耦），然后走
// MarkUsed → Get/List → SetVerified。
func TestCredsUsedOnArrayRoundTripPG(t *testing.T) {
	d := credsPGDB(t)
	s := NewStore(d, nil)
	ctx := context.Background()

	user := credsUnique(t)
	secret := "RoundTrip-" + user
	v := s.Vault()
	ct, nonce, err := v.Encrypt(secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	fp := v.Fingerprint("password|" + strings.ToLower(user) + "|" + secret)

	var id int64
	if err := d.QueryRow(
		`INSERT INTO creds_vault (kind, username, secret_enc, secret_nonce, fingerprint, verified, status, used_on, use_count, source)
		 VALUES ('password', $1, $2, $3, $4, false, 'discovered', '{}', 0, 'pg-test-seed')
		 RETURNING id`, user, ct, nonce, fp).Scan(&id); err != nil {
		t.Fatalf("seed cred: %v", err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM creds_vault WHERE id = $1`, id) })

	// MarkUsed 两次（同一目标会重复追加，这是当前语义：记录每一次使用）。
	target1 := "10.0.0.5:3306"
	target2 := "10.0.0.6:22"
	if err := s.MarkUsed(ctx, id, target1); err != nil {
		t.Fatalf("MarkUsed #1: %v", err)
	}
	if err := s.MarkUsed(ctx, id, target2); err != nil {
		t.Fatalf("MarkUsed #2: %v", err)
	}

	e, plain, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after MarkUsed: %v", err)
	}
	if plain != secret {
		t.Fatalf("plaintext mismatch: %q", plain)
	}
	if e.UseCount != 2 {
		t.Fatalf("use_count=%d, want 2", e.UseCount)
	}
	if len(e.UsedOn) != 2 || e.UsedOn[0] != target1 || e.UsedOn[1] != target2 {
		t.Fatalf("used_on readback = %#v, want [%q %q]", e.UsedOn, target1, target2)
	}
	if e.Status != "in_use" {
		t.Fatalf("status=%q, want in_use after first use", e.Status)
	}

	// List 也要能读回 used_on（与 Get 共用同一段 array_to_json 逻辑）。
	list, err := s.List(ctx, 0, 100)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found *Entry
	for i := range list {
		if list[i].ID == id {
			found = &list[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("List did not return seeded cred id=%d (got %d rows)", id, len(list))
	}
	if len(found.UsedOn) != 2 || found.UsedOn[0] != target1 || found.UsedOn[1] != target2 {
		t.Fatalf("List used_on = %#v, want [%q %q]", found.UsedOn, target1, target2)
	}
	if found.UseCount != 2 {
		t.Fatalf("List use_count=%d, want 2", found.UseCount)
	}

	// SetVerified → verified/status 更新，used_on 不受影响。
	if err := s.SetVerified(ctx, id, true); err != nil {
		t.Fatalf("SetVerified: %v", err)
	}
	e, _, err = s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after SetVerified: %v", err)
	}
	if !e.Verified || e.Status != "verified" {
		t.Fatalf("after SetVerified: verified=%v status=%q", e.Verified, e.Status)
	}
	if len(e.UsedOn) != 2 || e.UsedOn[0] != target1 || e.UsedOn[1] != target2 {
		t.Fatalf("used_on changed by SetVerified: %#v", e.UsedOn)
	}

	// 密文非明文（再次从库里读，防止某次 UPDATE 把明文写进去）。
	var raw []byte
	if err := d.QueryRow(`SELECT secret_enc FROM creds_vault WHERE id = $1`, id).Scan(&raw); err != nil {
		t.Fatalf("read secret_enc: %v", err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("secret_enc contains plaintext after updates")
	}
}

// TestCredsFingerprintDedupePG 锁住指纹去重（partial unique index + fingerfprint 查询回退）。
// 预置一行后再次 Add 同一秘密：即使 INSERT 走了 ON CONFLICT 回退路径，也必须返回 existing=true + 原 id。
func TestCredsFingerprintDedupePG(t *testing.T) {
	d := credsPGDB(t)
	s := NewStore(d, nil)
	ctx := context.Background()

	user := credsUnique(t)
	secret := "Dedupe-" + user
	v := s.Vault()
	ct, nonce, err := v.Encrypt(secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	fp := v.Fingerprint("password|" + strings.ToLower(user) + "|" + secret)

	var id int64
	if err := d.QueryRow(
		`INSERT INTO creds_vault (kind, username, secret_enc, secret_nonce, fingerprint, verified, status, used_on, use_count, source)
		 VALUES ('password', $1, $2, $3, $4, false, 'discovered', '{}', 0, 'pg-test-seed')
		 RETURNING id`, user, ct, nonce, fp).Scan(&id); err != nil {
		t.Fatalf("seed cred: %v", err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM creds_vault WHERE id = $1`, id) })

	gotID, existing, err := s.Add(ctx, Entry{Kind: "password", Username: user, Source: "pg-test"}, secret)
	if err != nil {
		t.Fatalf("Add(existing secret) failed: %v", err)
	}
	if !existing || gotID != id {
		t.Fatalf("dedupe: id=%d existing=%v, want id=%d existing=true", gotID, existing, id)
	}

	// 同一秘密 + 不同用户名 → 不同指纹，不应被去重。
	other := credsUnique(t)
	otherFP := v.Fingerprint("password|" + strings.ToLower(other) + "|" + secret)
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM creds_vault WHERE fingerprint = $1`, otherFP) })
	if otherFP == fp {
		t.Fatal("fingerprint must include username")
	}
}
