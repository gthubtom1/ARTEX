package kb

import (
	"strings"
	"testing"
)

// 真素材测试：直接读仓库里的 kb/tactics（6 篇 428 行）。
func TestLoadPlaybooksRealFiles(t *testing.T) {
	pbs := LoadPlaybooks("../../kb")
	if len(pbs) < 5 {
		t.Fatalf("应至少解析出 5 篇打法，得到 %d", len(pbs))
	}
	var sqli *Playbook
	for i := range pbs {
		if pbs[i].ID == "tactics/sqli" {
			sqli = &pbs[i]
		}
		if pbs[i].Title == "" {
			t.Fatalf("%s 标题为空（frontmatter/H1 解析失败）", pbs[i].ID)
		}
	}
	if sqli == nil {
		t.Fatal("没解析到 tactics/sqli")
	}
	if len(sqli.Criteria) < 3 {
		t.Fatalf("sqli 判据应 ≥3 条，得到 %v", sqli.Criteria)
	}
	if len(sqli.NotHit) < 2 {
		t.Fatalf("sqli 应解析出常见误判，得到 %v", sqli.NotHit)
	}
	if len(sqli.Steps) < 5 {
		t.Fatalf("sqli 检查顺序应 ≥5 步，得到 %v", sqli.Steps)
	}
	if !strings.Contains(strings.Join(sqli.Steps, " "), "基线") {
		t.Fatalf("检查顺序第一步应是基线，得到 %v", sqli.Steps)
	}
}

// P0-02 的核心判据：按形状匹配。
func TestMatchPlaybooksByShape(t *testing.T) {
	pbs := LoadPlaybooks("../../kb")
	cases := []struct {
		shape string
		want  string
	}{
		{"POST /doLogin uid/passw", "tactics/sqli"},
		{"GET /api/order?orderId=1", "tactics/access-control"},
		{"upload filename=.php multipart", "tactics/upload-to-rce"},
		{"SMB 445 内网横向", "tactics/lateral-pivot"},
	}
	for _, c := range cases {
		got := MatchPlaybooks(c.shape, pbs, 2)
		if len(got) == 0 {
			t.Fatalf("%q 没匹配到任何打法", c.shape)
		}
		hit := false
		for _, pb := range got {
			if pb.ID == c.want {
				hit = true
			}
		}
		if !hit {
			ids := []string{}
			for _, pb := range got {
				ids = append(ids, pb.ID)
			}
			t.Fatalf("%q 应包含 %s，得到 %v", c.shape, c.want, ids)
		}
	}
	// 认不出的形状也要给出通用两篇（不能返回空让 worker 干瞪眼）
	got := MatchPlaybooks("随便一个页面", pbs, 2)
	if len(got) != 2 {
		t.Fatalf("未识别形状应返回 2 篇通用打法，得到 %d", len(got))
	}
}

// 上下文纪律：渲染结果必须有硬上限（不许把 kb 变成上下文炸弹）。
func TestPlaybookRenderIsBounded(t *testing.T) {
	pbs := LoadPlaybooks("../../kb")
	for _, pb := range pbs {
		s := pb.Render(8, 4)
		if len(s) > 1800 {
			t.Fatalf("%s 渲染 %d 字节，超过硬限", pb.ID, len(s))
		}
		if !strings.Contains(s, "算命中") && !strings.Contains(s, "检查顺序") {
			t.Fatalf("%s 渲染缺关键段：%q", pb.ID, s[:80])
		}
	}
}
