package gatechain

import "testing"

func TestRankMergesSameAnchorSameDirection(t *testing.T) {
	in := []Intent{
		{ID: 1, Summary: "对 /login.php 做布尔盲注验证", AssetIDs: []int64{10}, Stage: "endpoint"},
		{ID: 2, Summary: "对 /login.php 做布尔盲注验证（换 payload）", AssetIDs: []int64{10}, Stage: "endpoint"},
	}
	out := Rank(in, Options{})
	if len(out) != 1 {
		t.Fatalf("want 1 merged intent, got %d: %+v", len(out), out)
	}
	if out[0].ID != 1 {
		t.Fatalf("want representative id=1, got %d", out[0].ID)
	}
}

func TestRankKeepsDifferentAnchors(t *testing.T) {
	in := []Intent{
		{ID: 1, Summary: "对 /login.php 做布尔盲注验证", AssetIDs: []int64{10}},
		{ID: 2, Summary: "对 /login.php 做布尔盲注验证", AssetIDs: []int64{11}},
	}
	out := Rank(in, Options{})
	if len(out) != 2 {
		t.Fatalf("different anchors must not merge, got %d", len(out))
	}
}

func TestRankNegativeSpaceBeatsRepetition(t *testing.T) {
	in := []Intent{
		{ID: 1, Summary: "继续 /login.php 参数注入 payload 变异", AssetIDs: []int64{10}, Stage: "endpoint", Attempts: 4, Yields: 0},
		{ID: 2, Summary: "验证 10.10.10.5:3306 是否可用已收凭据直连", AssetIDs: []int64{20}, Stage: "host", Attempts: 0},
	}
	out := Rank(in, Options{LayerGap: []string{"host", "data"}})
	if len(out) != 2 {
		t.Fatalf("nothing may be dropped here, got %d", len(out))
	}
	if out[0].ID != 2 {
		t.Fatalf("host-layer untested intent should rank first, got %+v", out)
	}
	if out[1].Score >= out[0].Score {
		t.Fatalf("stale direction should score lower: %+v", out)
	}
}

func TestRankKeepsDuplicateWhenItIsFresher(t *testing.T) {
	// 代表分数更低（空转过），新的同方向意图反而更高 → 保留新的。
	in := []Intent{
		{ID: 1, Summary: "/api/user 越权遍历 id", AssetIDs: []int64{7}, Stage: "endpoint", Attempts: 3},
		{ID: 2, Summary: "/api/user 越权遍历 id 比对 200/403", AssetIDs: []int64{7}, Stage: "data"},
	}
	out := Rank(in, Options{LayerGap: []string{"data"}})
	if len(out) != 1 {
		t.Fatalf("want merged single, got %d: %+v", len(out), out)
	}
	if out[0].ID != 2 {
		t.Fatalf("fresher intent should represent the group, got %d", out[0].ID)
	}
}

func TestSimilarity(t *testing.T) {
	if s := similarity("对 /login.php 做布尔盲注", "对 /login.php 做布尔盲注验证"); s < 0.6 {
		t.Fatalf("expected high similarity, got %f", s)
	}
	if s := similarity("端口扫描", "数据库导出"); s > 0.2 {
		t.Fatalf("expected low similarity, got %f", s)
	}
}

func TestStageWeightMonotonic(t *testing.T) {
	if StageWeight("data") <= StageWeight("access") {
		t.Fatal("data should outweigh access")
	}
	if StageWeight("lateral") <= StageWeight("recon") {
		t.Fatal("lateral should outweigh recon")
	}
}

func TestShouldFreeze(t *testing.T) {
	if !ShouldFreeze(4, 0) {
		t.Fatal("4 attempts with no yield must freeze")
	}
	if ShouldFreeze(3, 0) || ShouldFreeze(4, 1) {
		t.Fatal("freeze must require 4 attempts AND zero yield")
	}
	if !SameDirection([]int64{1, 2}, []int64{2, 1}, "对 /login.php 做布尔盲注验证", "对 /login.php 做布尔盲注验证（变体）", 0.72) {
		t.Fatal("same anchors + similar summary should match")
	}
}
