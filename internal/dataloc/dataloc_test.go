package dataloc

import (
	"strings"
	"testing"
)

const mysqlTablesOut = `mysql> SELECT table_schema, table_name, table_rows FROM information_schema.tables WHERE table_schema = DATABASE();
+--------------+------------+------------+
| TABLE_SCHEMA | TABLE_NAME | TABLE_ROWS |
+--------------+------------+------------+
| app          | users      |       1200 |
| app          | orders     |          0 |
+--------------+------------+------------+
2 rows in set (0.00 sec)
`

const mysqlShowTablesOut = `mysql> show tables;
+-------------------+
| Tables_in_app     |
+-------------------+
| users             |
| orders            |
+-------------------+
`

const mysqlCountOut = `mysql> SELECT COUNT(*) FROM users;
+----------+
| COUNT(*) |
+----------+
|     1200 |
+----------+
1 row in set (0.01 sec)
`

const mysqlColumnsOut = `mysql> SELECT table_name, column_name, data_type FROM information_schema.columns WHERE table_schema = DATABASE();
+------------+-------------+-----------+
| TABLE_NAME | COLUMN_NAME | DATA_TYPE |
+------------+-------------+-----------+
| users      | id          | bigint    |
| users      | user_email  | varchar   |
| users      | phone       | varchar   |
| users      | password    | varchar   |
| orders     | amount      | decimal   |
+------------+-------------+-----------+
`

const psqlTablesOut = ` schema_name |   relname   | n_live_tup
-------------+-------------+------------
 public      | users       |         42
 public      | audit_log   |          0
(2 rows)
`

const psqlColumnsOut = `  table_name   | column_name | data_type
---------------+-------------+-----------
 users         | email       | text
 users         | id          | bigint
(2 rows)
`

const mssqlTablesOut = "schema_name\ttable_name\trows\n-----------\t----------\t------\ndbo\tusers\t1200\ndbo\torders\t3\n"

func findTable(t *testing.T, r Result, name string) Table {
	t.Helper()
	for _, tb := range r.Tables {
		if tb.Name == name {
			return tb
		}
	}
	t.Fatalf("table %q not parsed, got %+v", name, r.Tables)
	return Table{}
}

func TestParseMySQLTablesD2(t *testing.T) {
	r := ParseMySQL(mysqlTablesOut)
	if r.ProofLevel != LevelRows {
		t.Fatalf("proof level = %s, want %s (%v)", r.ProofLevel, LevelRows, r.Notes)
	}
	if len(r.Tables) != 2 {
		t.Fatalf("tables = %+v", r.Tables)
	}
	users := findTable(t, r, "users")
	if users.Rows != 1200 {
		t.Fatalf("users rows = %d", users.Rows)
	}
	orders := findTable(t, r, "orders")
	if orders.Rows != 0 {
		t.Fatalf("orders rows = %d", orders.Rows)
	}
	if len(users.SensitiveColumns) != 0 {
		t.Fatalf("unexpected sensitive columns: %+v", users.SensitiveColumns)
	}
}

func TestParseMySQLShowTablesD1(t *testing.T) {
	r := ParseMySQL(mysqlShowTablesOut)
	if r.ProofLevel != LevelName {
		t.Fatalf("proof level = %s, want %s (%v)", r.ProofLevel, LevelName, r.Notes)
	}
	if len(r.Tables) != 2 {
		t.Fatalf("tables = %+v", r.Tables)
	}
	for _, tb := range r.Tables {
		if tb.Rows != -1 {
			t.Fatalf("%s rows = %d, want -1 (unknown)", tb.Name, tb.Rows)
		}
	}
}

func TestParseMySQLCountAttachesToEchoedTable(t *testing.T) {
	r := ParseMySQL(mysqlCountOut)
	if len(r.Tables) != 1 {
		t.Fatalf("tables = %+v", r.Tables)
	}
	tb := findTable(t, r, "users")
	if tb.Rows != 1200 {
		t.Fatalf("rows = %d, want 1200", tb.Rows)
	}
	if r.ProofLevel != LevelRows {
		t.Fatalf("proof level = %s", r.ProofLevel)
	}
}

func TestParseMySQLColumnsD3(t *testing.T) {
	r := ParseMySQL(mysqlColumnsOut)
	if r.ProofLevel != LevelSens {
		t.Fatalf("proof level = %s, want %s (%v)", r.ProofLevel, LevelSens, r.Notes)
	}
	users := findTable(t, r, "users")
	want := []string{"user_email", "phone", "password"}
	if strings.Join(users.SensitiveColumns, ",") != strings.Join(want, ",") {
		t.Fatalf("sensitive columns = %v, want %v", users.SensitiveColumns, want)
	}
	orders := findTable(t, r, "orders")
	if len(orders.SensitiveColumns) != 0 {
		t.Fatalf("orders = %+v", orders)
	}
}

func TestParsePostgresTablesD2(t *testing.T) {
	r := ParsePostgres(psqlTablesOut)
	if r.ProofLevel != LevelRows {
		t.Fatalf("proof level = %s (%v)", r.ProofLevel, r.Notes)
	}
	users := findTable(t, r, "users")
	if users.Rows != 42 {
		t.Fatalf("users rows = %d", users.Rows)
	}
	if findTable(t, r, "audit_log").Rows != 0 {
		t.Fatalf("audit_log rows not parsed")
	}
}

func TestParsePostgresColumnsD3(t *testing.T) {
	r := ParsePostgres(psqlColumnsOut)
	if r.ProofLevel != LevelSens {
		t.Fatalf("proof level = %s (%v)", r.ProofLevel, r.Notes)
	}
	users := findTable(t, r, "users")
	if len(users.SensitiveColumns) != 1 || users.SensitiveColumns[0] != "email" {
		t.Fatalf("sensitive columns = %v", users.SensitiveColumns)
	}
}

func TestParseMSSQLTablesD2(t *testing.T) {
	r := ParseMSSQL(mssqlTablesOut)
	if r.ProofLevel != LevelRows {
		t.Fatalf("proof level = %s (%v)", r.ProofLevel, r.Notes)
	}
	if len(r.Tables) != 2 {
		t.Fatalf("tables = %+v", r.Tables)
	}
	if findTable(t, r, "users").Rows != 1200 {
		t.Fatalf("users rows = %d", findTable(t, r, "users").Rows)
	}

	// sqlcmd 默认的 2+ 空格对齐输出同样能解析。
	spaced := "schema_name  table_name  rows\n" +
		"-----------  ----------  ------\n" +
		"dbo          users       1200\n" +
		"dbo          orders      3\n"
	r = ParseMSSQL(spaced)
	if len(r.Tables) != 2 || findTable(t, r, "users").Rows != 1200 {
		t.Fatalf("space aligned mssql output not parsed: %+v", r.Tables)
	}
}

func TestParsePostgresNameOnlyD1(t *testing.T) {
	out := `  table_name
------------
 users
 orders
(2 rows)`
	r := ParsePostgres(out)
	if r.ProofLevel != LevelName {
		t.Fatalf("proof level = %s (%v)", r.ProofLevel, r.Notes)
	}
	if len(r.Tables) != 2 {
		t.Fatalf("tables = %+v", r.Tables)
	}
	if findTable(t, r, "users").Rows != -1 {
		t.Fatalf("rows should be unknown")
	}
}

func TestParseBareTableList(t *testing.T) {
	r := ParseMySQL("users\norders\npayments\n")
	if len(r.Tables) != 3 {
		t.Fatalf("tables = %+v", r.Tables)
	}
	if r.ProofLevel != LevelName {
		t.Fatalf("proof level = %s", r.ProofLevel)
	}
}

func TestParseEmptyOutput(t *testing.T) {
	r := ParseMySQL("")
	if r.ProofLevel != LevelNone {
		t.Fatalf("proof level = %s", r.ProofLevel)
	}
	if r.Tables == nil || len(r.Tables) != 0 {
		t.Fatalf("tables = %+v", r.Tables)
	}
	if len(r.Notes) == 0 {
		t.Fatalf("expected a note explaining empty result")
	}
}

func TestParseD4ExportOnlyAnnotated(t *testing.T) {
	mysqlOut := `mysql> SELECT * FROM users INTO OUTFILE '/tmp/users.txt';
ERROR 1 (HY000): Can't create/write to file '/tmp/users.txt' (Errcode: 13)`
	r := ParseMySQL(mysqlOut)
	if r.ProofLevel != LevelExpor {
		t.Fatalf("proof level = %s, want %s (%v)", r.ProofLevel, LevelExpor, r.Notes)
	}
	joined := strings.Join(r.Notes, " ")
	if !strings.Contains(joined, NoteNoExec) {
		t.Fatalf("notes must state %q: %v", NoteNoExec, r.Notes)
	}

	pgOut := `postgres=# COPY users TO '/tmp/users.csv' WITH CSV;
COPY 2`
	r = ParsePostgres(pgOut)
	if r.ProofLevel != LevelExpor {
		t.Fatalf("postgres proof level = %s (%v)", r.ProofLevel, r.Notes)
	}
	if !strings.Contains(strings.Join(r.Notes, " "), NoteNoExec) {
		t.Fatalf("postgres notes must state %q: %v", NoteNoExec, r.Notes)
	}
}

func TestPlanProbeIsReadOnly(t *testing.T) {
	engines := []string{"mysql", "postgres", "mssql"}
	forbidden := []string{"mysqldump", "pg_dump", "update ", "delete ", "insert ", "drop ", "into outfile", "truncate ", "grant "}
	for _, eng := range engines {
		stmts := PlanProbe(eng)
		if len(stmts) < 3 {
			t.Fatalf("%s: too few probes: %v", eng, stmts)
		}
		joined := strings.ToLower(strings.Join(stmts, "\n"))
		if !strings.Contains(joined, "information_schema") && !strings.Contains(joined, "pg_stat_user_tables") && !strings.Contains(joined, "sys.tables") {
			t.Fatalf("%s: first probe should read catalog: %v", eng, stmts)
		}
		if !strings.Contains(joined, "count(") {
			t.Fatalf("%s: missing COUNT(*) probe: %v", eng, stmts)
		}
		for _, bad := range forbidden {
			if strings.Contains(joined, bad) {
				t.Fatalf("%s: probe contains forbidden %q: %v", eng, bad, stmts)
			}
		}
		for _, stmt := range stmts {
			l := strings.ToLower(stmt)
			if !strings.Contains(l, "select") {
				continue
			}
			if strings.Contains(l, "select *") || strings.Contains(l, "* from") {
				if !strings.Contains(l, "limit") && !strings.Contains(l, "top 1") {
					t.Fatalf("%s: unbounded SELECT * probe: %s", eng, stmt)
				}
			}
		}
	}
	if got := PlanProbe("oracle"); got != nil {
		t.Fatalf("unknown engine should return nil, got %v", got)
	}
}

func TestPlanProbeSensitiveSampleLimitedToOne(t *testing.T) {
	for _, eng := range []string{"mysql", "postgres", "mssql"} {
		stmts := PlanProbe(eng)
		last := strings.ToLower(stmts[len(stmts)-1])
		if !strings.Contains(last, "limit 1") && !strings.Contains(last, "top 1") {
			t.Fatalf("%s: sample probe must be LIMIT 1: %s", eng, last)
		}
	}
}

func TestMaskValue(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"13800138000", "138******00"},
		{"zhangsan@example.com", "z***@example.com"},
		{"110101199003071234", "1101************34"},
		{"supersecret", "su*******et"},
		{"abc", "***"},
		{"", ""},
		{"+8613800138000", "+86*********00"},
	}
	for _, c := range cases {
		if got := MaskValue(c.in); got != c.want {
			t.Fatalf("MaskValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// 打码结果不得包含原文中段。
	got := MaskValue("13800138000")
	if strings.Contains(got, "00138") || strings.Contains(got, "80013") {
		t.Fatalf("mask leaks digits: %s", got)
	}
}

func TestSensitiveCol(t *testing.T) {
	yes := []string{"phone", "user_mobile", "Email", "id_card", "身份证号", "home_address", "地址",
		"password_hash", "passwd", "api_token", "client_secret", "bank_account", "card_no"}
	no := []string{"id", "created_at", "amount", "status", "updated_at", "remark"}
	for _, c := range yes {
		if !SensitiveCol(c) {
			t.Fatalf("SensitiveCol(%q) = false, want true", c)
		}
	}
	for _, c := range no {
		if SensitiveCol(c) {
			t.Fatalf("SensitiveCol(%q) = true, want false", c)
		}
	}
}

func TestParseUnknownEngineStillParses(t *testing.T) {
	r := Parse("oracle", mysqlTablesOut)
	if len(r.Tables) != 2 {
		t.Fatalf("tables = %+v", r.Tables)
	}
	if !strings.Contains(strings.Join(r.Notes, " "), "unknown") {
		t.Fatalf("notes should mention unknown engine: %v", r.Notes)
	}
}
