package report

import (
	"fmt"
	"strings"

	"github.com/Autumn-27/artex/internal/capability"
	"github.com/Autumn-27/artex/internal/ledger"
)

// CapabilitySection 渲染「能力卡菜单 + 13 组覆盖度 + 持有台账」这一段。
// 零基础读者看这一段就能回答两个问题：这个站能做什么、打下来的东西还在不在。
func CapabilitySection(cards []capability.Card, negatives []string, owned []ledger.Owned) string {
	var b strings.Builder
	b.WriteString("## 能力菜单（我能做什么）\n\n")
	b.WriteString(capability.RenderPlain(cards, negatives))
	b.WriteString("\n\n")

	b.WriteString("## 覆盖度自检\n\n")
	b.WriteString("| 组 | 名称 | 测过 | 结论 |\n|---|---|---|---|\n")
	for _, row := range capability.Coverage(cards) {
		tested := "否"
		if row.Tested {
			tested = "是"
		}
		fmt.Fprintf(&b, "| %d | %s | %s | %s |\n", row.Group, row.Name, tested, row.Verdict)
	}
	b.WriteString("\n")

	if len(owned) > 0 {
		b.WriteString("## 持有资产台账\n\n")
		b.WriteString("| 资产 | 类型 | 权限 | 跳数 | 状态 | 最近验证 |\n|---|---|---|---|---|---|\n")
		for _, o := range owned {
			host := o.Host
			if host == "" {
				host = o.IP
			}
			if o.Port > 0 {
				host = fmt.Sprintf("%s:%d", host, o.Port)
			}
			verified := "—"
			if !o.LastVerifiedAt.IsZero() {
				verified = o.LastVerifiedAt.Format("2006-01-02 15:04")
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %d | %s | %s |\n",
				host, o.AccessType, o.Privilege, o.Depth, o.Status, verified)
		}
		b.WriteString("\n")
	}
	return b.String()
}
