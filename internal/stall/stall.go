// Package stall 是「停顿检测器」：全部是确定性计算（零 token、零 LLM），
// 每次规划轮结束算一次，命中任一信号就把诊断包交给 adviser。
//
// 信号来源见 docs/顾问agent设计.md 第四节。
package stall

import (
	"sort"
	"strings"
	"time"
)

// IntentRef 是最近一条意图的画像（用于重复度/前端集中度）。
type IntentRef struct {
	ID         int64    `json:"id"`
	Summary    string   `json:"summary"`
	AssetIDs   []int64  `json:"asset_ids,omitempty"`
	AssetTypes []string `json:"asset_types,omitempty"`
	Stage      string   `json:"stage,omitempty"`
	Yields     int      `json:"yields,omitempty"`
	Attempts   int      `json:"attempts,omitempty"`
	State      string   `json:"state,omitempty"`
}

// RoundStat 是一轮规划的产出。
type RoundStat struct {
	NewFacts    int `json:"new_facts"`
	NewFindings int `json:"new_findings"`
}

// WorkerRef 是在跑的 worker（诊断包里给顾问看）。
type WorkerRef struct {
	IntentID int64   `json:"intent_id"`
	Summary  string  `json:"summary"`
	Minutes  float64 `json:"minutes"`
	Writes   int     `json:"writes"`
}

// Input 是检测器的全部输入（由 server 从图里聚合）。
type Input struct {
	Rounds        []RoundStat    `json:"rounds"`
	Intents       []IntentRef    `json:"intents"` // 按时间升序
	LayerFacts    map[string]int `json:"layer_facts"`
	AssetsInScope int            `json:"assets_in_scope"`
	AssetsTested  int            `json:"assets_tested"`
	Elapsed       time.Duration  `json:"elapsed"`
	Budget        time.Duration  `json:"budget"`
	Running       []WorkerRef    `json:"running,omitempty"`
}

// Thresholds 可配（settings 里留口子）。
type Thresholds struct {
	NoProgressRounds int     `json:"no_progress_rounds"`
	FactVelocity     float64 `json:"fact_velocity"`
	RepeatRatio      float64 `json:"repeat_ratio"`
	LayerGapCount    int     `json:"layer_gap_count"`
	FrontOnlyRatio   float64 `json:"front_only_ratio"`
	BudgetBurnPct    float64 `json:"budget_burn_pct"`
}

// DefaultThresholds 是出厂阈值（与设计文档一致）。
func DefaultThresholds() Thresholds {
	return Thresholds{
		NoProgressRounds: 3,
		FactVelocity:     0.6,
		RepeatRatio:      0.4,
		LayerGapCount:    2,
		FrontOnlyRatio:   0.9,
		BudgetBurnPct:    35,
	}
}

// Report 是给 adviser 的诊断包（前端也可直接展示）。
type Report struct {
	Rounds           int            `json:"rounds"`
	NoProgressRounds int            `json:"no_progress_rounds"`
	FactVelocity     float64        `json:"fact_velocity"`
	RepeatRatio      float64        `json:"repeat_ratio"`
	LayerGap         []string       `json:"layer_gap"`
	FrontOnlyRatio   float64        `json:"front_only_ratio"`
	UntestedInScope  int            `json:"untested_in_scope"`
	CoveragePct      float64        `json:"coverage_pct"`
	BudgetBurnPct    float64        `json:"budget_burn_pct"`
	LayerFootprint   map[string]int `json:"layer_footprint"`
	Stuck            []string       `json:"stuck"`
	Intents          []IntentRef    `json:"intents,omitempty"`
	Running          []WorkerRef    `json:"running,omitempty"`
}

// Hit 表示命中至少一个唤醒信号。
func (r Report) Hit() bool { return len(r.Stuck) > 0 }

// Detect 计算诊断包。
func Detect(in Input, th Thresholds) Report {
	d := DefaultThresholds()
	if th.NoProgressRounds > 0 {
		d.NoProgressRounds = th.NoProgressRounds
	}
	if th.FactVelocity > 0 {
		d.FactVelocity = th.FactVelocity
	}
	if th.RepeatRatio > 0 {
		d.RepeatRatio = th.RepeatRatio
	}
	if th.LayerGapCount > 0 {
		d.LayerGapCount = th.LayerGapCount
	}
	if th.FrontOnlyRatio > 0 {
		d.FrontOnlyRatio = th.FrontOnlyRatio
	}
	if th.BudgetBurnPct > 0 {
		d.BudgetBurnPct = th.BudgetBurnPct
	}

	rep := Report{Rounds: len(in.Rounds), LayerFootprint: map[string]int{}, Intents: in.Intents, Running: in.Running}

	// 连续无产出轮数（从尾部数）。
	for i := len(in.Rounds) - 1; i >= 0; i-- {
		if in.Rounds[i].NewFacts == 0 && in.Rounds[i].NewFindings == 0 {
			rep.NoProgressRounds++
			continue
		}
		break
	}

	// fact 速度：最近 5 轮新增 fact / 5。
	window := in.Rounds
	if len(window) > 5 {
		window = window[len(window)-5:]
	}
	facts := 0
	for _, r := range window {
		facts += r.NewFacts
	}
	if len(window) > 0 {
		rep.FactVelocity = round2(float64(facts) / float64(len(window)))
	}

	// 重复度：最近 10 条意图两两相似度 > 0.75 的比例。
	recent := in.Intents
	if len(recent) > 10 {
		recent = recent[len(recent)-10:]
	}
	if len(recent) >= 2 {
		pairs, dup := 0, 0
		for i := 0; i < len(recent); i++ {
			for j := i + 1; j < len(recent); j++ {
				pairs++
				if similarity(recent[i].Summary, recent[j].Summary) > 0.75 {
					dup++
				}
			}
		}
		if pairs > 0 {
			rep.RepeatRatio = round2(float64(dup) / float64(pairs))
		}
	}

	// 层级足迹 + 负空间。
	layers := []string{"host", "service", "endpoint", "data", "auth"}
	if len(in.LayerFacts) > 0 {
		layers = layers[:0]
		for k := range in.LayerFacts {
			layers = append(layers, k)
		}
		sort.Strings(layers)
	}
	for _, l := range layers {
		n := in.LayerFacts[l]
		rep.LayerFootprint[l] = n
		if n == 0 {
			rep.LayerGap = append(rep.LayerGap, l)
		}
	}

	// 前端集中度：最近 8 条意图锚定在 endpoint 的比例。
	front := in.Intents
	if len(front) > 8 {
		front = front[len(front)-8:]
	}
	if len(front) > 0 {
		only := 0
		for _, it := range front {
			if anchorIsEndpoint(it) {
				only++
			}
		}
		rep.FrontOnlyRatio = round2(float64(only) / float64(len(front)))
	}

	// 范围内未测资产。
	if in.AssetsInScope > 0 {
		rep.UntestedInScope = in.AssetsInScope - in.AssetsTested
		if rep.UntestedInScope < 0 {
			rep.UntestedInScope = 0
		}
		rep.CoveragePct = round2(float64(in.AssetsTested) / float64(in.AssetsInScope) * 100)
	}
	if in.Budget > 0 {
		rep.BudgetBurnPct = round2(float64(in.Elapsed) / float64(in.Budget) * 100)
	}

	// 命中判定。
	if rep.NoProgressRounds >= d.NoProgressRounds {
		rep.Stuck = append(rep.Stuck, "no_progress_rounds="+itoa(rep.NoProgressRounds))
	}
	if rep.Rounds >= 5 && rep.FactVelocity < d.FactVelocity {
		rep.Stuck = append(rep.Stuck, "fact_velocity="+ftoa(rep.FactVelocity))
	}
	if rep.RepeatRatio >= d.RepeatRatio {
		rep.Stuck = append(rep.Stuck, "repeat_ratio="+ftoa(rep.RepeatRatio))
	}
	if len(rep.LayerGap) >= d.LayerGapCount {
		rep.Stuck = append(rep.Stuck, "layer_gap="+strings.Join(rep.LayerGap, "/"))
	}
	if rep.FrontOnlyRatio >= d.FrontOnlyRatio && len(front) >= 4 {
		rep.Stuck = append(rep.Stuck, "surface_concentration="+ftoa(rep.FrontOnlyRatio))
	}
	if rep.BudgetBurnPct >= d.BudgetBurnPct && rep.NoProgressRounds >= 2 {
		rep.Stuck = append(rep.Stuck, "budget_burn="+ftoa(rep.BudgetBurnPct)+"%")
	}
	return rep
}

func anchorIsEndpoint(it IntentRef) bool {
	if strings.EqualFold(it.Stage, "endpoint") || strings.EqualFold(it.Stage, "recon") || it.Stage == "" {
		// 没标阶段时看资产类型；两者都没有则按"前端参数面"计。
		if len(it.AssetTypes) == 0 {
			return true
		}
	}
	for _, t := range it.AssetTypes {
		if strings.EqualFold(t, "endpoint") {
			return true
		}
	}
	return false
}

func similarity(a, b string) float64 {
	x, y := grams(a), grams(b)
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
	return float64(inter) / float64(union)
}

func grams(s string) map[string]struct{} {
	s = strings.ToLower(strings.Join(strings.Fields(s), ""))
	r := []rune(s)
	out := map[string]struct{}{}
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

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func ftoa(f float64) string {
	n := int64(f*100 + 0.5)
	ip := n / 100
	fp := n % 100
	s := itoa(int(ip)) + "."
	if fp < 10 {
		s += "0"
	}
	return s + itoa(int(fp))
}
