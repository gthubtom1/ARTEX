package server

import (
	"encoding/json"
	"testing"

	"github.com/Autumn-27/artex/db"
)

func findingNode(payload map[string]any) *db.Node {
	b, _ := json.Marshal(payload)
	return &db.Node{ID: 11, Kind: db.KindFinding, Payload: b}
}

func TestFindingsForVerifierExtractsFields(t *testing.T) {
	nodes := []*db.Node{
		findingNode(map[string]any{"name": "订单越权", "severity": "high", "surface": "/api/order?id=",
			"evidence": "GET /api/order?id=1002 → 200 返回他人手机号"}),
		findingNode(map[string]any{"summary": "SQL 注入", "severity": "critical",
			"evidence": map[string]any{"poc": "sqlmap 输出：back-end DBMS: MySQL"}}),
	}
	got := findingsForVerifier(nodes)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if got[0].Title != "订单越权" || got[0].Surface != "/api/order?id=" || got[0].Evidence == "" {
		t.Fatalf("finding 1: %+v", got[0])
	}
	if got[1].Title != "SQL 注入" || got[1].Evidence != "sqlmap 输出：back-end DBMS: MySQL" {
		t.Fatalf("finding 2 evidence map not handled: %+v", got[1])
	}
}

func TestVerifierGateCooldown(t *testing.T) {
	g := &verifierGate{}
	if !g.allow("t1", 0) {
		t.Fatal("first call should pass")
	}
	if g.allow("t1", 1e9) {
		t.Fatal("second call inside cooldown should be blocked")
	}
	if !g.allow("t2", 1e9) {
		t.Fatal("different task should pass")
	}
}

func TestDowngradeLevel(t *testing.T) {
	cases := []struct{ current, verdict, want string }{
		{"P4", "rejected", "P1"},
		{"P2", "rejected", "P1"},
		{"", "rejected", "P1"},
		{"P4", "inferred", "P2"},
		{"P3", "inferred", "P2"},
		{"D4", "inferred", "P2"},
		{"D3", "inferred", "P2"},
		{"P2", "inferred", "P2"},
		{"P1", "inferred", "P1"},
		{"D2", "inferred", "D2"},
		{"", "inferred", "P2"},
		{"P4", "verified", "P4"},
		{"p3", "REJECTED", "P1"},
	}
	for _, c := range cases {
		if got := downgradeLevel(c.current, c.verdict); got != c.want {
			t.Fatalf("downgradeLevel(%q,%q)=%q want %q", c.current, c.verdict, got, c.want)
		}
	}
}
