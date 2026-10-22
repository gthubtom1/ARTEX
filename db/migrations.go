package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"
)

// 迁移文件按文件名字典序执行，版本号 = 文件名（如 0001_p0_schema.sql）。
// 与 db/schema.sql 的分工：
//
//	schema.sql      = "建库基线"（既有 59 张表），启动时整串幂等执行；
//	migrations/*.sql = "增量 DDL"，在基线之后按版本执行，并记入 schema_migrations。
//
// 两者都在 withSchemaMigrationLock 的同一个会话内执行，避免多实例并发建表。
//
//go:embed migrations/*.sql
var migrationFS embed.FS

const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// applyMigrations 在基线 schema 应用之后执行所有未应用的迁移。
// 每个迁移在自己的事务里跑；失败即中止启动（宁可起不来，也不要半套表）。
func applyMigrations(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, schemaMigrationsDDL); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}
	names, err := migrationNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		var applied bool
		if err := conn.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name,
		).Scan(&applied); err != nil {
			return fmt.Errorf("migration %s: check ledger: %w", name, err)
		}
		if applied {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("migration %s: read: %w", name, err)
		}
		stmt := stripTxControl(string(body))
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("migration %s: begin: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: exec: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: record: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %s: commit: %w", name, err)
		}
		log.Printf("[db] migration applied: %s", name)
	}
	return nil
}

// migrationNames 返回 migrations/ 下所有 .sql，按文件名升序。
func migrationNames() ([]string, error) {
	ents, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("migrations dir: %w", err)
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// stripTxControl 去掉文件内的 BEGIN/COMMIT/END，避免与 runner 的事务嵌套。
// 迁移文件本身仍必须幂等（IF NOT EXISTS / ON CONFLICT），因为手工 psql 过的库
// 没有 ledger 记录，下次启动会再执行一次。
func stripTxControl(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(ln), ";"))
		switch strings.ToUpper(t) {
		case "BEGIN", "COMMIT", "END", "START TRANSACTION":
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}
