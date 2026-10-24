package server

import (
	"net/http"
	"strings"
)

// 动态快捷短语（P1）：按"这个站现在到底拿到了什么"生成 3–5 条，点一下只**填入**输入框。
//
// 为什么不用全局常用语就够：全局那 30 条是"任何时候都可能有用的话"；
// 这里要的是"此刻最该说的那一句" —— 已经进后台和还没进后台，该说的话完全不同。
//
// 纪律：
//   - 纯规则、确定性（同输入同输出），不调模型、不写库、不花 token；
//   - 只生成"说法"，不触发任何动作；点一下只填进输入框，发不发由人决定（P2 的铁律）；
//   - 数量硬性 3–5 条：少于 3 条用通用兜底补齐，多于 5 条按优先级截断。

type taskPhrase struct {
	Key  string `json:"key"`
	Text string `json:"text"`
	Why  string `json:"why"` // 为什么现在说这句（悬停提示，也让人能判断要不要删）
}

type taskPhrasesResp struct {
	Phrases []taskPhrase `json:"phrases"`
	Source  string       `json:"source"` // rule（P1 只做规则版）
	Has     []string     `json:"has"`    // 命中的战果分类，方便前端解释为什么是这几条
}

func (s *Server) registerPhraseRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/phrases", s.taskPhrases)
}

func (s *Server) taskPhrases(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	fs := s.lootFindings(t)
	rows := s.lootRowsForTask(r.Context(), t, fs)
	phrases, has := buildTaskPhrases(fs, rows)
	writeJSON(w, http.StatusOK, taskPhrasesResp{Phrases: phrases, Source: "rule", Has: has})
}

// buildTaskPhrases 是唯一的生成入口（纯函数，可单测）。
// 返回 (3–5 条短语, 命中的战果分类)。
func buildTaskPhrases(fs []lootFinding, rows []lootRow) ([]taskPhrase, []string) {
	done := map[string]bool{}
	tried := map[string]bool{}
	for _, r := range rows {
		switch r.Status {
		case "done":
			done[r.Key] = true
		case "tried":
			tried[r.Key] = true
		}
	}
	titles := make([]string, 0, len(fs))
	for _, f := range fs {
		titles = append(titles, f.title+" "+f.summary)
	}
	blob := strings.ToLower(strings.Join(titles, " | "))
	hasWord := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(blob, strings.ToLower(s)) {
				return true
			}
		}
		return false
	}

	var has []string
	cand := []taskPhrase{}

	// ① 已经有命令执行：最值钱的是"从机器里往外捞东西"，不是再打一个洞。
	if done["rce"] {
		has = append(has, "shell")
		cand = append(cand, taskPhrase{
			Key:  "rce_config",
			Text: "在服务器上读一下数据库连接串、.env 和后台配置，看还有哪些库和账号能用",
			Why:  "已经有命令执行：配置里的凭据能直接把权限扩到数据库和内网",
		}, taskPhrase{
			Key:  "rce_proof",
			Text: "把现在的命令执行证据补完整：命令、回显、机器名一起留档",
			Why:  "命令执行是最高价值的结论，要能复现才算数",
		})
	}
	// ② 拿到后台：用户/订单数据是委托人最关心的现实后果。
	if done["admin"] {
		has = append(has, "admin")
		cand = append(cand, taskPhrase{
			Key:  "admin_data",
			Text: "进后台把用户列表和订单各截一屏，够证明能读到真实数据就行",
			Why:  "已进后台：先取证，别急着改数据",
		})
	}
	// ③ 有数据库/数据读取：把它变成"可核对的样本"。
	if done["db"] || done["data"] {
		has = append(has, "db")
		cand = append(cand, taskPhrase{
			Key:  "db_sample",
			Text: "把订单表按时间倒序取前 20 条，字段名和条数一起给我",
			Why:  "已能读库：样本量 + 字段名一起给，才算读到业务数据",
		})
	}
	// ④ 有凭据但没验证：先验证一次，别把"可能可用"当结论。
	if done["cred"] || hasWord("凭据", "口令", "credential") {
		has = append(has, "cred")
		cand = append(cand, taskPhrase{
			Key:  "cred_verify",
			Text: "用这条凭据登录一次，把登录成功的页面和账号权限范围记下来",
			Why:  "凭据没验证过就不算战果",
		})
	}
	// ⑤ 能落文件没拿到 shell：往"文件 → 命令"推一步。
	if done["filewrite"] || hasWord("上传", "写文件", "webshell") {
		has = append(has, "filewrite")
		cand = append(cand, taskPhrase{
			Key:  "filewrite_rce",
			Text: "试一下上传的文件能不能被访问、能不能执行命令，只做一次最小验证",
			Why:  "能落文件离命令执行只差一步",
		})
	}
	// ⑥ 有洞但没收益：催"从确认推到达成"。
	if len(cand) == 0 && len(fs) > 0 {
		cand = append(cand, taskPhrase{
			Key:  "push_gain",
			Text: "别停在报错回显：这个洞能不能读回真实数据？给一个能证明的最小动作",
			Why:  "已确认漏洞但还没有任何实际收益",
		})
	}
	// ⑦ 试过没成：让人决定换手法还是放弃。
	if tried["rce"] || tried["filewrite"] || tried["lateral"] {
		cand = append(cand, taskPhrase{
			Key:  "retry_or_drop",
			Text: "这条路试过没通，给出两种换手法；都不行就标记放弃，别耗时间",
			Why:  "有路线已经试过但没成",
		})
	}

	// 通用兜底：保证至少 3 条，且都能"只填入不发送"。
	generics := []taskPhrase{
		{Key: "summary3", Text: "把现在的结论写成三句话：拿到了什么、还差什么、下一步最值得做什么", Why: "随时可问，用来对齐进度"},
		{Key: "next_step", Text: "下一步你建议我先做哪一件事？只说一件，并给判据", Why: "避免一次派太多活"},
		{Key: "alt_route", Text: "如果这条路走不通，列两条备选路线和各自的代价", Why: "防止单点卡死"},
		{Key: "evidence", Text: "把这条结论的原始请求和响应贴出来，我要能自己复现", Why: "结论必须可复现"},
	}
	for _, g := range generics {
		if len(cand) >= 5 {
			break
		}
		cand = append(cand, g)
	}
	if len(cand) > 5 {
		cand = cand[:5]
	}
	// 去重（同一个 key 只留第一条），并保证至少 3 条。
	seen := map[string]bool{}
	out := make([]taskPhrase, 0, len(cand))
	for _, p := range cand {
		if seen[p.Key] {
			continue
		}
		seen[p.Key] = true
		out = append(out, p)
	}
	for len(out) < 3 && len(generics) > 0 {
		out = append(out, generics[len(out)%len(generics)])
	}
	if has == nil {
		has = []string{}
	}
	return out, has
}
