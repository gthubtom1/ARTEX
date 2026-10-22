package adviser

import (
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/internal/stall"
)

type fakeSink struct {
	hints  []string
	steers map[int64]string
	kills  []int64
}

func (f *fakeSink) Hint(text string, _ []int64) (int64, error) {
	f.hints = append(f.hints, text)
	return int64(len(f.hints)), nil
}
func (f *fakeSink) Steer(id int64, msg string) error {
	if f.steers == nil {
		f.steers = map[int64]string{}
	}
	f.steers[id] = msg
	return nil
}
func (f *fakeSink) Kill(id int64) error { f.kills = append(f.kills, id); return nil }

func stuckReport() stall.Report {
	return stall.Report{
		Rounds: 4, NoProgressRounds: 3, FactVelocity: 0.2, RepeatRatio: 0.6,
		LayerGap: []string{"host", "data"}, FrontOnlyRatio: 1.0,
		BudgetBurnPct: 58, LayerFootprint: map[string]int{"host": 0, "data": 0, "endpoint": 6},
		Stuck:   []string{"no_progress_rounds=3", "repeat_ratio=0.60", "layer_gap=host/data"},
		Running: []stall.WorkerRef{{IntentID: 23, Summary: "/api/user?id= 越权", Minutes: 12, Writes: 0}},
	}
}

func TestDiagnoseProducesActionableHints(t *testing.T) {
	led := Ledger{
		Goal: "拿下 example.com 管理员权限", Rounds: 4, IntentCount: 11,
		Elapsed: 42 * time.Minute, Budget: 72 * time.Minute, NewFacts: 6, Findings: 0,
		Directions: []Direction{
			{IntentID: 21, Summary: "/login.php 弱口令枚举", Facts: 0, Attempts: 3},
			{IntentID: 22, Summary: "/login.php 参数 sql 注入（时间盲注）", Facts: 0, Attempts: 2},
		},
	}
	neg := NegativeSpace{UntestedHosts: []string{"10.10.10.5:22", "10.10.10.5:3306"}}
	d := Diagnose(stuckReport(), led, neg)
	if len(d.Hints) == 0 {
		t.Fatalf("expected hints, got %+v", d)
	}
	if len(d.Hints) > 3 {
		t.Fatalf("hints must be <=3, got %d", len(d.Hints))
	}
	joined := strings.Join(d.Hints, "\n")
	if !strings.Contains(joined, "10.10.10.5:22") {
		t.Fatalf("host-layer hint must name concrete host:port:\n%s", joined)
	}
	if !strings.Contains(joined, "停止") {
		t.Fatalf("stale direction must be called out:\n%s", joined)
	}
	for _, h := range d.Hints {
		if !Actionable(h) {
			t.Fatalf("hint not actionable: %q", h)
		}
	}
	if d.Pivot == nil || d.Pivot.To == "" {
		t.Fatalf("expected pivot, got %+v", d.Pivot)
	}
	if len(d.Kill) == 0 || d.Kill[0] != 23 {
		t.Fatalf("long-running no-write worker should be flagged for kill: %+v", d.Kill)
	}
}

func TestDiagnoseSilentWhenHealthy(t *testing.T) {
	d := Diagnose(stall.Report{}, Ledger{}, NegativeSpace{})
	if len(d.Hints) != 0 {
		t.Fatalf("healthy run must be silent, got %+v", d.Hints)
	}
	if d.Confidence != "low" {
		t.Fatalf("want low confidence, got %s", d.Confidence)
	}
}

func TestApplyWritesAllThreeChannels(t *testing.T) {
	s := &fakeSink{}
	d := Diagnosis{
		Hints: []string{"直连 10.10.10.5:3306 用已收口令试登录；成功标准：登录成功回显"},
		Steer: []Steer{{IntentID: 7, Message: "别继续枚举 id，比对 200/403 响应体差异"}},
		Kill:  []int64{9},
	}
	n, errs := Apply(d, s)
	if n != 1 || len(errs) != 0 {
		t.Fatalf("apply failed: n=%d errs=%v", n, errs)
	}
	if len(s.hints) != 1 || s.steers[7] == "" || len(s.kills) != 1 {
		t.Fatalf("sink not filled: %+v", s)
	}
}

func TestPromptEmbedsDiagnosis(t *testing.T) {
	p := Prompt("拿下目标", stuckReport(), Ledger{Goal: "g"}, NegativeSpace{})
	for _, want := range []string{"资深负责人", "layer_gap", "严格 JSON"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

func TestParseJSONLenient(t *testing.T) {
	raw := "这里是诊断：\n```json\n" + `{
      "diagnosis": "在后端认证面绕了 4 轮",
      "hints": ["直连 10.10.10.5:22 用已收口令试登录", "再深入一点", "换个思路"],
      "steer": [{"intent_id": 23, "message": "别继续枚举 id"}],
      "kill": [27],
      "confidence": "HIGH"
    }` + "\n```\n以上。"
	d, err := ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Hints) != 1 || !strings.Contains(d.Hints[0], "10.10.10.5:22") {
		t.Fatalf("non-actionable hints must be dropped: %+v", d.Hints)
	}
	if d.Confidence != "high" || d.Source != "llm" {
		t.Fatalf("fields: %+v", d)
	}
	if len(d.Kill) != 1 || d.Kill[0] != 27 || len(d.Steer) != 1 {
		t.Fatalf("steer/kill lost: %+v", d)
	}
}

func TestParseJSONRejectsGarbage(t *testing.T) {
	if _, err := ParseJSON("no json here"); err == nil {
		t.Fatal("expected error")
	}
}
