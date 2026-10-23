package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/llmrec"
)

// 战果清单的两层按需 AI：
//   L1 /loot/explain —— 只把一行写成人话，按该行证据 hash 缓存；
//   L2 /loot/suggest —— 补这个站特有的能力项，必须引用真实 finding id。
//
// 纪律：状态（done/tried/run）与 plain/objects/proof_state 全部由 loot.go 的规则算；
// 这里只写解释，不改战果，不造第二真相源。

const lootExplainSystem = `你是给非技术委托人解释渗透战果的助手。只根据给定的一行证据，解释：能拿什么、有多严重、下一步值不值得做。要求：最多 3 句；全中文；不出现英文技术术语、产品名、命令、URL；不写具体数量；不编造证据里没有的表名、字段或对象；不要提“证据”“输入”这些词。`

const lootSuggestSystem = `你是渗透测试战果归纳助手。请从给定站点的已确认漏洞摘要里，找出这个站特有、通用能力模板没有覆盖的 1-3 条能力项（例如卡密、支付流水、订阅信息）。硬约束：每条必须引用列表里真实存在的 finding id；引用不到就不能返回这一条；不得编造表名、字段、数量或漏洞。不要重复现有通用能力项。只输出一个 JSON 对象，不要 Markdown：{"rows":[{"key":"短英文slug","label":"中文名称","plain":"一句人话","why":"为什么这个站特有","evidence":[{"kind":"finding","id":123,"title":"原始证据标题"}]}]}。没有可补充的就输出 {"rows":[]}。`

var lootSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

type lootSuggestEvidence struct {
	Kind  string `json:"kind"`
	ID    any    `json:"id"`
	Title string `json:"title"`
}

type lootSuggestCandidate struct {
	Key      string                `json:"key"`
	Label    string                `json:"label"`
	Plain    string                `json:"plain"`
	Why      string                `json:"why"`
	Inferred bool                  `json:"inferred"`
	Evidence []lootSuggestEvidence `json:"evidence"`
}

type lootSuggestion struct {
	Key      string          `json:"key"`
	Label    string          `json:"label"`
	Plain    string          `json:"plain"`
	Why      string          `json:"why"`
	Inferred bool            `json:"inferred"`
	Evidence []candoEvidence `json:"evidence"`
}

// lootExplain：L1，解释清单里的单行。
func (s *Server) lootExplain(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	if s.m == nil || s.m.pg == nil {
		writeErr(w, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	var body struct {
		RowKey string `json:"row_key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil || strings.TrimSpace(body.RowKey) == "" {
		writeErr(w, http.StatusBadRequest, "row_key 必填")
		return
	}
	rowKey := strings.TrimSpace(body.RowKey)
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)

	fs := s.lootFindings(t)
	rows := s.lootRowsForTask(r.Context(), t, fs)
	var row *lootRow
	for i := range rows {
		if rows[i].Key == rowKey {
			row = &rows[i]
			break
		}
	}
	if row == nil {
		writeErr(w, http.StatusNotFound, "这一行不存在（没有证据的行不会出现）")
		return
	}

	hash := row.evidenceHash
	if hash == "" {
		hash = lootRowHash(row.Key, *row, nil)
	}

	// 命中成功缓存 → 0 token，立即返回。
	var cachedText, cachedModel, status string
	err := s.m.pg.QueryRow(`SELECT status,text,model FROM loot_explanations
		WHERE task_id=$1 AND row_key=$2 AND evidence_hash=$3`, taskID, rowKey, hash).
		Scan(&status, &cachedText, &cachedModel)
	if err == nil && status == "done" {
		writeJSON(w, http.StatusOK, map[string]any{"text": cachedText, "cached": true, "model": nz2(cachedModel, "adviser")})
		return
	}

	ctx, cancel := context.WithTimeout(llmrec.WithWorker(r.Context(), "loot"), 45*time.Second)
	defer cancel()
	rt := &taskLLMRuntime{s: s, taskID: t.ID, agentKey: "adviser"}
	text, lerr := collectText(ctx, rt, true, lootExplainSystem, lootExplainUser(row), 700)
	if lerr != nil {
		log.Printf("[loot] explain llm failed (task=%d row=%s): %v", taskID, rowKey, lerr)
		s.saveLootExplanation(taskID, rowKey, hash, "failed", "", "rule")
		writeJSON(w, http.StatusOK, map[string]any{"text": "", "cached": false, "model": "rule"})
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		s.saveLootExplanation(taskID, rowKey, hash, "failed", "", "rule")
		writeJSON(w, http.StatusOK, map[string]any{"text": "", "cached": false, "model": "rule"})
		return
	}
	s.saveLootExplanation(taskID, rowKey, hash, "done", text, "adviser")
	writeJSON(w, http.StatusOK, map[string]any{"text": text, "cached": false, "model": "adviser"})
}

func lootExplainUser(row *lootRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "目标行：%s\n", row.Label)
	fmt.Fprintf(&b, "当前状态：%s（%s）\n", lootStatusCN(row.Status), row.Why)
	fmt.Fprintf(&b, "一句人话：%s\n", row.Plain)
	if len(row.Objects) > 0 {
		b.WriteString("具体能拿到：\n")
		for _, o := range row.Objects {
			if strings.TrimSpace(o.Fields) != "" {
				fmt.Fprintf(&b, "- %s（字段：%s）\n", o.Name, o.Fields)
			} else {
				fmt.Fprintf(&b, "- %s\n", o.Name)
			}
		}
	} else {
		b.WriteString("具体能拿到：证据里没有抽出明确的表名或字段。\n")
	}
	if len(row.Evidence) > 0 {
		b.WriteString("依据：\n")
		for _, e := range row.Evidence {
			fmt.Fprintf(&b, "- #%d %s\n", e.ID, e.Title)
		}
	}
	b.WriteString("请用最多 3 句说明：能拿什么、有多严重、下一步值不值得做。")
	return b.String()
}

func lootStatusCN(status string) string {
	switch status {
	case "done":
		return "已拿到"
	case "tried":
		return "试过没成"
	case "run":
		return "正在试"
	default:
		return "还没试"
	}
}

// lootSuggest：L2，补这个站特有的能力项。每条必须能落到真实 finding id。
func (s *Server) lootSuggest(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	if s.m == nil || s.m.pg == nil {
		writeErr(w, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	fs := s.lootFindings(t)
	if len(fs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"rows": []lootSuggestion{}})
		return
	}
	rows := s.lootRowsForTask(r.Context(), t, fs)
	hash, digest := lootSuggestDigest(fs, rows)

	// 命中成功缓存 → 直接返回校验后的结果。
	var cachedText string
	err := s.m.pg.QueryRow(`SELECT text FROM loot_explanations
		WHERE task_id=$1 AND row_key='suggest' AND evidence_hash=$2 AND status='done'`, taskID, hash).
		Scan(&cachedText)
	if err == nil {
		var cands []lootSuggestCandidate
		if json.Unmarshal([]byte(cachedText), &cands) == nil {
			writeJSON(w, http.StatusOK, map[string]any{"rows": lootValidateSuggestions(cands, fs)})
			return
		}
	}

	ctx, cancel := context.WithTimeout(llmrec.WithWorker(r.Context(), "loot"), 45*time.Second)
	defer cancel()
	rt := &taskLLMRuntime{s: s, taskID: t.ID, agentKey: "adviser"}
	user := fmt.Sprintf("目标：%s\n\n%s\n请只补充这个站特有、通用模板没有的能力项；没有就返回空数组。", nz2(strings.TrimSpace(t.Goal), "（这个任务没有写目标）"), digest)
	raw, lerr := collectText(ctx, rt, true, lootSuggestSystem, user, 1200)
	if lerr != nil {
		log.Printf("[loot] suggest llm failed (task=%d): %v", taskID, lerr)
		s.saveLootExplanation(taskID, "suggest", hash, "failed", "", "rule")
		writeJSON(w, http.StatusOK, map[string]any{"rows": []lootSuggestion{}})
		return
	}

	out := parseLootSuggestions(raw, fs)
	payload, _ := json.Marshal(out)
	s.saveLootExplanation(taskID, "suggest", hash, "done", string(payload), "adviser")
	writeJSON(w, http.StatusOK, map[string]any{"rows": out})
}

// lootSuggestDigest 生成 (缓存 hash, 给模型看的摘要)。
// hash 覆盖全部 findings 的 id/标题/摘要；提示词只放前 80 条，避免单次输入失控。
func lootSuggestDigest(fs []lootFinding, rows []lootRow) (string, string) {
	sorted := append([]lootFinding(nil), fs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].id < sorted[j].id })

	var hashB, promptB strings.Builder
	hashB.WriteString("rows\n")
	promptB.WriteString("现有通用能力清单（不要重复）：\n")
	for _, r := range rows {
		fmt.Fprintf(&hashB, "%s|%s\n", r.Key, r.Label)
		fmt.Fprintf(&promptB, "- %s：%s\n", r.Key, r.Label)
	}
	hashB.WriteString("findings\n")
	promptB.WriteString("\n该任务已确认的漏洞摘要：\n")
	for i, f := range sorted {
		line := fmt.Sprintf("#%d %s | %s\n", f.id, f.title, truncateRunes(strings.TrimSpace(f.summary), 200))
		hashB.WriteString(line)
		if i < 80 {
			promptB.WriteString(line)
		}
	}
	if len(sorted) > 80 {
		promptB.WriteString("（后面还有更多漏洞，已省略）\n")
	}
	sum := sha256.Sum256([]byte(hashB.String()))
	return hex.EncodeToString(sum[:16]), promptB.String()
}

func parseLootSuggestions(raw string, fs []lootFinding) []lootSuggestion {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return []lootSuggestion{}
	}
	var wire struct {
		Rows []lootSuggestCandidate `json:"rows"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &wire); err != nil {
		log.Printf("[loot] suggest output is not valid JSON: %v", err)
		return []lootSuggestion{}
	}
	return lootValidateSuggestions(wire.Rows, fs)
}

// lootValidateSuggestions 是 L2 的硬闸：没有真实 finding id 的行一律丢弃；
// 标题用库里的真实标题覆盖模型输出，避免模型改写证据。
func lootValidateSuggestions(cands []lootSuggestCandidate, fs []lootFinding) []lootSuggestion {
	byID := make(map[int64]string, len(fs))
	for _, f := range fs {
		byID[f.id] = f.title
	}
	out := []lootSuggestion{}
	usedKey := map[string]bool{}
	for _, c := range cands {
		evidence := []candoEvidence{}
		seenID := map[int64]bool{}
		for _, e := range c.Evidence {
			id, ok := lootAnyInt64(e.ID)
			if !ok || seenID[id] {
				continue
			}
			title, ok := byID[id]
			if !ok {
				continue
			}
			kind := strings.TrimSpace(e.Kind)
			if kind == "" {
				kind = "finding"
			}
			if kind != "finding" {
				continue
			}
			seenID[id] = true
			evidence = append(evidence, candoEvidence{Kind: "finding", ID: id, Title: title})
		}
		if len(evidence) == 0 {
			continue
		}
		label := strings.TrimSpace(c.Label)
		if label == "" {
			continue
		}
		plain := strings.TrimSpace(c.Plain)
		if plain == "" {
			plain = label
		}
		slug := lootSlug(c.Key)
		if slug == "" {
			slug = fmt.Sprintf("item-%d", len(out)+1)
		}
		key := "ai:" + slug
		for usedKey[key] {
			key = "ai:" + slug + "-" + strconv.Itoa(len(out)+1)
		}
		usedKey[key] = true
		out = append(out, lootSuggestion{
			Key:      key,
			Label:    label,
			Plain:    plain,
			Why:      strings.TrimSpace(c.Why),
			Inferred: true,
			Evidence: evidence,
		})
		if len(out) >= 3 {
			break
		}
	}
	return out
}

func lootAnyInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		if x <= 0 || x > 1e15 {
			return 0, false
		}
		n := int64(x)
		return n, float64(n) == x
	case int64:
		return x, x > 0
	case int:
		return int64(x), x > 0
	case json.Number:
		n, err := strconv.ParseInt(x.String(), 10, 64)
		return n, err == nil && n > 0
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n, err == nil && n > 0
	default:
		return 0, false
	}
}

func lootSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "ai:")
	s = lootSlugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if r := []rune(s); len(r) > 40 {
		s = strings.Trim(string(r[:40]), "-")
	}
	return s
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (s *Server) saveLootExplanation(taskID int64, rowKey, hash, status, text, model string) {
	if s.m == nil || s.m.pg == nil {
		return
	}
	_, err := s.m.pg.Exec(`INSERT INTO loot_explanations(task_id,row_key,evidence_hash,status,text,model,tokens_in,tokens_out)
		VALUES($1,$2,$3,$4,$5,$6,0,0)
		ON CONFLICT (task_id,row_key,evidence_hash) DO UPDATE SET
			status=EXCLUDED.status,text=EXCLUDED.text,model=EXCLUDED.model,created_at=now()`,
		taskID, rowKey, hash, status, text, model)
	if err != nil {
		log.Printf("[loot] save explanation failed (task=%d row=%s): %v", taskID, rowKey, err)
	}
}
