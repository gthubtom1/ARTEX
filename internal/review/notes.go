package review

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	pgdb "github.com/Autumn-27/artex/db"
)

// Notes 是复盘官自己的记忆（每任务一行，≤400 token，固定六行）。
// 它是"全局观"的唯一载体：跨介入保留，但不读别人的对话。
func LoadNotes(ctx context.Context, d *pgdb.DB, taskID int64) (body string, version int, err error) {
	if d == nil {
		return "", 0, fmt.Errorf("db unavailable")
	}
	err = d.QueryRowContext(ctx, `SELECT body, version FROM review_notes WHERE task_id=$1`, taskID).Scan(&body, &version)
	if err != nil {
		return "", 0, nil // 没有笔记不是错误
	}
	return body, version, nil
}

// SaveNotes 乐观锁写入：版本不符时基于最新版本重写（不静默覆盖）。
func SaveNotes(ctx context.Context, d *pgdb.DB, taskID int64, body string, expectVersion int) (int, error) {
	if d == nil {
		return 0, fmt.Errorf("db unavailable")
	}
	body = clampNotes(body)
	var ver int
	err := d.QueryRowContext(ctx, `
INSERT INTO review_notes(task_id, version, body, updated_at)
VALUES($1, 1, $2, now())
ON CONFLICT (task_id) DO UPDATE
  SET body = EXCLUDED.body,
      version = review_notes.version + 1,
      updated_at = now()
WHERE $3 <= 0 OR review_notes.version = $3
RETURNING version`, taskID, body, expectVersion).Scan(&ver)
	if err != nil {
		var cur int
		if e2 := d.QueryRowContext(ctx, `SELECT version FROM review_notes WHERE task_id=$1`, taskID).Scan(&cur); e2 == nil {
			if e3 := d.QueryRowContext(ctx, `
UPDATE review_notes SET body=$2, version=version+1, updated_at=now()
WHERE task_id=$1 RETURNING version`, taskID, body).Scan(&ver); e3 == nil {
				return ver, nil
			}
		}
		return 0, err
	}
	return ver, nil
}

func clampNotes(s string) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > 1200 {
		return string([]rune(s)[:1200])
	}
	return s
}

// Action 是允许下发到引擎的动作（docs/14 §3：四类）。
type Action struct {
	Kind   string `json:"kind"`             // kill_intent|retry_intent|set_next_stage|propose_intent
	Intent int64  `json:"intent,omitempty"` // kill/retry 用
	Stage  string `json:"stage,omitempty"`  // set_next_stage 用
	Why    string `json:"why"`              // propose_intent 时这里就是「新方向 + 依据」
}

// Cite 是证据锚点（必须真实 id）。
type Cite struct {
	Kind string `json:"kind"` // intent|finding|fact
	ID   int64  `json:"id"`
}

// Verdict 是一次评审的结构化结论。
type Verdict struct {
	Verdict     string   `json:"verdict"` // continue|pivot|wrapup
	Body        string   `json:"body"`
	Cites       []Cite   `json:"cites"`
	Actions     []Action `json:"actions"`
	Expectation string   `json:"expectation"` // 一句话、可机检的预期（E1：有动作时必须写）
	Confidence  string   `json:"confidence"`  // high|medium|low
	Notes       string   `json:"notes"`       // 更新后的笔记（六行）
}

// SystemPrompt 是复盘官的人设：外部评估者 + 不信前判断 + 只给四类动作。
const SystemPrompt = `你是这次渗透任务请来的「外部评估者」。你不参与执行，不跑命令，只读一份结构化快照和你自己上次的笔记。

你的判断对象只有一件：整条方向还成不成立。不是某条漏洞真假，也不是某条命令对不对。

硬规矩：
1. 只信快照里的结构化事实；快照里没写的东西不许推测，写"未确认"；
2. 每条结论后面用 [intent:ID] / [finding:ID] / [fact:ID] 标注依据；没有依据的句子删掉；
3. 区分两件事：目标侧明确拒绝（死路）与 我方故障（self_error/env_error，没真正试过，应建议重跑）；
4. 只允许五类动作：kill_intent（标死已被证死的路）、retry_intent（重跑被我方故障打断的路）、
   set_next_stage（换阶段）、propose_intent（只提议新方向，附依据，永远不自动执行）、
   ask_human（需要人提供东西：账号/授权/在两条路里选一条）。任何"改目标/改数据/收工"都不是你的动作；
4b. **判断战况只看快照里的 war 段**（等级 level / 已拿到 has / 三段式 can_standing / 真能做的 doable / 停摆 stall）：
   漏洞条数多不代表有战果；can_standing.got=0 就是"一个现实后果都还没有"，别把 one_step 当已成立；
   如果 war.stall 是 stop（很久没有新证据），你**必须**给 wrapup 或至少一条具体动作 + expectation，
   不许只写"继续努力"；
4c. 任何动作都必须带 expectation（一句话：做完怎么算成功，能被后续证据机检）。缺了就等于没给；
4d. **不许只在正文抱怨**：如果 body 里写了"还差什么/没登记/证据不全/缺账号"，就必须给出对应动作
   （要人给东西 → ask_human；要主链路去补 → propose_intent；换阶段 → set_next_stage），并写 expectation；
   一句话都不做只描述现状，等于没审。
5. 输出必须是一个 JSON 对象，不要 Markdown 代码块，字段：
   {"verdict":"continue|pivot|wrapup","body":"给完全不懂技术的人看的 3-6 句","cites":[{"kind":"intent","id":20}],
    "actions":[{"kind":"kill_intent","intent":21,"why":"目标 OPTIONS 明确不含 PUT，连续 4 次"},
               {"kind":"ask_human","why":"需要一个普通用户账号才能验证越权"}],
    "expectation":"3 轮内出现涉及 IIS 配置的新事实",
    "confidence":"high|medium|low","notes":"更新后的笔记，固定六行，≤400字"}
6. body 里不许出现英文术语与命令；只写"手里有什么、还差什么、下一步值不值得做"。
7. 必须简短：body ≤200 字，notes ≤150 字，actions ≤3 条，cites ≤6 条；一次写完且以 } 结尾。
8. E1：只要 actions 非空，expectation 就是必填——一句话写清「到什么程度算这条建议起效」，且必须能被事实摘要机检（例："3 轮内出现涉及 IIS 配置的新事实"）。写不出预期信号就不要给动作。
9. 快照里的 last_rejections 是最近被合理拒绝的建议与理由：不要重复提，除非有新证据能推翻那个理由。`

// Parse 解析模型输出（容忍 ```json 包裹）。
func Parse(raw string) (Verdict, error) {
	txt := strings.TrimSpace(raw)
	if i := strings.Index(txt, "{"); i > 0 {
		txt = txt[i:]
	}
	if j := strings.LastIndex(txt, "}"); j >= 0 && j+1 <= len(txt) {
		txt = txt[:j+1]
	}
	var v Verdict
	if err := json.Unmarshal([]byte(txt), &v); err != nil {
		return Verdict{}, err
	}
	switch v.Verdict {
	case "continue", "pivot", "wrapup":
	default:
		v.Verdict = "continue"
	}
	if v.Confidence == "" {
		v.Confidence = "low"
	}
	kept := v.Actions[:0]
	for _, a := range v.Actions {
		switch a.Kind {
		case "kill_intent", "retry_intent", "set_next_stage", "propose_intent":
			kept = append(kept, a)
		}
	}
	v.Actions = kept
	return v, nil
}

// ParseLenient 在 JSON 不完整（常见于输出被 max_tokens 截断）时抢救正文。
func ParseLenient(raw string) (Verdict, bool) {
	pattern := `(?s)"body"\s*:\s*"((?:[^"\\]|\\.)*)"`
	re := regexp.MustCompile(pattern)
	if m := re.FindStringSubmatch(raw); len(m) >= 2 {
		if body := unescapeBody(m[1]); len([]rune(body)) >= 12 {
			return Verdict{Verdict: "continue", Body: body, Confidence: "low"}, true
		}
	}
	// 未闭合（输出被 max_tokens 截断）：从 "body" 后面一直取到末尾。
	i := strings.Index(raw, `"body"`)
	if i < 0 {
		return Verdict{}, false
	}
	rest := raw[i+len(`"body"`):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return Verdict{}, false
	}
	rest = rest[j+1:]
	if k := strings.Index(rest, `",`); k > 0 {
		rest = rest[:k]
	}
	if body := unescapeBody(strings.TrimRight(rest, `\\`)); len([]rune(body)) >= 12 {
		return Verdict{Verdict: "continue", Body: body, Confidence: "low"}, true
	}
	return Verdict{}, false
}

func unescapeBody(s string) string {
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\"`, `"`)
	return strings.TrimSpace(s)
}

// ProsePrompt 是「只要一段人话」的变体：结构化动作由规则引擎给，
// 模型只负责把现状说人话（避免小模型输出长 JSON 被截断）。
const ProsePrompt = `你是这次渗透任务请来的「外部评估者」。你不参与执行，不跑命令。
读下面的结构化事实，用完全不懂技术的委托人能听懂的话，写 3–6 句：
手里已经有什么、还差什么、下一步值不值得做、哪条方向应该放下。
规矩：
1. 只信事实，没写的写"还没确认"；
2. 不出现英文术语、命令、模型名；
3. 不写数量细节（只说"有用户密码"这种程度）；
4. 直接输出这段话本身，不要 JSON、不要 Markdown、不要分点标题。
5. 快照里的 last_rejections 是最近被合理拒绝的建议与理由：不要重复提，除非有新证据能推翻那个理由。`

// RuleBased 是模型不可用时的确定性兜底（不烧 token、不编造）。
func RuleBased(s *Snapshot) Verdict {
	var selfFault, dead []int64
	for _, it := range s.Intents {
		switch it.LastError {
		case "self_error", "env_error":
			selfFault = append(selfFault, it.ID)
		case "target_blocked":
			dead = append(dead, it.ID)
		}
	}
	var b strings.Builder
	b.WriteString("（规则版判断，模型不可用）\n")
	fmt.Fprintf(&b, "现在有 %d 条方向在跑、%d 条漏洞、%d 条已确认的死路。\n", len(s.Intents), len(s.Findings), len(dead))
	if len(selfFault) > 0 {
		fmt.Fprintf(&b, "%d 条方向是我方接口故障断的，不算失败，可以重跑。", len(selfFault))
	}
	if len(dead) > 0 {
		fmt.Fprintf(&b, "另外 %d 条方向是目标明确拒绝的，别再试。", len(dead))
	}
	// 「串联漏洞才是渗透本质」：有洞但没变成任何访问权时，明确催它去串链。
	// 判据：review_runs.body 出现「还没有变成任何访问权」；只在有漏洞且无立足点时出现。
	if len(s.Findings) > 0 && len(s.Owned) == 0 {
		fmt.Fprintf(&b, "\n注意：已经有 %d 个漏洞，但还没有变成任何访问权（无凭据、无立足点）。下一步优先把其中一个变成登录/命令执行/数据样本，而不是继续找新洞。", len(s.Findings))
	}
	// g2：拿到命令执行但没登记 → 权限等级不会涨（用户会看到“明明拿到 shell 却还是 E0”）。
	if hasShellEvidence(s.Findings) && len(s.Owned) == 0 {
		fmt.Fprintln(&b, "", "已经拿到命令执行的证据，但还没登记立足点：立刻用 owned_register 登记这台机器（含权限级别），否则「权限阶梯」不会上升，后续提权/横移也没有起点。")
	}
	// g3：后渗透标准清单（拿到命令执行后照着走，避免漏掉关键一步）。
	if hasShellEvidence(s.Findings) {
		fmt.Fprintln(&b, "", "拿到命令执行后的标准动作清单：① 读配置找数据库连接 → ② 只读数据样本证明可读 → ③ 检查提权路径（sudo -l / SUID / 内核版本）→ ④ 有内网才考虑横移 → ⑤ 全程把证据写回事实。")
	}
	if b.Len() < 40 {
		b.WriteString("暂时没有明显走偏的迹象。")
	}
	v := Verdict{Verdict: "continue", Body: b.String(), Confidence: "low"}
	// 笔记：规则版也能积累（否则 review_notes 永远是空表）。
	v.Notes = fmt.Sprintf("目标：%s\n方向：%d 条 ｜ 漏洞：%d 条\n死路（目标拒绝）：%d\n我方故障（可重跑）：%d\n判断基线：连续3轮无新fact=该换；同手法4次同结果=死路",
		truncRunes(s.Goal, 60), len(s.Intents), len(s.Findings), len(dead), len(selfFault))
	for _, id := range dead {
		v.Actions = append(v.Actions, Action{Kind: "kill_intent", Intent: id, Why: "目标侧明确拒绝"})
		v.Cites = append(v.Cites, Cite{Kind: "intent", ID: id})
	}
	for _, id := range selfFault {
		v.Actions = append(v.Actions, Action{Kind: "retry_intent", Intent: id, Why: "我方故障，没真正试过"})
		v.Cites = append(v.Cites, Cite{Kind: "intent", ID: id})
	}
	// 2026-10-24：**cites 必须说清"这次判断看了什么"**。
	// 实测（VM task 7）：没有死路/故障时 cites 为空 → 卡片只能显示"（它没给依据）"，
	// 用户无法判断结论可不可信。这里补上"引用本次快照里最强的证据"：
	// 先引用已确认漏洞（最强），再补最近的事实；都没有才算"无依据"。
	if len(v.Cites) == 0 {
		for i, f := range s.Findings {
			if i >= 3 {
				break
			}
			v.Cites = append(v.Cites, Cite{Kind: "finding", ID: f.ID})
		}
		for i := len(s.Facts) - 1; i >= 0 && len(v.Cites) < 5; i-- {
			v.Cites = append(v.Cites, Cite{Kind: "fact", ID: s.Facts[i].ID})
		}
	}
	if len(v.Cites) == 0 {
		// 刚开局、真的什么都没有：明确降级，不要伪装成有依据的判断。
		v.Confidence = "low"
		v.Notes = ""
	}

	// E1：规则版给动作时同样必须给一句话可机检的预期；没有预期就不该有动作。
	v.Expectation = expectationFor(s, v.Actions)
	return v
}

// expectationFor 给规则版动作生成一句话可机检的预期。
// 关键词锚点优先用「被重开/被提议的具体方向」，其次用任务目标；这样观察窗回访时
// 能用 fact 摘要做确定性比对（见 gate.go 的 EvaluateObservation）。
func expectationFor(s *Snapshot, actions []Action) string {
	if s == nil || len(actions) == 0 {
		return ""
	}
	byID := map[int64]IntentBrief{}
	for _, it := range s.Intents {
		byID[it.ID] = it
	}
	goal := truncRunes(s.Goal, 40)
	for _, a := range actions {
		if a.Kind == "retry_intent" {
			anchor := truncRunes(byID[a.Intent].Summary, 40)
			if anchor == "" {
				anchor = goal
			}
			if anchor != "" {
				return fmt.Sprintf("3 轮内出现涉及「%s」的新事实", anchor)
			}
		}
	}
	for _, a := range actions {
		if a.Kind == "propose_intent" && strings.TrimSpace(a.Why) != "" {
			return fmt.Sprintf("3 轮内出现涉及「%s」的新事实", truncRunes(a.Why, 40))
		}
	}
	if goal == "" {
		goal = "整体目标"
	}
	return fmt.Sprintf("3 轮内出现与「%s」相关的新事实", goal)
}

func truncRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// hasShellEvidence 判断快照里是否已有"命令执行/代码执行"类漏洞证据。
// 只看标题与证明文本里的关键词（规则判定，可复算），不猜。
func hasShellEvidence(fs []FindingBrief) bool {
	for _, f := range fs {
		t := strings.ToLower(f.Title + " " + f.Proof)
		for _, kw := range []string{"rce", "命令执行", "代码执行", "webshell", "反序列化", "远程代码"} {
			if strings.Contains(t, kw) {
				return true
			}
		}
	}
	return false
}
