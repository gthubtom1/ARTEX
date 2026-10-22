package report

import (
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/internal/capability"
	"github.com/Autumn-27/artex/internal/ledger"
)

func TestCapabilitySectionRendersMenuCoverageAndLedger(t *testing.T) {
	cards := []capability.Card{
		{Group: 1, Item: "1.2", Can: "接口数据越权", Plain: "换个 id 就能看到别人的订单", Surface: "/api/order/detail?id=",
			Evidence: "A 的 token 读到 B 的订单", ProofLevel: "P2", Confidence: "observed", Value: "★★★★"},
		{Group: 4, Item: "4.1", Can: "命令执行", Plain: "能在服务器上跑命令", Surface: "/upload.php",
			Evidence: "uid=33(www-data)", ProofLevel: "P2", Confidence: "observed", Value: "★★★★★"},
	}
	owned := []ledger.Owned{
		{Host: "app.example.com", Port: 443, AccessType: "admin", Privilege: "admin", Depth: 0, Status: "verified",
			LastVerifiedAt: time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)},
	}
	out := CapabilitySection(cards, []string{"注入：6 种试法无差异"}, owned)

	for _, want := range []string{"能力菜单", "我能做 2 件事", "接口数据越权", "/api/order/detail?id=",
		"覆盖度自检", "| 1 | 数据 | 是 |", "| 6 | 主机与提权 | 否 |", "持有资产台账", "app.example.com:443", "2026-10-22 10:00"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "做不到什么") {
		t.Fatalf("negatives not rendered:\n%s", out)
	}
}

func TestCapabilitySectionEmpty(t *testing.T) {
	out := CapabilitySection(nil, nil, nil)
	if !strings.Contains(out, "暂时没拿到可确证的能力") {
		t.Fatalf("empty section text wrong:\n%s", out)
	}
	if strings.Contains(out, "持有资产台账") {
		t.Fatal("ledger section should be omitted when empty")
	}
}
