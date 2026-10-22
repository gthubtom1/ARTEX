package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveModifyRestore(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(filepath.Join(dataDir, "snapshots"))
	snap, err := s.Create(7, "before-write", []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Bytes != int64(len("original")) {
		t.Fatalf("bytes=%d", snap.Bytes)
	}

	if err := os.WriteFile(target, []byte("corrupted!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(snap.ID); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("restore produced %q", string(got))
	}
}

func TestMissingFileRecorded(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "snap"))
	missing := filepath.Join(t.TempDir(), "nope.txt")
	snap, err := s.Create(1, "before-delete", []string{missing})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Files) != 1 || !snap.Files[0].Missing {
		t.Fatalf("missing file must be recorded: %+v", snap.Files)
	}
	// Restore 对 Missing 项应为 no-op，不报错
	if err := s.Restore(snap.ID); err != nil {
		t.Fatalf("restore with missing entry: %v", err)
	}
}

func TestListAndPrune(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "snap"))
	f := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Create(9, "r", []string{f}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.List(9)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 snapshots, got %d", len(all))
	}
	removed, err := s.Prune(1)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("want 2 removed, got %d", removed)
	}
	left, _ := s.List(9)
	if len(left) != 1 {
		t.Fatalf("want 1 left, got %d", len(left))
	}
}

func TestBadIDRejected(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.Get("../etc/passwd"); err == nil {
		t.Fatal("path traversal id must be rejected")
	}
}
