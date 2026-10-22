package ledger

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOwnIsIdempotent(t *testing.T) {
	s := NewStore(nil, 24, 7)
	ctx := context.Background()
	id1, created1, err := s.Own(ctx, Owned{Host: "app.example.com", Port: 443, AccessType: "admin", Privilege: "admin", FirstTaskID: 1, Evidence: "admin/admin 登录成功"})
	if err != nil || !created1 {
		t.Fatalf("first own: %d %v %v", id1, created1, err)
	}
	id2, created2, err := s.Own(ctx, Owned{Host: "app.example.com", Port: 443, AccessType: "admin", FirstTaskID: 2})
	if err != nil || created2 || id2 != id1 {
		t.Fatalf("duplicate must reuse id: %d/%d created=%v err=%v", id1, id2, created2, err)
	}
	o, err := s.Get(ctx, id1)
	if err != nil || o.Evidence == "" {
		t.Fatalf("get: %+v %v", o, err)
	}
}

func TestRecordCheckTransitions(t *testing.T) {
	s := NewStore(nil, 1, 7)
	ctx := context.Background()
	id, _, _ := s.Own(ctx, Owned{Host: "10.0.0.5", Port: 22, AccessType: "ssh"})
	if err := s.RecordCheck(ctx, Check{OwnedID: id, Kind: "ssh", OK: true}); err != nil {
		t.Fatal(err)
	}
	o, _ := s.Get(ctx, id)
	if o.Status != "verified" || o.LastVerifiedAt.IsZero() {
		t.Fatalf("ok check should verify: %+v", o)
	}
	if err := s.RecordCheck(ctx, Check{OwnedID: id, Kind: "ssh", OK: false, Detail: "connection refused"}); err != nil {
		t.Fatal(err)
	}
	o, _ = s.Get(ctx, id)
	if o.Status != "stale" {
		t.Fatalf("failed check should mark stale: %+v", o)
	}
}

func TestDueAndCleanup(t *testing.T) {
	s := NewStore(nil, 1, 7)
	ctx := context.Background()
	id, _, _ := s.Own(ctx, Owned{Host: "10.0.0.7", AccessType: "shell"})
	due, err := s.Due(ctx, time.Now().Add(2*time.Hour), 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("should be due: %+v %v", due, err)
	}
	if err := s.Cleanup(ctx, id, "删除 /shell.php，清除临时账号"); err != nil {
		t.Fatal(err)
	}
	o, _ := s.Get(ctx, id)
	if o.Status != "cleaned" {
		t.Fatalf("cleanup status: %+v", o)
	}
	due, _ = s.Due(ctx, time.Now().Add(48*time.Hour), 10)
	if len(due) != 0 {
		t.Fatalf("cleaned asset must not be due: %+v", due)
	}
	stats, _ := s.Stats(ctx)
	if stats["cleaned"] != 1 || stats["total"] != 1 {
		t.Fatalf("stats: %+v", stats)
	}
}

func TestPlanChecks(t *testing.T) {
	specs := PlanChecks([]Owned{
		{ID: 1, IP: "10.0.0.5", Port: 22, AccessType: "ssh"},
		{ID: 2, Host: "app.example.com", AccessType: "webshell"},
		{ID: 3, IP: "10.0.0.6", Port: 3306, AccessType: "db"},
	})
	if len(specs) != 3 {
		t.Fatalf("want 3 specs, got %+v", specs)
	}
	if specs[0].Kind != "ssh" || specs[1].Kind != "http" {
		t.Fatalf("kinds wrong: %+v", specs)
	}
}

func TestExportJSON(t *testing.T) {
	s := NewStore(nil, 24, 7)
	ctx := context.Background()
	_, _, _ = s.Own(ctx, Owned{Host: "a.example.com", AccessType: "admin"})
	b, err := s.ExportJSON(ctx)
	if err != nil || len(b) == 0 {
		t.Fatalf("export: %v", err)
	}
	if !contains(string(b), "a.example.com") {
		t.Fatalf("export missing asset: %s", b)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestPlanChecksIncludesWebPort(t *testing.T) {
	specs := PlanChecks([]Owned{{ID: 1, Host: "127.0.0.1", Port: 8899, AccessType: "webshell"}})
	if len(specs) != 1 || !strings.Contains(specs[0].Command, "127.0.0.1:8899") {
		t.Fatalf("web check must include port: %+v", specs)
	}
}
