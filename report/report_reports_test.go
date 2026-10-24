package report

import (
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// TestMarkdownIncludesDetailedReport 锁定 A3：报告 Agent 写的详细正文必须出现在
// 导出报告里。2026-10-24 实测：findings.report 有 9812 字符正文，但报告页只渲染
// name/summary/PoC，用户拿不到可交付的报告。
func TestMarkdownIncludesDetailedReport(t *testing.T) {
	n := &db.Node{ID: 200, Kind: db.KindFinding}
	n.Payload = []byte(`{"name":"反射型 XSS","vulnclass":"xss","severity":"medium","summary":"HostName 参数未转义"}`)

	md := Markdown(Input{
		Title: "t", Goal: "g", GeneratedAt: time.Now(),
		Findings: []*db.Node{n},
		Reports:  map[int64]string{200: "## 详细\npayload=<script>alert(1)</script>"},
	})
	if !strings.Contains(md, "详细报告") {
		t.Fatal("报告里没有“详细报告”小节")
	}
	if !strings.Contains(md, "payload=<script>alert(1)</script>") {
		t.Fatal("报告正文没有带出来")
	}

	// 没有详细正文时保持旧行为（不出现空小节）。
	md2 := Markdown(Input{Title: "t", Goal: "g", GeneratedAt: time.Now(), Findings: []*db.Node{n}})
	if strings.Contains(md2, "详细报告") {
		t.Fatal("没有正文时不该出现“详细报告”小节")
	}
}
