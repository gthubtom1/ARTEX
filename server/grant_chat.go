package server

import (
	"context"
	"strconv"
	"strings"
)

// maybeGrantFromChat 把"用户在对话里明确要求去做"当成授权。
//
// 背景（2026-10-24 用户原话）：「我不知道它那种漏洞有什么用……我要的是我知道现在进度到哪、
// 可以做哪些东西。真正要动手我就在对话里说：帮我拿数据库信息、帮我挂个 webshell。」
// 所以：不需要用户去点"允许"按钮，他说这句话本身就是授权。
//
// 只在用户消息里出现明确动词时才授权，避免误判（例如"不要挂 webshell"→ 含否定词则跳过）。
func (s *Server) maybeGrantFromChat(ctx context.Context, t *Task, msg string) {
	if s == nil || t == nil || s.m == nil || s.m.pg == nil {
		return
	}
	text := strings.ToLower(strings.TrimSpace(msg))
	if text == "" {
		return
	}
	for _, neg := range []string{"不要", "别", "禁止", "不用", "先不"} {
		if strings.Contains(text, neg) {
			return // 否定句不当授权
		}
	}
	cls := ""
	switch {
	case strings.Contains(text, "删库") || strings.Contains(text, "删表") || strings.Contains(text, "drop") ||
		strings.Contains(text, "清日志") || strings.Contains(text, "删文件") || strings.Contains(text, "格式化"):
		cls = "destructive"
	case strings.Contains(text, "webshell") || strings.Contains(text, "木马") || strings.Contains(text, "上传") ||
		strings.Contains(text, "挂马") || strings.Contains(text, "改页面") || strings.Contains(text, "劫持"):
		cls = "file_write"
	case strings.Contains(text, "改钱") || strings.Contains(text, "加钱") || strings.Contains(text, "余额") ||
		strings.Contains(text, "改数据") || strings.Contains(text, "改状态") || strings.Contains(text, "开号") ||
		strings.Contains(text, "加管理员") || strings.Contains(text, "审批") || strings.Contains(text, "改金额"):
		cls = "data_write"
	case strings.Contains(text, "数据库") || strings.Contains(text, "拉数据") || strings.Contains(text, "取数据") ||
		strings.Contains(text, "读取数据") || strings.Contains(text, "拖库"):
		cls = "data_dump"
	case strings.Contains(text, "留账号") || strings.Contains(text, "后门") || strings.Contains(text, "计划任务"):
		cls = "persist"
	case strings.Contains(text, "横移") || strings.Contains(text, "内网") || strings.Contains(text, "跳板"):
		cls = "lateral"
	}
	if cls == "" {
		return
	}
	taskID, err := strconv.ParseInt(t.ID, 10, 64)
	if err != nil || taskID <= 0 {
		return
	}
	if _, err := s.m.pg.ExecContext(ctx, `INSERT INTO task_grants(task_id,action_class,granted,note,decided_at)
		VALUES($1,$2,true,'来自主对话指令',now())
		ON CONFLICT (task_id,action_class) DO UPDATE SET granted=true, note='来自主对话指令', decided_at=now()`,
		taskID, cls); err != nil {
		return
	}
	s.clearGrantCache(taskID)
	s.appendReviewActivity(ctx, taskID, "已按你在对话里的要求授权：「"+classCN(cls)+"」，接下来这一步会真的执行。")
}
