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
	"github.com/Autumn-27/artex/llmrec"
)

// 「能做到什么」地图（CARD-B01）。
//
// 设计约束（docs/08-详设-B01-能做到什么.md）：
//  1. 只读现有表（capabilities / findings / task_error_events / exploration_nodes）；
//  2. 任何字段映射不出来就显示「未确认」，不猜；
//  3. 唯一写入是 cando_annotations（AI 细化缓存）与 hint 节点（用户点「直接发给它」）。
//
// notImplemented 是 CARD-000 的占位响应（review.go 的 501 桩共用）。
func notImplemented(w http.ResponseWriter) {
	writeErr(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) registerCandoRoutes(mux *http.ServeMux) {
	s.registerLootRoutes(mux)
	mux.HandleFunc("GET /api/tasks/{id}/cando", s.candoGet)
	mux.HandleFunc("POST /api/tasks/{id}/cando/annotate", s.candoAnnotate)
	mux.HandleFunc("GET /api/tasks/{id}/cando/annotate", s.candoAnnotateStatus)
	mux.HandleFunc("POST /api/tasks/{id}/cando/send", s.candoSendHint)
	mux.HandleFunc("GET /api/system/hitrate", s.candoHitrate)
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

// candoEvidence 是卡片上可点开的证据（真实 finding 标题）。
type candoEvidence struct {
	Kind  string `json:"kind"`
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// candoNode 的语义按 kind 分别定义，**不要**用同一个状态枚举套所有节点：
//
//	entry   → 这条路线现在怎么样了（已打通 / 未试 / 封死）
//	can     → 能不能做（已证实 / 待证实）
//	data    → 拿到的东西核没核（已核实 / 未确认）
//	next    → 建议/可选动作，**没有状态**，只标注它是建议
//	blocked → 为什么堵（目标拒绝）
//	excluded→ 负结论
type candoNode struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"` // entry|can|data|next|blocked|excluded
	Title    string          `json:"title"`
	Body     string          `json:"body"`
	Badge    string          `json:"badge"`          // 该 kind 专用的一个词
	Tone     string          `json:"tone"`           // success|info|warn|danger|muted
	Live     bool            `json:"live,omitempty"` // 只有 entry 用：正在攻的呼吸点
	Items    []candoItem     `json:"items,omitempty"`
	Options  []candoOption   `json:"options,omitempty"`
	Cites    []int64         `json:"cites,omitempty"`
	Evidence []candoEvidence `json:"evidence,omitempty"`
	Prompt   string          `json:"prompt,omitempty"`
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

	// 正在攻：有 running 意图的摘要里出现该入口路径 → entry 呼吸点 + 徽标
	for li := range resp.Lanes {
		for ni := range resp.Lanes[li].Nodes {
			n := &resp.Lanes[li].Nodes[ni]
			if n.Kind == "entry" && summaryHits(runningSummaries, n.Title) {
				n.Live = true
				if n.Tone != "success" {
					n.Badge, n.Tone = "正在攻", "info"
				}
			}
		}
	}

	// loot：已证实（P2/P3）的能力项
	for _, c := range caps {
		eff := upgradeProof(c.Item, c.Can, c.ProofLevel, findings)
		if capability.ProofRank(eff) >= 2 && strings.TrimSpace(c.Can) != "" {
			resp.Loot = append(resp.Loot, candoLoot{
				Label: c.Can, Count: proofLabel(eff),
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
		// 路线整体状态：只要有一条能力被证实，这条路就是“已打通”
		anyProven := false
		for _, c := range cs {
			if capability.ProofRank(upgradeProof(c.Item, c.Can, c.ProofLevel, findings)) >= 2 {
				anyProven = true
				break
			}
		}
		entryBadge, entryTone := "未试", "muted"
		if anyProven {
			entryBadge, entryTone = "已打通", "success"
		}
		lane.Nodes = append(lane.Nodes, candoNode{
			ID: lane.Key, Kind: "entry", Title: key,
			Body:  "这条入口的地址 / 参数",
			Badge: entryBadge, Tone: entryTone,
		})
		for _, c := range cs {
			eff := upgradeProof(c.Item, c.Can, c.ProofLevel, findings)
			proven := capability.ProofRank(eff) >= 2
			canBadge, canTone := "待证实", "warn"
			if proven {
				canBadge, canTone = "已证实", "success"
			}
			lane.Nodes = append(lane.Nodes, candoNode{
				ID: "cap:" + c.Item, Kind: "can",
				Title: nz2(c.Can, "未命名能力"),
				Body:  nz2(c.Plain, nz2(c.Evidence, "未确认")),
				Badge: canBadge, Tone: canTone,
				Cites:    c.SourceNodeIDs,
				Evidence: evidenceFor(findings, c.SourceNodeIDs),
				Prompt:   s.candoPrompt(t, c.Surface, nz2(c.NextIfApproved, c.Can)),
			})
			if strings.TrimSpace(c.Impact) != "" {
				dataBadge, dataTone := "未确认", "muted"
				if proven {
					dataBadge, dataTone = "已核实", "success"
				}
				lane.Nodes = append(lane.Nodes, candoNode{
					ID: "data:" + c.Item, Kind: "data", Title: "拿到了什么",
					Body:  c.Impact,
					Badge: dataBadge, Tone: dataTone,
					Cites: c.SourceNodeIDs, Evidence: evidenceFor(findings, c.SourceNodeIDs),
				})
			}
			// “下一步”是建议，**不给状态**，避免出现“下一步 / 已做成”这种矛盾
			if strings.TrimSpace(c.NextIfApproved) != "" {
				lane.Nodes = append(lane.Nodes, candoNode{
					ID: "next:" + c.Item, Kind: "next", Title: "下一步可以这么走",
					Body: c.NextIfApproved, Badge: "建议", Tone: "info",
				})
			}
		}
		// 决策点：五类走法（只有已打通的路才需要选）
		if anyProven {
			lane.Nodes = append(lane.Nodes, candoNode{
				ID: "opt:" + key, Kind: "next", Title: "到这里有 5 种走法",
				Body: "点开右侧，按「要证据 / 要规模 / 要权限」自己选", Badge: "5 选 1", Tone: "info",
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
			Body: "能力卡还没派生出来，先按漏洞列表展示", Badge: "未归类", Tone: "muted",
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
				Badge: nz2(p.Severity, "info"), Tone: "warn",
				Cites:    []int64{f.ID},
				Evidence: evidenceFor(findings, []int64{f.ID}),
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
		Body:  fmt.Sprintf("目标侧拒绝 %d 次（403/405/Allow 不含等）。这是它说不，不是我打不通。", counts["target_blocked"]),
		Badge: "目标拒绝", Tone: "danger",
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
		Body: body, Badge: fmt.Sprintf("%d 项", len(negatives)), Tone: "muted"}
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

// evidenceFor 把节点引用的 id 变成「可点开的证据标题」。
func evidenceFor(findings []*db.Node, ids []int64) []candoEvidence {
	if len(ids) == 0 {
		return nil
	}
	byID := map[int64]string{}
	for _, f := range findings {
		var p struct {
			Name      string `json:"name"`
			VulnClass string `json:"vulnclass"`
			Summary   string `json:"summary"`
		}
		_ = json.Unmarshal(f.Payload, &p)
		t := nz2(p.Name, nz2(p.VulnClass, "漏洞"))
		if r := []rune(nz2(p.Summary, t)); len(r) > 40 {
			t = string(r[:40]) + "…"
		}
		byID[f.ID] = t
	}
	out := make([]candoEvidence, 0, len(ids))
	for _, id := range ids {
		title, ok := byID[id]
		if !ok {
			title = fmt.Sprintf("节点 #%d", id)
		}
		out = append(out, candoEvidence{Kind: "finding", ID: id, Title: title})
	}
	return out
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
		ctx, cancel := context.WithTimeout(llmrec.WithWorker(context.Background(), "cando"), 60*time.Second)
		defer cancel()
		user := fmt.Sprintf("目标：%s\n要分析的节点：%s\n任务描述：%s\n请按规矩写 3–5 句。", tk.Goal, nodeKey, tk.Description)
		rt := &taskLLMRuntime{s: s, taskID: tk.ID, agentKey: "adviser"}
		text, err := collectText(ctx, rt, true, candoSystemPrompt, user, 1200)
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

// ───────────────── 证明级别就地升级（显示层，不改库）─────────────────

// candoCapKeywords 把能力项映射到漏洞关键词。命中即认为"这条能力被真实证明了"。
// 只覆盖常见项；未列出的能力保持原级别（宁可不升级，也不虚报）。
var candoCapKeywords = map[string][]string{
	"任意文件读取": {"任意文件读取", "文件读取", "路径穿越", "path_traversal", "LFI", "目录穿越"},
	"口令哈希":   {"哈希", "hash", "MD5", "NTLM", "口令"},
	"命令执行":   {"命令执行", "RCE", "代码执行", "cmd", "webshell", "后门"},
	"SQL注入":  {"SQL", "注入", "injection", "堆叠", "盲注"},
	"数据读取":   {"跨库", "数据读取", "拖库", "用户表", "订单", "卡密"},
	"后台登录":   {"后台", "登录接管", "弱口令", "默认口令", "管理员"},
	"目录枚举":   {"目录", "枚举", "扫描"},
}

// upgradeProof 返回该能力项应有的证明级别：原级别更高则保留原级别。
func upgradeProof(capItem, capCan, proof string, findings []*db.Node) string {
	if capability.ProofRank(proof) >= 2 {
		return proof
	}
	text := capItem + " " + capCan
	for _, kw := range candoCapKeywords {
		matched := false
		for _, k := range kw {
			if strings.Contains(text, k) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		// 关键词组命中：看是否有对应 finding 带证据
		for _, f := range findings {
			var p struct {
				Name      string `json:"name"`
				VulnClass string `json:"vulnclass"`
				Evidence  string `json:"evidence"`
			}
			_ = json.Unmarshal(f.Payload, &p)
			hay := p.Name + " " + p.VulnClass
			// 只要求"存在同关键词的 finding"：能入库说明已过三门/有证据，
			// 不依赖 payload 里是否带 evidence 文本（不同版本字段不一致）。
			for _, k := range kw {
				if strings.Contains(hay, k) {
					return "P2"
				}
			}
		}
	}
	return proof
}

// ───────────────── 机制命中率看板（E01 度量，只读）─────────────────

// hitrateTables 是"二开机制有没有真的被写"的 16 张表（docs/00 §1）。
var hitrateTables = []string{
	"capabilities", "assets_owned", "asset_checks", "assumptions", "routes",
	"evidence_timeline", "finding_traffic_bindings", "finding_cases",
	"creds_vault", "recheck_state", "task_intelligence", "skill_usage",
	"tool_usage", "cando_annotations", "review_runs", "review_notes",
}

// candoHitrate 返回某任务的机制命中情况 + 工具使用比。
// 用途：每轮真实任务跑完看一眼，任一项为 0 说明对应机制仍是"空表"。
func (s *Server) candoHitrate(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.URL.Query().Get("task"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	out := map[string]int{}
	for _, tb := range hitrateTables {
		var n int
		var q string
		args := []any{taskID}
		switch tb {
		case "review_notes":
			q = `SELECT count(*) FROM review_notes WHERE task_id=$1`
		case "assets_owned":
			q = `SELECT count(*) FROM assets_owned WHERE first_task_id=$1`
		case "tool_usage":
			q = `SELECT count(*) FROM tool_usage WHERE task_id=$1`
		case "asset_checks":
			// asset_checks 没有 task_id：按资产回指持有台账的首次任务。
			q = `SELECT count(*) FROM asset_checks ac
				WHERE EXISTS (SELECT 1 FROM assets_owned ao
					WHERE ao.asset_id = ac.asset_id AND ao.first_task_id = $1)`
		case "finding_traffic_bindings":
			// 绑定表只有 finding_id：按 finding 回指任务。
			q = `SELECT count(*) FROM finding_traffic_bindings b
				WHERE EXISTS (SELECT 1 FROM findings f
					WHERE f.id = b.finding_id AND f.task_id = $1)`
		case "recheck_state":
			// 单例表（id=1），全库一行就是全局复查状态；保留 key，值取全表计数。
			q = `SELECT count(*) FROM recheck_state`
			args = nil
		default:
			q = `SELECT count(*) FROM ` + tb + ` WHERE task_id=$1`
		}
		// key 必须都在：查询失败也保留 0，避免前端少一格。
		out[tb] = 0
		if err := s.m.pg.QueryRow(q, args...).Scan(&n); err == nil {
			out[tb] = n
		}
	}
	// 工具使用比（activity 里 tool_use 的顶词分布）
	tools := map[string]int{}
	if rows, err := s.m.pg.Query(`SELECT substring(summary from '^[A-Za-z_]+') AS name, count(*)
		FROM activity WHERE kind='tool_use'
		  AND exploration_id = (SELECT exploration_id FROM tasks WHERE id=$1)
		GROUP BY name ORDER BY 2 DESC LIMIT 20`, taskID); err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			var n int
			if rows.Scan(&name, &n) == nil {
				tools[name] = n
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tables": out, "tools": tools})
}
