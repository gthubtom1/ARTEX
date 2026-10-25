package server

import (
	"strings"
	"testing"
)

// 进展清单的纪律：没做必须写原因、做完必须写判据，统计要对。
func TestRoadmapBuild(t *testing.T) {
	raw := []byte(`{"version":"9.9.9","groups":[{"id":"g","title":"组","items":[
		{"id":"a","title":"做完的","status":"done","evidence":"真机验证过"},
		{"id":"b","title":"没做的","status":"todo"},
		{"id":"c","title":"等环境的","status":"blocked","why_not":"没 docker"}
	]}]}`)
	out, err := buildRoadmap(raw)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if out.Total != 3 || out.Done != 1 || out.Todo != 2 || out.Blocked != 1 {
		t.Fatalf("统计不对：%+v", out)
	}
	if !strings.Contains(out.Headline, "还剩 2 件") {
		t.Fatalf("headline 应说还剩 2 件，得到 %q", out.Headline)
	}
	if !strings.Contains(out.Groups[0].Items[1].Why, "没写原因") {
		t.Fatalf("todo 没写原因时必须标出来，得到 %q", out.Groups[0].Items[1].Why)
	}
	if out.Groups[0].Items[0].Proof != "真机验证过" {
		t.Fatalf("done 的判据应原样保留，得到 %q", out.Groups[0].Items[0].Proof)
	}
	// 做完但没写判据的，也要标出来
	raw2 := []byte(`{"groups":[{"id":"g","items":[{"id":"x","title":"做完没写判据","status":"done"}]}]}`)
	out2, err := buildRoadmap(raw2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2.Groups[0].Items[0].Proof, "没写判据") {
		t.Fatalf("done 没写判据时必须标出来，得到 %q", out2.Groups[0].Items[0].Proof)
	}
}

// 真清单自检：仓库里这份 roadmap.json 必须能被解析，且每条 todo/blocked 都有原因。
func TestRoadmapRealFileIsHonest(t *testing.T) {
	out, err := buildRoadmap(roadmapJSON)
	if err != nil {
		t.Fatalf("server/roadmap.json 解析失败：%v", err)
	}
	if out.Total < 15 {
		t.Fatalf("清单太短（%d 条），是不是被删了", out.Total)
	}
	for _, g := range out.Groups {
		for _, it := range g.Items {
			if it.Status != "done" && strings.Contains(it.Why, "没写原因") {
				t.Fatalf("%s 没写 why_not", it.ID)
			}
			if it.Status == "done" && strings.Contains(it.Proof, "没写判据") {
				t.Fatalf("%s 没写 evidence", it.ID)
			}
		}
	}
}
