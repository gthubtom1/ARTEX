// Package dbiface 定义内部包所需的最小数据库接口（database/sql 口径），
// 以便复用项目既有的 *db.DB（内嵌 *sql.DB）同时保持测试可注入。
package dbiface

import (
	"context"
	"database/sql"
)

// DB 是 *sql.DB 的子集（项目里的 *db.DB 内嵌 *sql.DB，天然满足）。
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
