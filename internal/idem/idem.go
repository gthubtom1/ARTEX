// Package idem 提供幂等键：重跑同一个 (task, intent, step, kind) 时不会产生重复写回。
//
// 用法：
//	key := idem.KeyFor(taskID, intentID, step, "record_fact", payload)
//	first, err := guard.Once(ctx, taskID, key)
//	if !first { /* 已写过，跳过 */ }
package idem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/internal/dbiface"
)

// Key 把若干部分拼成稳定键（sha256 截断 24 字节 hex）。
func Key(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0x1f}) // 分隔符，避免 "ab"+"c" 与 "a"+"bc" 撞车
	}
	return hex.EncodeToString(h.Sum(nil)[:24])
}

// KeyFor 是常用组合：task + intent + step + kind + payload。
func KeyFor(taskID, intentID int64, step int, kind string, payload []byte) string {
	h := sha256.New()
	h.Write([]byte(strconv.FormatInt(taskID, 10)))
	h.Write([]byte{0x1f})
	h.Write([]byte(strconv.FormatInt(intentID, 10)))
	h.Write([]byte{0x1f})
	h.Write([]byte(strconv.Itoa(step)))
	h.Write([]byte{0x1f})
	h.Write([]byte(kind))
	h.Write([]byte{0x1f})
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil)[:24])
}

// Guard 用 DB 唯一键做"只执行一次"。
type Guard struct {
	db dbiface.DB
}

func New(db dbiface.DB) *Guard { return &Guard{db: db} }

// Once 尝试占用 key：
//   first=true  → 第一次，调用方继续执行
//   first=false → 已存在，调用方跳过
//
// DB 为 nil 时退化为内存实现（测试/无库场景）。
func (g *Guard) Once(ctx context.Context, taskID int64, key string) (first bool, err error) {
	if key == "" {
		return false, fmt.Errorf("idem: empty key")
	}
	if g == nil || g.db == nil {
		return g.memOnce(key), nil
	}
	tag, err := g.db.ExecContext(ctx,
		`INSERT INTO idem_keys (key, task_id) VALUES ($1, NULLIF($2,0))
		 ON CONFLICT (key) DO NOTHING`, key, taskID)
	if err != nil {
		return false, fmt.Errorf("idem: once: %w", err)
	}
	n, raErr := tag.RowsAffected()
	if raErr != nil {
		return false, fmt.Errorf("idem: rows affected: %w", raErr)
	}
	return n == 1, nil
}

// Release 释放键（例如写回失败需要允许重试时）。
func (g *Guard) Release(ctx context.Context, key string) error {
	if g == nil || g.db == nil {
		g.memRelease(key)
		return nil
	}
	_, err := g.db.ExecContext(ctx, `DELETE FROM idem_keys WHERE key = $1`, key)
	return err
}

// Cleanup 清理某任务超过 keep 天的键（定时任务调用）。
func (g *Guard) Cleanup(ctx context.Context, keepDays int) (int64, error) {
	if g == nil || g.db == nil || keepDays <= 0 {
		return 0, nil
	}
	tag, err := g.db.ExecContext(ctx,
		`DELETE FROM idem_keys WHERE created_at < now() - ($1 || ' days')::interval`,
		strconv.Itoa(keepDays))
	if err != nil {
		return 0, err
	}
	n, raErr := tag.RowsAffected()
	if raErr != nil {
		return 0, raErr
	}
	return n, nil
}

// ── 内存实现（无 DB 时的退化路径） ──
var memStore = map[string]struct{}{}

func (g *Guard) memOnce(key string) bool {
	key = strings.TrimSpace(key)
	if _, ok := memStore[key]; ok {
		return false
	}
	memStore[key] = struct{}{}
	return true
}

func (g *Guard) memRelease(key string) { delete(memStore, key) }
