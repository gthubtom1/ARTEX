package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/internal/impact"
)

// 目标对齐（Goal Fit）—— 零基础用户真正的问题不是"有哪些漏洞"，而是
// 「我的目标是拖库／进后台／改钱，现在这堆洞到底能不能做到？做不到还差什么？」
//
// 设计依据（2026-10-24 调研）：
//   - 业界共识一：验证必须**确定性**（Basilisk 的原则："不问模型成没成，只认证据"）。
//     所以这里全部用 impact.Stats（凭据库/台账/证据词）+ 战果行状态算，不调模型。
//   - 业界共识二：**链式思维**（Chain Thinking）——每个洞只问"它让我拿到了什么下一个位置"。
//     所以每个目标给的是"需要的几个能力 + 现在有哪几个 + 缺哪个"。
//
// 纪律：只读。它只是把已有证据翻译成"你的目标能到什么程度"，不派活、不改状态。

type goalDef struct {
	Key   string   // 机器名
	Label string   // 人话（选择器里显示）
	Need  []string // 需要的战果行 key（全部满足才算"能"）
	Level int      // 至少要到哪一级（impact.Level 数值）
	Risk  string   // 现实风险（给人做决定用）
	Why   string   // 一句话解释"为什么需要这些"
}

var goalDefs = []goalDef{
	{
		Key: "data_dump", Label: "把数据库拖下来（用户/订单/卡密）",
		Need: []string{"db", "data"}, Level: int(impact.E4),
		Why:  "要能查库、并且真的读回过业务数据，才算「拖得下来」",
		Risk: "整表导出量大、易被审计记录；建议先取样本再决定量",
	},
	{
		Key: "admin_access", Label: "进后台（管理员界面）",
		Need: []string{"admin"}, Level: int(impact.E2),
		Why:  "要有后台会话或管理员口令，缺一个就只能「猜」",
		Risk: "登录行为会留日志；账号锁定策略可能误伤",
	},
	{
		Key: "sample", Label: "先看几条真实数据（确认真假）",
		Need: []string{"data"}, Level: int(impact.E4),
		Why:  "样本 = 真的读到过业务数据（≤3 条就够）",
		Risk: "读到即涉及真实用户数据，取多少由你定",
	},
	{
		Key: "money", Label: "改钱（加余额/审批放行）",
		Need: []string{"admin", "db"}, Level: int(impact.E2),
		Why:  "改钱要同时能进后台、并且能写库（两条都硬）",
		Risk: "写操作改真实业务数据，会留变更记录，风险最高",
	},
	{
		Key: "account", Label: "开一个管理员账号",
		Need: []string{"admin"}, Level: int(impact.E2),
		Why:  "开号必须先进后台或有命令执行",
		Risk: "新增账号容易被管理员发现",
	},
	{
		Key: "deface", Label: "改首页 / 挂广告 / 换收款地址",
		Need: []string{"filewrite"}, Level: int(impact.E3),
		Why:  "要能落文件（上传或写权限）才谈得上改页面",
		Risk: "改页面立即对外可见，最容易暴露",
	},
	{
		Key: "server", Label: "控制服务器（命令执行）",
		Need: []string{"rce"}, Level: int(impact.E3),
		Why:  "命令执行是「控制机器」的唯一硬证据",
		Risk: "命令执行噪音大，可能触发主机防护",
	},
	{
		Key: "persist", Label: "留后门（账号/计划任务）",
		Need: []string{"rce"}, Level: int(impact.E3),
		Why:  "持久化要先有命令执行",
		Risk: "留后门是入侵痕迹里最重的一类，任务结束应清理",
	},
}

func goalDefByKey(k string) (goalDef, bool) {
	for _, d := range goalDefs {
		if d.Key == k {
			return d, true
		}
	}
	return goalDef{}, false
}

type goalFit struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Status  string   `json:"status"` // yes|one_step|no
	Have    []string `json:"have"`   // 已经满足的能力（人话）
	Missing []string `json:"missing"`
	Level   string   `json:"level"`
	Why     string   `json:"why"`
	Risk    string   `json:"risk"`
	Steps   []string `json:"steps"`  // 推荐路径（最多 3 条，一句话一步）
	SayIt   string   `json:"say_it"` // 可直接复制的"该说什么"
}

// fitForGoal 是纯函数：给定目标定义 + 能力计数 + 战果行，判"能不能达到"。
func fitForGoal(def goalDef, st impact.Stats, rows []lootRow) goalFit {
	done := map[string]bool{}
	for _, r := range rows {
		if r.Status == "done" {
			done[r.Key] = true
		}
	}
	lv, _ := impact.Compute(st)

	out := goalFit{Key: def.Key, Label: def.Label, Level: lv.String(), Why: def.Why, Risk: def.Risk}
	have := []string{}
	missing := []string{}
	for _, need := range def.Need {
		label := lootRowLabel(need)
		// 只用**能力**判（rowCapability 与等级同源）。
		// 真机踩过：战果行 status=done 依赖"漏洞文字里出现关键词"，#14 明明已经有后台会话、
		// 有业务数据样本，但 admin/data 两行没被文字匹配到 → 目标误判成"做不到"。
		if rowCapability(need, st) {
			have = append(have, label)
			continue
		}
		missing = append(missing, label)
	}
	out.Have = have
	out.Missing = missing
	switch {
	case len(missing) == 0:
		out.Status = "yes"
	case len(have) > 0:
		out.Status = "one_step"
	default:
		out.Status = "no"
	}

	// 推荐路径：先补齐缺的能力，再谈业务动作。
	steps := []string{}
	for _, need := range def.Need {
		if !done[need] || !rowCapability(need, st) {
			steps = append(steps, missingStep(need))
		}
	}
	if out.Status == "yes" {
		steps = append(steps, "能力已经够了：先做最小验证（1 条样本 / 一次登录）留证，再按你的量做")
	}
	steps = append(steps, "做完把证据登记（记录表名/字段/条数），等级才会涨")
	out.Steps = steps
	out.SayIt = goalSayIt(def.Key, out.Status, missing)
	return out
}

// missingStep 把"缺哪个能力"翻译成一句能照着说的话。
func missingStep(key string) string {
	switch key {
	case "db":
		return "先拿到数据库读取通道（注入/配置里的连接串/越权接口），并 SELECT 版本号确认能查"
	case "data":
		return "用读通道取 1 条业务数据（订单/用户各 1 条），确认读的是真实数据"
	case "admin":
		return "先拿到后台会话（默认口令/登录注入/越权），进后台首页即算"
	case "filewrite":
		return "找一个能落文件的上传点（只传无害验证文件），确认能访问"
	case "rce":
		return "把文件/上传变成命令回显（最小命令 id 即可）"
	}
	return "补齐「" + lootRowLabel(key) + "」"
}

func goalSayIt(key, status string, missing []string) string {
	if status == "yes" {
		switch key {
		case "data_dump":
			return "能力已经够了：按订单表先取 20 条、字段名一起给我，别整表拖"
		case "admin_access":
			return "进后台把用户列表和订单各截一屏，够证明能读到就行"
		case "money":
			return "给这个账号加 100，把改前改后都记下来"
		case "sample":
			return "取 3 条真实数据样本（脱敏），确认不是测试数据"
		}
		return "先做一次最小验证并留证"
	}
	if len(missing) > 0 {
		return "现在目标是「" + lootRowLabelByLabel(missing[0]) + "」，先把它补上：" + missingStep(missingKeyFor(key, missing))
	}
	return "先把入口摸全，再回来看这个目标"
}

// missingKeyFor / lootRowLabelByLabel 只是为了让 say_it 里能同时给出"人话"和"下一步"。
func missingKeyFor(goalKey string, missing []string) string {
	def, ok := goalDefByKey(goalKey)
	if !ok {
		return ""
	}
	for _, need := range def.Need {
		for _, m := range missing {
			if lootRowLabel(need) == m {
				return need
			}
		}
	}
	return def.Need[0]
}

func lootRowLabelByLabel(label string) string { return label }

// briefGoalFit 给 /brief 用：所有目标的状态 + 默认目标（按任务 goal 文本猜）。
func (s *Server) briefGoalFit(st impact.Stats, rows []lootRow, goalText string) ([]goalFit, string) {
	out := make([]goalFit, 0, len(goalDefs))
	for _, d := range goalDefs {
		out = append(out, fitForGoal(d, st, rows))
	}
	return out, guessGoalKey(goalText)
}

// guessGoalKey 从任务目标里猜用户想要什么（纯规则，猜不到就默认"先看几条真实数据"）。
func guessGoalKey(goal string) string {
	t := strings.ToLower(goal)
	switch {
	case strings.Contains(t, "拖库") || strings.Contains(t, "导出") || strings.Contains(t, "订单表") || strings.Contains(t, "数据库"):
		return "data_dump"
	case strings.Contains(t, "后台") || strings.Contains(t, "管理员") || strings.Contains(t, "登录"):
		return "admin_access"
	case strings.Contains(t, "改钱") || strings.Contains(t, "余额") || strings.Contains(t, "审批") ||
		strings.Contains(t, "加钱") || strings.Contains(t, "加 100") || strings.Contains(t, "加100") ||
		strings.Contains(t, "充值") || strings.Contains(t, "改金额"):
		return "money"
	case strings.Contains(t, "首页") || strings.Contains(t, "挂马") || strings.Contains(t, "收款"):
		return "deface"
	case strings.Contains(t, "命令执行") || strings.Contains(t, "服务器") || strings.Contains(t, "shell"):
		return "server"
	}
	return "sample"
}

func (s *Server) registerGoalFitRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/fit", s.taskGoalFit)
}

func (s *Server) taskGoalFit(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	st := s.impactStats(r.Context(), t, taskID)
	rows := s.lootRowsForTask(r.Context(), t, s.lootFindings(t))
	fits, guess := s.briefGoalFit(st, rows, t.Goal)
	writeJSON(w, http.StatusOK, map[string]any{"fits": fits, "guess": guess})
}

// ── 失败归因（确定性）────────────────────────────────────────────────
//
// 零基础用户跑完没成果时，最需要一句话：「卡在哪」，而不是"还没探够"。
// 判据只用库里已有的事实：错误事件分类（target_blocked / self_error / env_error…）+ 是否没证据。
// 不调模型、不猜。

type blocker struct {
	Class  string `json:"class"`
	Count  int    `json:"count"`
	Advice string `json:"advice"` // 一句人话：怎么绕
}

// blockerAdvice 把错误类翻成"下一步怎么做"。
func blockerAdvice(class string) string {
	switch class {
	case "target_blocked":
		return "目标在拦（WAF/403/验证码）：换手法或降低频率，别硬撞"
	case "auth_required":
		return "需要登录态：先拿到后台会话或凭据，再回来打这条路"
	case "env_error":
		return "是我们这边环境问题（工具/网络），换一条等价命令重试"
	case "self_error":
		return "是命令本身写错了（我方失误），直接重写重试"
	case "timeout":
		return "超时：把动作拆小（限定条数/超时时间），别一次拉太多"
	case "not_found":
		return "入口不存在或路径错了：先用只读探测确认真实路径"
	}
	return "这条路的失败原因未归类，建议换手法再试一次并留证"
}

// briefBlockers 读最近 6 小时的错误事件，按分类聚合（最多 3 条，多的不展示）。
func (s *Server) briefBlockers(ctx context.Context, taskID int64) []blocker {
	out := []blocker{}
	if s.m == nil || s.m.pg == nil || taskID <= 0 {
		return out
	}
	rows, err := s.m.pg.QueryContext(ctx, `SELECT COALESCE(class,'unknown'), count(*)
		FROM task_error_events WHERE task_id=$1 AND ts > now() - interval '6 hours'
		GROUP BY class ORDER BY count(*) DESC LIMIT 3`, taskID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var class string
		var n int
		if rows.Scan(&class, &n) == nil && n > 0 {
			out = append(out, blocker{Class: class, Count: n, Advice: blockerAdvice(class)})
		}
	}
	return out
}
