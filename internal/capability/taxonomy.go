// Package capability 是「能力卡」层：把打出来的 fact / finding 翻译成
// 用户能看懂的"这个站我能做什么"，并落到 13 组 91 项的标准清单上。
//
// 设计纪律（docs/最终方案-v1.md ADR A8）：能力卡**派生自 finding/fact**，
// 不独立维护一套真相；同一 (组, 项, 入口) 只保留一张卡，证明级别取最高。
package capability

import (
	"fmt"
	"sort"
	"strings"
)

// Taxon 是清单里的一项（对应 kb/capabilities/taxonomy.md 的一行）。
type Taxon struct {
	Group int    `json:"group"`
	Item  string `json:"item"`
	Can   string `json:"can"`   // 术语
	Plain string `json:"plain"` // 白话
	Proof string `json:"proof"` // 该组要求的证明级别
	Value string `json:"value"` // 星级
	Stage string `json:"stage"` // 阶段/红线标注
}

// ProofLevels 是允许的证明级别（数据类用 D 级，其余用 P 级）。
var ProofLevels = []string{"P1", "P2", "P3", "P4", "D1", "D2", "D3", "D4"}

// ProofRank 把证明级别映射成可比大小（0 = 未知）。
func ProofRank(level string) int {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "P1", "D1":
		return 1
	case "P2", "D2":
		return 2
	case "P3", "D3":
		return 3
	case "P4", "D4":
		return 4
	}
	return 0
}

// ValidProof 判断级别是否合法。
func ValidProof(level string) bool {
	return ProofRank(level) > 0
}

// Taxonomy 是清单容器。
type Taxonomy struct {
	byItem  map[string]Taxon
	byGroup map[int][]Taxon
	order   []Taxon
}

// Default 返回内置清单（13 组 91 项，解析自 kb/capabilities/taxonomy.md）。
func Default() *Taxonomy {
	t := &Taxonomy{byItem: map[string]Taxon{}, byGroup: map[int][]Taxon{}}
	for _, it := range taxonomyData {
		t.byItem[it.Item] = it
		t.byGroup[it.Group] = append(t.byGroup[it.Group], it)
		t.order = append(t.order, it)
	}
	return t
}

// Get 按编号取一项。
func (t *Taxonomy) Get(item string) (Taxon, bool) {
	x, ok := t.byItem[strings.TrimSpace(item)]
	return x, ok
}

// Group 返回某组全部项。
func (t *Taxonomy) Group(g int) []Taxon { return t.byGroup[g] }

// Items 返回全部项。
func (t *Taxonomy) Items() []Taxon { return t.order }

// Groups 返回存在的组号（升序）。
func (t *Taxonomy) Groups() []int {
	out := make([]int, 0, len(t.byGroup))
	for g := range t.byGroup {
		out = append(out, g)
	}
	sort.Ints(out)
	return out
}

// GroupName 返回组的中文名。
func GroupName(g int) string {
	names := map[int]string{
		1: "数据", 2: "身份与凭据", 3: "权限", 4: "代码执行", 5: "文件系统",
		6: "主机与提权", 7: "网络与横向", 8: "持久化与隐蔽", 9: "业务影响",
		10: "云与基础设施", 11: "邮件与通信", 12: "供应链", 13: "可用性",
	}
	if n, ok := names[g]; ok {
		return n
	}
	return fmt.Sprintf("组 %d", g)
}
