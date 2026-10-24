package server

import (
	"strings"
	"testing"

	"github.com/Autumn-27/artex/internal/impact"
)

// 委托方一页纸：必须四件事齐全（能进哪/拿到什么/影响多大/还差什么）+ 怎么修，
// 且不能出现没证据的吹牛（没读到数据就不许说"能拖库"）。
func TestOnePagerWithData(t *testing.T) {
	task := &Task{ID: "14", Name: "验收站"}
	rows := []lootRow{
		{Key: "data", Label: "数据拉取（用户/订单/卡密）", Status: "done",
			Objects: []lootObject{{Name: "orders", Fields: "id, amount, user_id"}}},
	}
	fs := []lootFinding{{id: 1, title: "SQL 注入", text: "SQL Injection 报错注入", summary: "q 参数"}}
	p := buildOnePager(task, impact.Stats{Findings: 1, DataSamples: 1}, rows, fs, nil)

	if len(p.Access) == 0 && p.Level == "" {
		t.Fatal("等级不能为空")
	}
	if len(p.DataObjects) != 1 || p.DataObjects[0].Name != "orders" {
		t.Fatalf("应带上表名：%+v", p.DataObjects)
	}
	if !strings.Contains(p.Impact, "业务数据") {
		t.Fatalf("影响描述应提到业务数据：%q", p.Impact)
	}
	for _, want := range []string{"【渗透测试结论（一页纸）】", "1. 能进哪", "2. 拿到什么数据", "3. 影响多大", "4. 还差什么", "5. 怎么修"} {
		if !strings.Contains(p.Text, want) {
			t.Fatalf("一页纸缺段落 %q：\n%s", want, p.Text)
		}
	}
	if len(p.Fixes) == 0 || !strings.Contains(p.Text, "参数化") {
		t.Fatalf("应按漏洞类型给修复建议：%+v", p.Fixes)
	}
}

// 什么都没拿到时：不许说"能"，且要明确"还没有读到数据"。
func TestOnePagerEmpty(t *testing.T) {
	p := buildOnePager(&Task{ID: "17"}, impact.Stats{Findings: 1}, nil,
		[]lootFinding{{id: 1, title: "未授权接口泄露", text: "未授权 API", summary: "status"}},
		[]blocker{{Class: "target_blocked", Count: 3, Advice: "换手法"}})
	if !strings.Contains(p.Impact, "只有漏洞") {
		t.Fatalf("没有能力时影响描述应说明尚未可利用：%q", p.Impact)
	}
	if len(p.Access) != 0 {
		t.Fatalf("没拿到访问权就不许写能进：%+v", p.Access)
	}
	if !strings.Contains(strings.Join(p.Gaps, "/"), "数据") {
		t.Fatalf("缺口应包含数据：%+v", p.Gaps)
	}
	if !strings.Contains(p.Text, "还没有拿到任何访问权") {
		t.Fatalf("一页纸要明说不许含糊：\n%s", p.Text)
	}
}
