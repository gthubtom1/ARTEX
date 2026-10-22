// Package settings 提供统一的运行时参数读写（复用既有 settings 表：key/value/updated_at）。
//
// 设计纪律（见 docs/最终方案-v1.md §5）：
//   - 这里**只放运行参数**（并发、超时、预算、代理端口…），
//   - **不放任何权限开关 / 风险分级**——能力默认全开，避免模型读到自己被限制而误拒。
package settings

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/internal/dbiface"
)

// Defaults 是可调运行参数的默认值。改这里等于改出厂设置。
var Defaults = map[string]string{
	// 并发与预算
	"work.workers":            "3",      // 同时运行的 worker 数
	"work.max_turns":          "40",     // 单次 run 最大轮数（0=不限）
	"budget.tokens_per_task":  "800000", // 单任务 token 上限（0=不限）
	"budget.seconds_per_task": "7200",   // 单任务墙钟上限（秒）

	// 工具面
	"tool.scan_mode":      "full", // fast(只扫 PATH/常见目录) | full(含全盘)
	"tool.scan_timeout_s": "180",
	"tool.auto_install":   "true",
	"tool.max_parallel":   "3",
	"tool.cmd_timeout_s":  "300",
	"tool.output_cap_kb":  "2048",

	// 出口（netguard）
	"net.proxy_http":       "http://127.0.0.1:7897",
	"net.proxy_socks":      "socks5://127.0.0.1:7897",
	"net.clash_controller": "http://127.0.0.1:9097",
	"net.clash_secret":     "",
	"net.verify_every_s":   "300",
	"net.verify_urls":      "https://api.ipify.org,https://ifconfig.me",

	// 知识/注入
	"kb.path":             "kb",
	"kb.search_limit":     "5",
	"inject.mode":         "hybrid", // always | on-miss | on-refusal | hybrid | off
	"inject.max_chars":    "900",
	"inject.max_retries":  "2",      // 单次 run 最多做几次拒答重定向
	"inject.custom":       "",       // 用户自定义破甲词/人设（整段原文）
	"inject.persona_mode": "cached", // cached=缺失时补 | every-turn=每轮注入
	"inject.preset":       "on",     // on=Custom 为空时用内置默认人设 | off=只用 Custom
	// P8-3：report_finding 的三门质量闸（质量门，不是权限门）。
	// off=不过门 | infer=证据不足改写为待验证事实（默认）| strict=inferred 也不入库
	"findings.verify_gate": "infer",

	// 审批自动裁决（off=完全保持原人工/模型路径；reviewer=本地规则自动裁决；allow_all=直接放行）
	"intercept_auto_decide": "off",

	// 复查
	"asset.recheck_hours":   "24",
	"asset.recheck_minutes": "60", // 复查执行器轮询间隔（0=关闭）
	"asset.stale_days":      "7",
}

// Store 是 settings 表的内存缓存封装。读走缓存（毫秒级），写穿透到 DB。
type Store struct {
	db dbiface.DB

	mu     sync.RWMutex
	cache  map[string]string
	loaded bool
}

func New(db dbiface.DB) *Store {
	return &Store{db: db, cache: make(map[string]string, len(Defaults))}
}

// Load 全量拉取一次；服务启动时调用。DB 为 nil 时只保留默认值。
func (s *Store) Load(ctx context.Context) error {
	m := make(map[string]string, len(Defaults))
	for k, v := range Defaults {
		m[k] = v
	}
	if s.db != nil {
		rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
		if err != nil {
			return fmt.Errorf("settings: load: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err != nil {
				return fmt.Errorf("settings: scan: %w", err)
			}
			m[k] = v
		}
		if rows.Err() != nil {
			return fmt.Errorf("settings: rows: %w", rows.Err())
		}
	}
	s.mu.Lock()
	s.cache = m
	s.loaded = true
	s.mu.Unlock()
	return nil
}

// Set 写入并更新缓存（DB 为 nil 时只改内存，便于测试）。
func (s *Store) Set(ctx context.Context, key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("settings: empty key")
	}
	if s.db != nil {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO settings (key, value, updated_at) VALUES ($1,$2,now())
			 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
			key, value)
		if err != nil {
			return fmt.Errorf("settings: set %s: %w", key, err)
		}
	}
	s.mu.Lock()
	s.cache[key] = value
	s.mu.Unlock()
	return nil
}

// String 返回字符串参数；未配置时回落到 Defaults，再回落到 def。
func (s *Store) String(key, def string) string {
	s.mu.RLock()
	v, ok := s.cache[key]
	s.mu.RUnlock()
	if ok && v != "" {
		return v
	}
	if d, ok := Defaults[key]; ok && d != "" {
		return d
	}
	return def
}

func (s *Store) Int(key string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(s.String(key, ""))); err == nil {
		return v
	}
	return def
}

func (s *Store) Bool(key string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s.String(key, ""))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func (s *Store) Duration(key string, def time.Duration) time.Duration {
	if n := s.Int(key, -1); n >= 0 {
		return time.Duration(n) * time.Second
	}
	return def
}

// List 返回所有字符串切片类参数（逗号分隔）。
func (s *Store) List(key string) []string {
	raw := s.String(key, "")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Snapshot 返回当前缓存的副本（给前端/审计用）。
func (s *Store) Snapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.cache))
	for k, v := range s.cache {
		out[k] = v
	}
	return out
}
