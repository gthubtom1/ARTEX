package server

import (
	"context"
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
	Cached    bool   `json:"cached"` // true = 命中缓存（没花 token）
}

type briefAction struct {
	Class string `json:"class"`
	Title string `json:"title"`
	State string `json:"state"`  // idle（永不自动执行；P2 的执行状态看 run_state）
	Need  string `json:"need"`   // 前置条件
	SayIt string `json:"say_it"` // 想让我做就发这句
	// P2：工单状态（none=还没人发话 / requested=已登记 / done=有证据 / failed / aborted）
	RunState     string `json:"run_state"`
	RunID        int64  `json:"run_id,omitempty"`
	RunEvidence  string `json:"run_evidence,omitempty"`
	RunSayIt     string `json:"run_say_it,omitempty"` // 人当时的原话（授权凭据）
	EvidenceNeed string `json:"evidence_need"`        // 做完该留下什么证据
	// Doable：**现在**就能做到（前置对应的战果行已拿到）。做不到的灰着，别骗人。
	Doable bool `json:"doable"`
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
	// NewSince：上次打开战果面板之后新拿到的战果行数（胶囊红点）。
	// 游标存在 loot_seen（0014），只看 done 行的证据 id 是否越过游标。
	NewSince int64 `json:"new_since"`
}

func (s *Server) registerBriefRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/brief", s.taskBrief)
	// P0-c：「能换到什么」交给 AI 写一遍（按证据 hash 缓存，失败回落规则模板）
	mux.HandleFunc("POST /api/tasks/{id}/brief/ai", s.taskBriefAI)
}

// briefCan 组装「每个漏洞能换到什么」。
// 优先用 AI 缓存（source=ai / cached=true），否则用规则模板 abilityText（source=rule）。
// 缓存命中要求 evidence_hash 与当前证据一致：证据改了就不再拿旧解释充数。
func (s *Server) briefCan(ctx context.Context, t *Task, taskID int64) []briefAbility {
	out := []briefAbility{}
	cache := s.briefAbilityCache(ctx, taskID)
	byID := map[int64]lootFinding{}
	if t != nil {
		for _, f := range s.lootFindings(t) {
			byID[f.id] = f
		}
	}
	for _, f := range s.chainFindings(ctx, taskID) {
		item := briefAbility{FindingID: f.ID, Title: f.Title, Grants: abilityText(f.Title), Doable: true, Source: "rule"}
		if lf, ok := byID[f.ID]; ok {
			if c, hit := cache[f.ID]; hit && c.Hash == briefAbilityHash(lf) && strings.TrimSpace(c.Text) != "" {
				item.Grants = c.Text
				item.Source = "ai"
				item.Cached = true
			}
		}
		out = append(out, item)
	}
	return out
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

	// 每个漏洞能换到什么（先 AI 缓存；没有就用规则模板，见 brief_ai.go）
	out.Can = s.briefCan(r.Context(), t, taskID)

	// 未读战果（胶囊红点）：done 行的证据 id 越过 loot_seen 游标 => 算新。
	// 读不到游标/没库时 lootSeenFindingID 返回 0，此时全部算新（首屏就该提示）。
	fs := s.lootFindings(t)
	rows := s.lootRowsForTask(r.Context(), t, fs)
	out.NewSince = countUnreadLoot(rows, s.lootSeenFindingID(r.Context(), taskID))

	// 业务动作（P2）：列表来自 actionDefs（唯一事实源），state 恒为 idle（永不自动执行），
	// 真正"人要求过没有 / 有没有证据"看 run_state。先做一次证据收口再读工单。
	s.closeActionsByEvidence(r.Context(), t, taskID)
	out.Actions = s.briefActions(r.Context(), taskID, rows)

	if len(out.Has) > 0 {
		out.SayThis = "想做上面哪一步，直接在下面主对话里说，例如「" + out.Actions[0].SayIt + "」；我不会自己动手。"
	} else {
		out.SayThis = "现在还没有可直接利用的权限。先跑②或③模板把漏洞打出来。"
	}
	writeJSON(w, http.StatusOK, out)
}

// countUnreadLoot 数"上次看过战果之后新拿到"的行：只看已拿到（done）的行，
// 用该行证据 id 的最大值跟游标比。行是实时算的，所以不需要行的创建时间。
func countUnreadLoot(rows []lootRow, seen int64) int64 {
	var n int64
	for _, row := range rows {
		if row.Status != "done" {
			continue
		}
		var maxID int64
		for _, e := range row.Evidence {
			if e.ID > maxID {
				maxID = e.ID
			}
		}
		if maxID > seen {
			n++
		}
	}
	return n
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
