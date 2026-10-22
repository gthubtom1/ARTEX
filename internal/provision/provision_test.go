package provision

import (
	"strings"
	"testing"
)

func TestPlanPrefersAPT(t *testing.T) {
	steps := Plan([]string{"sqlmap", "nmap"})
	if len(steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(steps))
	}
	if steps[0].Tool != "nmap" || steps[0].Method != MethodAPT {
		t.Fatalf("want nmap via apt first (sorted), got %+v", steps[0])
	}
	if !strings.Contains(steps[0].Command, "nmap") {
		t.Fatalf("command=%q", steps[0].Command)
	}
	if !strings.Contains(steps[0].Note, "自检") {
		t.Fatalf("verify note missing: %q", steps[0].Note)
	}
}

func TestPlanGoAndRelease(t *testing.T) {
	steps := Plan([]string{"nuclei", "chisel"})
	byTool := map[string]Step{}
	for _, s := range steps {
		byTool[s.Tool] = s
	}
	if byTool["nuclei"].Method != MethodGo {
		t.Fatalf("nuclei method=%s", byTool["nuclei"].Method)
	}
	if byTool["chisel"].Method != MethodRelease || !strings.Contains(byTool["chisel"].Command, "jpillora/chisel") {
		t.Fatalf("chisel step=%+v", byTool["chisel"])
	}
}

func TestPlanUnknownIsManual(t *testing.T) {
	steps := Plan([]string{"some-unknown-tool"})
	if len(steps) != 1 || steps[0].Method != MethodManual {
		t.Fatalf("want manual, got %+v", steps)
	}
	if steps[0].Note == "" {
		t.Fatal("manual step must carry a note")
	}
}

func TestPlanDedupAndSort(t *testing.T) {
	steps := Plan([]string{"ffuf", "ffuf", " ", "hydra"})
	if len(steps) != 2 {
		t.Fatalf("want dedup to 2, got %d", len(steps))
	}
	if steps[0].Tool != "ffuf" || steps[1].Tool != "hydra" {
		t.Fatalf("not sorted: %+v", steps)
	}
}

func TestFallbackAndRisk(t *testing.T) {
	if fb := Fallback("sqlmap"); len(fb) == 0 {
		t.Fatal("sqlmap must have a fallback")
	}
	if fb := Fallback("nope"); fb != nil {
		t.Fatalf("unknown tool fallback=%v", fb)
	}
	if r := HighRisk([]string{"sqlmap"}); len(r) != 0 {
		t.Fatalf("sqlmap is medium, got high=%v", r)
	}
}
