package db

import (
	"fmt"
	"testing"
	"time"
)

// TestSeedPromptVersioned pins the fingerprint-dedupe contract used by startup
// seeding: old built-in versions upgrade in place, user edits are never switched
// away from, and the same built-in body is never appended twice.
func TestSeedPromptVersioned(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()

	key := fmt.Sprintf("t-prompt-seed-%d", time.Now().UnixNano())
	a, err := d.CreateAgent(key, "prompt seed test", "")
	if err != nil {
		t.Fatal(err)
	}
	settingKey := "agent_prompt_seed." + key
	t.Cleanup(func() {
		_ = d.DeleteAgent(key)
		_, _ = d.Exec(`DELETE FROM settings WHERE key=$1`, settingKey)
	})

	body1 := "旧内置模板 v1"
	body2 := "新内置模板 v2"
	body3 := "更新内置模板 v3"
	userBody := "用户手改的正文"

	// Simulate a legacy DB: version 1 is a built-in body with no settings key.
	if _, err := d.SavePrompt(a.ID, body1, "内置默认", "system"); err != nil {
		t.Fatal(err)
	}
	switched, appendedOnly, err := d.SeedPromptVersioned(key, a.ID, body2)
	if err != nil || !switched || appendedOnly {
		t.Fatalf("old built-in should upgrade: switched=%v appended=%v err=%v", switched, appendedOnly, err)
	}
	if cur, _ := d.CurrentPrompt(a.ID); cur != body2 {
		t.Fatalf("current prompt = %q, want %q", cur, body2)
	}
	if got, _, _ := d.GetSetting(settingKey); got != PromptFingerprint(body2) {
		t.Fatalf("settings fingerprint = %q, want %q", got, PromptFingerprint(body2))
	}
	vers, _ := d.ListPromptVersions(a.ID)
	if len(vers) != 2 || vers[0].Note != builtinPromptNotePrefix+PromptFingerprint(body2) {
		t.Fatalf("upgrade should append one built-in version: %+v", vers)
	}

	// Idempotent restart: same built-in body, no new version.
	switched, appendedOnly, err = d.SeedPromptVersioned(key, a.ID, body2)
	if err != nil || switched || appendedOnly {
		t.Fatalf("same body must be a no-op: switched=%v appended=%v err=%v", switched, appendedOnly, err)
	}
	if vers, _ = d.ListPromptVersions(a.ID); len(vers) != 2 {
		t.Fatalf("no duplicate version expected, got %d", len(vers))
	}

	// User edit then a new built-in body: append for the record, keep user text.
	if _, err := d.SavePrompt(a.ID, userBody, "手改", "ui"); err != nil {
		t.Fatal(err)
	}
	switched, appendedOnly, err = d.SeedPromptVersioned(key, a.ID, body3)
	if err != nil || switched || !appendedOnly {
		t.Fatalf("user-edited prompt must not switch: switched=%v appended=%v err=%v", switched, appendedOnly, err)
	}
	if cur, _ := d.CurrentPrompt(a.ID); cur != userBody {
		t.Fatalf("user prompt clobbered: %q", cur)
	}
	if got, _, _ := d.GetSetting(settingKey); got != PromptFingerprint(body3) {
		t.Fatalf("appended-only fingerprint = %q, want %q", got, PromptFingerprint(body3))
	}
	vers, _ = d.ListPromptVersions(a.ID)
	if len(vers) != 4 || vers[0].Template != body3 || vers[0].Note != builtinPromptNotePrefix+PromptFingerprint(body3) {
		t.Fatalf("new built-in should be appended to history only: %+v", vers)
	}

	// Restart again: the body3 fingerprint is already in history, no duplicate.
	switched, appendedOnly, err = d.SeedPromptVersioned(key, a.ID, body3)
	if err != nil || switched || appendedOnly {
		t.Fatalf("history dedupe failed: switched=%v appended=%v err=%v", switched, appendedOnly, err)
	}
	if vers, _ = d.ListPromptVersions(a.ID); len(vers) != 4 {
		t.Fatalf("duplicate built-in version appended: %d", len(vers))
	}
}
