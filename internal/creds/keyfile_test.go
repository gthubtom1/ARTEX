package creds

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadOrCreateKeyGeneratesFile(t *testing.T) {
	dir := t.TempDir()
	key, fp, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	if len(key) != 64 {
		t.Fatalf("key length = %d, want 64 (%q)", len(key), key)
	}
	if _, err := hex.DecodeString(key); err != nil {
		t.Fatalf("key is not hex: %v", err)
	}
	if fp != Fingerprint(key) || len(fp) != 8 {
		t.Fatalf("fp = %q, want %q (8 hex chars)", fp, Fingerprint(key))
	}

	path := filepath.Join(dir, KeyFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.TrimSpace(string(data)) != key {
		t.Fatalf("file content = %q, want %q", data, key)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := st.Mode().Perm(); got != 0o600 {
			t.Fatalf("mode = %04o, want 0600", got)
		}
	}
}

func TestLoadOrCreateKeyReusesExistingFile(t *testing.T) {
	dir := t.TempDir()
	first, _, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	path := filepath.Join(dir, KeyFileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	second, fp, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second != first {
		t.Fatalf("second key = %q, want %q", second, first)
	}
	if fp != Fingerprint(first) {
		t.Fatalf("second fp = %q, want %q", fp, Fingerprint(first))
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("cred.key was rewritten: before=%q after=%q", before, after)
	}
}

func TestLoadOrCreateKeyEnvWinsAndCreatesNoFile(t *testing.T) {
	t.Setenv(KeyEnvVar, "abc")
	dir := t.TempDir()
	key, fp, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	if key != "abc" {
		t.Fatalf("key = %q, want abc", key)
	}
	if fp != Fingerprint("abc") {
		t.Fatalf("fp = %q, want %q", fp, Fingerprint("abc"))
	}
	if _, err := os.Stat(filepath.Join(dir, KeyFileName)); !os.IsNotExist(err) {
		t.Fatalf("env path must not create cred.key, stat err = %v", err)
	}
}

func TestLoadOrCreateKeyInvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, KeyFileName)
	if err := os.WriteFile(path, []byte("xx"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadOrCreateKey(dir)
	if !errors.Is(err, ErrKeyFileInvalid) {
		t.Fatalf("err = %v, want ErrKeyFileInvalid", err)
	}
}

func TestLoadOrCreateKeyTrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	key := strings.Repeat("ab", 32)
	path := filepath.Join(dir, KeyFileName)
	if err := os.WriteFile(path, []byte("  "+key+" \r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	if got != key {
		t.Fatalf("key = %q, want %q", got, key)
	}
}

func TestFingerprint(t *testing.T) {
	if got := Fingerprint(""); got != "none" {
		t.Fatalf("Fingerprint(\"\") = %q, want none", got)
	}
	if Fingerprint("  ") != "none" {
		t.Fatalf("Fingerprint(blank) should be none")
	}
	a1, a2 := Fingerprint("same-key"), Fingerprint("same-key")
	if a1 != a2 {
		t.Fatalf("fingerprint unstable: %q != %q", a1, a2)
	}
	if len(a1) != 8 {
		t.Fatalf("fingerprint length = %d, want 8", len(a1))
	}
	if Fingerprint("other-key") == a1 {
		t.Fatal("different keys must have different fingerprints")
	}
}
