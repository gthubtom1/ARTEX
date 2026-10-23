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
	Status     string          `json:"status"` // done|tried|blank
	Why        string          `json:"why"`
	Plain      string          `json:"plain,omitempty"`
	Objects    []lootObject    `json:"objects,omitempty"`
	ProofState string          `json:"proof_state,omitempty"` // real|reachable|tried
	Hint       string          `json:"hint,omitempty"`
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
	{"admin", "管理员 / 后台登录", []string{"后台", "登录接管", "弱口令", "默认口令", "管理员"}},
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
}

func (s *Server) taskLoot(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	fs := s.lootFindings(t)
	rows := s.lootRowsForTask(r.Context(), t, fs)

	done := 0
	for _, row := range rows {
		if row.Status == "done" {
			done++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"goal":     t.Goal,
		"done":     done,
		"total":    len(rows),
		"rows":     rows,
		"findings": len(fs),
	})
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
			continue
		}

		if len(row.Evidence) > 3 {
			row.Evidence = row.Evidence[:3]
		}
		row.Objects = extractLootObjects(matched)
		row.ProofState = lootProofState(row.Status, matched)
		row.Plain = lootPlain(def.key, def.label, row.Why, row.Objects, row.Status)
		row.evidenceHash = lootRowHash(def.key, row, matched)
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Status != rows[j].Status {
			return rows[i].Status == "done"
		}
		return rows[i].Key < rows[j].Key
	})
	return rows
}

// lootProofState 只做确定性判定：证据里出现"真的导出了"的字样才算 real。
func lootProofState(status string, matched []lootFinding) string {
	if status == "tried" {
		return "tried"
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
	obj := strings.TrimSpace(label)
	if len(objects) > 0 && strings.TrimSpace(objects[0].Name) != "" {
		obj = objects[0].Name
	}
	switch key {
	case "data":
		return "能把它的" + obj + "拉出来"
	case "admin":
		return "能改它的账号口令并登录进后台"
	case "db":
		return "能查它数据库里的" + obj
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

var (
	// 库名.dbo.表名 / xxx.users / schema.table
	lootQualifiedRe = regexp.MustCompile(`(?i)\b[A-Za-z_][A-Za-z0-9_$#]*(?:\.[A-Za-z_][A-Za-z0-9_$#]*){1,3}`)
	// SQL 里 from/join/update/table 后面的表名（也覆盖 dbo.users）。
	lootSQLTableRe = regexp.MustCompile(`(?i)\b(?:from|join|into|update|table|describe|desc|show\s+tables\s+from)\s+([A-Za-z_][A-Za-z0-9_$#]*(?:\.[A-Za-z_][A-Za-z0-9_$#]*){0,3})`)
	// 中文业务表名（可带后面的英文标识），例：用户表 users / 订单表 / 支付流水表。
	lootCNTableRe = regexp.MustCompile(`((?:用户|会员|账号|账户|订单|卡密|商品|支付|交易|流水|订阅|客户|员工|管理员|地址|手机|邮箱|库存|财务|发票|优惠券|资金|资产|日志|文件|学生|教师|病历|患者|课程|成绩|评论|文章|消息|配置|后台|权限|角色|部门|公司|项目|合同|工单|设备|车辆|房源|保单|积分|红包|钱包|余额|银行卡)[\p{Han}]{0,6}表)\s*[（(]?\s*([A-Za-z_][A-Za-z0-9_$#]*(?:\.[A-Za-z_][A-Za-z0-9_$#]*){0,3})?`)
	// 表名：users / 表 `users`
	lootCNTableIdentRe = regexp.MustCompile(`(?:表名|数据表|表)\s*[：:（(]?\s*([A-Za-z_][A-Za-z0-9_$#]*(?:\.[A-Za-z_][A-Za-z0-9_$#]*){0,3})`)
	// 英文表名后面直接跟中文“表/字段/包含”，例：users 表包含用户名、密码。
	lootIdentTableRe = regexp.MustCompile(`(?i)\b([A-Za-z_][A-Za-z0-9_$#]*(?:\.[A-Za-z_][A-Za-z0-9_$#]*){0,3})\s*(?:表|字段|包含|里有|中的)`)
)

var lootFieldWords = []string{
	"用户名", "密码", "口令", "哈希", "手机号", "邮箱", "卡号", "订单", "卡密", "地址", "时间", "金额", "标题", "正文", "作者",
}

var lootBadObjectNames = map[string]bool{
	"select": true, "from": true, "where": true, "join": true, "into": true,
	"update": true, "table": true, "values": true, "set": true, "and": true,
	"or": true, "not": true, "null": true, "true": true, "false": true,
	"the": true, "this": true, "that": true, "http": true, "https": true,
	"request": true, "response": true, "information_schema": true,
	"pg_catalog": true, "mysql": true, "sys": true, "master": true,
	"tempdb": true, "msdb": true, "sql": true, "api": true, "url": true,
	"uri": true, "html": true, "json": true, "xml": true, "php": true,
}

var lootBadTails = map[string]bool{
	"com": true, "net": true, "org": true, "cn": true, "io": true, "co": true,
	"uk": true, "de": true, "jp": true, "ru": true, "info": true, "biz": true,
	"top": true, "xyz": true, "online": true, "site": true, "app": true,
	"dev": true, "cloud": true, "local": true, "lan": true, "internal": true,
	"localhost": true, "html": true, "htm": true, "php": true, "asp": true,
	"aspx": true, "jsp": true, "js": true, "css": true, "json": true,
	"xml": true, "txt": true, "log": true, "exe": true, "dll": true,
	"zip": true, "rar": true, "7z": true, "gz": true, "tar": true,
	"pem": true, "key": true, "crt": true, "cer": true, "ini": true,
	"cfg": true, "conf": true, "bak": true, "sql": true, "db": true,
	"mdb": true, "md": true, "yaml": true, "yml": true, "go": true,
	"py": true, "sh": true, "bat": true, "ps1": true,
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
			name := strings.TrimSpace(c.name)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			fields := lootFieldsNear(text, c.start, c.end)
			if i, ok := index[key]; ok {
				if out[i].Fields == "" && fields != "" {
					out[i].Fields = fields
				}
				continue
			}
			if len(out) >= 5 {
				continue
			}
			out = append(out, lootObject{Name: name, Fields: fields})
			index[key] = len(out) - 1
		}
	}
	return out
}

func lootObjectCandidates(text string) []lootObjCandidate {
	cands := make([]lootObjCandidate, 0)
	add := func(name string, start, end int) {
		name = strings.TrimSpace(name)
		if !acceptLootObjectName(name) {
			return
		}
		cands = append(cands, lootObjCandidate{name: name, start: start, end: end})
	}

	for _, m := range lootQualifiedRe.FindAllStringIndex(text, -1) {
		add(text[m[0]:m[1]], m[0], m[1])
	}
	for _, m := range lootSQLTableRe.FindAllStringSubmatchIndex(text, -1) {
		if len(m) >= 4 && m[2] >= 0 {
			add(text[m[2]:m[3]], m[2], m[3])
		}
	}
	for _, m := range lootCNTableRe.FindAllStringSubmatchIndex(text, -1) {
		if len(m) < 6 {
			continue
		}
		if m[4] >= 0 && m[5] > m[4] {
			// 中文表名后面跟了英文名时，以英文名作为对象（更适合展示/核对）。
			add(text[m[4]:m[5]], m[4], m[5])
			continue
		}
		add(text[m[2]:m[3]], m[2], m[3])
	}
	for _, m := range lootCNTableIdentRe.FindAllStringSubmatchIndex(text, -1) {
		if len(m) >= 4 && m[2] >= 0 {
			add(text[m[2]:m[3]], m[2], m[3])
		}
	}
	for _, m := range lootIdentTableRe.FindAllStringSubmatchIndex(text, -1) {
		if len(m) >= 4 && m[2] >= 0 {
			add(text[m[2]:m[3]], m[2], m[3])
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

func acceptLootObjectName(name string) bool {
	name = strings.Trim(strings.TrimSpace(name), ".,;:()[]{}<>\"'")
	if len(name) < 3 || len(name) > 80 {
		return false
	}
	low := strings.ToLower(name)
	if lootBadObjectNames[low] || strings.HasPrefix(low, "www.") {
		return false
	}
	parts := strings.Split(low, ".")
	last := parts[len(parts)-1]
	if lootBadTails[last] || last == "" {
		return false
	}
	allDigits := true
	for _, r := range last {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return false
	}
	return true
}

// lootFieldsNear 只摘录证据里字面出现的中文白名单词，并排除表名自身。
func lootFieldsNear(text string, start, end int) string {
	lo := start - 80
	if lo < 0 {
		lo = 0
	}
	hi := end + 180
	if hi > len(text) {
		hi = len(text)
	}
	if lo >= hi {
		return ""
	}
	window := []byte(text[lo:hi])
	maskStart, maskEnd := start-lo, end-lo
	for i := maskStart; i < maskEnd && i < len(window); i++ {
		window[i] = ' '
	}
	masked := string(window)

	type fieldPos struct {
		word string
		pos  int
	}
	found := make([]fieldPos, 0, len(lootFieldWords))
	seen := map[string]bool{}
	for _, w := range lootFieldWords {
		if seen[w] {
			continue
		}
		if p := strings.Index(masked, w); p >= 0 {
			found = append(found, fieldPos{word: w, pos: p})
			seen[w] = true
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].pos < found[j].pos })
	words := make([]string, 0, len(found))
	for _, f := range found {
		words = append(words, f.word)
	}
	return strings.Join(words, "、")
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
