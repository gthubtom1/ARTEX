package relay

import (
	"os"
	"strings"
	"testing"
)

func TestAppendNewestFirst(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, Block(1, "第一条", "done", "继续 A")); err != nil {
		t.Fatal(err)
	}
	if err := Append(dir, Block(2, "第二条", "blocked", "改打 B")); err != nil {
		t.Fatal(err)
	}
	got := Read(dir, 10000)
	if !strings.Contains(got, "intent#2") {
		t.Fatalf("newest block missing: %s", got)
	}
	if strings.Index(got, "intent#2") > strings.Index(got, "intent#1") {
		t.Fatalf("newest must come first:\n%s", got)
	}
}

func TestReadMaxBytes(t *testing.T) {
	dir := t.TempDir()
	_ = Append(dir, Block(1, strings.Repeat("A", 500), "done", "next"))
	short := Read(dir, 80)
	if len(short) > 80 {
		t.Fatalf("len=%d", len(short))
	}
	if short == "" {
		t.Fatal("must not be empty")
	}
}

func TestReadMissingFile(t *testing.T) {
	if got := Read(t.TempDir(), 100); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestTrimToMaxSize(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 200; i++ {
		_ = Append(dir, Block(int64(i), strings.Repeat("X", 500), "done", "next"))
	}
	st, err := os.Stat(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() > maxFileSize+64 {
		t.Fatalf("file too big: %d", st.Size())
	}
}

func TestReset(t *testing.T) {
	dir := t.TempDir()
	_ = Append(dir, Block(1, "x", "done", "y"))
	if err := Reset(dir); err != nil {
		t.Fatal(err)
	}
	if got := Read(dir, 100); got != "" {
		t.Fatalf("reset failed: %q", got)
	}
	if err := Reset(dir); err != nil {
		t.Fatalf("reset on missing file must be nil, got %v", err)
	}
}

func TestBlockDefaultNext(t *testing.T) {
	b := Block(7, "s", "exhausted", "")
	if !strings.Contains(b, "下一步") {
		t.Fatalf("missing next line: %s", b)
	}
}
