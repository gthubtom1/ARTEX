package recheck

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/internal/ledger"
)

type fakeExec struct {
	outputs map[string]string
	errs    map[string]error
	ran     []string
}

func (f *fakeExec) Run(_ context.Context, cmd string, _ time.Duration) (string, error) {
	f.ran = append(f.ran, cmd)
	if err, ok := f.errs[cmd]; ok {
		return f.outputs[cmd], err
	}
	return f.outputs[cmd], nil
}

func seedLedger(t *testing.T) (*ledger.Store, int64, int64) {
	t.Helper()
	s := ledger.NewStore(nil, 1, 7)
	ctx := context.Background()
	sshID, _, err := s.Own(ctx, ledger.Owned{Host: "10.0.0.5", Port: 22, AccessType: "ssh", FirstTaskID: 42})
	if err != nil {
		t.Fatal(err)
	}
	webID, _, err := s.Own(ctx, ledger.Owned{Host: "app.example.com", AccessType: "admin", FirstTaskID: 42})
	if err != nil {
		t.Fatal(err)
	}
	return s, sshID, webID
}

func TestRunOnceChecksDueAssets(t *testing.T) {
	s, sshID, webID := seedLedger(t)
	ex := &fakeExec{outputs: map[string]string{
		"nc -z -w5 10.0.0.5 22": "succeeded",
		"curl -s -o /dev/null -w '%{http_code}' -m 10 http://app.example.com": "200",
	}}
	var events []Event
	r := &Runner{Ledger: s, Exec: ex, Now: func() time.Time { return time.Now().Add(2 * time.Hour) },
		OnEvent: func(e Event) { events = append(events, e) }}
	res, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Checked != 2 || res.OK != 2 || res.Failed != 0 || res.Skipped != 0 {
		t.Fatalf("result: %+v", res)
	}
	if len(events) != 2 || events[0].TaskID != 42 {
		t.Fatalf("events: %+v", events)
	}
	for _, id := range []int64{sshID, webID} {
		o, _ := s.Get(context.Background(), id)
		if o.Status != "verified" {
			t.Fatalf("owned %d should be verified, got %s", id, o.Status)
		}
	}
}

func TestRunOnceMarksStaleOnFailure(t *testing.T) {
	s, sshID, _ := seedLedger(t)
	cmd := "nc -z -w5 10.0.0.5 22"
	ex := &fakeExec{outputs: map[string]string{cmd: "connection refused"},
		errs: map[string]error{cmd: errors.New("exit status 1")}}
	// 只让 ssh 到期，避免另一条的 curl 也被执行。
	_ = s.SetStatus(context.Background(), sshID+1, "cleaned", "test")
	r := &Runner{Ledger: s, Exec: ex, Now: func() time.Time { return time.Now().Add(2 * time.Hour) }}
	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	o, _ := s.Get(context.Background(), sshID)
	if o.Status != "stale" {
		t.Fatalf("failed check should mark stale, got %+v", o)
	}
}

func TestRunOnceSkipsManualChecks(t *testing.T) {
	s := ledger.NewStore(nil, 1, 7)
	ctx := context.Background()
	id, _, _ := s.Own(ctx, ledger.Owned{Host: "10.0.0.9", AccessType: "shell"}) // 无端口
	var events []Event
	r := &Runner{Ledger: s, Exec: &fakeExec{}, Now: func() time.Time { return time.Now().Add(2 * time.Hour) },
		OnEvent: func(e Event) { events = append(events, e) }}
	res, err := r.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 || res.Checked != 0 {
		t.Fatalf("manual check should be skipped: %+v", res)
	}
	if len(events) != 1 || !strings.Contains(events[0].Detail, "人工复查") {
		t.Fatalf("event detail: %+v", events)
	}
	o, _ := s.Get(ctx, id)
	if o.Status != "owned" {
		t.Fatalf("skipped check must not change status, got %s", o.Status)
	}
}

func TestRunOnceHTTPStatusCode(t *testing.T) {
	s := ledger.NewStore(nil, 1, 7)
	ctx := context.Background()
	id, _, _ := s.Own(ctx, ledger.Owned{Host: "app.example.com", AccessType: "webshell"})
	ex := &fakeExec{outputs: map[string]string{
		"curl -s -o /dev/null -w '%{http_code}' -m 10 http://app.example.com": "503",
	}}
	r := &Runner{Ledger: s, Exec: ex, Now: func() time.Time { return time.Now().Add(2 * time.Hour) }}
	if _, err := r.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	o, _ := s.Get(ctx, id)
	if o.Status != "stale" {
		t.Fatalf("5xx should be stale, got %s", o.Status)
	}
}

func TestRunOnceBatchLimit(t *testing.T) {
	s := ledger.NewStore(nil, 1, 7)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _, _ = s.Own(ctx, ledger.Owned{Host: "10.0.1." + string(rune('1'+i)), Port: 22, AccessType: "ssh"})
	}
	ex := &fakeExec{outputs: map[string]string{}}
	r := &Runner{Ledger: s, Exec: ex, MaxBatch: 2, Now: func() time.Time { return time.Now().Add(2 * time.Hour) }}
	res, err := r.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Checked != 2 {
		t.Fatalf("batch limit not applied: %+v", res)
	}
}
