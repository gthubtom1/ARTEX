package stall

import (
	"testing"
	"time"
)

func baseInput() Input {
	return Input{
		Rounds: []RoundStat{
			{NewFacts: 3, NewFindings: 0},
			{NewFacts: 1, NewFindings: 0},
			{NewFacts: 0, NewFindings: 0},
			{NewFacts: 0, NewFindings: 0},
			{NewFacts: 0, NewFindings: 0},
		},
		Intents: []IntentRef{
			{ID: 1, Summary: "对 /login.php 做布尔盲注（payload A）", Stage: "endpoint"},
			{ID: 2, Summary: "对 /login.php 做布尔盲注（payload B）", Stage: "endpoint"},
			{ID: 3, Summary: "对 /login.php 做布尔盲注（payload C）", Stage: "endpoint"},
			{ID: 4, Summary: "对 /login.php 做布尔盲注（payload D）", Stage: "endpoint"},
			{ID: 5, Summary: "对 /login.php 做布尔盲注（payload E）", Stage: "endpoint"},
		},
		LayerFacts:    map[string]int{"host": 0, "service": 1, "endpoint": 4, "data": 0, "auth": 0},
		AssetsInScope: 10,
		AssetsTested:  3,
		Elapsed:       40 * time.Minute,
		Budget:        100 * time.Minute,
	}
}

func TestDetectHitsAllSignals(t *testing.T) {
	rep := Detect(baseInput(), Thresholds{})
	if rep.NoProgressRounds != 3 {
		t.Fatalf("want 3 no-progress rounds, got %d", rep.NoProgressRounds)
	}
	if len(rep.LayerGap) != 3 {
		t.Fatalf("want 3 layer gaps, got %v", rep.LayerGap)
	}
	if rep.UntestedInScope != 7 {
		t.Fatalf("want 7 untested assets, got %d", rep.UntestedInScope)
	}
	if !rep.Hit() {
		t.Fatalf("expected signals, got none: %+v", rep)
	}
	has := func(sig string) bool {
		for _, s := range rep.Stuck {
			if len(s) >= len(sig) && s[:len(sig)] == sig {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"no_progress_rounds", "repeat_ratio", "layer_gap", "surface_concentration"} {
		if !has(want) {
			t.Fatalf("missing signal %q in %v", want, rep.Stuck)
		}
	}
}

func TestDetectHealthyIsQuiet(t *testing.T) {
	in := Input{
		Rounds:     []RoundStat{{NewFacts: 4}, {NewFacts: 3}, {NewFacts: 5}},
		LayerFacts: map[string]int{"host": 1, "service": 2, "endpoint": 3, "data": 2, "auth": 1},
		Intents: []IntentRef{
			{ID: 1, Summary: "子域枚举", Stage: "recon"},
			{ID: 2, Summary: "主机层 3306 直连", Stage: "host"},
			{ID: 3, Summary: "数据层表结构", Stage: "data"},
		},
		AssetsInScope: 5, AssetsTested: 4,
	}
	rep := Detect(in, Thresholds{})
	if rep.Hit() {
		t.Fatalf("healthy run must not trigger: %+v", rep)
	}
}
