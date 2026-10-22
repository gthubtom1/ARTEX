// Package gatechain 是"意图闸门"：在 planner 产出一批探索方向之后、进入
// frontier 之前，做**去重 + 优先级排序**。
//
// 设计纪律（docs/最终方案-v1.md ADR A5/A6）：
//   - **绝不拒绝**：任何意图线索都会被保留；重复的合并到一条"代表意图"，
//     返回结果里用 Merged 标注合并了几条（不产生错误文案，也不丢线索）。
//   - 排序是**正向加权**：把"还没碰过的层/资产"抬上去，把"同一锚点同一手法
//     连续空转"降下去，而不是写"禁止"。
//   - 全部是纯函数，便于单测；不依赖 DB / LLM。
package gatechain

import (
	"math"
	"sort"
	"strings"
)

// Intent 是待排序的一条探索方向（只取排序需要的字段）。
type Intent struct {
	ID         int64    `json:"id"`
	Summary    string   `json:"summary"`
	AssetIDs   []int64  `json:"asset_ids,omitempty"`
	AssetTypes []string `json:"asset_types,omitempty"` // 锚定资产的类型：endpoint/service/host...
	Stage      string   `json:"stage,omitempty"`       // recon|access|foothold|privesc|lateral|data
	Attempts   int      `json:"attempts,omitempty"`    // 同方向已尝试次数
	Yields     int      `json:"yields,omitempty"`      // 同方向已产出的 fact+finding 数
	PriorityIn int      `json:"priority_in,omitempty"` // planner 给的原始优先级（越小越先，兼容旧口径）
}

// Options 控制阈值与权重。
type Options struct {
	// MergeThreshold 是两条意图判定为"同一方向"的文本相似度阈值。
	// 0 → 默认 0.72（对"一句话摘要"足够严，避免把不同方向并掉）。
	MergeThreshold float64
	// LayerGap 是本任务尚未产出 fact 的层（host/service/endpoint/data/auth）。
	LayerGap []string
	// HintKeywords 是 hints 里出现的关键词（资产/端口/接口），命中者加权。
	HintKeywords []string
	// UntestedAssets 是范围内还没碰过的资产 key（host:port / url）。
	UntestedAssets []string
}

// Scored 是一条排序结果（代表意图）。
type Scored struct {
	Intent
	Score   float64  `json:"score"`
	Merged  int      `json:"merged,omitempty"` // 被合并进来的同方向意图条数
	Reasons []string `json:"reasons,omitempty"`
}

// StageWeight 把阶段映射成权重增量：越"往下打"的方向在没做过时越值钱。
func StageWeight(stage string) float64 {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "data":
		return 1.6
	case "lateral":
		return 1.4
	case "privesc":
		return 1.3
	case "foothold":
		return 1.0
	case "access":
		return 0.6
	default: // recon / 未标注
		return 0
	}
}

// stageLayer 把阶段映射到能力层级，用于比对 LayerGap。
func stageLayer(stage string) string {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "data":
		return "data"
	case "lateral", "foothold":
		return "host"
	case "privesc":
		return "service"
	case "access":
		return "auth"
	default:
		return "endpoint"
	}
}

type rankGroup struct {
	rep     int
	members []int
}

// Rank 对一批意图做去重与排序：顺序即建议下发顺序。
// 返回的切片可能短于输入——重复项合并到代表意图（合并 ≠ 拒绝：线索仍在
// 代表意图上，Merged 记录并了几条），调用方按返回顺序下发即可。
func Rank(in []Intent, opt Options) []Scored {
	if opt.MergeThreshold <= 0 {
		opt.MergeThreshold = 0.72
	}
	scores := make([]Scored, len(in))
	for i, it := range in {
		scores[i] = Scored{Intent: it}
		scores[i].Score, scores[i].Reasons = scoreOf(it, opt)
	}

	var groups []*rankGroup
	for i := range in {
		gi := -1
		for k, g := range groups {
			for _, m := range g.members {
				if isSameDirection(in[i], in[m], opt) {
					gi = k
					break
				}
			}
			if gi >= 0 {
				break
			}
		}
		if gi < 0 {
			groups = append(groups, &rankGroup{rep: i, members: []int{i}})
			continue
		}
		g := groups[gi]
		g.members = append(g.members, i)
		if scores[i].Score > scores[g.rep].Score {
			g.rep = i // 更新鲜 / 更高分的作为代表
		}
	}

	out := make([]Scored, 0, len(groups))
	for _, g := range groups {
		rep := scores[g.rep]
		if n := len(g.members) - 1; n > 0 {
			rep.Merged = n
			rep.Reasons = append(rep.Reasons, "合并 "+itoa(int64(n))+" 条同锚点同方向意图")
		}
		out = append(out, rep)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if math.Abs(out[a].Score-out[b].Score) > 1e-9 {
			return out[a].Score > out[b].Score
		}
		return out[a].ID < out[b].ID
	})
	return out
}

// isSameDirection：同锚点集合 + 文本足够相似 → 同一方向。
func isSameDirection(a, b Intent, opt Options) bool {
	ida, idb := normalizeIDs(a.AssetIDs), normalizeIDs(b.AssetIDs)
	if len(ida) == 0 || len(idb) == 0 || !sameIDs(ida, idb) {
		return false
	}
	return similarity(a.Summary, b.Summary) >= opt.MergeThreshold
}

func scoreOf(it Intent, opt Options) (float64, []string) {
	var reasons []string
	score := 5.0

	// planner 原始优先级（旧口径：1 最高）→ 小幅加成，不做主导。
	if it.PriorityIn > 0 && it.PriorityIn < 5 {
		score += float64(5-it.PriorityIn) * 0.15
	}

	// 阶段加权：往下打的方向默认更值钱。
	if w := StageWeight(it.Stage); w > 0 {
		score += w
		reasons = append(reasons, "阶段="+it.Stage+" +"+ftoa(w))
	}

	// 负空间加权：尚未产出 fact 的层，本轮优先去碰。
	layer := stageLayer(it.Stage)
	for _, g := range opt.LayerGap {
		if strings.EqualFold(strings.TrimSpace(g), layer) {
			score += 1.2
			reasons = append(reasons, "该层("+layer+")还没有 fact +1.2")
			break
		}
	}

	// 未测资产加权。
	for _, key := range opt.UntestedAssets {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if strings.Contains(strings.ToLower(it.Summary), strings.ToLower(strings.TrimSpace(key))) {
			score += 1.0
			reasons = append(reasons, "覆盖未测资产 "+key+" +1.0")
			break
		}
	}

	// hint 命中：人/顾问说的方向要真的先做。
	for _, kw := range opt.HintKeywords {
		kw = strings.TrimSpace(kw)
		if len([]rune(kw)) < 3 {
			continue
		}
		if strings.Contains(strings.ToLower(it.Summary), strings.ToLower(kw)) {
			score += 0.8
			reasons = append(reasons, "命中 hint「"+kw+"」+0.8")
			break
		}
	}

	// ROI 惩罚：同一方向已经空转过 → 降权（不拒绝）。
	if it.Attempts >= 2 && it.Yields == 0 {
		pen := 1.2 + 0.4*float64(it.Attempts-2)
		if pen > 3.2 {
			pen = 3.2
		}
		score -= pen
		reasons = append(reasons, "同方向 "+itoa(int64(it.Attempts))+" 次无产出 -"+ftoa(pen))
	} else if it.Yields > 0 {
		bonus := math.Min(1.0, 0.3*float64(it.Yields))
		score += bonus
		reasons = append(reasons, "已有产出 +"+ftoa(bonus))
	}
	return math.Round(score*100) / 100, reasons
}

// similarity 用字符 3-gram 相似度（中文也适用；不引 embedding）。
//
// 同时看 Jaccard（整体重合）与"包含度"（短句是否被长句包含）：
// 后者专治"同一方向换了个说法 / 多加了半句解释"的重复，实际渗透摘要里
// 这种占绝大多数。包含度只在短句够长（≥8 gram）时生效，避免短词误并。
func similarity(a, b string) float64 {
	x := grams(a)
	y := grams(b)
	if len(x) == 0 || len(y) == 0 {
		return 0
	}
	inter := 0
	for g := range x {
		if _, ok := y[g]; ok {
			inter++
		}
	}
	union := len(x) + len(y) - inter
	if union == 0 {
		return 0
	}
	jaccard := float64(inter) / float64(union)
	minLen := len(x)
	if len(y) < minLen {
		minLen = len(y)
	}
	if minLen >= 8 {
		containment := float64(inter) / float64(minLen)
		if containment >= 0.85 && containment*0.92 > jaccard {
			return containment * 0.92
		}
	}
	return jaccard
}

func grams(s string) map[string]struct{} {
	s = strings.ToLower(strings.Join(strings.Fields(s), ""))
	r := []rune(s)
	out := make(map[string]struct{}, len(r))
	if len(r) < 3 {
		if len(r) > 0 {
			out[string(r)] = struct{}{}
		}
		return out
	}
	for i := 0; i+3 <= len(r); i++ {
		out[string(r[i:i+3])] = struct{}{}
	}
	return out
}

func normalizeIDs(ids []int64) []int64 {
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok || id == 0 {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func ftoa(f float64) string {
	neg := f < 0
	if neg {
		f = -f
	}
	n := int64(f*10 + 0.5)
	s := itoa(n/10) + "." + itoa(n%10)
	if neg {
		return "-" + s
	}
	return s
}

// Similar 暴露文本相似度（供调用方做方向冻结判定）。
func Similar(a, b string) float64 { return similarity(a, b) }

// SameDirection 判断两个方向是否同锚点、同手法（与 Rank 的合并口径一致）。
func SameDirection(aAnchors, bAnchors []int64, aSummary, bSummary string, threshold float64) bool {
	if threshold <= 0 {
		threshold = 0.72
	}
	ida, idb := normalizeIDs(aAnchors), normalizeIDs(bAnchors)
	if len(ida) == 0 || len(idb) == 0 || !sameIDs(ida, idb) {
		return false
	}
	return similarity(aSummary, bSummary) >= threshold
}

// ShouldFreeze 是"减法"的硬判定：同方向已尝试 attempts 次且 0 产出 → 冻结。
func ShouldFreeze(attempts, yields int) bool { return attempts >= 4 && yields == 0 }
