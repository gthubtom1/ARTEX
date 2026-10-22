// Package audit 把每一步动作落库（audit_log），**静默、不打断运行**。
//
// 设计纪律：审计是"后台记录"，不是"运行期确认"——
// 任何一次 Log 失败都只写日志，绝不阻塞 agent 执行，也绝不返回错误给模型。
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"time"

	"github.com/Autumn-27/artex/internal/dbiface"
)

// Entry 是一条审计记录。
type Entry struct {
	TaskID int64          `json:"task_id,omitempty"`
	Actor  string         `json:"actor,omitempty"`  // planner|worker:<id>|adviser|human|tool
	Tool   string         `json:"tool,omitempty"`   // Bash|nmap|report_finding|...
	Target string         `json:"target,omitempty"` // 目标 host/IP/URL
	Action string         `json:"action,omitempty"` // exec|write|read|net|install|switch
	Result string         `json:"result,omitempty"` // ok|fail|timeout|degraded
	Err    string         `json:"err,omitempty"`
	Args   []byte         `json:"-"` // 原始参数（只存摘要，不存原文）
	Meta   map[string]any `json:"meta,omitempty"`
}

// Logger 写 audit_log。
type Logger struct {
	db dbiface.DB
}

func New(db dbiface.DB) *Logger { return &Logger{db: db} }

// Nop 返回一个空实现（db 为 nil 时 Log 直接返回）。
func Nop() *Logger { return &Logger{} }

// Digest 返回参数摘要（sha256 前 16 字节 hex）。原文永不落库。
func Digest(args []byte) string {
	if len(args) == 0 {
		return ""
	}
	sum := sha256.Sum256(args)
	return hex.EncodeToString(sum[:16])
}

// Log 写入一条记录。best-effort：失败只打日志，不返回错误、不阻塞调用方。
func (l *Logger) Log(ctx context.Context, e Entry) {
	if l == nil || l.db == nil {
		return
	}
	meta, _ := json.Marshal(e.Meta)
	if len(meta) == 0 || string(meta) == "null" {
		meta = []byte("{}")
	}
	// 超时保护：审计永远不能拖慢主流程。
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := l.db.ExecContext(c,
		`INSERT INTO audit_log (task_id, actor, tool, target, action, result, args_digest, err, meta)
		 VALUES ($1::bigint, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`,
		e.TaskID, e.Actor, e.Tool, e.Target, e.Action, e.Result, Digest(e.Args), e.Err, string(meta))
	if err != nil {
		log.Printf("[audit] write failed (ignored): %v", err)
	}
}

// LogOK / LogFail 是常用的两个快捷方式。
func (l *Logger) LogOK(ctx context.Context, taskID int64, actor, tool, target, action string, args []byte) {
	l.Log(ctx, Entry{TaskID: taskID, Actor: actor, Tool: tool, Target: target, Action: action, Result: "ok", Args: args})
}

func (l *Logger) LogFail(ctx context.Context, taskID int64, actor, tool, target, action, errMsg string, args []byte) {
	l.Log(ctx, Entry{TaskID: taskID, Actor: actor, Tool: tool, Target: target, Action: action, Result: "fail", Err: errMsg, Args: args})
}
