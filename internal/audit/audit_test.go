package audit

import (
	"context"
	"testing"
)

func TestDigestNeverEmptyAndStable(t *testing.T) {
	a := Digest([]byte(`{"cmd":"id"}`))
	b := Digest([]byte(`{"cmd":"id"}`))
	if a == "" || a != b {
		t.Fatalf("digest unstable: %q %q", a, b)
	}
	if len(a) != 32 {
		t.Fatalf("want 32 hex chars (16 bytes), got %d", len(a))
	}
	if Digest(nil) != "" {
		t.Fatal("nil args must produce empty digest")
	}
}

func TestNopLoggerDoesNotPanic(t *testing.T) {
	l := Nop()
	l.Log(context.Background(), Entry{TaskID: 1, Tool: "Bash", Action: "exec"})
	l.LogOK(context.Background(), 1, "worker:1", "nmap", "10.0.0.1", "exec", []byte("nmap -sT"))
	l.LogFail(context.Background(), 1, "worker:1", "nmap", "10.0.0.1", "exec", "boom", nil)
}
