package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// 能做到什么（战果表）：按"渗透里常见的可得项"出一张表，
// 只列这个站**相关**的行（有证据 或 试过），并用 ✓ / 试过未成 标注。
//
// 用户要的形态：不是在某个页面里点开才有数据——数据是**实时从库里算的**，
// 面板只是把它显示出来；面板关着也不影响判定。
//
// 状态永远由规则算；AI 只负责在按需调用时写解释（见 loot_ai.go）。

type lootObject struct {
	Name   string `json:"name"`
	Fields string `json:"fields"`
}

type lootRow struct {
	Key        string          `json:"key"`
	Label      string          `json:"label"`
	Status     string          `json:"status"` // done|run|tried|todo|na
	Why        string          `json:"why"`
	Plain      string          `json:"plain,omitempty"`
	Objects    []lootObject    `json:"objects,omitempty"`
	ProofState string          `json:"proof_state,omitempty"` // real|reachable|tried
	Hint       string          `json:"hint,omitempty"`
	NextPrompt string          `json:"next_prompt,omitempty"` // 确定性模板：可直接粘贴到主会话的下一步指令
	Evidence   []candoEvidence `json:"evidence,omitempty"`

	// evidenceHash 只用于 L1 解释缓存键，不进入 JSON。
	evidenceHash string
}

type lootRowDef struct {
	key   string
	label string
	kws   []string
}

var lootRows = []lootRowDef{
	// 2026-10-25（#22）：认证绕过类的洞标题写"绕过口令直接登录为 Admin"，
	// 旧关键词表（后台/登录接管/弱口令/默认口令/管理员）一个都不中 → 明明进了后台却标"差一步"。
	{"admin", "管理员 / 后台登录", []string{"后台", "登录接管", "弱口令", "默认口令", "管理员", "认证绕过", "登录绕过", "绕过认证", "Hello Admin", "sign off"}},
	{"db", "数据库查询", []string{"SQL", "注入", "跨库", "盲注"}},
	{"data", "数据拉取（用户/订单/卡密）", []string{"用户表", "订单", "卡密", "手机号", "邮箱", "数据读取", "拖库"}},
	{"fileread", "读服务器文件", []string{"文件读取", "路径穿越", "目录穿越", "LFI", "源码泄露"}},
	{"filewrite", "写文件 / 上传", []string{"上传", "写入", "PUT", "落盘", "webshell"}},
	{"rce", "服务器命令执行", []string{"命令执行", "代码执行", "RCE", "shell", "后门", "xp_cmdshell"}},
	{"cred", "凭据（口令 / 哈希 / 连接串）", []string{"凭据", "哈希", "口令", "连接串", "NTLM", "MD5"}},
	{"lateral", "内网横向", []string{"横向", "内网", "隧道", "跳板"}},
}

// lootFinding 是一行 finding 的只读摘要。
//   - text 只含 vulnclass/name/summary，用于保持原有的行匹配语义；
//   - evidence 是 payload.evidence 的纯文本，只用于对象/字段/证明级别抽取。
type lootFinding struct {
	id       int64
	title    string
	text     string
	summary  string
	evidence string
}

func (s *Server) registerLootRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/loot", s.taskLoot)
	mux.HandleFunc("POST /api/tasks/{id}/loot/explain", s.lootExplain)
	mux.HandleFunc("POST /api/tasks/{id}/loot/suggest", s.lootSuggest)
	mux.HandleFunc("POST /api/tasks/{id}/loot/state", s.lootSetState)
	mux.HandleFunc("POST /api/tasks/{id}/loot/seen", s.lootMarkSeen)
}

// lootSeenFindingID 读"上次看到哪儿"的游标（0014）。
// 读不到（没库 / 表还不存在 / 从没看过）一律返回 0 = 全都算未读。
func (s *Server) lootSeenFindingID(ctx context.Context, taskID int64) int64 {
	if s.m == nil || s.m.pg == nil || taskID == 0 {
		return 0
	}
	var seen int64
	if err := s.m.pg.QueryRowContext(ctx,
		`SELECT seen_finding_id FROM loot_seen WHERE task_id=$1`, taskID).Scan(&seen); err != nil {
		return 0
	}
	return seen
}

// lootMarkSeen 把游标推到当前任务的最大 finding id —— 人打开战果面板就调一次。
// 只写游标，不动任何战果/证据：未读是显示层的东西，不能反过来改判定。
func (s *Server) lootMarkSeen(w http.ResponseWriter, r *http.Request) {
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
	var maxID int64
	for _, f := range fs {
		if f.id > maxID {
			maxID = f.id
		}
	}
	if _, err := s.m.pg.ExecContext(r.Context(), `INSERT INTO loot_seen(task_id,seen_finding_id,updated_at)
		VALUES($1,$2,now())
		ON CONFLICT (task_id) DO UPDATE SET seen_finding_id=GREATEST(loot_seen.seen_finding_id, EXCLUDED.seen_finding_id), updated_at=now()`,
		taskID, maxID); err != nil {
		writeErr(w, http.StatusInternalServerError, "标记已读失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "seen_finding_id": maxID, "new_since": 0})
}

func (s *Server) taskLoot(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	fs := s.lootFindings(t)
	rows := s.lootRowsForTask(r.Context(), t, fs)

	// total 只数"适用的行"：`不适用` 是用户主动排除的，不能把分母做大。
	done, todo, na := 0, 0, 0
	for _, row := range rows {
		switch row.Status {
		case "done":
			done++
		case "todo":
			todo++
		case "na":
			na++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"goal":     t.Goal,
		"done":     done,
		"todo":     todo,
		"na":       na,
		"total":    len(rows) - na,
		"rows":     rows,
		"findings": len(fs),
	})
}

// lootStateRe 限定人工标记的 row_key：内置行是小写 key，AI 推断行是 slug。
var lootStateRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// lootSetState 记住"这行还没试"（todo）或"这行跟我这个站无关"（na）。
// state 传空串 = 取消标记（回到规则算出来的状态）。
//
// 只写 loot_row_states，不改 findings/capabilities：证据永远优先于人的标记
// （有证据的行即使标了 na 仍然显示 done），见 0011 迁移的注释。
func (s *Server) lootSetState(w http.ResponseWriter, r *http.Request) {
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
		State  string `json:"state"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "row_key / state 必填")
		return
	}
	rowKey := strings.TrimSpace(body.RowKey)
	state := strings.TrimSpace(body.State)
	if !lootStateRe.MatchString(rowKey) {
		writeErr(w, http.StatusBadRequest, "row_key 不合法")
		return
	}
	if state != "" && state != "todo" && state != "na" {
		writeErr(w, http.StatusBadRequest, "state 只能是 todo / na / 空")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	if state == "" {
		if _, err := s.m.pg.Exec(`DELETE FROM loot_row_states WHERE task_id=$1 AND row_key=$2`, taskID, rowKey); err != nil {
			writeErr(w, http.StatusInternalServerError, "取消标记失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "row_key": rowKey, "state": ""})
		return
	}
	if _, err := s.m.pg.Exec(`INSERT INTO loot_row_states(task_id,row_key,state,updated_at)
		VALUES($1,$2,$3,now())
		ON CONFLICT (task_id,row_key) DO UPDATE SET state=EXCLUDED.state, updated_at=now()`,
		taskID, rowKey, state); err != nil {
		writeErr(w, http.StatusInternalServerError, "标记失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "row_key": rowKey, "state": state})
}

// lootRowStates 一次取全任务的人工标记；失败就当没有标记（读不到不阻塞战果表）。
func (s *Server) lootRowStates(ctx context.Context, taskID int64) map[string]string {
	out := map[string]string{}
	if s.m == nil || s.m.pg == nil || taskID == 0 {
		return out
	}
	rows, err := s.m.pg.QueryContext(ctx, `SELECT row_key,state FROM loot_row_states WHERE task_id=$1`, taskID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			out[k] = v
		}
	}
	return out
}

// lootFindings 读任务里确认的漏洞，并保留可用于抽取的 evidence 文本。
func (s *Server) lootFindings(t *Task) []lootFinding {
	findings, _ := t.Store.ListByKind(db.KindFinding, 500)
	out := make([]lootFinding, 0, len(findings))
	for _, f := range findings {
		var p struct {
			Name      string          `json:"name"`
			VulnClass string          `json:"vulnclass"`
			Summary   string          `json:"summary"`
			Evidence  json.RawMessage `json:"evidence"`
		}
		_ = json.Unmarshal(f.Payload, &p)
		title := p.Name
		if strings.TrimSpace(title) == "" {
			title = p.VulnClass
		}
		if r := []rune(title); len(r) > 46 {
			title = string(r[:46]) + "…"
		}
		out = append(out, lootFinding{
			id:       f.ID,
			title:    title,
			text:     p.VulnClass + " " + p.Name + " " + p.Summary,
			summary:  p.Summary,
			evidence: jsonText(p.Evidence),
		})
	}
	return out
}

// lootRowsForTask 是实时战果表的唯一计算入口（GET 与两个 AI 接口共用）。
func (s *Server) lootRowsForTask(ctx context.Context, t *Task, fs []lootFinding) []lootRow {
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	// 能力卡（有 impact / 已证实的，用来补 hint 与 "试过未成"）
	caps, _ := s.loadCapabilityCards(ctx, taskID)
	// 在跑的意图（用来标"正在试"）
	running := s.candoRunningIntents(taskID)

	// 人工标记（todo / na）：只在没有证据时才决定这一行显不显示。
	states := s.lootRowStates(ctx, taskID)

	rows := make([]lootRow, 0, len(lootRows))
	for _, def := range lootRows {
		row := lootRow{Key: def.key, Label: def.label, Status: "blank"}
		matched := make([]lootFinding, 0)
		for _, f := range fs {
			if containsAnyFold(f.text, def.kws...) {
				matched = append(matched, f)
				row.Status = "done"
				row.Why = "已确认"
				row.Evidence = append(row.Evidence, candoEvidence{Kind: "finding", ID: f.id, Title: f.title})
			}
		}
		for _, c := range caps {
			text := c.Item + " " + c.Can
			if !containsAnyFold(text, def.kws...) {
				continue
			}
			if row.Hint == "" {
				row.Hint = c.NextIfApproved
			}
			if row.Status == "blank" && c.Confidence == "observed" {
				row.Status = "tried"
				row.Why = "试过，还没成功"
			}
		}
		if row.Status == "blank" {
			for _, rs := range running {
				if containsAnyFold(rs, def.kws...) {
					row.Status = "run"
					row.Why = "worker 正在试"
					break
				}
			}
		}
		if row.Status == "blank" {
			// 规则没有证据 → 看人有没有标记过。没有标记就不显示（保持原语义）。
			if !lootApplyState(&row, states[def.key]) {
				continue
			}
		}

		if len(row.Evidence) > 3 {
			row.Evidence = row.Evidence[:3]
		}
		row.Objects = extractLootObjects(matched)
		row.ProofState = lootProofState(row.Status, matched)
		row.Plain = lootPlain(def.key, def.label, row.Why, row.Objects, row.Status)
		if row.Status == "na" {
			// 不适用 = 用户主动排除，没有"下一步"，也不该被复制成派单指令。
			row.NextPrompt = ""
		} else if row.Status == "todo" {
			row.NextPrompt = lootTryPrompt(def.key, def.label, t.Goal)
		} else {
			row.NextPrompt = lootNextPrompt(def.key, row.Objects, row.Hint, t.Goal)
		}
		row.evidenceHash = lootRowHash(def.key, row, matched)
		rows = append(rows, row)
	}
	// 排序：已拿到 → 正在试 → 试过未成 → 还没试 → 不适用（na 沉底，别占视线）。
	lootRank := func(s string) int {
		switch s {
		case "done":
			return 0
		case "run":
			return 1
		case "tried":
			return 2
		case "todo":
			return 3
		default:
			return 4
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if ri, rj := lootRank(rows[i].Status), lootRank(rows[j].Status); ri != rj {
			return ri < rj
		}
		return rows[i].Key < rows[j].Key
	})
	return rows
}

// lootApplyState 把人工标记套到一行"规则没算出任何证据"的行上。
// 返回 false = 这行不显示（没标记）。证据优先的规则在调用侧保证：
// 只有 Status 还是 blank 的行才会走到这里，已拿到/正在试/试过未成的行不受标记影响。
func lootApplyState(row *lootRow, state string) bool {
	switch state {
	case "na":
		row.Status = "na"
		row.Why = "这个站不适用（你标的）"
		return true
	case "todo":
		row.Status = "todo"
		row.Why = "还没试（你标的）"
		return true
	}
	return false
}

// lootProofState 只做确定性判定：证据里出现"真的导出了"的字样才算 real。
func lootProofState(status string, matched []lootFinding) string {
	if status == "tried" {
		return "tried"
	}
	if status == "todo" || status == "na" {
		return ""
	}
	if len(matched) == 0 {
		return ""
	}
	for _, f := range matched {
		if containsAnyFold(f.evidence, "导出", "导出为", "已读出", "返回 200 且正文含", "跨库读出") {
			return "real"
		}
	}
	return "reachable"
}

func lootPlain(key, label, why string, objects []lootObject, status string) string {
	if status == "tried" {
		return "试过，没成：" + lootTriedReason(why, label)
	}
	if status == "run" {
		return "正在试：" + label
	}
	if status == "todo" {
		return "还没试：「" + label + "」这一项你标了要做，还没有证据"
	}
	if status == "na" {
		return "不适用：这个站没有「" + label + "」这类东西（你标的）"
	}
	names := lootObjectCNList(objects, 2)
	switch key {
	case "data":
		if len(names) == 0 {
			return "能读到它的业务数据"
		}
		return "能把它的" + strings.Join(names, "、") + "拉出来"
	case "admin":
		return "能改它的账号口令并登录进后台"
	case "db":
		if len(names) == 0 {
			return "能查它的数据库"
		}
		return "能查它数据库里的" + strings.Join(names, "、")
	case "fileread":
		return "能读它服务器上的文件"
	case "filewrite":
		return "能往它服务器上写文件"
	case "rce":
		return "能在它服务器上执行命令"
	case "cred":
		return "能拿到它的账号口令或哈希"
	case "lateral":
		return "能从它这里往内网横向"
	default:
		return "已经能做到：" + label
	}
}

// lootHostRe 从任务 goal 里取第一个目标 host。
var lootHostRe = regexp.MustCompile(`(?i)https?://([^/\s（）()]+)`)

// lootGoalHost 返回 goal 里的第一个 host；取不到写“目标”。
func lootGoalHost(goal string) string {
	m := lootHostRe.FindStringSubmatch(goal)
	if m == nil {
		return "目标"
	}
	host := strings.TrimSpace(m[1])
	host = strings.TrimRight(host, "。，,.;；、")
	if host == "" {
		return "目标"
	}
	return host
}

// lootNextPrompt 生成“可直接粘贴到主会话”的下一步指令。
// 纯模板拼接，不调模型：同样的行永远产出同样的文案。
func lootNextPrompt(key string, objects []lootObject, hint, goal string) string {
	host := lootGoalHost(goal)
	names := lootObjectCNList(objects, 2)
	obj := "数据"
	if len(names) > 0 {
		obj = strings.Join(names, "、")
	}
	switch key {
	case "data":
		return "沿已确认的入口，把它的" + obj + "各拉 3 条样本证明能读到（只读，不要整表导出）。只打 " + host + "，20 分钟内给我结论。"
	case "admin":
		return "用拿到的方式登录它后台，确认能看到用户/订单列表，取 1 条样本作为证据，不要修改任何数据。只打 " + host + "，15 分钟内给我结论。"
	case "cred":
		return "用拿到的账号口令去验证：先试它自己的数据库/后台登录（只读验证），成功就停，别扩大范围。只打 " + host + "，15 分钟内给我结论。"
	case "fileread":
		return "读它服务器上的关键文件（源码/配置文件）证明任意文件读取，取关键 20 行作为证据，不要写文件。只打 " + host + "，15 分钟内给我结论。"
	case "filewrite":
		return "换一种写入手法（换目录/换扩展名/换上传点）再试一次，落一个最小验证文件并立即删掉。只打 " + host + "，20 分钟内给我结论。"
	case "rce":
		h := strings.TrimSpace(hint)
		if h != "" {
			return "按 hint 里的备选手法（" + h + "）再试一次，30 分钟内没有命令回显就判死，写负结论并停止这条方向。只打 " + host + "。"
		}
		return "按 hint 里的备选手法再试一次，30 分钟内没有命令回显就判死，写负结论并停止这条方向。只打 " + host + "。"
	case "hash":
		return "离线把已拿到的口令哈希还原成明文，先跑常用字典，出 1-2 个明文就够证明影响。不要碰目标站。"
	case "lateral":
		return "确认同网段还有哪些可达资产，只做只读探测，列出候选后停。只打 " + host + "。"
	default:
		return "沿这条已确认的方向再取一个最小样本作为证据。只打 " + host + "，20 分钟内给我结论。"
	}
}

// lootTryPrompt 是「还没试」行的下一步：与 lootNextPrompt 的区别是
// **不假设已经有入口**，所以从"先找到入口"开始写，仍然可整句粘贴。
func lootTryPrompt(key, label, goal string) string {
	host := lootGoalHost(goal)
	switch key {
	case "admin":
		return "先找它的后台入口（目录/指纹/robots），再试默认口令与弱口令；能进去就取 1 条样本作为证据，不要改任何数据。只打 " + host + "，20 分钟内给我结论。"
	case "db":
		return "先找注入点（GET/POST 参数、Cookie、搜索框），用布尔/报错两种手法各验一次；能读出库名就停并报告。只打 " + host + "，20 分钟内给我结论。"
	case "data":
		return "先找能返回业务数据的接口（列表/详情/导出），确认未授权或可越权后各取 3 条样本证明能读到（只读，不要整表导出）。只打 " + host + "，20 分钟内给我结论。"
	case "fileread":
		return "先找文件读取参数（download/file/path/模板），用相对路径读一个无害文件（如 robots.txt）确认，再读配置文件取证，不要写文件。只打 " + host + "，20 分钟内给我结论。"
	case "filewrite":
		return "先找上传点与写入参数，落一个最小验证文件并立即删掉；换目录/换扩展名各试一次。只打 " + host + "，20 分钟内给我结论。"
	case "rce":
		return "先找命令/代码执行的可能入口（上传后解析、模板、反序列化），用最小回显（id）证明，不要留后门。只打 " + host + "，20 分钟内给我结论。"
	case "cred":
		return "先从配置泄露/源码/日志/备份里找凭据，拿到后只做只读验证（登录或连库 SELECT 1），成功就停。只打 " + host + "，20 分钟内给我结论。"
	case "lateral":
		return "先确认这台机器上有什么（网段/凭据/代理），只做只读探测并列出可达候选，不要横向登录。只打 " + host + "，20 分钟内给我结论。"
	default:
		return "试一下「" + label + "」这一项：只做最小验证，取到 1 条样本就停；20 分钟内没有结果就判死并写负结论。只打 " + host + "。"
	}
}

func lootTriedReason(why, label string) string {
	reason := strings.TrimSpace(why)
	for _, p := range []string{"试过，还没成功", "试过，尚未成功", "试过，没成", "试过"} {
		reason = strings.TrimSpace(strings.TrimPrefix(reason, p))
	}
	reason = strings.Trim(reason, "，。；;、：: ")
	if reason == "" {
		reason = label + "这条路还没打通"
	}
	return reason
}

// lootRowHash 把"这一行看到的证据"压成缓存键：证据文本/标题/对象任一变化即失效。
func lootRowHash(key string, row lootRow, matched []lootFinding) string {
	var b strings.Builder
	b.WriteString(key)
	b.WriteString("\x00")
	b.WriteString(row.Label)
	b.WriteString("\x00")
	b.WriteString(row.Status)
	b.WriteString("\x00")
	b.WriteString(row.Why)
	b.WriteString("\x00")
	b.WriteString(row.Plain)
	b.WriteString("\x00")
	b.WriteString(row.ProofState)
	for _, o := range row.Objects {
		b.WriteString("\x00o:")
		b.WriteString(o.Name)
		b.WriteString("\x00")
		b.WriteString(o.Fields)
	}
	for _, e := range row.Evidence {
		b.WriteString("\x00e:")
		b.WriteString(strconv.FormatInt(e.ID, 10))
		b.WriteString("\x00")
		b.WriteString(e.Title)
	}
	for _, f := range matched {
		b.WriteString("\x00f:")
		b.WriteString(strconv.FormatInt(f.id, 10))
		b.WriteString("\x00")
		b.WriteString(f.title)
		b.WriteString("\x00")
		b.WriteString(f.text)
		b.WriteString("\x00")
		b.WriteString(f.evidence)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:16])
}

// ───────────────────────── 确定性对象/字段抽取 ─────────────────────────

// 对象只认两种来源，其余一律丢弃（避免把 ASP/VBScript 代码标识符当表名）：
//  1. schema 限定名：库.dbo.表（例：acublog.dbo.users）
//  2. 已知表名词表（大小写不敏感、整词匹配）
var (
	// schema 限定名：库名.dbo.表名
	lootSchemaRe = regexp.MustCompile(`(?i)\b[A-Za-z_]\w*\.dbo\.[A-Za-z_]\w*\b`)
	// 已知表名词表（最长优先，整词匹配）
	lootKnownTableRe = regexp.MustCompile(`(?i)\b(?:` + strings.Join(lootKnownTables, "|") + `)\b`)
)

// lootKnownTables 是唯一允许的“裸表名”来源。
var lootKnownTables = []string{
	"order_list", "card_keys", "accounts", "payments", "threads", "forums",
	"admins", "members", "orders", "cards", "posts", "users", "goods",
	"logs", "user", "card", "admin", "forum",
}

// lootObjectBlacklist 是代码标识符/框架对象黑名单（按 . 分段精确比较，
// 避免误伤 acublog 这类库名）。
var lootObjectBlacklist = map[string]bool{
	"conn": true, "connection": true, "request": true, "response": true,
	"server": true, "session": true, "application": true, "recordset": true,
	"rs": true, "cmd": true, "obj": true, "object": true, "document": true,
	"window": true, "system": true, "string": true, "math": true,
	"json": true, "asp": true, "sql": true,
}

// lootObjectMethodParts 出现在第二段及以后就丢弃（例：Request.QueryString）。
var lootObjectMethodParts = map[string]bool{
	"execute": true, "querystring": true, "form": true, "cookies": true,
	"servervariables": true, "createobject": true, "mappath": true,
	"write": true, "open": true, "close": true, "item": true,
	"count": true, "value": true, "text": true,
}

// lootFieldWords 是字段白名单：只摘录这些中文词。
var lootFieldWords = []string{
	"用户名", "密码", "口令", "哈希", "手机号", "邮箱", "卡号", "订单", "卡密",
	"地址", "金额", "时间", "标题", "正文", "作者", "账号", "凭据", "连接串",
}

// lootFieldWindow 只从对象出现位置之后看这么多个字符（中文按字算）。
const lootFieldWindow = 120

// lootObjectCNNames 是对象中文名映射（裸表名，小写）。
var lootObjectCNNames = map[string]string{
	"users": "用户表", "user": "用户表", "accounts": "用户表", "members": "用户表",
	"orders": "订单表", "order_list": "订单表",
	"cards": "卡密表", "card": "卡密表", "card_keys": "卡密表",
	"admins": "管理员表", "admin": "管理员表",
	"forum": "论坛帖子", "forums": "论坛帖子", "posts": "论坛帖子", "threads": "论坛帖子",
	"goods":    "商品表",
	"payments": "支付流水",
	"logs":     "日志",
}

type lootObjCandidate struct {
	name       string
	start, end int
}

func extractLootObjects(matched []lootFinding) []lootObject {
	out := []lootObject{}
	index := map[string]int{}
	for _, f := range matched {
		text := strings.TrimSpace(f.text + "\n" + f.evidence)
		if text == "" {
			continue
		}
		text = strings.NewReplacer("[", "", "]", "", "`", "").Replace(text)
		for _, c := range lootObjectCandidates(text) {
			key := strings.ToLower(c.name)
			fields := lootFieldsAfter(text, c.end)
			if i, ok := index[key]; ok {
				out[i].Fields = lootMergeFields(out[i].Fields, fields)
				continue
			}
			if len(out) >= 5 {
				continue
			}
			out = append(out, lootObject{Name: c.name, Fields: fields})
			index[key] = len(out) - 1
		}
	}
	return out
}

// lootObjectCandidates 只产出 schema 限定名与已知表名两种候选；
// schema 限定名里包含的表名不会重复产出。
func lootObjectCandidates(text string) []lootObjCandidate {
	cands := make([]lootObjCandidate, 0)
	ranges := make([][2]int, 0, 4)
	for _, m := range lootSchemaRe.FindAllStringIndex(text, -1) {
		ranges = append(ranges, [2]int{m[0], m[1]})
		if c, ok := newLootObjCandidate(text[m[0]:m[1]], m[0], m[1]); ok {
			cands = append(cands, c)
		}
	}
	for _, m := range lootKnownTableRe.FindAllStringIndex(text, -1) {
		if insideLootRanges(m[0], m[1], ranges) {
			continue
		}
		if c, ok := newLootObjCandidate(text[m[0]:m[1]], m[0], m[1]); ok {
			cands = append(cands, c)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].start != cands[j].start {
			return cands[i].start < cands[j].start
		}
		return cands[i].end < cands[j].end
	})
	return cands
}

func insideLootRanges(start, end int, ranges [][2]int) bool {
	for _, r := range ranges {
		if start >= r[0] && end <= r[1] {
			return true
		}
	}
	return false
}

func newLootObjCandidate(name string, start, end int) (lootObjCandidate, bool) {
	name = strings.Trim(strings.TrimSpace(name), ".,;:()[]{}<>\"'`")
	if !acceptLootObjectName(name) {
		return lootObjCandidate{}, false
	}
	return lootObjCandidate{name: name, start: start, end: end}, true
}

// acceptLootObjectName 只放行“裸表名”和“库.dbo.表”，其余（连接对象、方法调用等）丢弃。
func acceptLootObjectName(name string) bool {
	if name == "" || len(name) > 120 {
		return false
	}
	parts := strings.Split(strings.ToLower(name), ".")
	if len(parts) != 1 && len(parts) != 3 {
		return false
	}
	for i, p := range parts {
		if p == "" || lootObjectBlacklist[p] {
			return false
		}
		if i > 0 && lootObjectMethodParts[p] {
			return false
		}
	}
	return true
}

// lootFieldsAfter 只在对象结束位置之后 120 个字符内就近匹配字段白名单词，
// 匹配不到就返回空串（不把全文的字段挂到每个对象上）。
func lootFieldsAfter(text string, end int) string {
	if end < 0 || end >= len(text) {
		return ""
	}
	window := lootRunePrefix(text[end:], lootFieldWindow)
	if window == "" {
		return ""
	}
	type fieldHit struct {
		word string
		pos  int
	}
	hits := make([]fieldHit, 0, len(lootFieldWords))
	for _, w := range lootFieldWords {
		if p := strings.Index(window, w); p >= 0 {
			hits = append(hits, fieldHit{word: w, pos: p})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].pos < hits[j].pos })
	words := make([]string, 0, len(hits))
	for _, h := range hits {
		words = append(words, h.word)
	}
	return strings.Join(words, "、")
}

// lootRunePrefix 取 s 的前 n 个字符（按 rune 计，中文按字算）。
func lootRunePrefix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// lootMergeFields 合并两次就近匹配到的字段（去重保序）。
func lootMergeFields(a, b string) string {
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	for _, s := range []string{a, b} {
		for _, w := range strings.Split(s, "、") {
			w = strings.TrimSpace(w)
			if w == "" || seen[w] {
				continue
			}
			seen[w] = true
			out = append(out, w)
		}
	}
	return strings.Join(out, "、")
}

// lootObjectCN 映射对象中文名；schema 限定名取表名部分再映射，映射不到就用原名。
func lootObjectCN(name string) string {
	base := strings.TrimSpace(name)
	if i := strings.LastIndex(base, "."); i >= 0 {
		base = base[i+1:]
	}
	if cn, ok := lootObjectCNNames[strings.ToLower(base)]; ok {
		return cn
	}
	return name
}

// lootObjectCNList 取对象中文名，去重后最多 max 个。
func lootObjectCNList(objects []lootObject, max int) []string {
	out := make([]string, 0, max)
	seen := map[string]bool{}
	for _, o := range objects {
		n := strings.TrimSpace(o.Name)
		if n == "" {
			continue
		}
		cn := lootObjectCN(n)
		if cn == "" || seen[cn] {
			continue
		}
		seen[cn] = true
		out = append(out, cn)
		if len(out) >= max {
			break
		}
	}
	return out
}

// ───────────────────────── finding payload 文本化 ─────────────────────────

// jsonText 把 payload.evidence（常见是 {"by":...,"poc":"..."}，也可能是纯字符串）
// 摊平成纯文本；map 按 key 排序，保证同一份证据每次抽取结果一致。
func jsonText(raw json.RawMessage) string {
	if len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	var b strings.Builder
	appendJSONText(&b, v)
	return b.String()
}

func appendJSONText(b *strings.Builder, v any) {
	switch x := v.(type) {
	case string:
		b.WriteString(x)
		b.WriteString("\n")
	case []any:
		for _, e := range x {
			appendJSONText(b, e)
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			appendJSONText(b, x[k])
		}
	}
}

func containsAnyFold(hay string, needles ...string) bool {
	up := strings.ToUpper(hay)
	for _, n := range needles {
		if n != "" && strings.Contains(up, strings.ToUpper(n)) {
			return true
		}
	}
	return false
}
