package creds

import (
	"context"
	"strings"
	"testing"
)

func TestVaultRoundTrip(t *testing.T) {
	v := NewEphemeralVault()
	ct, nonce, err := v.Encrypt("P@ssw0rd!")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ct), "P@ssw0rd") {
		t.Fatal("ciphertext leaks plaintext")
	}
	pt, err := v.Decrypt(ct, nonce)
	if err != nil || pt != "P@ssw0rd!" {
		t.Fatalf("decrypt: %q %v", pt, err)
	}
	// 同明文两次加密结果不同（随机 nonce）。
	ct2, nonce2, _ := v.Encrypt("P@ssw0rd!")
	if string(ct) == string(ct2) || string(nonce) == string(nonce2) {
		t.Fatal("nonce reuse")
	}
}

func TestFingerprintStableAndNotPlaintext(t *testing.T) {
	v, _ := NewVault("0123456789abcdef0123456789abcdef")
	fp1 := v.Fingerprint("secret")
	fp2 := v.Fingerprint("secret")
	if fp1 != fp2 {
		t.Fatal("fingerprint unstable")
	}
	if strings.Contains(fp1, "secret") {
		t.Fatal("fingerprint leaks plaintext")
	}
	if v.Fingerprint("other") == fp1 {
		t.Fatal("different secrets must differ")
	}
}

func TestStoreAddDedupeAndMarkUsed(t *testing.T) {
	s := NewStore(nil, nil)
	ctx := context.Background()
	id1, existing, err := s.Add(ctx, Entry{TaskID: 1, Kind: "password", Username: "root", Source: "config"}, "toor")
	if err != nil || existing || id1 == 0 {
		t.Fatalf("first add: id=%d existing=%v err=%v", id1, existing, err)
	}
	id2, existing, err := s.Add(ctx, Entry{TaskID: 1, Kind: "password", Username: "root"}, "toor")
	if err != nil || !existing || id2 != id1 {
		t.Fatalf("duplicate must return same id: %d/%d existing=%v", id1, id2, existing)
	}
	if err := s.MarkUsed(ctx, id1, "10.0.0.5:3306"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerified(ctx, id1, true); err != nil {
		t.Fatal(err)
	}
	e, pt, err := s.Get(ctx, id1)
	if err != nil || pt != "toor" {
		t.Fatalf("get: %v %v", pt, err)
	}
	if !e.Verified || e.UseCount != 1 || e.Status != "verified" {
		t.Fatalf("metadata not updated: %+v", e)
	}
	list, err := s.List(ctx, 1, 10)
	if err != nil || len(list) != 1 || list[0].Fingerprint == "" {
		t.Fatalf("list: %+v %v", list, err)
	}
}

func TestPlanReuseOrdersAndFilters(t *testing.T) {
	e := Entry{Kind: "password", Username: "root", Verified: true}
	plan := PlanReuse(Target{Host: "10.0.0.5"}, e, nil)
	if len(plan) < 5 {
		t.Fatalf("expected multiple attempts, got %d", len(plan))
	}
	if plan[0].Proto != "ssh" {
		t.Fatalf("verified cred should try ssh first, got %s", plan[0].Proto)
	}
	for _, a := range plan {
		if strings.Contains(a.Command, "toor") {
			t.Fatal("command template must not embed plaintext")
		}
	}
	plan = PlanReuse(Target{Host: "10.0.0.5"}, e, []int{3306, 6379})
	if len(plan) != 2 {
		t.Fatalf("port filter should narrow attempts, got %+v", plan)
	}
	hash := PlanReuse(Target{Host: "10.0.0.5"}, Entry{Kind: "hash"}, nil)
	for _, a := range hash {
		if a.Proto != "smb" && a.Proto != "rdp" {
			t.Fatalf("hash should only target windows auth, got %s", a.Proto)
		}
	}
}

func TestPlanWebReuse(t *testing.T) {
	got := PlanWebReuse("app.example.com", nil)
	if len(got) != 4 || got[0].Proto != "httpbasic" {
		t.Fatalf("web plan: %+v", got)
	}
	got = PlanWebReuse("app.example.com", []int{80, 3306, 8443})
	if len(got) != 2 || got[0].Target.Port != 80 || got[1].Target.Port != 8443 {
		t.Fatalf("filtered web plan: %+v", got)
	}
}
