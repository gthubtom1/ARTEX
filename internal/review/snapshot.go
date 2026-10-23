// Package review 是复盘官的数据层：结构化快照 + 评审笔记。
//
// 硬规则（docs/09-详设-C01-复盘官.md §7）：
// 快照里只放结构化事实——不放对话文本、不放 planner 的思考、不放 worker 的推理。
// 这是"有全局观但不继承隧道视野"的物理保证。
package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	pgdb "github.com/Autumn-27/artex/db"
	artexkb "github.com/Autumn-27/artex/internal/kb"
)

// 经验层（docs/14 §2 L3）的规模上限：宁可少说，也不要拿一堆噪声淹掉模型。
const (
	deadEndMax     = 8   // 死路台账最多 8 条
	deadEndTextMax = 100 // 每条截断 100 字
	kbMaxHits      = 2   // KB 最多 2 条命中
	kbMaxQueries   = 2   // KB 最多 2 个查询词
)

// KBHit 是一条能写进结论的「经验」：哪个词查到的、哪份知识、命中的上下文。
type KBHit struct {
	Query   string `json:"query"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// IntentBrief 是一条意图的对外摘要（不含执行细节）。
type IntentBrief struct {
	ID         int64  `json:"id"`
	Summary    string `json:"summary"`
	State      string `json:"state"`
	Priority   int    `json:"priority"`
	LastError  string `json:"last_error,omitempty"` // self_error/env_error/target_blocked
	Attempts   int    `json:"attempts"`
	UpdatedAgo string `json:"updated_ago"`
}

type FindingBrief struct {
	ID       int64  `json:"id"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Proof    string `json:"proof"`
}

type FactBrief struct {
	ID      int64  `json:"id"`
	Summary string `json:"summary"`
}

type CapBrief struct {
	Item  string `json:"item"`
	Can   string `json:"can"`
	Proof string `json:"proof"`
}

// Snapshot 是喂给复盘官的全部内容。
type Snapshot struct {
	Goal       string         `json:"goal"`
	RoundNo    int64          `json:"round_no"`
	BudgetLeft string         `json:"budget_left"`
	Intents    []IntentBrief  `json:"intents"`
	Findings   []FindingBrief `json:"findings"`
	Facts      []FactBrief    `json:"facts"`
	Errors     map[string]int `json:"errors_by_class"`
	Negatives  []string       `json:"negatives"`
	Owned      []string       `json:"owned"`
	Caps       []CapBrief     `json:"capabilities"`
	Notes      string         `json:"notes"`
	// 经验两字段（docs/14 §7-1）：死路台账 + KB 命中。两条都可能为空，空就整段不出现。
	DeadEnds []string `json:"dead_ends,omitempty"`
	KBHits   []KBHit  `json:"kb_hits,omitempty"`
	// 拒因回读（docs/15 §3 / docs/14 §7-4）：该任务最近 3 条被拒理由（带 run id），
	// 让它下次别再提已经被合理拒绝的建议。空就整段不出现。
	LastRejections []string `json:"last_rejections,omitempty"`
}

// Hash 是快照指纹（落地对账用；不含时间，否则缓存永远失效）。
func (s Snapshot) Hash() string {
	raw, _ := json.Marshal(struct {
		Goal    string
		Round   int64
		Intents []IntentBrief
		Finds   []FindingBrief
		Facts   []FactBrief
	}{s.Goal, s.RoundNo, s.Intents, s.Findings, s.Facts})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

// Prompt 把快照渲染成模型输入（紧凑 JSON，省 token）。
func (s Snapshot) Prompt() string {
	raw, _ := json.MarshalIndent(s, "", " ")
	return string(raw)
}

// Build 组装快照。notes 由调用方从 review_notes 读好传入。
func Build(ctx context.Context, d *pgdb.DB, taskID int64, notes string) (*Snapshot, error) {
	return BuildWithKB(ctx, d, taskID, notes, "")
}

// BuildWithKB 与 Build 相同，但可以指定知识库根目录（空=自动找 kb/ 或 skills/）。
func BuildWithKB(ctx context.Context, d *pgdb.DB, taskID int64, notes, kbRoot string) (*Snapshot, error) {
	if d == nil {
		return nil, fmt.Errorf("db unavailable")
	}
	s := &Snapshot{Errors: map[string]int{}, Notes: notes}

	// 目标 + 预算
	var timeout int
	var firstRun *interface{}
	_ = d.QueryRowContext(ctx, `SELECT COALESCE(goal,''), COALESCE(timeout_seconds,0) FROM tasks WHERE id=$1`, taskID).
		Scan(&s.Goal, &timeout)
	_ = firstRun
	_ = d.QueryRowContext(ctx, `SELECT COALESCE(round_no,0) FROM explorations WHERE id = (SELECT exploration_id FROM tasks WHERE id=$1)`, taskID).
		Scan(&s.RoundNo)
	if timeout > 0 {
		var leftSec float64
		_ = d.QueryRowContext(ctx, `SELECT EXTRACT(EPOCH FROM (COALESCE(first_run_at,created_at) + make_interval(secs => $2) - now()))
			FROM tasks WHERE id=$1`, taskID, timeout).Scan(&leftSec)
		if leftSec < 0 {
			s.BudgetLeft = "已超时"
		} else {
			s.BudgetLeft = fmt.Sprintf("%d 分钟", int(leftSec/60))
		}
	}

	// 先取最近的错误事件（用于给意图标注错误类；避免在 SQL 里做大数字拼接）
	// intent_id：0=旧数据/归属未知，>0=精确归属到某条意图。
	var errs []errorEvent
	if erows, eerr := d.QueryContext(ctx, `SELECT COALESCE(class,''), COALESCE(detail,''), COALESCE(intent_id,0)
		FROM task_error_events WHERE task_id=$1 AND ts > now() - interval '6 hours'
		ORDER BY ts DESC, id DESC LIMIT 200`, taskID); eerr == nil {
		defer erows.Close()
		for erows.Next() {
			var e errorEvent
			if erows.Scan(&e.class, &e.detail, &e.intentID) == nil {
				errs = append(errs, e)
			}
		}
	}

	// 意图：只取摘要/状态/优先级/错误类，不含执行细节
	rows, err := d.QueryContext(ctx, `SELECT n.id, COALESCE(n.payload->>'summary',''), COALESCE(n.state,''),
			COALESCE(n.priority,0),
			EXTRACT(EPOCH FROM (now() - COALESCE(n.updated_at, now())))::int
		FROM exploration_nodes n
		WHERE n.kind='intent' AND n.exploration_id = (SELECT exploration_id FROM tasks WHERE id=$1)
		ORDER BY n.id DESC LIMIT 40`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it IntentBrief
		var ago int
		if err := rows.Scan(&it.ID, &it.Summary, &it.State, &it.Priority, &ago); err != nil {
			continue
		}
		it.UpdatedAgo = fmt.Sprintf("%d 分钟前", ago/60)
		if len([]rune(it.Summary)) > 80 {
			it.Summary = string([]rune(it.Summary)[:80]) + "…"
		}
		s.Intents = append(s.Intents, it)
	}
	attributeErrors(s.Intents, errs)

	// 漏洞（顺带记下 vulnclass：它是 KB 检索词的第二来源）
	var vulnClasses []string
	frows, err := d.QueryContext(ctx, `SELECT id, COALESCE(severity,''), COALESCE(NULLIF(name,''), vulnclass, '未命名'), COALESCE(status,''),
			COALESCE(vulnclass,'')
		FROM findings WHERE task_id=$1 ORDER BY id DESC LIMIT 30`, taskID)
	if err == nil {
		defer frows.Close()
		for frows.Next() {
			var f FindingBrief
			var vc string
			if err := frows.Scan(&f.ID, &f.Severity, &f.Title, &f.Proof, &vc); err == nil {
				s.Findings = append(s.Findings, f)
				if strings.TrimSpace(vc) != "" {
					vulnClasses = append(vulnClasses, vc)
				}
			}
		}
	}

	// 最近事实
	xrows, err := d.QueryContext(ctx, `SELECT n.id, COALESCE(n.payload->>'summary','')
		FROM exploration_nodes n
		WHERE n.exploration_id = (SELECT exploration_id FROM tasks WHERE id=$1)
		  AND n.kind='fact' ORDER BY n.id DESC LIMIT 10`, taskID)
	if err == nil {
		defer xrows.Close()
		for xrows.Next() {
			var f FactBrief
			var sum string
			if err := xrows.Scan(&f.ID, &sum); err == nil {
				if len([]rune(sum)) > 70 {
					sum = string([]rune(sum)[:70]) + "…"
				}
				f.Summary = sum
				s.Facts = append(s.Facts, f)
			}
		}
	}

	// 错误分类计数
	erows, err := d.QueryContext(ctx, `SELECT class, count(*) FROM task_error_events
		WHERE task_id=$1 AND ts > now() - interval '24 hours' GROUP BY class`, taskID)
	if err == nil {
		defer erows.Close()
		for erows.Next() {
			var c string
			var n int
			if err := erows.Scan(&c, &n); err == nil {
				s.Errors[c] = n
			}
		}
	}

	// 能力卡（只取三项，不做渲染）
	crows, err := d.QueryContext(ctx, `SELECT item, COALESCE(can,''), COALESCE(proof_level,'')
		FROM capabilities WHERE task_id=$1 ORDER BY id DESC LIMIT 20`, taskID)
	if err == nil {
		defer crows.Close()
		for crows.Next() {
			var c CapBrief
			if err := crows.Scan(&c.Item, &c.Can, &c.Proof); err == nil {
				s.Caps = append(s.Caps, c)
			}
		}
	}

	// 立足点
	orows, err := d.QueryContext(ctx, `SELECT COALESCE(asset_key,'') FROM assets_owned WHERE first_task_id=$1 LIMIT 20`, taskID)
	if err == nil {
		defer orows.Close()
		for orows.Next() {
			var k string
			if err := orows.Scan(&k); err == nil && strings.TrimSpace(k) != "" {
				s.Owned = append(s.Owned, k)
			}
		}
	}

	// 负结论：从 fact 里挑带否定的（与 report.negativesFromTask 同一套标记词）
	markers := []string{"无差异", "未成立", "不可利用", "没有回显", "失败", "不存在", "未发现", "不支持"}
	nrows, err := d.QueryContext(ctx, `SELECT COALESCE(n.payload->>'summary','')
		FROM exploration_nodes n
		WHERE n.exploration_id = (SELECT exploration_id FROM tasks WHERE id=$1)
		  AND n.kind='fact' ORDER BY n.id DESC LIMIT 200`, taskID)
	if err == nil {
		defer nrows.Close()
		seen := map[string]bool{}
		for nrows.Next() {
			var sum string
			if err := nrows.Scan(&sum); err != nil || sum == "" {
				continue
			}
			for _, m := range markers {
				if strings.Contains(sum, m) {
					if len([]rune(sum)) > 90 {
						sum = string([]rune(sum)[:90]) + "…"
					}
					if !seen[sum] {
						seen[sum] = true
						s.Negatives = append(s.Negatives, sum)
					}
					break
				}
			}
			if len(s.Negatives) >= 8 {
				break
			}
		}
	}
	sort.SliceStable(s.Negatives, func(i, j int) bool { return s.Negatives[i] < s.Negatives[j] })

	// 死路台账：assumptions 里还没了结的假设，就是「这条路通常走不通」的经验。
	// 读失败不算错误——没有台账就当作没有经验。
	drows, derr := d.QueryContext(ctx, `SELECT COALESCE(text,'') FROM assumptions
		WHERE task_id=$1 AND COALESCE(status,'') <> 'resolved'
		ORDER BY id DESC LIMIT $2`, taskID, deadEndMax)
	if derr == nil {
		defer drows.Close()
		for drows.Next() {
			var txt string
			if drows.Scan(&txt) == nil && strings.TrimSpace(txt) != "" {
				s.DeadEnds = append(s.DeadEnds, truncRunes(txt, deadEndTextMax))
			}
		}
	}

	// KB 经验：目录不存在/检索失败一律静默跳过，绝不阻塞评审。
	s.KBHits = buildKBHits(kbRoot, kbQueries(s.Errors, vulnClasses))

	// 拒因回读：被拒理由也是「经验」——同样的建议不要提第二次，除非有新证据。
	// 读失败不算错误，没有拒因就整段不出现。
	if rrows, rerr := d.QueryContext(ctx, `SELECT id, COALESCE(ack_reason,'')
		FROM review_runs WHERE task_id=$1 AND ack_status='rejected'
		ORDER BY COALESCE(ack_at, created_at) DESC, id DESC LIMIT 3`, taskID); rerr == nil {
		defer rrows.Close()
		for rrows.Next() {
			var id int64
			var reason string
			if rrows.Scan(&id, &reason) != nil {
				continue
			}
			reason = truncRunes(strings.TrimSpace(reason), 160)
			if reason == "" {
				reason = "（未写理由）"
			}
			s.LastRejections = append(s.LastRejections, fmt.Sprintf("run #%d：%s", id, reason))
		}
	}
	return s, nil
}

// errorEvent 是一条任务级错误事件在快照侧的投影。
// intentID=0 表示归属未知（0010 之前的历史行 / 平台级事件）。
type errorEvent struct {
	class, detail string
	intentID      int64
}

// attributeErrors 给每条意图挑一个错误类。
//
// 优先级：intent_id 精确匹配（0010 起 errev 写入时带上 worker 的意图 id）>
// 旧数据的 detail 子串兜底（只有 intent_id=0 的历史行参与）。
//
// 为什么要保留兜底：升级前的行没有意图 id，全丢掉等于历史经验归零。
// 为什么只对 intent_id=0 生效：精确行不需要猜；而且子串猜中的大多数是
// IP/端口数字撞上了意图 id（实测 task#2 run#19 把 70/43/17 判成重开，
// 真正 blocked 的是 20/47/53）。
func attributeErrors(intents []IntentBrief, errs []errorEvent) {
	if len(intents) == 0 || len(errs) == 0 {
		return
	}
	// errs 已按 ts DESC 排好：首次写入即「最新一条」。
	exact := make(map[int64]string, len(errs))
	legacy := make([]errorEvent, 0, len(errs))
	for _, e := range errs {
		if e.intentID > 0 {
			if _, ok := exact[e.intentID]; !ok {
				exact[e.intentID] = e.class
			}
			continue
		}
		legacy = append(legacy, e)
	}
	for i := range intents {
		it := &intents[i]
		if it.ID <= 0 {
			continue
		}
		if cls, ok := exact[it.ID]; ok {
			it.LastError = cls
			continue
		}
		if len(legacy) == 0 {
			continue
		}
		idStr := fmt.Sprintf("%d", it.ID)
		for _, e := range legacy {
			if strings.Contains(e.detail, idStr) {
				it.LastError = e.class
				break
			}
		}
	}
}

// errClassQuery 是错误类到 KB 检索词的映射。空串表示这类错误不值得检索经验：
// 我方故障（self_error/env_error）说明的是平台坏了，不是这条路不通。
// 其余未知类保持原样，避免历史数据里的自定义类被静默丢弃。
var errClassQuery = map[string]string{
	"target_blocked": "WAF",
	"policy_blocked": "权限策略",
	"self_error":     "",
	"env_error":      "",
	"tool_missing":   "工具缺失",
}

// kbQueries 从快照生成检索词（最多 2 个）：最频繁的错误类 + 最近漏洞的类型词。
// 错误类先映射成人类可读词；vulnclass 往往是「SQL Injection (stacked queries)」
// 这种整串，KB 里只有短标题，必须归一化后才能命中。
func kbQueries(errs map[string]int, vulnClasses []string) []string {
	var out []string
	add := func(q string) {
		q = strings.TrimSpace(q)
		if q == "" {
			return
		}
		for _, old := range out {
			if strings.EqualFold(old, q) {
				return
			}
		}
		if len(out) < kbMaxQueries {
			out = append(out, q)
		}
	}

	bestC, bestQ, bestN := "", "", 0
	for c, n := range errs {
		q, mapped := errClassQuery[c]
		if !mapped {
			if c == "" {
				continue
			}
			q = c
		}
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		if n > bestN || (n == bestN && c < bestC) {
			bestC, bestQ, bestN = c, q, n
		}
	}
	add(bestQ)

	for _, v := range vulnClasses {
		if q := vulnQueryTerm(v); q != "" {
			add(q)
			break
		}
	}
	if len(out) > kbMaxQueries {
		out = out[:kbMaxQueries]
	}
	return out
}

// vulnQueryTerm 把 vulnclass 整串压成一个 KB 检索词。
//
// 先去掉括号里的补充说明（如「SQL Injection (stacked queries)」→「SQL Injection」），
// 再按分隔符切分取最长片段；只有括号里才有内容时，退回原始串再切一次。
func vulnQueryTerm(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// 括号内容通常是上下文（stacked queries / 时间盲注），不是 KB 标题。
	if q := longestVulnFragment(stripBracketContent(raw)); q != "" {
		return q
	}
	return longestVulnFragment(raw)
}

// stripBracketContent 去掉 () 与全角（）内的内容，保留括号外的原文。
func stripBracketContent(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(', '（':
			depth++
		case ')', '）':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// longestVulnFragment 按分隔符切分后取最长且够长的片段。
// 中文片段（如「任意文件读取」）长度 ≥2 即可；纯英文片段仍需 ≥3。
func longestVulnFragment(s string) string {
	frags := strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case '(', ')', '/', ',', '、', '（', '）', '-', '：', ':':
			return true
		}
		return false
	})
	best := ""
	bestLen := 0
	for _, f := range frags {
		f = strings.TrimSpace(f)
		n := len([]rune(f))
		if n == 0 || !vulnFragmentLongEnough(f, n) {
			continue
		}
		if n > bestLen {
			best, bestLen = f, n
		}
	}
	return best
}

func vulnFragmentLongEnough(s string, n int) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return n >= 2
		}
	}
	return n >= 3
}

// resolveKBRoot 决定检索哪个目录：显式配置优先，其次 kb/，再次 skills/；都没有就返回 ""。
func resolveKBRoot(explicit string) string {
	for _, c := range []string{explicit, "kb", "skills"} {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return ""
}

// buildKBHits 复用 internal/kb 的只读检索（文件名+正文、按分数排序），
// 再按「命中行前后 2 行」生成 Snippet、Title 用文件名（docs/14 §7-1）。
// 任何错误都只跳过，不返回、不阻塞。
func buildKBHits(root string, queries []string) []KBHit {
	root = resolveKBRoot(root)
	if root == "" || len(queries) == 0 {
		return nil
	}
	var out []KBHit
	seen := map[string]bool{}
	for _, q := range queries {
		if len(out) >= kbMaxHits {
			break
		}
		hits, err := artexkb.Search(root, q, artexkb.Options{MaxFiles: 400, Limit: 8})
		if err != nil {
			continue
		}
		for _, h := range hits {
			if len(out) >= kbMaxHits {
				break
			}
			if seen[h.Path] {
				continue
			}
			seen[h.Path] = true
			out = append(out, KBHit{Query: q, Title: filepath.Base(h.Path), Snippet: kbSnippet(root, h, q)})
		}
	}
	return out
}

// kbSnippet 取命中行前后各 2 行；读不到/没命中就退回 kb.Search 给的片段。
func kbSnippet(root string, h artexkb.Hit, query string) string {
	body, err := artexkb.Read(root, h.Path, 256*1024)
	if err != nil {
		return h.Snippet
	}
	lines := strings.Split(body, "\n")
	at := -1
	low := strings.ToLower(query)
	for i, ln := range lines {
		if strings.Contains(strings.ToLower(ln), low) {
			at = i
			break
		}
	}
	if at < 0 {
		return h.Snippet
	}
	start, end := at-2, at+2
	if start < 0 {
		start = 0
	}
	if end > len(lines)-1 {
		end = len(lines) - 1
	}
	// 压掉多余空白：Snippet 进 Prompt，省 token 也更好读。
	sn := strings.Join(strings.Fields(strings.Join(lines[start:end+1], " ")), " ")
	return truncRunes(sn, 300)
}
