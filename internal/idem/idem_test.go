package idem

import (
	"context"
	"testing"
)

func TestKeyStableAndDistinct(t *testing.T) {
	a := Key("task", "1", "intent", "2")
	b := Key("task", "1", "intent", "2")
	c := Key("task", "1", "intent", "3")
	if a != b {
		t.Fatal("same parts must produce same key")
	}
	if a == c {
		t.Fatal("different parts must produce different key")
	}
	if len(a) != 48 {
		t.Fatalf("want 48 hex chars, got %d", len(a))
	}
	// 分隔符防碰撞：("ab","c") 与 ("a","bc") 必须不同
	if Key("ab", "c") == Key("a", "bc") {
		t.Fatal("separator collision")
	}
}

func TestKeyForChangesWithPayload(t *testing.T) {
	k1 := KeyFor(1, 2, 3, "record_fact", []byte(`{"a":1}`))
	k2 := KeyFor(1, 2, 3, "record_fact", []byte(`{"a":2}`))
	k3 := KeyFor(1, 2, 4, "record_fact", []byte(`{"a":1}`))
	if k1 == k2 || k1 == k3 {
		t.Fatal("payload/step must affect the key")
	}
}

func TestOnceWithoutDB(t *testing.T) {
	g := New(nil)
	ctx := context.Background()
	key := Key("unit-test-once")

	first, err := g.Once(ctx, 0, key)
	if err != nil || !first {
		t.Fatalf("first call: first=%v err=%v", first, err)
	}
	second, err := g.Once(ctx, 0, key)
	if err != nil || second {
		t.Fatalf("second call must be deduped: second=%v err=%v", second, err)
	}
	if err := g.Release(ctx, key); err != nil {
		t.Fatal(err)
	}
	again, err := g.Once(ctx, 0, key)
	if err != nil || !again {
		t.Fatalf("after release must be first again: again=%v err=%v", again, err)
	}
}

func TestEmptyKeyRejected(t *testing.T) {
	if _, err := New(nil).Once(context.Background(), 0, ""); err == nil {
		t.Fatal("empty key must be rejected")
	}
}
