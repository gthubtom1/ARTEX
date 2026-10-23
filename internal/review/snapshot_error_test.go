package review

import (
	"context"
	"testing"

	pgdb "github.com/Autumn-27/artex/db"
)

// intentID 精确匹配：带 intent_id 的错误行必须挂到同一条意图上，
// 且不受 detail 里出现的其它数字（IP/端口）影响。
func TestAttributeErrorsExactIntentID(t *testing.T) {
	intents := []IntentBrief{{ID: 92}, {ID: 70}, {ID: 47}}
	errs := []errorEvent{
		{class: "env_error", detail: "dial tcp 10.0.0.5:8080: connect: connection refused", intentID: 92},
		{class: "target_blocked", detail: "HTTP 403 on /admin (intent 47 的旧文本)", intentID: 47},
	}
	attributeErrors(intents, errs)

	if intents[0].LastError != "env_error" {
		t.Fatalf("意图 92 应拿到 env_error，得到 %q", intents[0].LastError)
	}
	if intents[1].LastError != "" {
		t.Fatalf("意图 70 没有对应错误行，不该被 IP/端口数字误挂：%q", intents[1].LastError)
	}
	if intents[2].LastError != "target_blocked" {
		t.Fatalf("意图 47 应拿到 target_blocked，得到 %q", intents[2].LastError)
	}
}

// intent_id=0 的旧行：只在没有精确匹配时按 detail 子串兜底，
// 并且不得覆盖已经精确匹配上的意图。
func TestAttributeErrorsLegacyFallbackOnlyWithoutExact(t *testing.T) {
	intents := []IntentBrief{{ID: 92}, {ID: 20}, {ID: 17}}
	errs := []errorEvent{
		// 精确行：意图 92 是 self_error
		{class: "self_error", detail: "模型返回 429 的旧文本里也有 20", intentID: 92},
		// 旧行（无 intent_id）：detail 里带 20 和 92
		{class: "target_blocked", detail: "worker:work#2 意图 20 blocked; port 92 closed", intentID: 0},
	}
	attributeErrors(intents, errs)

	if intents[0].LastError != "self_error" {
		t.Fatalf("意图 92 应取精确匹配 self_error，旧行不得覆盖：%q", intents[0].LastError)
	}
	if intents[1].LastError != "target_blocked" {
		t.Fatalf("意图 20 无精确行，应走旧子串兜底拿到 target_blocked：%q", intents[1].LastError)
	}
	if intents[2].LastError != "" {
		t.Fatalf("意图 17 既无精确行也没在旧行里出现，不该被挂错误：%q", intents[2].LastError)
	}
}

// 同一意图多条错误：取最新一条（errs 已按 ts DESC 排好）。
func TestAttributeErrorsNewestWins(t *testing.T) {
	intents := []IntentBrief{{ID: 5}}
	errs := []errorEvent{
		{class: "target_blocked", detail: "newest", intentID: 5},
		{class: "env_error", detail: "older", intentID: 5},
	}
	attributeErrors(intents, errs)
	if intents[0].LastError != "target_blocked" {
		t.Fatalf("应取最新一条，得到 %q", intents[0].LastError)
	}
}

// PG 端到端：真的把带 intent_id 的行写进 task_error_events，再用 Build 的同一条
// 查询取回。没有可用库时跳过（与 db/agent 包的约定一致）。
// task_id 用一个不可能与真实任务冲突的高位值，并在结束时清理。
func TestErrorEventsIntentIDRoundTripPG(t *testing.T) {
	dsn, _, err := pgdb.DSN()
	if err != nil {
		t.Skipf("no database config (%v) — skipping", err)
	}
	d, err := pgdb.Open(dsn)
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()

	const taskID = int64(990927)
	if _, err := d.Exec(`DELETE FROM task_error_events WHERE task_id=$1`, taskID); err != nil {
		t.Skipf("task_error_events unavailable (%v) — skipping", err)
	}
	defer d.Exec(`DELETE FROM task_error_events WHERE task_id=$1`, taskID) //nolint:errcheck

	if _, err := d.Exec(`INSERT INTO task_error_events (task_id, intent_id, class, tool, detail) VALUES
		($1, 92, 'env_error', 'worker:work#2', 'dial tcp 10.0.0.5:8080: connection refused'),
		($1, 92, 'self_error', 'worker:work#2', 'older row with 20 inside'),
		($1, 0, 'target_blocked', 'worker:work#1', 'port 92 and intent 20 blocked')`, taskID); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var errs []errorEvent
	rows, err := d.QueryContext(context.Background(), `SELECT COALESCE(class,''), COALESCE(detail,''), COALESCE(intent_id,0)
		FROM task_error_events WHERE task_id=$1 AND ts > now() - interval '6 hours'
		ORDER BY ts DESC, id DESC LIMIT 200`, taskID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e errorEvent
		if err := rows.Scan(&e.class, &e.detail, &e.intentID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		errs = append(errs, e)
	}
	if len(errs) != 3 {
		t.Fatalf("应读到 3 条错误行，得到 %d", len(errs))
	}

	intents := []IntentBrief{{ID: 92}, {ID: 20}}
	attributeErrors(intents, errs)
	if intents[0].LastError != "self_error" {
		t.Fatalf("意图 92 应取最新精确行 self_error，得到 %q", intents[0].LastError)
	}
	if intents[1].LastError != "target_blocked" {
		t.Fatalf("意图 20 无精确行，应走旧子串兜底，得到 %q", intents[1].LastError)
	}
}
