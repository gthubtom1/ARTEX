package server

import (
	"context"
	"strings"
	"time"

	"github.com/Autumn-27/artex/agent"
)

// 「人的话是最高级别」（P4 收口 2026-10-24）。
//
// 背景（用户原话）：「我肯定可以在对话里面和他沟通，我的话语就是最高级别，
// 那个一些东西它是拦不了我说的话的。比如说我说我要删库，那就是要删库。」
//
// 分两层，别混：
//  1. 影响阶梯授权（task_grants）：我允许它做"这一类事"（写文件/拖库/持久化/横移）；
//  2. 工具审批（intercept ask 规则）：拦的是"具体这条命令"（rm -rf / DROP / 写系统）。
//
// 这里把 2 接回 1：**同一任务里人刚亲口说过这一类事（30 分钟内），工具审批就不再问**，
// 直接记成 allowed（decision_source 里写明"按对话授权自动放行"）。
// 人没说话的场合一切照旧 —— 拦截层不动，模型仍然会被拦、会问人。

const humanGrantWindow = 30 * time.Minute

// recentChatGrants 取本任务"来自主对话指令"的已授权类别（窗口期内）。
func (s *Server) recentChatGrants(taskID int64) map[string]bool {
	out := map[string]bool{}
	if s.m == nil || s.m.pg == nil || taskID <= 0 {
		return out
	}
	rows, err := s.m.pg.Query(`SELECT action_class FROM task_grants
		WHERE task_id=$1 AND granted=true AND note='来自主对话指令'
		  AND decided_at > now() - $2::interval`, taskID, humanGrantWindow.String())
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var cls string
		if rows.Scan(&cls) == nil {
			out[cls] = true
		}
	}
	return out
}

// commandClass 从工具名 + 命令原文判断"这是哪一类动作"（纯函数，可单测）。
// 只认明确的破坏/利用特征，宁可返回空（=照常问人），也不猜。
func commandClass(toolName, input string) string {
	t := strings.ToLower(toolName + " " + input)
	switch {
	// 破坏性：这条最要紧 —— 人说了删库才算，模型自己想删还是会被问
	case containsAny(t, "drop table", "drop database", "truncate ", "rm -rf /", "mkfs", "format ",
		"删库", "删表", "清空表", "清日志", "删日志", "shred ", "dd if=", "> /dev/sd"):
		return "destructive"
	// 写文件 / 上传 / 落 webshell
	case containsAny(t, "webshell", "upload", "上传", "cmd.php", "shell.php", "jsp;", ".aspx",
		"put /", "curl -o", "wget -o", "tee ", "chmod +x"):
		return "file_write"
	// 数据库读写
	case containsAny(t, "select ", "union select", "information_schema", "sqlmap", "dump", "拖库",
		"update ", "insert into", "into outfile"):
		return "data_dump"
	// 持久化
	case containsAny(t, "crontab", "systemd", "计划任务", "schtasks", "用户添加到", "useradd",
		"authorized_keys", "留账号", "后门"):
		return "persist"
	// 内网横移
	case containsAny(t, "proxychains", "chisel", "frp", "ssh -l", "psexec", "wmiexec", "横向", "跳板"):
		return "lateral"
	}
	return ""
}

func containsAny(hay string, subs ...string) bool {
	for _, s := range subs {
		if s != "" && strings.Contains(hay, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// humanAuthorizedForTool 是接给 intercept 的回调：人说过这一类事吗？
func (s *Server) humanAuthorizedForTool(ctx context.Context, toolName string, input []byte) (bool, string) {
	ri := runInfoTaskID(ctx)
	if ri <= 0 {
		return false, ""
	}
	cls := commandClass(toolName, string(input))
	if cls == "" {
		return false, ""
	}
	granted := s.recentChatGrants(ri)
	if !granted[cls] {
		return false, ""
	}
	return true, classCN(cls) + "（你在对话里说过）"
}

// runInfoTaskID 取当前执行上下文的任务 id（无上下文返回 0）。
func runInfoTaskID(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	return agent.RunInfoFrom(ctx).TaskID
}
