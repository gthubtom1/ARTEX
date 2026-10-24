package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/internal/impact"
)

// 战况卡（P0）：一次拿全"我在哪 / 我有什么 / 能换到什么 / 能做什么"，避免前端拼多张卡。
// 全部只读；动作只展示不执行（P2 的铁律）。
type briefItem struct {
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	RefID int64  `json:"ref_id,omitempty"`
}

type briefAbility struct {
	FindingID int64  `json:"finding_id"`
	Title     string `json:"title"`
	Grants    string `json:"grants"`
	Doable    bool   `json:"doable"`
	Source    string `json:"source"` // rule|ai
}

type briefAction struct {
	Class string `json:"class"`
	Title string `json:"title"`
	State string `json:"state"`  // idle（永不自动执行）
	Need  string `json:"need"`   // 前置条件
	SayIt string `json:"say_it"` // 想让我做就发这句
}

type brief struct {
	Level      int            `json:"level"`
	LevelLabel string         `json:"level_label"`
	Basis      string         `json:"basis"`
	Has        []briefItem    `json:"has"`
	Can        []briefAbility `json:"can"`
	Actions    []briefAction  `json:"actions"`
	SayThis    string         `json:"say_this"`
	Verdict    string         `json:"verdict"`
	VerdictTag string         `json:"verdict_label"`
}

func (s *Server) registerBriefRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/brief", s.taskBrief)
}

func (s *Server) taskBrief(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	st := s.impactStats(r.Context(), t, taskID)
	lv, basis := impact.Compute(st)
	v := s.computeVerdict(r.Context(), t)

	out := brief{
		Level: int(lv), LevelLabel: lv.String(), Basis: basis,
		Has: []briefItem{}, Can: []briefAbility{}, Actions: []briefAction{},
		Verdict: v.Verdict, VerdictTag: v.Label,
	}

	// 已拿到
	if st.OwnedApp > 0 {
		out.Has = append(out.Has, briefItem{Kind: "app", Text: "后台管理员会话（可浏览后台功能）"})
	}
	if st.OwnedRoot > 0 || st.OwnedHosts > 0 {
		out.Has = append(out.Has, briefItem{Kind: "os", Text: "服务器立足点（台账已登记）"})
	}
	if st.Shells > 0 {
		out.Has = append(out.Has, briefItem{Kind: "shell", Text: "命令执行能力"})
	}
	if st.DataSamples > 0 {
		out.Has = append(out.Has, briefItem{Kind: "data", Text: "业务数据样本"})
	}
	if st.Creds > 0 {
		out.Has = append(out.Has, briefItem{Kind: "cred", Text: "凭据（账号/哈希，未验证登录）"})
	}

	// 每个漏洞能换到什么（先规则，AI 版按证据 hash 缓存后续接）
	for _, f := range s.chainFindings(r.Context(), taskID) {
		out.Can = append(out.Can, briefAbility{
			FindingID: f.ID, Title: f.Title,
			Grants: abilityText(f.Title), Doable: true, Source: "rule",
		})
	}

	// 业务动作（P2）：只展示，state 恒为 idle
	out.Actions = []briefAction{
		{Class: "data_export", Title: "按条件导出数据（订单/用户/卡密）", State: "idle", Need: "注入点或数据库权限", SayIt: "帮我把它数据库里的订单表数据拉出来，先各取几条看看"},
		{Class: "account_read", Title: "看后台的用户名/密码或哈希", State: "idle", Need: "后台管理员或数据库", SayIt: "把后台的用户列表和密码（或哈希）给我看看"},
		{Class: "account_create", Title: "开一个管理员账号", State: "idle", Need: "后台或命令执行", SayIt: "用管理员权限帮我开一个后台账号，我登录看看"},
		{Class: "money_adjust", Title: "给指定账号加钱/改余额", State: "idle", Need: "后台或写库权限", SayIt: "给这个账号加 100，把改前改后都记下来"},
		{Class: "money_approve", Title: "把提款/订单审批通过", State: "idle", Need: "后台", SayIt: "在后台把这笔提款审批通过"},
		{Class: "deface", Title: "改首页 / 挂马 / 换收款地址", State: "idle", Need: "上传或写入权限", SayIt: "把它首页的收款地址换成我的，先备份原文件"},
		{Class: "persist", Title: "留账号/计划任务（持久化）", State: "idle", Need: "命令执行", SayIt: "留一个可用的后台账号，任务结束前删掉"},
	}
	if len(out.Has) > 0 {
		out.SayThis = "想做上面哪一步，直接在下面主对话里说，例如「" + out.Actions[0].SayIt + "」；我不会自己动手。"
	} else {
		out.SayThis = "现在还没有可直接利用的权限。先跑②或③模板把漏洞打出来。"
	}
	writeJSON(w, http.StatusOK, out)
}

// abilityText 把漏洞标题翻成"能换到什么"（规则版；AI 版后续按证据 hash 缓存替换）。
func abilityText(title string) string {
	t := title
	switch {
	case briefFold(t, "sql", "注入"):
		return "能读它的数据库（用户表/订单表等），也可能写入或提权"
	case briefFold(t, "idor", "越权", "未授权"):
		return "能直接读到别人的数据（订单/资料），不用登录"
	case briefFold(t, "登录绕过", "auth bypass", "认证"):
		return "能进它的后台，之后可看用户与订单、找上传点"
	case briefFold(t, "xss"):
		return "能偷管理员会话，进而接管后台"
	case briefFold(t, "上传", "写文件"):
		return "能落文件，进一步可拿命令执行"
	case briefFold(t, "rce", "命令执行", "代码执行"):
		return "能在服务器上执行命令，进而控制整机"
	case briefFold(t, "文件读取", "路径穿越", "lfi"):
		return "能读服务器文件（配置里常有数据库口令）"
	}
	return "需要人工判断：把证据打开看能读到什么、能做什么"
}

func briefFold(s string, subs ...string) bool {
	ls := ""
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			ls += string(r + 32)
		} else {
			ls += string(r)
		}
	}
	for _, sub := range subs {
		if len(sub) == 0 {
			continue
		}
		if strings.Contains(ls, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}
