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

type goalMove struct {
	Text   string `json:"text"`   // 一句话：去做什么
	Why    string `json:"why"`    // 为什么现在做这个（依据）
	Cost   string `json:"cost"`   // 花多少（请求/时间）
	Expect string `json:"expect"` // 成功长什么样
	Fail   string `json:"fail"`   // 失败了怎么办
}

type goalFit struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Status string `json:"status"` // yes|one_step|no
	// Applicable：这个站**看起来**支持不支持这个目标（与"有没有能力"是两件事）。
	// 用户抱怨过："我让它拖库，可这站根本没数据库"——所以要先把"这站像不像有"说清楚。
	Applicable    string     `json:"applicable"` // likely|unknown|unlikely
	ApplicableWhy string     `json:"applicable_why"`
	NextMoves     []goalMove `json:"next_moves,omitempty"`
	// 三个口味：继续 / 换路线 / 收工（点了只填入，不发送）
	SayContinue string   `json:"say_continue,omitempty"`
	SayPivot    string   `json:"say_pivot,omitempty"`
	SayStop     string   `json:"say_stop,omitempty"`
	Have        []string `json:"have"` // 已经满足的能力（人话）
	Missing     []string `json:"missing"`
	Level       string   `json:"level"`
	Why         string   `json:"why"`
	Risk        string   `json:"risk"`
	Steps       []string `json:"steps"`  // 推荐路径（最多 3 条，一句话一步）
	SayIt       string   `json:"say_it"` // 可直接复制的"该说什么"
}

// fitForGoal 是纯函数：给定目标定义 + 能力计数 + 战果行，判"能不能达到"。
func fitForGoal(def goalDef, st impact.Stats, rows []lootRow, fs []lootFinding, blockers []blocker) goalFit {
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
	out.Applicable, out.ApplicableWhy = applyApplicability(def, fs, rows)
	out.NextMoves = buildNextMoves(def, out, fs, blockers)
	out.SayContinue, out.SayPivot, out.SayStop = buildSayVariants(def, out, blockers)
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
func (s *Server) briefGoalFit(st impact.Stats, rows []lootRow, fs []lootFinding, goalText string, blockers []blocker) ([]goalFit, string) {
	out := make([]goalFit, 0, len(goalDefs))
	for _, d := range goalDefs {
		out = append(out, fitForGoal(d, st, rows, fs, blockers))
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
	blockers := s.briefBlockers(r.Context(), taskID)
	fits, guess := s.briefGoalFit(st, rows, s.lootFindings(t), t.Goal, blockers)
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
	case "policy_blocked":
		return "被策略/规则拦下（不是目标拒绝）：换一条等价手法或换个入口再试，别重复同一条"
	case "rate_limited":
		return "被限速：降低频率、加延时，或改打别的接口"
	case "waf_blocked":
		return "被 WAF 拦：换编码/换参数位置，或先找没防护的接口"
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

// ── 自适应：这个站像不像支持这个目标（与"有没有能力"分开判）──────────
//
// 用户原话："我太傻了，他明明这个网站没有数据库，我让他去脱库，会怎么样呢？"
// 所以要先用**站点迹象**回答"这站有没有这类东西"，再谈"我拿到了没有"。

// signalSet 是判"站点迹象"用的关键词组（与目标一一对应）。
var goalSignals = map[string][]string{
	"data_dump":    {"sql", "注入", "数据库", "information_schema", "select", "mysql", "mssql", "postgres", "oracle", "连接串", "db_"},
	"sample":       {"sql", "注入", "数据库", "越权", "idor", "未授权", "api", "数据"},
	"admin_access": {"登录", "后台", "admin", "login", "管理", "console", "口令", "认证"},
	"money":        {"订单", "支付", "余额", "提现", "审批", "金额", "钱包"},
	"account":      {"登录", "后台", "admin", "管理", "注册"},
	"deface":       {"上传", "upload", "写文件", "模板", "编辑器", "cms"},
	"server":       {"rce", "命令执行", "代码执行", "反序列化", "shell", "模板注入", "ssti"},
	"persist":      {"rce", "命令执行", "代码执行", "反序列化", "shell", "ssh"},
}

// applyApplicability 用"任务里已确认的漏洞文字 + 战果行是否存在"判这个目标的把握。
func applyApplicability(def goalDef, fs []lootFinding, rows []lootRow) (string, string) {
	// 战果行存在（哪怕 blank 也算出现过迹象）：说明站点这类东西是存在的
	for _, r := range rows {
		for _, need := range def.Need {
			if r.Key == need {
				return "likely", "已经看到「" + r.Label + "」这一类的迹象"
			}
		}
	}
	// P0-07：与战况卡三段式用**同一张表**（findingRowHints）。
	// 否则会出现"卡片上说差一步：进后台，目标那块说这站看不出有这类东西"——同一屏自相矛盾。
	// 例：XSS 通向后台（差一步），所以"进后台"这个目标是 likely，而不是 unlikely。
	for _, f := range fs {
		blob1 := f.title + " " + f.text + " " + f.summary
		for _, h := range findingRowHints {
			if !containsAnyFold(blob1, h.kws...) {
				continue
			}
			for _, k := range h.rows {
				for _, need := range def.Need {
					if k == need {
						return "likely", "已确认的漏洞里有通向「" + lootRowLabel(k) + "」的类型（还差把它变成实际访问）"
					}
				}
			}
		}
	}
	blob := strings.Builder{}
	for _, f := range fs {
		blob.WriteString(f.title + " " + f.text + " " + f.summary + " ")
	}
	text := strings.ToLower(blob.String())
	kws := goalSignals[def.Key]
	for _, kw := range kws {
		if strings.Contains(text, strings.ToLower(kw)) {
			return "likely", "已确认的漏洞里出现「" + kw + "」相关迹象"
		}
	}
	if len(fs) == 0 {
		return "unknown", "还没有任何已确认漏洞，先做侦察（①模板）再判断"
	}
	return "unlikely", "已确认的漏洞里没有任何与「" + def.Label + "」相关的迹象（没入口/没参数/没连接串）"
}

// buildNextMoves 给 Top3 建议：先排阻塞，再补最近的缺口，最后才是"换路线"。
// 每条都带成本 / 成功长相 / 失败怎么办 —— 用户靠这三样做决定。
func buildNextMoves(def goalDef, fit goalFit, fs []lootFinding, blockers []blocker) []goalMove {
	out := []goalMove{}
	// ① 有阻塞先解阻塞（最省钱的一步）
	if len(blockers) > 0 {
		b := blockers[0]
		out = append(out, goalMove{
			Text: "先把「" + b.Class + "」这个阻塞处理掉：" + b.Advice,
			Why:  "最近 6 小时这类失败 " + strconv.Itoa(b.Count) + " 次，不解决后面都是白跑",
			Cost: "1–3 次请求", Expect: "同样的请求不再被拦（状态码/响应长度正常）",
			Fail: "连续两次还是被拦 → 换手法或收工出结论",
		})
	}
	// ② 补最接近的缺口
	for _, need := range def.Need {
		if !containsStr(fit.Have, lootRowLabel(need)) {
			out = append(out, goalMove{
				Text: missingStep(need),
				Why:  "这个目标是「" + def.Label + "」，缺这一环就做不到",
				Cost: "1–5 次请求", Expect: successLooksLike(need),
				Fail: "两次不成立 → 把这条标「不适用」，换下一个目标",
			})
			break
		}
	}
	// ③ 能力齐了：做最小验证 + 留证
	if fit.Status == "yes" {
		out = append(out, goalMove{
			Text: fit.SayIt, Why: "能力已经够了，先做最小验证并留证，再按你要的量做",
			Cost: "1 次请求", Expect: "拿到 1 条可核对的数据/一次成功登录", Fail: "拿不到就说明能力是虚的，回退到上一个缺口",
		})
	}
	// ④ 兜底：换路线（从已确认漏洞里挑一条最接近的）
	if len(out) < 3 && len(fs) > 0 {
		f := fs[0]
		out = append(out, goalMove{
			Text: "换一条线：从「" + f.title + "」往它的下一步推（去掉 payload 再发一遍做对照，确认差异）",
			Why:  "这是当前最接近可用的一条已确认漏洞",
			Cost: "1–2 次请求", Expect: "响应出现可对比差异", Fail: "没差异 → 标「试过未成」，换下一条",
		})
	}
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// successLooksLike 把"缺的能力"翻成"成功长什么样"（用户靠这个判断成没成）。
func successLooksLike(need string) string {
	switch need {
	case "db":
		return "注入点能回显数据库版本号，或连接串能连上"
	case "data":
		return "返回 1 条真实业务数据（订单号/用户名一类）"
	case "admin":
		return "后台首页能打开（有菜单/用户管理）"
	case "filewrite":
		return "上传的文件能通过 URL 访问到"
	case "rce":
		return "命令回显（id/whoami 有输出）"
	}
	return "对应能力出现可核对的证据"
}

// buildSayVariants 三个口味：继续 / 换路线 / 收工。全部由当前证据拼，不用固定模板。
func buildSayVariants(def goalDef, fit goalFit, blockers []blocker) (string, string, string) {
	goalWord := def.Label
	cont := ""
	switch fit.Status {
	case "yes":
		cont = "目标是「" + goalWord + "」，能力已经够了：" + fit.SayIt + "。先做最小验证并留证，别一上来就整表拉。"
	case "one_step":
		cont = "目标是「" + goalWord + "」，现在是差一步（缺：" + strings.Join(fit.Missing, "/") + "）。先去把它补上：" + fit.Steps[0]
	default:
		if fit.Applicable == "unlikely" {
			cont = "目标是「" + goalWord + "」，但这站目前没看到相关迹象（" + fit.ApplicableWhy + "）。先花 5 分钟只读确认一遍再决定，别直接上量。"
		} else {
			cont = "目标是「" + goalWord + "」，现在什么都不具备。先按最便宜的一步试：" + fit.Steps[0]
		}
	}
	pivot := "「" + goalWord + "」这条线先放一放，换两条更便宜的：① 先摸登录/上传面看有没有更直接的入口；② 把已确认的漏洞里最像能读数据的那条做一次对照验证。给我两条路各自的第一步。"
	stop := "用现在已有的证据出报告并停：写清「进到哪一级、拿到了什么、还差什么」，别再试新的手法。"
	if len(blockers) > 0 {
		stop = "已经卡在「" + blockers[0].Class + "」上了，先出当前结论报告并停，别继续烧；要再试就明确换一个手法。"
	}
	return cont, pivot, stop
}
