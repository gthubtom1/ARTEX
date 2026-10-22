package verifier

import (
	"strings"
	"testing"
)

func TestPromptCarriesEvidenceAndRules(t *testing.T) {
	p := Prompt("拿下后台", Input{FindingID: 7, Title: "订单越权", Severity: "high",
		Surface: "/api/order?id=", Evidence: "GET /api/order?id=1002 → 200 返回他人手机号"})
	for _, want := range []string{"独立复核员", "订单越权", "/api/order?id=", "返回他人手机号", "verified|inferred|rejected"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q:\n%s", want, p)
		}
	}
}

func TestParseJSONFenced(t *testing.T) {
	raw := "复核如下\n```json\n" + `{"verdict":"INFERRED","reason":"只验证了版本匹配","missing":["实际触发一次"]}` + "\n```"
	r, err := ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if r.Verdict != VerdictInferred || r.Source != "llm" || len(r.Missing) != 1 {
		t.Fatalf("review: %+v", r)
	}
}

func TestParseJSONRejectsBadVerdict(t *testing.T) {
	if _, err := ParseJSON(`{"verdict":"probably","reason":"x"}`); err == nil {
		t.Fatal("unknown verdict must error")
	}
	if _, err := ParseJSON("no json"); err == nil {
		t.Fatal("garbage must error")
	}
}

func TestParseJSONDefaultsReason(t *testing.T) {
	r, err := ParseJSON(`{"verdict":"rejected"}`)
	if err != nil || r.Reason == "" {
		t.Fatalf("reason default: %+v %v", r, err)
	}
}
