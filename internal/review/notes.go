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

// Action 是允许下发到引擎的动作（docs/09 §6：只有这三类）。
type Action struct {
	Kind   string `json:"kind"`             // kill_intent|retry_intent|set_next_stage
	Intent int64  `json:"intent,omitempty"` // kill/retry 用
	Stage  string `json:"stage,omitempty"`  // set_next_stage 用
	Why    string `json:"why"`
}

// Cite 是证据锚点（必须真实 id）。
type Cite struct {
	Kind string `json:"kind"` // intent|finding|fact
	ID   int64  `json:"id"`
}

// Verdict 是一次评审的结构化结论。
type Verdict struct {
	Verdict    string   `json:"verdict"` // continue|pivot|wrapup
	Body       string   `json:"body"`
	Cites      []Cite   `json:"cites"`
	Actions    []Action `json:"actions"`
	Confidence string   `json:"confidence"` // high|medium|low
	Notes      string   `json:"notes"`      // 更新后的笔记（六行）
}

// SystemPrompt 是复盘官的人设：外部评估者 + 不信前判断 + 只给三类动作。
const SystemPrompt = `你是这次渗透任务请来的「外部评估者」。你不参与执行，不跑命令，只读一份结构化快照和你自己上次的笔记。

你的判断对象只有一件：整条方向还成不成立。不是某条漏洞真假，也不是某条命令对不对。

硬规矩：
1. 只信快照里的结构化事实；快照里没写的东西不许推测，写"未确认"；
2. 每条结论后面用 [intent:ID] / [finding:ID] / [fact:ID] 标注依据；没有依据的句子删掉；
3. 区分两件事：目标侧明确拒绝（死路）与 我方故障（self_error/env_error，没真正试过，应建议重跑）；
4. 只允许三类动作：kill_intent（标死已被证死的路）、retry_intent（重跑被我方故障打断的路）、set_next_stage（换阶段）。任何"改目标/改数据/收工"都不是你的动作；
5. 输出必须是一个 JSON 对象，不要 Markdown 代码块，字段：
   {"verdict":"continue|pivot|wrapup","body":"给完全不懂技术的人看的 3-6 句","cites":[{"kind":"intent","id":20}],
    "actions":[{"kind":"kill_intent","intent":21,"why":"目标 OPTIONS 明确不含 PUT，连续 4 次"}],
    "confidence":"high|medium|low","notes":"更新后的笔记，固定六行，≤400字"}
6. body 里不许出现英文术语与命令；只写"手里有什么、还差什么、下一步值不值得做"。
7. 必须简短：body ≤200 字，notes ≤150 字，actions ≤3 条，cites ≤6 条；一次写完且以 } 结尾。`

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
		case "kill_intent", "retry_intent", "set_next_stage":
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
	m := re.FindStringSubmatch(raw)
	if len(m) < 2 || strings.TrimSpace(m[1]) == "" {
		return Verdict{}, false
	}
	body := m[1]
	body = strings.ReplaceAll(body, "\\n", "\n")
	body = strings.ReplaceAll(body, "\\\"", "\"")
	if len([]rune(body)) < 12 {
		return Verdict{}, false
	}
	return Verdict{Verdict: "continue", Body: body, Confidence: "low"}, true
}

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
	if b.Len() < 40 {
		b.WriteString("暂时没有明显走偏的迹象。")
	}
	v := Verdict{Verdict: "continue", Body: b.String(), Confidence: "low"}
	for _, id := range dead {
		v.Actions = append(v.Actions, Action{Kind: "kill_intent", Intent: id, Why: "目标侧明确拒绝"})
		v.Cites = append(v.Cites, Cite{Kind: "intent", ID: id})
	}
	for _, id := range selfFault {
		v.Actions = append(v.Actions, Action{Kind: "retry_intent", Intent: id, Why: "我方故障，没真正试过"})
		v.Cites = append(v.Cites, Cite{Kind: "intent", ID: id})
	}
	return v
}
