package capability

import (
	"strings"
	"testing"
)

func TestTaxonomyLoads13Groups(t *testing.T) {
	tax := Default()
	groups := tax.Groups()
	if len(groups) != 13 {
		t.Fatalf("want 13 groups, got %d: %v", len(groups), groups)
	}
	if len(tax.Items()) < 80 {
		t.Fatalf("want >=80 items, got %d", len(tax.Items()))
	}
	if _, ok := tax.Get("1.1"); !ok {
		t.Fatal("item 1.1 missing")
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]string{
		"订单接口水平越权，换 id 可看别人资料":     "1.2",
		"上传点可解析 php 并执行 id":        "4.3",
		"配置里有 MySQL 明文口令":          "2.1",
		"Redis 未授权访问可读会话":          "1.8",
		"从 www-data sudo 提权到 root": "6.2",
	}
	for text, want := range cases {
		got, ok := Classify(text)
		if !ok || got != want {
			t.Fatalf("Classify(%q)=%q,%v want %q", text, got, ok, want)
		}
	}
}

func TestDeriveDedupesAndKeepsHighestProof(t *testing.T) {
	cards := Derive([]Signal{
		{Item: "1.2", Surface: "/api/order/detail?id=", Evidence: "A 的 token 读到 B 的订单", ProofLevel: "P2", Confidence: "observed"},
		{Item: "1.2", Surface: "/api/order/detail?id=", Evidence: "枚举 3 个 id 均返回他人数据", ProofLevel: "P3", Confidence: "observed"},
		{Item: "4.1", Surface: "/upload.php", Evidence: "命令输出 uid=33", ProofLevel: "P2", Confidence: "observed"},
	})
	if len(cards) != 2 {
		t.Fatalf("want 2 cards after dedupe, got %d: %+v", len(cards), cards)
	}
	if cards[0].Item != "1.2" || cards[0].ProofLevel != "P3" {
		t.Fatalf("want data card first with P3, got %+v", cards[0])
	}
	if !strings.Contains(cards[0].Evidence, "枚举") || !strings.Contains(cards[0].Evidence, "A 的 token") {
		t.Fatalf("evidence should be merged: %q", cards[0].Evidence)
	}
}

func TestDeriveDefaultsToP1(t *testing.T) {
	cards := Derive([]Signal{{Item: "2.1", Surface: "config.php", Evidence: "明文口令"}})
	if cards[0].ProofLevel != "P1" {
		t.Fatalf("missing proof level must default P1, got %s", cards[0].ProofLevel)
	}
	if cards[0].Confidence != "inferred" {
		t.Fatalf("missing confidence must default inferred, got %s", cards[0].Confidence)
	}
}

func TestDeriveUnknownItemIsKept(t *testing.T) {
	cards := Derive([]Signal{{Item: "9.99", Text: "某个未分类能力", Surface: "/x", ProofLevel: "P2"}})
	if len(cards) != 1 || cards[0].Group != 0 {
		t.Fatalf("unknown item must be kept as group 0, got %+v", cards)
	}
}

func TestRenderPlainContainsMenuAndNegatives(t *testing.T) {
	cards := Derive([]Signal{{Item: "1.2", Surface: "/api/order", Evidence: "读到他人订单", ProofLevel: "P2", Confidence: "observed"}})
	out := RenderPlain(cards, []string{"注入：6 种试法无差异"})
	for _, want := range []string{"我能做 1 件事", "【数据】", "/api/order", "做不到什么", "注入"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
}

func TestCoverageHas13Rows(t *testing.T) {
	rows := Coverage(Derive([]Signal{{Item: "1.2", Surface: "/x", Evidence: "e", ProofLevel: "P2"}}))
	if len(rows) != 13 {
		t.Fatalf("want 13 rows, got %d", len(rows))
	}
	if !rows[0].Tested || rows[5].Tested {
		t.Fatalf("coverage marks wrong: %+v", rows)
	}
}

func TestProofRankOrdering(t *testing.T) {
	if ProofRank("P3") <= ProofRank("P1") {
		t.Fatal("P3 must outrank P1")
	}
	if ValidProof("X9") {
		t.Fatal("X9 must be invalid")
	}
}
