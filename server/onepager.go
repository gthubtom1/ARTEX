package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/internal/impact"
)

// 委托方一页纸（docs/27 决定 5）—— 拿到东西之后，唯一能交出去的那一页。
//
// 为什么需要：技术报告是给工程师看的；委托人只想知道四件事：
//   能进哪 / 拿到什么数据 / 影响多大 / 还差什么（+ 怎么修）。
// 判据全部来自已有证据（impact.Stats / 战果行 / findings），不调模型、不猜。

type onePager struct {
	TaskID   int64  `json:"task_id"`
	TaskName string `json:"task_name"`
	Goal     string `json:"goal"`
	Level    string `json:"level"`
	Basis    string `json:"basis"`
	// 能进哪：已拿到的访问（后台/命令行/数据）
	Access []string `json:"access"`
	// 拿到什么数据：表名 + 字段（来自战果行的对象抽取）
	DataObjects []lootObject `json:"data_objects"`
	// 影响多大：一句人话 + 等级
	Impact string `json:"impact"`
	// 还差什么
	Gaps []string `json:"gaps"`
	// 怎么修（按已确认漏洞类型给建议）
	Fixes []string `json:"fixes"`
	// 可直接复制的纯文本（给邮件/微信/文档）
	Text string `json:"text"`
}

// fixAdvice 把漏洞类型翻成"该怎么修"（确定性词表，覆盖常见类）。
var fixAdvice = []struct {
	kws    []string
	advice string
}{
	{[]string{"sql", "注入"}, "所有数据库查询改参数化/预编译；关闭生产环境报错回显；数据库账号最小权限"},
	{[]string{"idor", "越权", "未授权"}, "所有按 id 取数据的接口加归属校验（当前用户 == 记录属主）；统一鉴权中间件，禁止前端传角色"},
	{[]string{"xss"}, "输出按上下文转义；加 CSP；Cookie 加 HttpOnly"},
	{[]string{"上传", "写文件"}, "上传白名单后缀 + 重命名 + 落地目录禁止执行；文件类型按内容校验"},
	{[]string{"rce", "命令执行", "代码执行", "反序列化"}, "升级受影响组件；禁止反序列化不可信数据；命令执行启用白名单"},
	{[]string{"文件读取", "路径穿越", "lfi"}, "路径规范化后校验必须在允许目录内；禁止拼接用户输入"},
	{[]string{"口令", "credential", "默认密码", "弱口令"}, "强制改默认口令；启用多因素；限制登录失败次数"},
	{[]string{"jwt", "认证"}, "校验签名与算法白名单；令牌短有效期 + 服务端可吊销"},
	{[]string{"泄露", "git", "备份", "敏感信息"}, "移除 .git/备份/调试端点；配置与代码分离，密钥入密管"},
}

func fixesFor(fs []lootFinding) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, f := range fs {
		blob := strings.ToLower(f.title + " " + f.text)
		for _, rule := range fixAdvice {
			hit := false
			for _, kw := range rule.kws {
				if strings.Contains(blob, strings.ToLower(kw)) {
					hit = true
					break
				}
			}
			if hit && !seen[rule.advice] {
				seen[rule.advice] = true
				out = append(out, rule.advice)
			}
		}
	}
	if len(out) == 0 {
		out = append(out, "按已确认漏洞逐条修复；优先修能直接拿到数据或权限的那几条")
	}
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

// buildOnePager 是纯函数（可单测）：把战况/战果/漏洞翻成委托方一页纸。
func buildOnePager(t *Task, st impact.Stats, rows []lootRow, fs []lootFinding, blockers []blocker) onePager {
	lv, basis := impact.Compute(st)
	out := onePager{
		Level: lv.String(), Basis: basis, Access: []string{}, DataObjects: []lootObject{},
		Gaps: []string{}, Fixes: fixesFor(fs),
	}
	if t != nil {
		if id, err := strconv.ParseInt(t.ID, 10, 64); err == nil {
			out.TaskID = id
		}
		out.TaskName = strings.TrimSpace(t.Name)
		if out.TaskName == "" {
			out.TaskName = strings.TrimSpace(t.Description)
		}
		out.Goal = strings.TrimSpace(t.Goal)
	}

	// 能进哪
	if st.OwnedApp > 0 || st.AppControl > 0 {
		out.Access = append(out.Access, "可以登录进入管理后台（管理员界面）")
	}
	if st.Shells > 0 {
		out.Access = append(out.Access, "可以在服务器上执行命令")
	}
	if st.OwnedRoot > 0 || st.OwnedHosts > 1 {
		out.Access = append(out.Access, "持有服务器级权限 / 可跨主机")
	}
	if st.Creds > 0 {
		out.Access = append(out.Access, "手里有可用账号或口令哈希（未逐条验证）")
	}

	// 拿到什么数据（表名/字段来自战果行对象抽取）
	for _, r := range rows {
		if r.Status != "done" {
			continue
		}
		if r.Key == "data" || r.Key == "db" {
			for _, o := range r.Objects {
				out.DataObjects = append(out.DataObjects, o)
			}
		}
	}
	if len(out.DataObjects) > 6 {
		out.DataObjects = out.DataObjects[:6]
	}

	// 影响多大（一句人话，按等级落）
	switch {
	case st.DataSamples > 0 && st.Shells > 0:
		out.Impact = "最严重的一条：可以在服务器上执行命令，并且已经读到真实业务数据 —— 等于业务数据与服务器同时失守"
	case st.DataSamples > 0:
		out.Impact = "最严重的一条：可以读到真实业务数据（有样本为证）"
	case st.Shells > 0:
		out.Impact = "最严重的一条：可以在服务器上执行命令（等于控制机器）"
	case st.OwnedApp > 0 || st.AppControl > 0:
		out.Impact = "最严重的一条：可以进入管理后台，能看到并操作后台里的数据与功能"
	case st.Creds > 0:
		out.Impact = "目前有疑似可用的账号/口令，但还没有验证能登录进去"
	case st.Findings > 0:
		out.Impact = "目前只有漏洞，还没有把它变成访问权（说明风险存在，但尚未被证明可利用）"
	default:
		out.Impact = "目前只有侦察信息，还没有确认漏洞"
	}

	// 还差什么（复用目标对齐的缺口口径：能力缺失 + 阻塞）
	if st.DataSamples == 0 {
		out.Gaps = append(out.Gaps, "还没有读到业务数据样本")
	}
	if st.OwnedApp == 0 && st.AppControl == 0 {
		out.Gaps = append(out.Gaps, "还没有进入后台")
	}
	if st.Shells == 0 {
		out.Gaps = append(out.Gaps, "还没有命令执行")
	}
	for _, b := range blockers {
		out.Gaps = append(out.Gaps, "存在阻塞："+b.Class+"（"+b.Advice+"）")
	}
	if len(out.Gaps) > 4 {
		out.Gaps = out.Gaps[:4]
	}

	out.Text = renderOnePagerText(out)
	return out
}

// renderOnePagerText 生成可直接粘贴的纯文本（给邮件/微信/文档用）。
func renderOnePagerText(p onePager) string {
	var b strings.Builder
	name := p.TaskName
	if name == "" {
		name = "（未命名任务）"
	}
	b.WriteString("【渗透测试结论（一页纸）】\n")
	b.WriteString("对象：" + name + "\n")
	if p.Goal != "" {
		b.WriteString("目标：" + p.Goal + "\n")
	}
	b.WriteString("当前权限等级：" + p.Level + "（" + p.Basis + "）\n\n")

	b.WriteString("1. 能进哪\n")
	if len(p.Access) == 0 {
		b.WriteString("  - 目前还没有拿到任何访问权\n")
	} else {
		for _, a := range p.Access {
			b.WriteString("  - " + a + "\n")
		}
	}

	b.WriteString("\n2. 拿到什么数据\n")
	if len(p.DataObjects) == 0 {
		b.WriteString("  - 还没有取到业务数据样本\n")
	} else {
		for _, o := range p.DataObjects {
			// 只有表名、没抽到字段时写「字段未知」，不要留空（真机 #19 的一页纸出现过空字段）。
			fields := strings.TrimSpace(o.Fields)
			if fields == "" {
				fields = "字段未知"
			}
			b.WriteString("  - " + o.Name + "（字段：" + fields + "）\n")
		}
	}

	b.WriteString("\n3. 影响多大\n  - " + p.Impact + "\n")

	b.WriteString("\n4. 还差什么\n")
	if len(p.Gaps) == 0 {
		b.WriteString("  - 已确认的目标都达到；未探到的面见技术报告\n")
	} else {
		for _, g := range p.Gaps {
			b.WriteString("  - " + g + "\n")
		}
	}

	b.WriteString("\n5. 怎么修（优先修能直接拿到数据或权限的那几条）\n")
	for _, f := range p.Fixes {
		b.WriteString("  - " + f + "\n")
	}
	return b.String()
}

func (s *Server) registerOnePagerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/onepager", s.taskOnePager)
}

func (s *Server) taskOnePager(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	st := s.impactStats(r.Context(), t, taskID)
	fs := s.lootFindings(t)
	rows := s.lootRowsForTask(r.Context(), t, fs)
	blockers := s.briefBlockers(r.Context(), taskID)
	writeJSON(w, http.StatusOK, buildOnePager(t, st, rows, fs, blockers))
}
