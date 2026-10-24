package server

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// 「值不值得打」结论（阶段 A1）。
//
// 为什么要有：用户是零基础，任务跑完给一堆 fact/hint/意图，他没法自己判断
// "这个站到底值不值得继续投入"。文档里写的"建议时限/让他自己看"都不算交付，
// 必须有一张**结论卡**：值得 / 不值得 / 还没探够 + 最多 3 条理由 + 攻击面列表。
//
// 纪律（对应 docs/20-完工方案.md §1 完成判据）：
//  1. 只读现有表（findings / exploration_nodes / assumptions），不写任何东西；
//  2. 判定是**确定性规则**，不调模型 —— 同类输入永远同样输出，可复算；
//  3. 每条理由必须带 id，界面点得进去；抽不到 id 的理由不显示（宁缺不假）。

var verdictURLRe = regexp.MustCompile(`https?://[^\s"'<>（）()，。；、]+`)

type verdictReason struct {
	Kind string `json:"kind"` // finding|fact|assumption
	ID   int64  `json:"id,omitempty"`
	Text string `json:"text"`
}

type verdictSurface struct {
	URL  string `json:"url"`
	Note string `json:"note"`
	Kind string `json:"kind"` // confirmed（有漏洞）|observed（只观测到）
	Ref  int64  `json:"ref,omitempty"`
}

type taskVerdict struct {
	Verdict  string           `json:"verdict"` // worth|not_worth|unclear
	Label    string           `json:"label"`
	Headline string           `json:"headline"`
	Reasons  []verdictReason  `json:"reasons"`
	Surface  []verdictSurface `json:"surface"`
	Counts   map[string]int   `json:"counts"`
	Advice   string           `json:"advice"`
	Rule     string           `json:"rule"` // 命中的判定规则（可复算/可解释）
}

func (s *Server) registerVerdictRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/verdict", s.taskVerdictAPI)
}

func (s *Server) taskVerdictAPI(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, s.computeVerdict(r.Context(), t))
}

// computeVerdict 是唯一判定入口：GET 接口与报告页共用。
func (s *Server) computeVerdict(ctx context.Context, t *Task) taskVerdict {
	out := taskVerdict{
		Verdict: "unclear",
		Label:   "还没探够",
		Reasons: []verdictReason{},
		Surface: []verdictSurface{},
		Counts:  map[string]int{},
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)

	// ── 1. 已确认漏洞（最强证据）──
	findings, _ := t.Store.ListByKind(db.KindFinding, 200)
	type findingInfo struct {
		id    int64
		title string
		url   string
	}
	fis := make([]findingInfo, 0, len(findings))
	for _, f := range findings {
		var p struct {
			Name      string          `json:"name"`
			VulnClass string          `json:"vulnclass"`
			Evidence  json.RawMessage `json:"evidence"`
		}
		_ = json.Unmarshal(f.Payload, &p)
		title := strings.TrimSpace(p.Name)
		if title == "" {
			title = p.VulnClass
		}
		ev := jsonText(p.Evidence)
		u := ""
		if m := verdictURLRe.FindString(ev + " " + title + " " + p.VulnClass); m != "" {
			u = m
		}
		fis = append(fis, findingInfo{id: f.ID, title: title, url: u})
	}
	out.Counts["findings"] = len(fis)

	// ── 2. 事实（观测面）──
	facts, _ := t.Store.ListByKind(db.KindFact, 500)
	out.Counts["facts"] = len(facts)
	seenURL := map[string]bool{}
	addSurface := func(u, note, kind string, ref int64) {
		u = strings.TrimRight(strings.TrimSpace(u), "。，,.;；、")
		if u == "" || seenURL[u] || len(out.Surface) >= 10 {
			return
		}
		seenURL[u] = true
		out.Surface = append(out.Surface, verdictSurface{URL: u, Note: note, Kind: kind, Ref: ref})
	}
	for _, fi := range fis {
		if fi.url != "" {
			addSurface(fi.url, fi.title, "confirmed", fi.id)
		}
	}
	hardSignals := 0
	for _, f := range facts {
		txt := jsonText(f.Payload)
		for _, m := range verdictURLRe.FindAllString(txt, 3) {
			addSurface(m, "", "observed", f.ID)
		}
		low := strings.ToLower(txt)
		// "硬"的信号：WAF / CDN / 全站登录 / 401-403 成片
		for _, kw := range []string{"waf", "防火墙", "cloudflare", "cdn", "需登录", "未授权", "401", "403"} {
			if strings.Contains(low, kw) {
				hardSignals++
				break
			}
		}
	}

	// ── 3. 否定结论（沉在 assumptions 里：试过、走不通）──
	neg, _ := s.assumptionTexts(ctx, taskID, 20)
	out.Counts["assumptions"] = len(neg)

	// ── 4. 规则判定（顺序敏感，越往后越"不确定"）──
	switch {
	case len(fis) > 0:
		out.Verdict, out.Label = "worth", "值得打"
		out.Rule = "findings>=1：已经有可复现漏洞，投入产出比明确"
		out.Headline = "已经能打出东西，值得继续深入"
	case len(neg) >= 3:
		out.Verdict, out.Label = "not_worth", "不值得继续"
		out.Rule = "findings=0 且否定结论>=3：多条路子试过都走不通"
		out.Headline = "试过 " + strconv.Itoa(len(neg)) + " 条路都不通，建议换目标"
	case len(facts) >= 8 && hardSignals >= 3:
		out.Verdict, out.Label = "unclear", "偏硬，需要换打法"
		out.Rule = "findings=0 且硬度信号>=3：面看得见但打不进去"
		out.Headline = "攻击面看得见，但防守明显（WAF/登录锁），要换打法"
	default:
		out.Verdict, out.Label = "unclear", "还没探够"
		out.Rule = "证据不足：findings=0 且否定结论<3"
		out.Headline = "目前还判断不了，建议继续跑或换②模板把入口摸全"
	}

	// ── 5. 理由：最多 3 条，全部带 id ──
	for i, fi := range fis {
		if i >= 2 {
			break
		}
		out.Reasons = append(out.Reasons, verdictReason{Kind: "finding", ID: fi.id, Text: "已确认漏洞：" + fi.title})
	}
	seenNeg := map[string]bool{}
	for i := len(neg) - 1; i >= 0 && len(out.Reasons) < 3; i-- {
		txt := clipLine(strings.TrimSpace(neg[i]), 110)
		if txt == "" || seenNeg[txt] {
			continue // 实测同一条否定结论会被写两次，去重后再进理由
		}
		seenNeg[txt] = true
		out.Reasons = append(out.Reasons, verdictReason{Kind: "assumption", Text: txt})
	}
	if len(out.Reasons) < 3 && len(facts) > 0 {
		out.Reasons = append(out.Reasons, verdictReason{
			Kind: "fact", ID: facts[len(facts)-1].ID,
			Text: "已记录 " + strconv.Itoa(len(facts)) + " 条观测事实（入口/端口/指纹）",
		})
	}

	// ── 6. 下一步建议（可整句照做）──
	switch out.Verdict {
	case "worth":
		out.Advice = "继续用③模板：把已确认漏洞逐个取证到可复现，再评估能拿到什么数据。"
	case "not_worth":
		out.Advice = "停在这里即可：把这几条走不通的路写成否定结论，换下一个目标。"
	default:
		out.Advice = "还缺入口清单：用②模板（摸入口和攻击面）再跑一轮，或把时间放宽到 30 分钟。"
	}
	sort.SliceStable(out.Surface, func(i, j int) bool {
		if out.Surface[i].Kind != out.Surface[j].Kind {
			return out.Surface[i].Kind == "confirmed"
		}
		return false
	})
	return out
}

// assumptionTexts 读否定结论（"试过 X，走不通"）。表不在/查询失败时返回空，
// 不影响判定（退化成"还没探够"，不会误判成"不值得"）。
func (s *Server) assumptionTexts(ctx context.Context, taskID int64, limit int) ([]string, error) {
	if s.m == nil || s.m.pg == nil || taskID == 0 {
		return nil, nil
	}
	rows, err := s.m.pg.QueryContext(ctx,
		`SELECT text FROM assumptions WHERE task_id=$1 ORDER BY id DESC LIMIT $2`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var tx string
		if rows.Scan(&tx) == nil && strings.TrimSpace(tx) != "" {
			out = append(out, strings.TrimSpace(tx))
		}
	}
	return out, rows.Err()
}
