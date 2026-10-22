// Package dataloc 是「数据定位器」：把数据库探测的原始输出解析成
// 表名/行数/敏感列，并给出 D1–D4 的数据可达性证明级别。
//
// 纪律：这里只做**记录与标注**。PlanProbe 只生成只读 SQL 模板（information_schema
// → COUNT(*) → LIMIT 1 样本），绝不生成 mysqldump / SELECT * 全表 / UPDATE / DELETE / INSERT；
// 输出里出现导出能力（INTO OUTFILE / COPY ... TO 等）时只把级别标成 D4 并写明"仅标注不执行"。
package dataloc

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// 证明级别：D1 只有表名/列名；D2 有行数；D3 有敏感列且确认存在；D4 出现导出能力证据。
const (
	LevelNone  = "D0"
	LevelName  = "D1"
	LevelRows  = "D2"
	LevelSens  = "D3"
	LevelExpor = "D4"
	NoteNoExec = "仅标注不执行"
)

// Table 是一张被证明存在（至少能看到表名）的表。
// Rows 为 -1 表示未取得行数（例如只跑了 SHOW TABLES）；0 表示确认是空表。
type Table struct {
	Name             string   `json:"name"`
	Rows             int64    `json:"rows"`
	SensitiveColumns []string `json:"sensitive_columns,omitempty"`
}

// Result 是一次解析的结论。
type Result struct {
	Tables     []Table  `json:"tables"`
	ProofLevel string   `json:"proof_level"`
	Notes      []string `json:"notes,omitempty"`
}

// ───────────────────────── 敏感列识别 ─────────────────────────

// sensitiveKeywords 命中即视为敏感列（大小写不敏感，子串匹配）。
// 覆盖手机号/邮箱/身份证/地址/口令/令牌/密钥/银行卡等常见落库字段。
var sensitiveKeywords = []string{
	"phone", "mobile", "telephone", "tel", "手机", "电话",
	"email", "mail", "邮箱", "邮件",
	"id_card", "idcard", "idno", "id_no", "identity", "ssn", "身份证", "证件",
	"address", "addr", "地址", "住址",
	"password", "passwd", "pwd", "密码", "口令",
	"token", "secret", "apikey", "api_key", "private_key", "credential", "access_key",
	"bank", "card", "银行卡", "信用卡",
	"realname", "real_name", "姓名",
	"passport", "护照",
}

// SensitiveCol 判断列名是否属于敏感字段（用于 D3 分级）。
func SensitiveCol(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.Trim(n, "`\"'[]")
	if n == "" {
		return false
	}
	for _, kw := range sensitiveKeywords {
		if strings.Contains(n, kw) {
			return true
		}
	}
	return false
}

// ───────────────────────── 打码 ─────────────────────────

var (
	rePhone  = regexp.MustCompile(`^\+?\d{7,15}$`)
	reIDCard = regexp.MustCompile(`^\d{17}[\dXx]$|^\d{15}$`)
	reEmail  = regexp.MustCompile(`^[^@\s]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)
)

// MaskValue 为报告/日志打码：手机号留前3后2，邮箱留首字母+域名，
// 身份证留前4后2，其余留首尾各2。短值（≤4）整体打码，避免"保留首尾"等于泄露全文。
func MaskValue(v string) string {
	if v == "" {
		return ""
	}
	s := strings.TrimSpace(v)
	switch {
	case reEmail.MatchString(s):
		at := strings.LastIndex(s, "@")
		local, domain := s[:at], s[at+1:]
		head := ""
		if local != "" {
			head = string([]rune(local)[:1])
		}
		return head + "***@" + domain
	case reIDCard.MatchString(s):
		return keepHeadTail(s, 4, 2)
	case rePhone.MatchString(s):
		return keepHeadTail(s, 3, 2)
	default:
		return keepHeadTail(s, 2, 2)
	}
}

func keepHeadTail(s string, head, tail int) string {
	r := []rune(s)
	n := len(r)
	if n == 0 {
		return ""
	}
	if head+tail >= n {
		// 太短：保留首尾会等于明文，整体打码。
		return strings.Repeat("*", n)
	}
	return string(r[:head]) + strings.Repeat("*", n-head-tail) + string(r[n-tail:])
}

// ───────────────────────── 解析入口 ─────────────────────────

// Parse 按引擎解析；未知引擎按通用规则解析并在 Notes 里说明。
func Parse(engine, out string) Result {
	eng := normalizeEngine(engine)
	s := newState(out)
	s.run()
	res := s.finish(eng)
	return res
}

// ParseMySQL 解析 mysql/mariadb 客户端输出。
func ParseMySQL(out string) Result { return Parse("mysql", out) }

// ParsePostgres 解析 psql 输出。
func ParsePostgres(out string) Result { return Parse("postgres", out) }

// ParseMSSQL 解析 sqlcmd/mssql 输出。
func ParseMSSQL(out string) Result { return Parse("mssql", out) }

func normalizeEngine(engine string) string {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "mysql", "mariadb":
		return "mysql"
	case "postgres", "postgresql", "psql", "pgsql", "pg":
		return "postgres"
	case "mssql", "sqlserver", "sql_server", "sql server", "tsql", "sqlcmd":
		return "mssql"
	default:
		return ""
	}
}

// ───────────────────────── 解析实现 ─────────────────────────

type colKind int

const (
	colSkip colKind = iota
	colName
	colRows
	colColumn
)

type grid struct {
	kinds   []colKind
	hasName bool
}

type state struct {
	res      Result
	tables   map[string]*Table
	order    []string
	grid     *grid
	curTable string
	wantRows bool
	raw      string
}

func newState(out string) *state {
	return &state{
		res:    Result{Tables: []Table{}},
		tables: map[string]*Table{},
		raw:    out,
	}
}

var (
	rePromptSQL = regexp.MustCompile(`(?i)^(?:mysql|mariadb|psql|postgres|sqlcmd|sqlplus)\s*[=\[\]\w.\-]*\s*[>#]\s*`)
	rePromptSym = regexp.MustCompile(`^(?:=>|->)\s*`)
	reSepLine   = regexp.MustCompile(`^[\s+\-=:|]+$`)
	reFooter    = regexp.MustCompile(`(?i)^\(\d+\s+rows?\)$|^\d+\s+rows?\s+in\s+set|^\(\d+\s+rows?\s+affected\)`)
	reIdent     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*$`)
	reQualIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*(\.[A-Za-z_][A-Za-z0-9_$]*)?$`)
	reTwoSpaces = regexp.MustCompile(`\s{2,}`)
)

func (s *state) run() {
	lines := strings.Split(strings.ReplaceAll(s.raw, "\r\n", "\n"), "\n")

	// 纯表名列表（没有任何表头）也能解析，如 `show tables` 的精简输出。
	if names, ok := bareTableList(lines); ok {
		for _, n := range names {
			s.table(n)
		}
		return
	}

	for _, line := range lines {
		s.line(line)
	}
}

func (s *state) line(raw string) {
	line := strings.TrimSpace(raw)
	if line == "" {
		return
	}
	for {
		next := rePromptSQL.ReplaceAllString(line, "")
		next = rePromptSym.ReplaceAllString(next, "")
		next = strings.TrimSpace(next)
		if next == line {
			break
		}
		line = next
	}
	if line == "" || reSepLine.MatchString(line) {
		return
	}
	if reFooter.MatchString(line) {
		return
	}
	// SQL 回显：更新当前表与"是否在取行数"的上下文。
	if name, isCount, ok := sqlTarget(line); ok {
		s.curTable = name
		s.wantRows = isCount
		if isCount {
			return
		}
	}

	// 管道/边框表格（mysql、psql 默认对齐输出）。
	if strings.Contains(line, "|") {
		cells := pipeCells(line)
		s.row(cells)
		return
	}
	// 制表符或 2+ 空格对齐的纯文本表格（sqlcmd / mysql --batch / psql unaligned）。
	if strings.Contains(line, "\t") {
		s.row(splitTrim(line, "\t"))
		return
	}
	if cells := reTwoSpaces.Split(line, -1); len(cells) > 1 {
		s.row(cells)
		return
	}

	// 单列输出：表头（Tables_in_xxx / table_name / Field）或纯数字行数。
	if isHeaderLine(line) {
		s.grid = buildGrid([]string{line})
		return
	}
	if s.grid != nil && len(s.grid.kinds) == 1 && s.grid.kinds[0] == colName && validName(line) {
		s.table(line)
		return
	}
	if n, ok := parseCount(line); ok {
		if s.curTable != "" && (s.wantRows || s.gridHasRows()) {
			s.table(s.curTable).Rows = n
			s.wantRows = false
		}
	}
}

func (s *state) row(cells []string) {
	cells = trimCells(cells)
	if len(cells) == 0 {
		return
	}
	if isDashRow(cells) {
		return
	}
	if isHeaderCells(cells) {
		s.grid = buildGrid(cells)
		return
	}
	if s.grid != nil {
		s.apply(cells)
		return
	}
	if len(cells) == 2 && validName(cells[0]) {
		if n, ok := parseCount(cells[1]); ok {
			s.table(cells[0]).Rows = n
		}
	}
}

func (s *state) apply(cells []string) {
	g := s.grid
	if g == nil {
		return
	}
	var name string
	rows := int64(-1)
	rowsKnown := false
	var cols []string
	for i, c := range cells {
		if i >= len(g.kinds) {
			break
		}
		switch g.kinds[i] {
		case colName:
			if name == "" && validName(c) {
				name = c
			}
		case colRows:
			if n, ok := parseCount(c); ok {
				rows = n
				rowsKnown = true
			}
		case colColumn:
			if validName(c) && SensitiveCol(c) {
				cols = append(cols, c)
			}
		}
	}
	target := name
	if target == "" {
		if g.hasName {
			// 该行有名字列但名字不可用（NULL/空），不要错挂到上一张表。
			return
		}
		target = s.curTable
	}
	if target == "" {
		return
	}
	tb := s.table(target)
	if rowsKnown {
		tb.Rows = rows
	}
	for _, c := range cols {
		tb.SensitiveColumns = appendUnique(tb.SensitiveColumns, c)
	}
}

func (s *state) table(name string) *Table {
	name = strings.TrimSpace(name)
	if tb, ok := s.tables[name]; ok {
		return tb
	}
	tb := &Table{Name: name, Rows: -1}
	s.tables[name] = tb
	s.order = append(s.order, name)
	return tb
}

func (s *state) gridHasRows() bool {
	if s.grid == nil {
		return false
	}
	for _, k := range s.grid.kinds {
		if k == colRows {
			return true
		}
	}
	return false
}

// ───────────────────────── 分级 ─────────────────────────

var exportPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\binto\s+(?:outfile|dumpfile)\b`),
	regexp.MustCompile(`(?i)\bcopy\b[^\n]*\bto\s+(?:stdout|program|'|")`),
	regexp.MustCompile(`(?i)\\\\copy\b[^\n]*\bto\b`),
	regexp.MustCompile(`(?i)\bbc[pf]\b[^\n]*\b(?:out|queryout)\b`),
	regexp.MustCompile(`(?i)\bmysqldump\b`),
	regexp.MustCompile(`(?i)\b(?:pg_dump|pg_read_file|lo_export)\b`),
	regexp.MustCompile(`(?i)\bsecure_file_priv\b`),
}

func exportEvidence(raw string) string {
	for _, re := range exportPatterns {
		if m := re.FindString(raw); m != "" {
			return strings.TrimSpace(m)
		}
	}
	return ""
}

func (s *state) finish(engine string) Result {
	res := s.res
	for _, name := range s.order {
		tb := *s.tables[name]
		if tb.SensitiveColumns == nil {
			tb.SensitiveColumns = nil
		}
		res.Tables = append(res.Tables, tb)
	}
	eng := engine
	if eng == "" {
		eng = "unknown"
	}
	if len(res.Tables) > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf("引擎 %s：解析到 %d 张表", eng, len(res.Tables)))
	}

	rowsKnown, rowsTotal := 0, int64(0)
	sensitive := 0
	for _, tb := range res.Tables {
		if tb.Rows >= 0 {
			rowsKnown++
			rowsTotal += tb.Rows
		}
		sensitive += len(tb.SensitiveColumns)
	}

	exp := exportEvidence(s.raw)
	switch {
	case exp != "":
		res.ProofLevel = LevelExpor
		res.Notes = append(res.Notes, fmt.Sprintf("输出中出现导出能力证据（%s）；%s", exp, NoteNoExec))
	case len(res.Tables) == 0:
		res.ProofLevel = LevelNone
		res.Notes = append(res.Notes, "未解析到表名/列名，无法给出数据可达证明（可先跑 information_schema.tables）")
	case sensitive > 0:
		res.ProofLevel = LevelSens
		res.Notes = append(res.Notes, fmt.Sprintf("已确认敏感列 %d 个（仅列名，未取任何值）", sensitive))
	case rowsKnown > 0:
		res.ProofLevel = LevelRows
		res.Notes = append(res.Notes, fmt.Sprintf("已取得行数证据（%d 张表，合计 %d 行）", rowsKnown, rowsTotal))
	default:
		res.ProofLevel = LevelName
		res.Notes = append(res.Notes, "仅表名/列名，行数未知（补一条 COUNT(*) 可升到 D2）")
	}
	return res
}

// ───────────────────────── PlanProbe ─────────────────────────

// PlanProbe 生成只读探测 SQL 模板（顺序：information_schema.tables →
// information_schema.columns → COUNT(*) → 指定列的 LIMIT 1 样本）。
// 绝不生成 mysqldump / SELECT * / UPDATE / DELETE / INSERT。
// `<table>` 是占位符，由调用方替换成已确认存在的表名。
func PlanProbe(engine string) []string {
	switch normalizeEngine(engine) {
	case "mysql":
		return []string{
			"SELECT table_schema, table_name, table_rows FROM information_schema.tables WHERE table_schema = DATABASE() ORDER BY table_rows DESC LIMIT 50",
			"SELECT table_name, column_name, data_type FROM information_schema.columns WHERE table_schema = DATABASE() ORDER BY table_name, ordinal_position LIMIT 200",
			"SELECT COUNT(*) FROM <table>",
			"SELECT <columns> FROM <table> LIMIT 1 -- <columns> 只填已确认的敏感列",
		}
	case "postgres":
		return []string{
			"SELECT schemaname, relname, n_live_tup FROM pg_stat_user_tables ORDER BY n_live_tup DESC LIMIT 50",
			"SELECT table_schema, table_name, column_name, data_type FROM information_schema.columns WHERE table_schema NOT IN ('pg_catalog', 'information_schema') ORDER BY table_schema, table_name, ordinal_position LIMIT 200",
			"SELECT COUNT(*) FROM \"<table>\"",
			"SELECT <columns> FROM \"<table>\" LIMIT 1 -- <columns> 只填已确认的敏感列",
		}
	case "mssql":
		return []string{
			"SELECT s.name AS schema_name, t.name AS table_name, p.rows FROM sys.tables t JOIN sys.schemas s ON t.schema_id = s.schema_id JOIN sys.partitions p ON t.object_id = p.object_id AND p.index_id IN (0, 1) ORDER BY p.rows DESC",
			"SELECT TABLE_NAME, COLUMN_NAME, DATA_TYPE FROM INFORMATION_SCHEMA.COLUMNS ORDER BY TABLE_NAME, ORDINAL_POSITION",
			"SELECT COUNT(*) FROM [<table>]",
			"SELECT TOP 1 <columns> FROM [<table>] -- <columns> 只填已确认的敏感列",
		}
	default:
		return nil
	}
}

// ───────────────────────── 文本工具 ─────────────────────────

func pipeCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	return splitTrim(line, "|")
}

func splitTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func trimCells(cells []string) []string {
	out := make([]string, 0, len(cells))
	for _, c := range cells {
		out = append(out, strings.TrimSpace(c))
	}
	// 去掉尾部空单元（mysql 边框/psql 末列对齐留下的空串）。
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func isDashRow(cells []string) bool {
	seen := false
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		seen = true
		if !reSepLine.MatchString(c) {
			return false
		}
	}
	return seen
}

func isHeaderCells(cells []string) bool {
	for _, c := range cells {
		if isHeaderLine(c) {
			return true
		}
	}
	return false
}

func isHeaderLine(cell string) bool {
	for _, kw := range headerKeywords {
		if strings.Contains(strings.ToLower(cell), kw) {
			return true
		}
	}
	return false
}

var headerKeywords = []string{
	"column_name", "columnname", "column", "field", "attname",
	"table_rows", "row_count", "num_rows", "n_live_tup", "rowcount", "rows",
	"count(", "estimated",
	"table_schema", "schemaname", "schema_name", "table_catalog",
	"table_name", "tablename", "tables_in_", "relname", "table",
	"name",
}

func buildGrid(cells []string) *grid {
	g := &grid{kinds: make([]colKind, len(cells))}
	for i, c := range cells {
		k := classifyHeader(c)
		g.kinds[i] = k
		if k == colName {
			g.hasName = true
		}
	}
	return g
}

func classifyHeader(cell string) colKind {
	c := strings.ToLower(strings.TrimSpace(cell))
	c = strings.Trim(c, "`\"'[]")
	if c == "" {
		return colSkip
	}
	// 顺序很重要：table_schema 含 table、column_name 含 name。
	switch {
	case containsAny(c, "column_name", "columnname", "column", "field", "attname"):
		return colColumn
	case containsAny(c, "table_rows", "row_count", "num_rows", "n_live_tup", "rowcount", "rows", "count(", "estimated"):
		return colRows
	case containsAny(c, "table_schema", "schemaname", "schema_name", "table_catalog", "owner"):
		return colSkip
	case containsAny(c, "table_name", "tablename", "tables_in_", "relname", "table", "name"):
		return colName
	default:
		return colSkip
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func sqlTarget(line string) (name string, isCount bool, ok bool) {
	l := strings.ToLower(line)
	isCount = strings.Contains(l, "count(")
	for _, kw := range []string{" from ", " desc ", " describe "} {
		i := strings.Index(l, kw)
		if i < 0 {
			continue
		}
		rest := strings.TrimSpace(line[i+len(kw):])
		if j := strings.IndexAny(rest, " \t;,()"); j >= 0 {
			rest = rest[:j]
		}
		rest = strings.Trim(rest, "`\"'[]")
		if k := strings.LastIndex(rest, "."); k >= 0 {
			rest = rest[k+1:]
		}
		if validName(rest) {
			return rest, isCount, true
		}
	}
	return "", isCount, false
}

func validName(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`\"'[]")
	if s == "" || len(s) > 128 {
		return false
	}
	u := strings.ToUpper(s)
	if u == "NULL" || u == "(NULL)" || u == "N/A" {
		return false
	}
	return reQualIdent.MatchString(s)
}

func parseCount(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`\"'")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")
	if s == "" || s == "-" {
		return 0, false
	}
	if strings.EqualFold(s, "null") {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func bareTableList(lines []string) ([]string, bool) {
	var names []string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		for {
			next := rePromptSQL.ReplaceAllString(line, "")
			next = strings.TrimSpace(next)
			if next == line {
				break
			}
			line = next
		}
		if line == "" || reFooter.MatchString(line) || reSepLine.MatchString(line) {
			continue
		}
		// 单列表头（table_name / Tables_in_xxx）是表头，不是表名。
		if isHeaderLine(line) {
			continue
		}
		if strings.ContainsAny(line, "| \t") {
			return nil, false
		}
		if !validName(line) {
			return nil, false
		}
		names = append(names, strings.Trim(line, "`\"'[]"))
	}
	if len(names) == 0 {
		return nil, false
	}
	return names, true
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return list
		}
	}
	return append(list, v)
}
