package decision

import (
	"strings"
	"testing"
)

func TestParseRejectsUnknown(t *testing.T) {
	if _, err := Parse("nuke"); err == nil {
		t.Fatal("unknown action must error")
	}
	for raw, want := range map[string]Action{"evidence": ActionEvidence, "只拿证据": ActionEvidence,
		"continue": ActionContinue, "继续深挖": ActionContinue, "switch": ActionSwitch, "换目标": ActionSwitch} {
		got, err := Parse(raw)
		if err != nil || got != want {
			t.Fatalf("Parse(%q)=%q,%v want %q", raw, got, err, want)
		}
	}
}

func TestDecideEvidenceDefaultsToP2(t *testing.T) {
	v, err := Decide(Item{CapID: 7, Can: "任意订单读取", Surface: "/api/order/detail?id=", AssetIDs: []int64{3}}, ActionEvidence, "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "approved" || v.ProofCap != "P2" {
		t.Fatalf("evidence verdict: %+v", v)
	}
	if !strings.Contains(v.Hint, "/api/order/detail?id=") || !strings.Contains(v.Hint, "≤1 行") {
		t.Fatalf("hint not actionable: %q", v.Hint)
	}
}

func TestDecideContinueHasBudget(t *testing.T) {
	v, err := Decide(Item{CapID: 8, Can: "命令执行", Surface: "/upload.php"}, ActionContinue, "只要证明能跑 id")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "approved" || v.ProofCap != "P3" || v.BudgetMinutes != 15 || v.BudgetRounds != 20 {
		t.Fatalf("continue verdict: %+v", v)
	}
	if !strings.Contains(v.Hint, "只要证明能跑 id") {
		t.Fatalf("note should be carried: %q", v.Hint)
	}
}

func TestDecideSwitchFreezesAssets(t *testing.T) {
	v, err := Decide(Item{CapID: 9, Can: "上传点", Surface: "/up.php", AssetIDs: []int64{11, 12}}, ActionSwitch, "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "declined" || len(v.FreezeAssetIDs) != 2 {
		t.Fatalf("switch verdict: %+v", v)
	}
	if !strings.Contains(v.Hint, "冻结") {
		t.Fatalf("switch hint: %q", v.Hint)
	}
}

func TestDecideRequiresCapID(t *testing.T) {
	if _, err := Decide(Item{}, ActionEvidence, ""); err == nil {
		t.Fatal("cap_id must be required")
	}
}

func TestPendingOnlyOpen(t *testing.T) {
	items := []Item{{CapID: 1, Status: "open"}, {CapID: 2, Status: "approved"}, {CapID: 3}}
	if got := Pending(items); len(got) != 2 || got[0].CapID != 1 || got[1].CapID != 3 {
		t.Fatalf("pending: %+v", got)
	}
}

func TestNormalizeBatchDedupesAndDropsZero(t *testing.T) {
	got, err := NormalizeBatch([]int64{3, 0, 3, -1, 7}, ActionEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 3 || got[1] != 7 {
		t.Fatalf("normalize: %+v", got)
	}
}

func TestNormalizeBatchRejectsEmptyAndOversize(t *testing.T) {
	if _, err := NormalizeBatch(nil, ActionContinue); err == nil {
		t.Fatal("empty ids must error")
	}
	if _, err := NormalizeBatch([]int64{0, -2}, ActionSwitch); err == nil {
		t.Fatal("all-invalid ids must error")
	}
	ids := make([]int64, MaxBatch+1)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	if _, err := NormalizeBatch(ids, ActionEvidence); err == nil {
		t.Fatalf("more than %d ids must error", MaxBatch)
	}
	ok, err := NormalizeBatch(ids[:MaxBatch], ActionEvidence)
	if err != nil || len(ok) != MaxBatch {
		t.Fatalf("exactly %d ids should pass: %d, %v", MaxBatch, len(ok), err)
	}
}

func TestNormalizeBatchRejectsUnknownAction(t *testing.T) {
	if _, err := NormalizeBatch([]int64{1}, Action("nuke")); err == nil {
		t.Fatal("unknown action must error")
	}
}
