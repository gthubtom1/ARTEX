// Package adviser 是「顾问」：拿停顿检测器的诊断包，产出**可执行**的纠偏意见。
//
// 两条硬纪律（docs/顾问agent设计.md §三/§七）：
//  1. 顾问不派意图、不跑命令——只通过 Hint / Steer / Kill 三个出口说话；
//  2. 每条 hint 必须带具体资产/端口/接口/参数，且能说清"做到什么算成功"；
//     没有真问题就沉默（检测器误报时输出 0 条）。
//
// 本包默认是**确定性规则**（零 token、离线可测）；同时提供 Prompt()，
// 需要 LLM 版本时把诊断包交给强模型，输出结构与本包 Diagnosis 一致。
package adviser

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Autumn-27/artex/internal/stall"
)

// Direction 是一条方向的 ROI 记账。
type Direction struct {
	IntentID   int64  `json:"intent_id"`
	Summary    string `json:"summary"`
	Facts      int    `json:"facts"`
	Attempts   int    `json:"attempts"`
	SameAnchor bool   `json:"same_anchor"`
}

// Ledger 是本任务的账本（诊断包的一部分）。
type Ledger struct {
	Goal        string        `json:"goal"`
	Rounds      int           `json:"rounds"`
	IntentCount int           `json:"intent_count"`
	Elapsed     time.Duration `json:"elapsed"`
	Budget      time.Duration `json:"budget"`
	NewFacts    int           `json:"new_facts"`
	Findings    int           `json:"findings"`
	Directions  []Direction   `json:"directions"`
}

// NegativeSpace 是"还没碰过什么"。
type NegativeSpace struct {
	UntestedHosts  []string `json:"untested_hosts"` // 形如 10.10.10.5:22
	UntestedURLs   []string `json:"untested_urls"`
	TestedSurfaces []string `json:"tested_surfaces"`
	Notes          []string `json:"notes,omitempty"`
}

// Pivot 是一次换层建议。
type Pivot struct {
	From string `json:"from"`
	To   string `json:"to"`
	Why  string `json:"why"`
}

// Steer 是对在跑 worker 的实时纠偏。
type Steer struct {
	IntentID int64  `json:"intent_id"`
	Message  string `json:"message"`
}

// Diagnosis 是顾问输出（与 LLM 版 schema 一致）。
type Diagnosis struct {
	Diagnosis  string   `json:"diagnosis"`
	Evidence   []string `json:"evidence,omitempty"`
	Pivot      *Pivot   `json:"pivot,omitempty"`
	Hints      []string `json:"hints"`
	Steer      []Steer  `json:"steer,omitempty"`
	Kill       []int64  `json:"kill,omitempty"`
	Confidence string   `json:"confidence"`       // high|medium|low
	Source     string   `json:"source,omitempty"` // rule|llm
}

// Sink 是顾问唯一允许的三个出口。
type Sink interface {
	Hint(text string, assetIDs []int64) (int64, error)
	Steer(intentID int64, msg string) error
	Kill(intentID int64) error
}

// Apply 把诊断写进图/worker。返回成功写入的 hint 数。
func Apply(d Diagnosis, s Sink) (int, []string) {
	var errs []string
	n := 0
	for _, h := range d.Hints {
		if _, err := s.Hint(h, nil); err != nil {
			errs = append(errs, "hint: "+err.Error())
			continue
		}
		n++
	}
	for _, st := range d.Steer {
		if err := s.Steer(st.IntentID, st.Message); err != nil {
			errs = append(errs, fmt.Sprintf("steer#%d: %v", st.IntentID, err))
		}
	}
	for _, id := range d.Kill {
		if err := s.Kill(id); err != nil {
			errs = append(errs, fmt.Sprintf("kill#%d: %v", id, err))
		}
	}
	return n, errs
}

// Diagnose 是确定性诊断（零 token）。
func Diagnose(rep stall.Report, led Ledger, neg NegativeSpace) Diagnosis {
	d := Diagnosis{Confidence: "low"}
	if !rep.Hit() {
		d.Diagnosis = "检测器未命中，本轮保持现状"
		return d
	}
	d.Confidence = "high"

	var ev []string
	ev = append(ev, "命中："+strings.Join(rep.Stuck, ", "))
	ev = append(ev, fmt.Sprintf("已跑 %d 轮 / %d 条意图", rep.Rounds, led.IntentCount))
	if led.Budget > 0 {
		ev = append(ev, fmt.Sprintf("用时 %s（占预算 %.0f%%），新增 fact %d / finding %d",
			led.Elapsed.Round(time.Minute), rep.BudgetBurnPct, led.NewFacts, led.Findings))
	}
	if len(rep.LayerGap) > 0 {
		ev = append(ev, "层级足迹为 0 的层："+strings.Join(rep.LayerGap, ", "))
	}
	d.Evidence = ev

	gap := pickGap(rep.LayerGap)
	if gap != "" {
		prev := dominantSurface(led.Directions)
		d.Pivot = &Pivot{
			From: prev,
			To:   gapHintTarget(gap, neg),
			Why:  "「" + gap + "」层至今 0 条 fact，所有产出都压在同一个面上",
		}
	}
	d.Diagnosis = summarize(rep, led, gap)

	// 1) 负空间优先：先补没碰过的层（最多 2 条）。
	for _, layer := range rep.LayerGap {
		if len(d.Hints) >= 2 {
			break
		}
		if h := layerHint(layer, neg); h != "" {
			d.Hints = append(d.Hints, h)
		}
	}

	// 2) ROI 止损：同方向连续空转 → 明确"停止 X"，并给替代去处。
	for _, dir := range staleDirections(led.Directions, 2) {
		if len(d.Hints) >= 3 {
			break
		}
		d.Hints = append(d.Hints, fmt.Sprintf(
			"停止「%s」（已 %d 次没有新增 fact）。把这一轮放到：%s；做到「拿到 1 条新的可验证 fact」就算成功。",
			clip(dir.Summary, 60), dir.Attempts, altTarget(rep, neg)))
		for _, w := range rep.Running {
			if w.IntentID == dir.IntentID && w.Writes == 0 {
				d.Steer = append(d.Steer, Steer{IntentID: w.IntentID,
					Message: "别继续枚举同一参数了：改成比对 200 与 403 的响应体差异，或用另一条独立路径复现一次。"})
			}
		}
	}

	// 3) 长跑无写回：建议 kill，把 worker 让给负空间。
	for _, w := range rep.Running {
		if w.Minutes >= 9 && w.Writes == 0 {
			d.Kill = append(d.Kill, w.IntentID)
			if len(d.Hints) < 3 {
				d.Hints = append(d.Hints, fmt.Sprintf(
					"intent#%d（%s）已跑 %.0f 分钟无写回，建议停掉并把 worker 换到：%s",
					w.IntentID, clip(w.Summary, 40), w.Minutes, altTarget(rep, neg)))
			}
		}
	}

	// 4) 一条都产不出来 → 说明检测器可能误报，保持沉默。
	if len(d.Hints) == 0 && len(d.Steer) == 0 && len(d.Kill) == 0 {
		d.Diagnosis = "命中信号但没有可执行的纠正动作（可能是检测器误报），本轮不干扰"
		d.Confidence = "low"
	}
	return d
}

func summarize(rep stall.Report, led Ledger, gap string) string {
	var b strings.Builder
	if gap != "" {
		fmt.Fprintf(&b, "在「%s」面上绕了 %d 轮（连续无产出 %d 轮）", dominantSurface(led.Directions), rep.Rounds, rep.NoProgressRounds)
		if len(rep.LayerGap) > 0 {
			fmt.Fprintf(&b, "，%s 层至今 0 条 fact", strings.Join(rep.LayerGap, "/"))
		}
	} else {
		fmt.Fprintf(&b, "连续 %d 轮没有新 fact", rep.NoProgressRounds)
	}
	if led.Findings == 0 {
		b.WriteString("，还没有可确证的 finding")
	}
	return b.String()
}

func pickGap(gaps []string) string {
	// 优先级：host > data > service > auth > endpoint（越远离前端越优先）。
	order := []string{"host", "data", "service", "auth", "endpoint"}
	for _, o := range order {
		for _, g := range gaps {
			if strings.EqualFold(g, o) {
				return o
			}
		}
	}
	if len(gaps) > 0 {
		return gaps[0]
	}
	return ""
}

func gapHintTarget(layer string, neg NegativeSpace) string {
	switch layer {
	case "host":
		if len(neg.UntestedHosts) > 0 {
			return "主机层（" + strings.Join(firstN(neg.UntestedHosts, 3), "、") + "）"
		}
		return "主机层（22/3306/6379 直连与凭据复用）"
	case "data":
		return "数据层（information_schema → COUNT(*) → 1 行打码样本）"
	case "service":
		return "服务层（开放端口对应的已知漏洞真实触发）"
	case "auth":
		return "认证后接口（登录后 API 面）"
	default:
		return "新攻击面（上传 / SSRF / 参数污染）"
	}
}

func layerHint(layer string, neg NegativeSpace) string {
	switch layer {
	case "host":
		if len(neg.UntestedHosts) > 0 {
			return "直连 " + strings.Join(firstN(neg.UntestedHosts, 3), "、") +
				"，用已收集到的口令试一次登录（ssh/mysql/redis）；成功标准：拿到一条新的服务 banner 或登录成功回显。"
		}
		return "对已确认的主机做一次端口与服务核对（nmap -sV 只扫已开放端口），并用已收口令试 22/3306/6379；成功标准：任意一个服务返回登录成功或版本信息。"
	case "data":
		return "在已确认的数据库/接口上取结构：先 information_schema.tables，再对最大表执行 SELECT COUNT(*)；成功标准：拿到表名+行数（只取 1 行样本并打码）。"
	case "service":
		return "把已开放的端口服务逐个验证版本与已知漏洞（先 -sV 拿 banner，再按版本查 1 个真实可利用点）；成功标准：至少一个服务返回可复现的漏洞响应。"
	case "auth":
		return "用已收凭据登录后台，只验证登录成功与一个列表页可读；成功标准：拿到认证后接口的 1 条响应。"
	case "endpoint":
		return "换维度而不是换 payload：在同一个入口试「认证后接口 / 上传点 / SSRF / 参数污染」四类中的一类；成功标准：新类型请求返回与基线不同的响应。"
	}
	return ""
}

func altTarget(rep stall.Report, neg NegativeSpace) string {
	if len(neg.UntestedHosts) > 0 {
		return "直连 " + neg.UntestedHosts[0] + " 并验证凭据复用"
	}
	if len(neg.UntestedURLs) > 0 {
		return "测 " + neg.UntestedURLs[0] + " 的认证后接口"
	}
	if len(rep.LayerGap) > 0 {
		return gapHintTarget(rep.LayerGap[0], neg)
	}
	return "范围内任一未测资产"
}

func staleDirections(dirs []Direction, threshold int) []Direction {
	var out []Direction
	for _, d := range dirs {
		if d.Attempts >= threshold && d.Facts == 0 && strings.TrimSpace(d.Summary) != "" {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Attempts > out[j].Attempts })
	return out
}

func dominantSurface(dirs []Direction) string {
	if len(dirs) == 0 {
		return "当前入口"
	}
	// 取最近一条的摘要尾部（多数摘要里含路径）。
	last := dirs[len(dirs)-1].Summary
	if strings.TrimSpace(last) == "" {
		return "当前入口"
	}
	return clip(last, 50)
}

// Actionable 判断一条 hint 是否具体到能执行（至少含端口/路径/参数/表名之一）。
func Actionable(h string) bool {
	h = strings.TrimSpace(h)
	if len([]rune(h)) < 16 {
		return false
	}
	markers := []string{":", "/", "参数", "端口", "接口", "表", "服务", "登录"}
	for _, m := range markers {
		if strings.Contains(h, m) {
			return true
		}
	}
	return false
}

// Prompt 生成给强模型的顾问提示词（把诊断包塞进去）。
func Prompt(goal string, rep stall.Report, led Ledger, neg NegativeSpace) string {
	rj, _ := json.MarshalIndent(rep, "", "  ")
	lj, _ := json.MarshalIndent(led, "", "  ")
	nj, _ := json.MarshalIndent(neg, "", "  ")
	return `你是这次渗透任务的"资深负责人"。你不是规划者，也不是执行者——你只做一件事：
在团队跑偏时把它拉回来，并且只说能立刻执行的话。

任务目标：` + goal + `

【诊断包】
` + string(rj) + `

【账本】
` + string(lj) + `

【负空间】
` + string(nj) + `

【三条铁律】
1. 只诊断、只建议：所有动作通过 add_hint / steer_work / kill_work 发出；绝不自己派意图、绝不自己跑命令。
2. 每轮最多 3 条 hint，每条必须带【具体资产/端口/接口/参数】+【为什么】+【做到什么算成功】。禁止"再深入一点""换个思路"这类空话。
3. 没有真问题就沉默（输出 0 条 hint 并说明"误报原因"）。宁可沉默，不要干扰。

【层级配额】host / service / endpoint / data / auth 五层，每层至少 1 条 fact。哪层是 0，本轮 hint 必须包含一条"去碰那层"的具体动作。
【ROI 记账】同一锚点同一手法连续 2 次无新 fact → 明确写"停止 X"，除非能说清这次与上次在机理上有什么不同。
【对抗式复核】出现 finding 时先问：能不能用另一条独立路径复现？不能就建议降级为 inferred。

【输出】严格 JSON：{"diagnosis":"","evidence":[],"pivot":{"from":"","to":"","why":""},"hints":[],"steer":[{"intent_id":0,"message":""}],"kill":[],"confidence":"high|medium|low"}`
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func firstN(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

// ParseJSON 解析 LLM 版顾问输出：容忍 ```json 围栏与前后废话，
// 并做硬性收敛（hint ≤3 条、非可执行的丢弃、confidence 归一）。
func ParseJSON(raw string) (Diagnosis, error) {
	var d Diagnosis
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		if j := strings.Index(rest, "```"); j >= 0 {
			rest = rest[:j]
		}
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "json"))
		s = rest
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return d, fmt.Errorf("adviser: 输出里没有 JSON 对象")
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &d); err != nil {
		return d, fmt.Errorf("adviser: JSON 解析失败：%w", err)
	}
	kept := make([]string, 0, 3)
	dropped := 0
	for _, h := range d.Hints {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if !Actionable(h) {
			dropped++
			continue
		}
		if len(kept) < 3 {
			kept = append(kept, h)
		}
	}
	d.Hints = kept
	switch strings.ToLower(strings.TrimSpace(d.Confidence)) {
	case "high":
		d.Confidence = "high"
	case "medium":
		d.Confidence = "medium"
	default:
		d.Confidence = "low"
	}
	d.Source = "llm"
	if d.Diagnosis == "" && len(kept) == 0 {
		return d, fmt.Errorf("adviser: 空诊断")
	}
	if dropped > 0 {
		d.Evidence = append(d.Evidence, fmt.Sprintf("丢弃 %d 条不可执行的 hint（缺具体资产/端口/接口）", dropped))
	}
	return d, nil
}
