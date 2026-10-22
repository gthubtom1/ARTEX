package access

import (
	"testing"
	"time"
)

func TestPoolCrossWorkerShare(t *testing.T) {
	p := NewPool(time.Minute)
	id, err := p.Register(Session{OwnedID: 1, Proto: "shell", Endpoint: "10.0.0.5:4444"})
	if err != nil || id == 0 {
		t.Fatalf("register: %d %v", id, err)
	}
	s, err := p.Lease("work#1", 1)
	if err != nil || s.ID != id {
		t.Fatalf("lease A: %+v %v", s, err)
	}
	// B 在 A 的租约内拿不到同一个会话。
	if _, err := p.Lease("work#2", 1); err == nil {
		t.Fatal("lease should be exclusive while active")
	}
	p.Release(id)
	s2, err := p.Lease("work#2", 1)
	if err != nil || s2.ID != id {
		t.Fatalf("B must reuse A's session after release: %+v %v", s2, err)
	}
	// 租约过期后也能被别人接手。
	p.nowFunc = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, err := p.Lease("work#3", 1); err != nil {
		t.Fatalf("expired lease should be stealable: %v", err)
	}
}

func TestPoolDowngradeAndPurge(t *testing.T) {
	p := NewPool(time.Minute)
	id, _ := p.Register(Session{OwnedID: 1, Proto: "webshell", Endpoint: "http://x/shell.php"})
	p.Mark(id, "down")
	if _, err := p.Lease("w", 1); err == nil {
		t.Fatal("down session must not be leased")
	}
	if n := p.PurgeDown(); n != 1 {
		t.Fatalf("purge down: %d", n)
	}
	if len(p.List(0)) != 0 {
		t.Fatal("pool not empty after purge")
	}
}

func TestRegisterIsIdempotent(t *testing.T) {
	p := NewPool(0)
	a, _ := p.Register(Session{OwnedID: 2, Proto: "ssh", Endpoint: "10.0.0.6:22"})
	b, _ := p.Register(Session{OwnedID: 2, Proto: "ssh", Endpoint: "10.0.0.6:22"})
	if a != b {
		t.Fatalf("same session must not duplicate: %d vs %d", a, b)
	}
	if got := p.List(2); len(got) != 1 {
		t.Fatalf("want 1 session, got %d", len(got))
	}
}
