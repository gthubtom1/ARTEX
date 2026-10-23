package review

// 本文件是审查官的「门」：投递前的自检五问 + 分级（read/action）+ 自动动作筛选 + 8 条防抖。
//
// 唯一事实源：docs/13-复盘官-闭环与防抖.md（§3 自检五问、§4 防抖 R1–R8）。
// 设计纪律：这里**全是纯函数**——不碰 DB、不碰 LLM、不碰意图状态。调用方（server/review.go）
// 只负责把事实读进来、把 Decide 的结论写回去，因此每条规则都能被单测钉住。

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// 三档权限（docs/09 A2、docs/13 §6）。
const (
	TierObserve = "observe" // 只看：只留结论卡，不动任务
	TierLowRisk = "lowrisk" // 低风险自动（默认）：只做"减浪费"两类动作
	TierAuto    = "auto"    // 全自动：同上，但配额更高、冷却更短
)

// 投递分级：read=只读结论（A 级），action=要动任务（B 级，需主 Agent 核对）。
const (
	LevelRead   = "read"
	LevelAction = "action"
)

// 触发原因（落 review_runs.reason）。
const (
	TriggerCond   = "cond"
	TriggerTimer  = "timer"
	TriggerManual = "manual"
	TriggerRebut  = "rebut"
)

// 自动动作只允许这两类"减浪费"（docs/13 R5）。
const (
	ClassRetrySelfFault = "retry:self_fault"    // 我方故障（self_error/env_error），没真正试过
	ClassKillRejected   = "kill:target_blocked" // 目标侧明确拒绝，且已试够次数
)

// RunBrief 是最近一次评审的摘要（去重 / 降权判定用）。
type RunBrief struct {
	DedupeKey   string
	Level       string
	ReasonClass string
	AckStatus   string // ''|held|accepted|rejected|escalated|stale
}

// State 是 review_state 的一行。
type State struct {
	Tier          string // ''=跟全局
	Interventions int
	AutoActions   int
	LastSentAt    time.Time
	LastFacts     int
	Tripped       bool
	TripReason    string
}

// GateIn 是 Decide 的全部输入。
type GateIn struct {
	Snap             Snapshot
	Verdict          Verdict
	State            State
	Recent           []RunBrief // 最近 N 条（新→旧），建议 30
	Now              time.Time
	Trigger          string // cond|timer|manual|rebut
	DefaultTier      string // 全局设置（task 未覆盖时用它）
	Cooldown         time.Duration
	MaxInterventions int // 0=默认 10
	MaxAuto          int // 0=默认 5
	// FactCount 是本次介入时的 fact 数（R6 判"有没有变好"）。
	FactCount int
}

// GateOut 是 Decide 的结论。
type GateOut struct {
	Drop        string   // 非空=不投递（只进内部日志，不进主会话、不亮红点）
	Notes       []string // 判定过程（写日志、给面板"为什么没说"）
	Level       string   // read|action
	DedupeKey   string
	ReasonClass string
	Actions     []Action // 允许下发的动作（已剔除过期/无依据/超权限）
	AutoApplied []Action // 本次由它自己执行的动作（tier!=observe 且有可自动动作）
	NeedHuman   bool     // 结论涉及收口/换目标：必须人决定（红点不消）
	Trip        bool     // 本次触发熔断 → tier 转 observe
	TripReason  string
}

// Decide 实现投递前的全部判定。顺序即优先级：
// 过期/无依据（Q2/Q3）→ 去重（R1/Q1）→ 降权（R4/Q5）→ 冷却（R2）→ 配额（R3）→ 分级与自动（R5）→ 熔断（R6）。
func Decide(in GateIn) GateOut {
	var out GateOut
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	maxInt, maxAuto := in.MaxInterventions, in.MaxAuto
	if maxInt <= 0 {
		maxInt = 10
	}
	if maxAuto <= 0 {
		maxAuto = 5
	}

	tier := strings.TrimSpace(in.State.Tier)
	if tier == "" {
		tier = strings.TrimSpace(in.DefaultTier)
	}
	if tier == "" {
		tier = TierLowRisk
	}
	if in.State.Tripped {
		tier = TierObserve
		out.Notes = append(out.Notes, "熔断生效中 → 只看")
	}

	// ── Q3 有依据吗 + Q2 过期吗：先清洗动作 ──
	// 动作必须①指向快照里真实存在的 intent；②该 intent 的状态/错误类仍支持这个动作；
	// ③在 cites 里被引用过（否则等于没依据）。
	cited := map[int64]bool{}
	for _, c := range in.Verdict.Cites {
		if c.Kind == "intent" {
			cited[c.ID] = true
		}
	}
	byID := map[int64]IntentBrief{}
	for _, it := range in.Snap.Intents {
		byID[it.ID] = it
	}
	var kept []Action
	var classes []string
	for _, a := range in.Verdict.Actions {
		// propose_intent（提新方向）不指向已有意图，所以不能走下面的「必须指向快照里真实意图」校验。
		// 它只需要一个依据（cites 非空 + why 非空），且永远不能自动执行。
		if a.Kind == "propose_intent" {
			if strings.TrimSpace(a.Why) == "" {
				out.Notes = append(out.Notes, "剔除 propose_intent：没说依据（why 为空）")
				continue
			}
			if len(in.Verdict.Cites) == 0 {
				out.Notes = append(out.Notes, "剔除 propose_intent：没有引用任何真实 id（Q3 无依据）")
				continue
			}
			out.NeedHuman = true
			kept = append(kept, a)
			continue
		}
		it, ok := byID[a.Intent]
		if !ok || a.Intent == 0 {
			out.Notes = append(out.Notes, fmt.Sprintf("剔除动作 %s#%d：快照里没有这条意图（已过期）", a.Kind, a.Intent))
			continue
		}
		if !cited[a.Intent] {
			out.Notes = append(out.Notes, fmt.Sprintf("剔除动作 %s#%d：没被 cites 引用（Q3 无依据）", a.Kind, a.Intent))
			continue
		}
		switch a.Kind {
		case "retry_intent":
			if it.LastError != "self_error" && it.LastError != "env_error" {
				out.Notes = append(out.Notes, fmt.Sprintf("剔除 retry#%d：错误类=%s，不是我方故障", a.Intent, it.LastError))
				continue
			}
			classes = append(classes, ClassRetrySelfFault)
		case "kill_intent":
			if it.LastError != "target_blocked" {
				out.Notes = append(out.Notes, fmt.Sprintf("剔除 kill#%d：错误类=%s，不是目标拒绝", a.Intent, it.LastError))
				continue
			}
			if it.Attempts < 3 {
				out.Notes = append(out.Notes, fmt.Sprintf("剔除 kill#%d：只试了 %d 次，未到 3 次", a.Intent, it.Attempts))
				continue
			}
			classes = append(classes, ClassKillRejected)
		case "set_next_stage":
			// 引擎没有"换阶段"接口：只能记账 + 抛给人，永远不自动执行。
			out.NeedHuman = true
		case "propose_intent":
			// 上文已单独处理（不指向已有意图）；这里只保留白名单语义。
			out.NeedHuman = true
		default:
			out.Notes = append(out.Notes, fmt.Sprintf("剔除未知动作 %q", a.Kind))
			continue
		}
		kept = append(kept, a)
	}
	out.Actions = kept
	out.ReasonClass = reasonClass(classes)

	// 收口/转向这类会改目标或结束任务的结论，一律要人拍板（docs/13 §2 第 3 步）。
	if in.Verdict.Verdict == "wrapup" || in.Verdict.Verdict == "pivot" {
		out.NeedHuman = true
	}

	// ── R1/Q1 只对变化说话：去重键 = 动作类型 + 目标 + 该目标当前状态 ──
	out.DedupeKey = dedupeKey(kept, byID)
	if out.DedupeKey != "" {
		for _, r := range in.Recent {
			if r.DedupeKey == out.DedupeKey && r.AckStatus != "stale" {
				out.Drop = "重复：同一动作与同一状态已经说过"
				out.Notes = append(out.Notes, out.Drop+"（R1/Q1）")
				return out
			}
		}
	}

	// ── R4/Q5 说了有用吗：同类建议被拒 ≥2 次 → 降为只读，不再推 B 级 ──
	downweighted := false
	if out.ReasonClass != "" {
		rejected := 0
		for _, r := range in.Recent {
			if r.ReasonClass == out.ReasonClass && r.AckStatus == "rejected" {
				rejected++
			}
		}
		if rejected >= 2 {
			downweighted = true
			out.Notes = append(out.Notes, fmt.Sprintf("同类建议已被拒 %d 次 → 只留结论卡（R4）", rejected))
			out.Actions = nil
			out.NeedHuman = false
		}
	}

	// ── R2 冷却（手动/反驳不受限：那是人主动要的）──
	if in.Trigger != TriggerManual && in.Trigger != TriggerRebut && in.Cooldown > 0 && !in.State.LastSentAt.IsZero() {
		if now.Sub(in.State.LastSentAt) < in.Cooldown {
			out.Drop = fmt.Sprintf("冷却中：距上次投递 %s < %s", now.Sub(in.State.LastSentAt).Round(time.Second), in.Cooldown)
			out.Notes = append(out.Notes, out.Drop+"（R2）")
			return out
		}
	}

	// ── R3 配额：介入次数上限（手动路径同样受限，避免人反复点烧钱）──
	if in.State.Interventions >= maxInt {
		out.Drop = fmt.Sprintf("配额用尽：已投递 %d 次/上限 %d", in.State.Interventions, maxInt)
		out.Notes = append(out.Notes, out.Drop+"（R3）")
		return out
	}

	// ── R6 无改善即收声（先于执行判定：熔断当次就不再动手）──
	// 自动动作已经做过 ≥3 个，而 fact 数没有比上次介入时增加 → 它没帮上忙，闭嘴转 observe。
	if in.State.AutoActions >= 3 && in.State.LastFacts > 0 && in.FactCount <= in.State.LastFacts {
		out.Trip = true
		out.TripReason = fmt.Sprintf("自动动作 %d 次后仍无新 fact（%d→%d），转只看",
			in.State.AutoActions, in.State.LastFacts, in.FactCount)
		out.Notes = append(out.Notes, out.TripReason+"（R6）")
	}

	// ── R5 分级与自动执行 ──
	out.Level = LevelRead
	if len(out.Actions) > 0 && !downweighted {
		out.Level = LevelAction
	}

	// ── E1 预期信号（docs/15 §5）：要动任务就必须能写出一句话的可机检预期 ──
	// 没有预期 = 事后无法判断这条建议有没有起效，直接不发（只留内部日志）。
	// 放在自动执行之前，保证 Drop 的结论一个动作也不会落到引擎上。
	if out.Level == LevelAction && strings.TrimSpace(in.Verdict.Expectation) == "" {
		out.Drop = "E1：有动作但没写可观测预期（expectation 为空），不投递"
		out.Notes = append(out.Notes, out.Drop)
		return out
	}

	if len(out.Actions) > 0 && tier != TierObserve && !downweighted && !out.Trip {
		if in.State.AutoActions >= maxAuto {
			out.Notes = append(out.Notes, fmt.Sprintf("自动动作已达上限 %d → 本次只提建议（R3）", maxAuto))
		} else {
			for _, a := range out.Actions {
				if isAutoSafe(a, byID[a.Intent]) {
					out.AutoApplied = append(out.AutoApplied, a)
					if len(out.AutoApplied)+in.State.AutoActions >= maxAuto {
						break
					}
				}
			}
			if len(out.AutoApplied) > 0 {
				out.Notes = append(out.Notes, fmt.Sprintf("按 %s 档自动执行 %d 个减浪费动作（R5）", tier, len(out.AutoApplied)))
			}
		}
	}
	if tier == TierObserve && len(out.Actions) > 0 {
		out.Notes = append(out.Notes, "只看档：动作只展示，不执行（R5）")
	}

	return out
}

// NeedHumanFor 判定单个动作是否必须由人（或主 Agent 核对后）拍板。
// propose_intent（提新方向）与 set_next_stage（换阶段）**永远不能自动执行**；
// 前者只会被写成一条建议（不改目标、不派活），后者引擎根本没有接口。
func NeedHumanFor(kind string) bool {
	switch kind {
	case "propose_intent", "set_next_stage":
		return true
	}
	return false
}

// EscalatePlan 把「ack 窗口超时、没人回应」的一组动作分成两类（docs/14 §4）：
//
//	auto    — 只减浪费的两类（标死已证死的路 / 重开我方故障断的路），可按档位自动执行；
//	suggest — 换阶段 / 提新方向 / 未知动作，只能写成给主 Agent 的建议，**绝不改目标、绝不收工**。
func EscalatePlan(actions []Action) (auto, suggest []Action) {
	for _, a := range actions {
		if NeedHumanFor(a.Kind) {
			suggest = append(suggest, a)
			continue
		}
		switch a.Kind {
		case "kill_intent", "retry_intent":
			auto = append(auto, a)
		default:
			suggest = append(suggest, a)
		}
	}
	return auto, suggest
}

// isAutoSafe 判定单个动作是否可以由它自己执行：只允许两类减浪费，且必须满足证据门槛。
func isAutoSafe(a Action, it IntentBrief) bool {
	if NeedHumanFor(a.Kind) {
		return false // propose_intent / set_next_stage 永远不自动
	}
	switch a.Kind {
	case "retry_intent":
		return it.LastError == "self_error" || it.LastError == "env_error"
	case "kill_intent":
		return it.LastError == "target_blocked" && it.Attempts >= 3
	}
	return false // set_next_stage 等一律不自动
}

// dedupeKey 把动作集合压成一个稳定键：kind:id:state|...（排序后拼接）。
// 状态进键里，所以"同一条意图重新出问题"会被当成新情况再说一次。
func dedupeKey(actions []Action, byID map[int64]IntentBrief) string {
	if len(actions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(actions))
	for _, a := range actions {
		if a.Kind == "propose_intent" {
			// 新方向没有目标 id，靠提议正文区分；否则所有提议会被当成同一条。
			parts = append(parts, "propose_intent:"+truncRunes(strings.TrimSpace(a.Why), 48))
			continue
		}
		it := byID[a.Intent]
		parts = append(parts, fmt.Sprintf("%s:%d:%s:%s", a.Kind, a.Intent, it.State, it.LastError))
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func reasonClass(classes []string) string {
	if len(classes) == 0 {
		return ""
	}
	uniq := map[string]bool{}
	var out []string
	for _, c := range classes {
		if !uniq[c] {
			uniq[c] = true
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// ───────── 观察窗（docs/15 §3/§5）：一句话预期 → 到点比对 fact ─────────

// review_runs.outcome 的三个取值（空串 = 还在观察窗里）。
const (
	OutcomeEffective   = "effective"
	OutcomeIneffective = "ineffective"
	OutcomeVoided      = "voided"
)

// ObservationResult 是观察窗回访的确定性判定结果。
type ObservationResult struct {
	Outcome string // effective|ineffective|voided
	FactID  int64  // 命中/推翻的那条 fact
	Keyword string // 命中的关键词（effective 时有值）
}

// EvaluateObservation 判定一条预期信号是否兑现（纯函数，不碰 DB）：
//
//	voided      —— 期间出现直接推翻建议的新事实（由调用方从 exploration_edges 判定后传入 conflict）；
//	effective   —— 新增 fact ≥1 且 expectation 里至少一个具体关键词出现在某条新 fact 摘要里；
//	ineffective —— 其余（没有新 fact、或新 fact 里一个关键词都没出现）。
//
// 关键词由 expectationKeywords 提取：先剔除"出现/涉及/新事实"这类空话，再做中文 2-gram；
// 提不出任何具体关键词的预期视为不可机检，按 ineffective 处理（宁可说没起效，不要假报达成）。
func EvaluateObservation(expectation string, newFacts []FactBrief, conflict *FactBrief) ObservationResult {
	if conflict != nil {
		return ObservationResult{Outcome: OutcomeVoided, FactID: conflict.ID}
	}
	if len(newFacts) == 0 {
		return ObservationResult{Outcome: OutcomeIneffective}
	}
	kws := expectationKeywords(expectation)
	if len(kws) == 0 {
		return ObservationResult{Outcome: OutcomeIneffective}
	}
	for _, f := range newFacts {
		sum := strings.ToLower(f.Summary)
		if strings.TrimSpace(sum) == "" {
			continue
		}
		for _, kw := range kws {
			if strings.Contains(sum, kw) {
				return ObservationResult{Outcome: OutcomeEffective, FactID: f.ID, Keyword: kw}
			}
		}
	}
	return ObservationResult{Outcome: OutcomeIneffective}
}

// expectationStopWords 是「一句话预期里的空话」：整词剔除后再做 2-gram，
// 否则"出现/涉及/新事实"会在几乎任何 fact 里命中，观察窗就永远判 effective。
var expectationStopWords = []string{
	"可观测", "新事实", "新方向", "新证据", "轮内", "以内", "之内", "至少",
	"出现", "涉及", "相关", "新增", "事实", "产生", "看到", "需要", "建议",
	"预期", "信号", "确认", "已经", "可以", "能够", "可能", "应该", "是否",
	"没有", "不再", "之间", "以及", "或者", "任何", "有关", "指向", "拿到",
	"得到", "用于", "进行", "存在", "这个", "那个", "一条", "两条", "三条",
	"新的", "一个", "分钟",
}

// expectationASCIIStop 是英文空话；具体技术词（IIS/SQL/RCE…）不在其中。
var expectationASCIIStop = map[string]bool{
	"the": true, "and": true, "for": true, "with": true,
	"new": true, "fact": true, "facts": true, "within": true,
	"round": true, "rounds": true, "expect": true, "expectation": true,
}

// expectationKeywords 从一句话预期里抽具体关键词：
//  1. 小写化，先整词剔除空话；
//  2. 中文串在「的/了/和/与/或/不/没…」等虚词处断开，再取 2-gram；
//  3. 英文/数字串只留长度 ≥2 且不在英文空话表里的词，纯数字丢弃（"3 轮内"里的 3 不算）。
func expectationKeywords(expectation string) []string {
	exp := strings.ToLower(strings.TrimSpace(expectation))
	if exp == "" {
		return nil
	}
	for _, w := range expectationStopWords {
		exp = strings.ReplaceAll(exp, w, " ")
	}

	seen := map[string]bool{}
	var out []string
	add := func(tok string) {
		tok = strings.TrimSpace(tok)
		if len([]rune(tok)) < 2 || seen[tok] || isAllDigits(tok) || expectationASCIIStop[tok] {
			return
		}
		seen[tok] = true
		out = append(out, tok)
	}
	emitHan := func(r []rune) {
		if len(r) < 2 {
			return
		}
		for i := 0; i+1 < len(r); i++ {
			add(string(r[i : i+2]))
		}
	}

	var han, ascii []rune
	flushHan := func() {
		start := 0
		for i, r := range han {
			if isHanFuncRune(r) {
				emitHan(han[start:i])
				start = i + 1
			}
		}
		emitHan(han[start:])
		han = han[:0]
	}
	flushASCII := func() {
		if len(ascii) >= 2 {
			add(string(ascii))
		}
		ascii = ascii[:0]
	}
	for _, r := range []rune(exp) {
		switch {
		case unicode.Is(unicode.Han, r):
			flushASCII()
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushHan()
			ascii = append(ascii, r)
		default:
			flushHan()
			flushASCII()
		}
	}
	flushHan()
	flushASCII()
	return out
}

// isHanFuncRune 是「在中文里几乎不可能构成实词的虚词」；只切这些，避免把
// 上传/下载/条件/中间件/分支这类实词切断。
func isHanFuncRune(r rune) bool {
	switch r {
	case '的', '了', '和', '与', '或', '并', '而', '就', '也', '都', '很', '再', '只', '不', '没', '未', '这', '那', '被', '把':
		return true
	}
	return false
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// AdviceText 生成 B 级投递给主 Agent 的建议正文（明确写"建议不是命令"）。
func AdviceText(body string, actions []Action, autoApplied []Action) string {
	var b strings.Builder
	b.WriteString("审查官建议（**建议不是命令**，收到后先核对现状再决定）：\n")
	if s := strings.TrimSpace(body); s != "" {
		b.WriteString(s)
		b.WriteString("\n")
	}
	if len(actions) > 0 {
		b.WriteString("拟动作：")
		for i, a := range actions {
			if i > 0 {
				b.WriteString("；")
			}
			switch a.Kind {
			case "kill_intent":
				fmt.Fprintf(&b, "标死 #%d（%s）", a.Intent, a.Why)
			case "retry_intent":
				fmt.Fprintf(&b, "重开 #%d（%s）", a.Intent, a.Why)
			case "set_next_stage":
				fmt.Fprintf(&b, "换阶段 %s（%s）", a.Stage, a.Why)
			case "propose_intent":
				fmt.Fprintf(&b, "建议新方向：%s（它没有派活，采纳与否由你核对后决定）", a.Why)
			default:
				fmt.Fprintf(&b, "%s #%d（%s）", a.Kind, a.Intent, a.Why)
			}
		}
		b.WriteString("\n")
	}
	if len(autoApplied) > 0 {
		b.WriteString("它已按低风险档自行处理的：")
		for i, a := range autoApplied {
			if i > 0 {
				b.WriteString("；")
			}
			fmt.Fprintf(&b, "%s #%d", a.Kind, a.Intent)
		}
		b.WriteString("\n")
	}
	b.WriteString("按协议：①读 graph_overview/list_findings 核对现状 ②一致才继续，过期就回「已过期，当前是 X」")
	b.WriteString(" ③会改目标/改数据/收工 → 不许自己执行，连理由抛给人 ④回执三行：结论 / 依据（真实 id+状态）/ 动作。")
	return b.String()
}
