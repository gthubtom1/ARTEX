package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/capability"
)

// 「能做到什么」地图（CARD-B01）。
//
// 设计约束（docs/08-详设-B01-能做到什么.md）：
//  1. 只读现有表（capabilities / findings / task_error_events / exploration_nodes）；
//  2. 任何字段映射不出来就显示「未确认」，不猜；
//  3. 唯一写入是 cando_annotations（AI 细化缓存）与 hint 节点（用户点「直接发给它」）。
// notImplemented 是 CARD-000 的占位响应（review.go 的 501 桩共用）。
func notImplemented(w http.ResponseWriter) {
	writeErr(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) registerCandoRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/cando", s.candoGet)
	mux.HandleFunc("POST /api/tasks/{id}/cando/annotate", s.candoAnnotate)
	mux.HandleFunc("GET /api/tasks/{id}/cando/annotate", s.candoAnnotateStatus)
	mux.HandleFunc("POST /api/tasks/{id}/cando/send", s.candoSendHint)
}

// ───────────────────────────── DTO ─────────────────────────────

type candoItem struct {
	Name   string `json:"name"`
	Count  string `json:"count"`
	Note   string `json:"note"`
	Hot    bool   `json:"hot,omitempty"`
	Danger bool   `json:"danger,omitempty"`
	Todo   bool   `json:"todo,omitempty"`
}

type candoOption struct {
	Text string `json:"text"`
	Cost string `json:"cost"`
	Risk string `json:"risk"` // lo|mid|hi
	Gain string `json:"gain"`
	Rec  bool   `json:"rec,omitempty"`
}

type candoNode struct {
	ID      string        `json:"id"`
	Kind    string        `json:"kind"` // entry|can|data|next|blocked|excluded
	Title   string        `json:"title"`
	Body    string        `json:"body"`
	Chip    string        `json:"chip"`
	State   string        `json:"state"` // attacking|taken|spinning|dead|self_fault|untried
	Items   []candoItem   `json:"items,omitempty"`
	Options []candoOption `json:"options,omitempty"`
	Cites   []int64       `json:"cites,omitempty"`
	Prompt  string        `json:"prompt,omitempty"`
}

type candoLane struct {
	Key   string      `json:"key"`
	Nodes []candoNode `json:"nodes"`
}

type candoBanner struct {
	Goal       string `json:"goal"`
	Met        bool   `json:"met"`
	Summary    string `json:"summary"`
	BudgetLeft string `json:"budget_left,omitempty"`
}

type candoLoot struct {
	Label   string `json:"label"`
	Count   string `json:"count"`
	NodeKey string `json:"node_key"`
}

type candoCite struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
}

type candoAnnotation struct {
	NodeKey    string      `json:"node_key"`
	Status     string      `json:"status"`
	Body       string      `json:"body"`
	Cites      []candoCite `json:"cites"`
	FinishedAt string      `json:"finished_at,omitempty"`
	Stale      bool        `json:"stale,omitempty"`
}

type candoResponse struct {
	Banner      candoBanner                `json:"banner"`
	Loot        []candoLoot                `json:"loot"`
	Lanes       []candoLane                `json:"lanes"`
	Annotations map[string]candoAnnotation `json:"annotations"`
}

// ─────────────────────────── 聚合 ───────────────────────────

// candoGet 返回当前任务的战果图。
func (s *Server) candoGet(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	if s.m.PG() == nil {
		writeErr(w, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)

	caps, _ := s.loadCapabilityCards(r.Context(), taskID)
	findings, _ := t.Store.ListByKind(db.KindFinding, 500)
	negatives := s.negativesFromTask(t)
	errCounts := s.candoErrorCounts(taskID)
	runningSummaries := s.candoRunningIntents(taskID)

	resp := candoResponse{Annotations: map[string]candoAnnotation{}, Loot: []candoLoot{}, Lanes: []candoLane{}}
	resp.Banner = candoBanner{Goal: strings.TrimSpace(t.Goal)}
	if resp.Banner.Goal == "" {
		resp.Banner.Goal = "（这个任务没有写目标）"
	}
	resp.Banner.BudgetLeft = s.candoBudgetLeft(taskID)

	lanes := s.candoLanes(t, caps, findings)
	resp.Lanes = lanes

	// 全局「卡住了」车道：只放目标侧明确拒绝（不是我方故障）
	if blocked := s.candoBlockedNodes(errCounts); len(blocked) > 0 {
		resp.Lanes = append(resp.Lanes, candoLane{Key: "errors", Nodes: blocked})
	}
	// 已排除（负结论）
	if n := candoExcludedNode(negatives); n != nil {
		resp.Lanes = append(resp.Lanes, candoLane{Key: "neg", Nodes: []candoNode{*n}})
	}

	// 状态着色：正在攻 = 有 running 意图的摘要里出现该入口路径
	for li := range resp.Lanes {
		for ni := range resp.Lanes[li].Nodes {
			n := &resp.Lanes[li].Nodes[ni]
			if n.Kind == "entry" && summaryHits(runningSummaries, n.Title) && n.State != "dead" {
				n.State = "attacking"
			}
		}
	}

	// loot：已证实（P2/P3）的能力项
	for _, c := range caps {
		if capability.ProofRank(c.ProofLevel) >= 2 && strings.TrimSpace(c.Can) != "" {
			resp.Loot = append(resp.Loot, candoLoot{
				Label: c.Can, Count: proofLabel(c.ProofLevel),
				NodeKey: "cap:" + c.Item,
			})
		}
	}
	if len(resp.Loot) > 6 {
		resp.Loot = resp.Loot[:6]
	}
	resp.Banner.Summary = fmt.Sprintf("%d 类漏洞 · %d 条上报 · %d 条入口线", distinctLen(caps), len(findings), len(lanes))

	// 附上已有 AI 细化缓存（按 node_key 取最新一条）
	resp.Annotations = s.candoAnnotations(taskID)

	writeJSON(w, http.StatusOK, resp)
}

// candoLanes 把能力卡按「入口（surface）」聚成车道；无能力卡时退回 findings。
func (s *Server) candoLanes(t *Task, caps []capability.Card, findings []*db.Node) []candoLane {
	lanes := []candoLane{}
	groups := map[string][]capability.Card{}
	order := []string{}
	for _, c := range caps {
		key := strings.TrimSpace(c.Surface)
		if key == "" {
			// 能力卡没标 surface（实测常态）：用能力名当入口，避免所有卡挤进"未标注入口"。
			key = nz2(c.Can, "未标注入口")
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], c)
	}
	for _, key := range order {
		cs := groups[key]
		lane := candoLane{Key: "ep:" + key}
		state := "untried"
		for _, c := range cs {
			if capability.ProofRank(c.ProofLevel) >= 2 && c.Confidence == "observed" {
				state = "taken"
			} else if state == "untried" && c.Confidence == "observed" {
				state = "spinning"
			}
		}
		lane.Nodes = append(lane.Nodes, candoNode{
			ID: lane.Key, Kind: "entry", Title: key,
			Body: "这条入口是从能力卡里读出来的（surface 字段）", Chip: chipForState(state), State: state,
		})
		for _, c := range cs {
			cs2 := "untried"
			if capability.ProofRank(c.ProofLevel) >= 2 && c.Confidence == "observed" {
				cs2 = "taken"
			}
			lane.Nodes = append(lane.Nodes, candoNode{
				ID: "cap:" + c.Item, Kind: "can",
				Title: nz2(c.Can, "未命名能力"),
				Body:  nz2(c.Plain, nz2(c.Evidence, "未确认")),
				Chip:  nz2(c.ProofLevel, "P?"), State: cs2,
				Cites:  c.SourceNodeIDs,
				Prompt: s.candoPrompt(t, c.Surface, nz2(c.NextIfApproved, c.Can)),
			})
			if strings.TrimSpace(c.Impact) != "" {
				lane.Nodes = append(lane.Nodes, candoNode{
					ID: "data:" + c.Item, Kind: "data", Title: "拿到了什么",
					Body: c.Impact, Chip: "已证实", State: cs2, Cites: c.SourceNodeIDs,
				})
			}
			if strings.TrimSpace(c.NextIfApproved) != "" {
				lane.Nodes = append(lane.Nodes, candoNode{
					ID: "next:" + c.Item, Kind: "next", Title: "下一步",
					Body: c.NextIfApproved, Chip: "可以试", State: "taken",
				})
			}
		}
		// 决策点：默认五类走法（对象现算，措辞固定）
		if state == "taken" {
			lane.Nodes = append(lane.Nodes, candoNode{
				ID: "opt:" + key, Kind: "next", Title: "到这里有几种走法（点开选）",
				Body: "要证据就取样本；要规模就拉一部分；要权限就换方向", Chip: "决策点", State: "taken",
				Options: defaultOptions(),
			})
		}
		lanes = append(lanes, lane)
	}
	if len(lanes) > 0 {
		return lanes
	}
	// 退回：用 findings 兜底，保证图不空
	if len(findings) > 0 {
		lane := candoLane{Key: "findings"}
		lane.Nodes = append(lane.Nodes, candoNode{
			ID: "findings", Kind: "entry", Title: "发现（未归类到能力卡）",
			Body: "能力卡还没派生出来，先按漏洞列表展示", Chip: "未归类", State: "untried",
		})
		for _, f := range findings {
			var p struct {
				Name      string `json:"name"`
				VulnClass string `json:"vulnclass"`
				Severity  string `json:"severity"`
				Summary   string `json:"summary"`
			}
			_ = json.Unmarshal(f.Payload, &p)
			lane.Nodes = append(lane.Nodes, candoNode{
				ID: "f:" + strconv.FormatInt(f.ID, 10), Kind: "can",
				Title: nz2(p.Name, nz2(p.VulnClass, "未命名漏洞")),
				Body:  nz2(p.Summary, "未确认"),
				Chip:  nz2(p.Severity, "info"), State: "untried",
				Cites: []int64{f.ID},
			})
		}
		return []candoLane{lane}
	}
	return nil
}

// candoBlockedNodes 把目标侧明确拒绝聚成「卡住了」节点（我方故障不算）。
func (s *Server) candoBlockedNodes(counts map[string]int) []candoNode {
	if counts["target_blocked"] < 1 {
		return nil
	}
	return []candoNode{{
		ID: "err:target_blocked", Kind: "blocked", Title: "它明确拒绝了这些写法",
		Body: fmt.Sprintf("目标侧拒绝 %d 次（403/405/Allow 不含等）。这是它说不，不是我打不通。", counts["target_blocked"]),
		Chip: "封死", State: "dead",
	}}
}

func candoExcludedNode(negatives []string) *candoNode {
	if len(negatives) == 0 {
		return nil
	}
	body := strings.Join(negatives, " · ")
	if len([]rune(body)) > 160 {
		body = string([]rune(body)[:160]) + "…"
	}
	return &candoNode{ID: "neg", Kind: "excluded", Title: "已排除（下次别再试）",
		Body: body, Chip: fmt.Sprintf("%d 项", len(negatives)), State: "dead"}
}

// ─────────────────────── 小工具 ───────────────────────

const candoSystemPrompt = `你是这次渗透任务的「外部评估者」。你只读结构化事实，不读对话，不跑命令。
用完全不懂技术的委托人能懂的话，写 3–5 句：这条线现在能拿到什么、还差什么、下一步值不值得做。
硬规矩：
1. 只写定性（"有未售卡密"），不写数量（"143 张"）；
2. 不许出现英文术语与命令；
3. 每条结论后面用 [finding:ID] / [fact:ID] 标注依据，没有依据的句子删掉；
4. 不确定就写"还没确认"。`

func (s *Server) candoAnnotations(taskID int64) map[string]candoAnnotation {
	out := map[string]candoAnnotation{}
	rows, err := s.m.pg.Query(`SELECT node_key,status,body,COALESCE(cites::text,'[]'),COALESCE(to_char(finished_at,'YYYY-MM-DD HH24:MI'),''),evidence_hash
		FROM cando_annotations WHERE task_id=$1 ORDER BY created_at DESC`, taskID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var a candoAnnotation
		var citesRaw, hash string
		if err := rows.Scan(&a.NodeKey, &a.Status, &a.Body, &citesRaw, &a.FinishedAt, &hash); err != nil {
			continue
		}
		if _, seen := out[a.NodeKey]; seen {
			continue // 只留最新一条
		}
		_ = json.Unmarshal([]byte(citesRaw), &a.Cites)
		out[a.NodeKey] = a
	}
	return out
}

func (s *Server) candoErrorCounts(taskID int64) map[string]int {
	out := map[string]int{}
	rows, err := s.m.pg.Query(`SELECT class, count(*) FROM task_error_events
		WHERE task_id=$1 AND ts > now() - interval '24 hours' GROUP BY class`, taskID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		var n int
		if err := rows.Scan(&c, &n); err == nil {
			out[c] = n
		}
	}
	return out
}

func (s *Server) candoRunningIntents(taskID int64) []string {
	out := []string{}
	rows, err := s.m.pg.Query(`SELECT COALESCE(payload->>'summary','') FROM exploration_nodes
		WHERE kind='intent' AND state='running' AND exploration_id = (SELECT exploration_id FROM tasks WHERE id=$1)`, taskID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s2 string
		if err := rows.Scan(&s2); err == nil && strings.TrimSpace(s2) != "" {
			out = append(out, s2)
		}
	}
	return out
}

func (s *Server) candoBudgetLeft(taskID int64) string {
	var timeout int
	var firstRun *time.Time
	var created time.Time
	err := s.m.pg.QueryRow(`SELECT COALESCE(timeout_seconds,0), first_run_at, created_at FROM tasks WHERE id=$1`, taskID).
		Scan(&timeout, &firstRun, &created)
	if err != nil || timeout <= 0 {
		return ""
	}
	start := created
	if firstRun != nil {
		start = *firstRun
	}
	left := time.Duration(timeout)*time.Second - time.Since(start)
	if left < 0 {
		return "已超时"
	}
	return fmt.Sprintf("%d 分钟", int(left.Minutes()))
}

var hostRe = regexp.MustCompile(`https?://([^/\s（）()]+)`)

func (s *Server) candoPrompt(t *Task, surface, next string) string {
	host := "目标"
	if m := hostRe.FindStringSubmatch(t.Goal); len(m) > 1 {
		host = m[1]
	}
	surface = nz2(surface, "已确认的入口")
	next = nz2(next, "沿当前入口继续")
	return fmt.Sprintf("主攻 %s：%s。已经放弃的方向不要重复试。只打 %s，20 分钟内给我结果。", surface, next, host)
}

func defaultOptions() []candoOption {
	return []candoOption{
		{Text: "只取 3–5 条样本，证明能读", Cost: "2 分钟", Risk: "lo", Gain: "报告里能复现的铁证", Rec: true},
		{Text: "拉一部分（比如 100 条）", Cost: "10 分钟", Risk: "mid", Gain: "能说明规模有多大"},
		{Text: "把整份数据导下来", Cost: "20–40 分钟", Risk: "hi", Gain: "数据全拿，但量大、易被发现"},
		{Text: "改一条数据再改回去，证明能改", Cost: "5 分钟", Risk: "hi", Gain: "能证明破坏力，但会动对方数据（不建议）"},
		{Text: "不碰数据，换方向冲更高目标", Cost: "30 分钟+", Risk: "hi", Gain: "只有真要权限时才值得"},
	}
}

func chipForState(st string) string {
	switch st {
	case "taken":
		return "已拿下"
	case "attacking":
		return "正在攻"
	case "spinning":
		return "有空转"
	case "dead":
		return "封死"
	case "self_fault":
		return "我方故障"
	}
	return "未试"
}

func proofLabel(p string) string {
	switch p {
	case "P3", "P2":
		return "已证实"
	}
	return "待确认"
}

func summaryHits(summaries []string, title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || title == "未标注入口" {
		return false
	}
	for _, s2 := range summaries {
		if strings.Contains(s2, title) {
			return true
		}
	}
	return false
}

func distinctLen(caps []capability.Card) int {
	seen := map[string]bool{}
	for _, c := range caps {
		seen[c.Item] = true
	}
	return len(seen)
}

func nz2(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// ───────────────────── AI 细化（B2，按需）─────────────────────

var candoMu sync.Map // taskID -> *sync.Mutex

func candoLock(taskID int64) *sync.Mutex {
	v, _ := candoMu.LoadOrStore(taskID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// candoAnnotate 提交一次按需细化：命中缓存直接回，否则入队后台跑。
func (s *Server) candoAnnotate(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	var body struct {
		NodeKey string `json:"node_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.NodeKey) == "" {
		writeErr(w, http.StatusBadRequest, "node_key 必填")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	hash := sha256.Sum256([]byte(body.NodeKey + "|" + t.Goal))
	evHash := hex.EncodeToString(hash[:8])

	// 缓存命中
	var status, stored string
	err := s.m.pg.QueryRow(`SELECT status, evidence_hash FROM cando_annotations
		WHERE task_id=$1 AND node_key=$2 ORDER BY created_at DESC LIMIT 1`, taskID, body.NodeKey).Scan(&status, &stored)
	if err == nil && status == "done" && stored == evHash {
		anns := s.candoAnnotations(taskID)
		if a, ok := anns[body.NodeKey]; ok {
			writeJSON(w, http.StatusOK, map[string]any{"status": "done", "cached": true, "annotation": a})
			return
		}
	}

	// 上限与互斥
	var n int
	_ = s.m.pg.QueryRow(`SELECT count(*) FROM cando_annotations WHERE task_id=$1`, taskID).Scan(&n)
	if n >= 20 {
		writeErr(w, http.StatusConflict, "这个任务的细化次数已到上限（20）")
		return
	}
	mu := candoLock(taskID)
	if !mu.TryLock() {
		writeErr(w, http.StatusTooManyRequests, "前面还有一个在分析，请稍后")
		return
	}

	_, _ = s.m.pg.Exec(`INSERT INTO cando_annotations(task_id,node_key,evidence_hash,status)
		VALUES($1,$2,$3,'pending')
		ON CONFLICT (task_id,node_key,evidence_hash) DO UPDATE SET status='pending', error=''`,
		taskID, body.NodeKey, evHash)

	go func(tk *Task, taskID int64, nodeKey, evHash string) {
		defer mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		user := fmt.Sprintf("目标：%s\n要分析的节点：%s\n任务描述：%s\n请按规矩写 3–5 句。", tk.Goal, nodeKey, tk.Description)
		rt := &taskLLMRuntime{s: s, taskID: tk.ID, agentKey: "adviser"}
		text, err := streamCollectText(ctx, rt, candoSystemPrompt, user)
		if err != nil {
			_, _ = s.m.pg.Exec(`UPDATE cando_annotations SET status='failed', error=$3, finished_at=now()
				WHERE task_id=$1 AND node_key=$2 AND evidence_hash=$4`, taskID, nodeKey, err.Error(), evHash)
			return
		}
		_, _ = s.m.pg.Exec(`UPDATE cando_annotations SET status='done', body=$3, finished_at=now()
			WHERE task_id=$1 AND node_key=$2 AND evidence_hash=$4`, taskID, nodeKey, strings.TrimSpace(text), evHash)
	}(t, taskID, body.NodeKey, evHash)

	writeJSON(w, http.StatusAccepted, map[string]any{"status": "pending"})
}

// candoAnnotateStatus 查询某个节点的细化状态。
func (s *Server) candoAnnotateStatus(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	nodeKey := r.URL.Query().Get("node_key")
	if strings.TrimSpace(nodeKey) == "" {
		writeErr(w, http.StatusBadRequest, "node_key 必填")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	anns := s.candoAnnotations(taskID)
	if a, ok := anns[nodeKey]; ok {
		writeJSON(w, http.StatusOK, a)
		return
	}
	writeErr(w, http.StatusNotFound, "还没有分析结果")
}

// ───────────────────── 下发指令（用户点「直接发给它」）─────────────────────

// candoRiskVerbs 是"不可逆动作"词表：命中即拒绝直发，只允许复制。
// 依据 docs/08 §10：危险动作必须多一道人工确认。
var candoRiskVerbs = []string{"改数据", "修改", "删除", "删掉", "写文件", "上传", "UPDATE", "INSERT", "DELETE", "DROP", "TRUNCATE"}

func candoIsRisky(text string) bool {
	up := strings.ToUpper(text)
	for _, v := range candoRiskVerbs {
		if strings.Contains(up, strings.ToUpper(v)) {
			return true
		}
	}
	return false
}

// candoSendHint 把用户确认过的指令写成 hint 节点并唤醒 planner。
func (s *Server) candoSendHint(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	var body struct {
		Text    string `json:"text"`
		NodeKey string `json:"node_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Text) == "" {
		writeErr(w, http.StatusBadRequest, "text 必填")
		return
	}
	if candoIsRisky(body.Text) {
		writeErr(w, http.StatusForbidden, "这条指令含不可逆动作，只能复制手动确认")
		return
	}
	id, err := t.Store.AddNode(db.KindHint, map[string]any{
		"text":   strings.TrimSpace(body.Text),
		"source": "cando",
		"node":   body.NodeKey,
	}, 0, "active", "user", nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "写入提示失败")
		return
	}
	t.NotifyHint([]string{strings.TrimSpace(body.Text)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}
