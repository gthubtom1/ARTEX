package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// 能做到什么（战果表）：按"渗透里常见的可得项"出一张表，
// 只列这个站**相关**的行（有证据 或 试过），并用 ✓ / 试过未成 标注。
//
// 用户要的形态：不是在某个页面里点开才有数据——数据是**实时从库里算的**，
// 面板只是把它显示出来；面板关着也不影响判定。

type lootRow struct {
	Key      string         `json:"key"`
	Label    string         `json:"label"`
	Status   string         `json:"status"` // done|tried|blank
	Why      string         `json:"why"`
	Hint     string         `json:"hint,omitempty"`
	Evidence []candoEvidence `json:"evidence,omitempty"`
}

type lootRowDef struct {
	key   string
	label string
	kws   []string
}

var lootRows = []lootRowDef{
	{"admin", "管理员 / 后台登录", []string{"后台", "登录接管", "弱口令", "默认口令", "管理员"}},
	{"db", "数据库查询", []string{"SQL", "注入", "跨库", "盲注"}},
	{"data", "数据拉取（用户/订单/卡密）", []string{"用户表", "订单", "卡密", "手机号", "邮箱", "数据读取", "拖库"}},
	{"fileread", "读服务器文件", []string{"文件读取", "路径穿越", "目录穿越", "LFI", "源码泄露"}},
	{"filewrite", "写文件 / 上传", []string{"上传", "写入", "PUT", "落盘", "webshell"}},
	{"rce", "服务器命令执行", []string{"命令执行", "代码执行", "RCE", "shell", "后门", "xp_cmdshell"}},
	{"cred", "凭据（口令 / 哈希 / 连接串）", []string{"凭据", "哈希", "口令", "连接串", "NTLM", "MD5"}},
	{"lateral", "内网横向", []string{"横向", "内网", "隧道", "跳板"}},
}

func (s *Server) registerLootRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/loot", s.taskLoot)
}

func (s *Server) taskLoot(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	findings, _ := t.Store.ListByKind(db.KindFinding, 500)

	type fBrief struct {
		id    int64
		title string
		text  string
	}
	fs := make([]fBrief, 0, len(findings))
	for _, f := range findings {
		var p struct {
			Name      string `json:"name"`
			VulnClass string `json:"vulnclass"`
			Summary   string `json:"summary"`
			Severity  string `json:"severity"`
		}
		_ = json.Unmarshal(f.Payload, &p)
		title := p.Name
		if strings.TrimSpace(title) == "" {
			title = p.VulnClass
		}
		if r := []rune(title); len(r) > 46 {
			title = string(r[:46]) + "…"
		}
		fs = append(fs, fBrief{id: f.ID, title: title, text: p.VulnClass + " " + p.Name + " " + p.Summary})
	}

	// 能力卡（有 impact / 已证实的，用来补 hint 与 "试过未成"）
	caps, _ := s.loadCapabilityCards(r.Context(), taskID)
	// 在跑的意图（用来标"正在试"）
	running := s.candoRunningIntents(taskID)

	rows := make([]lootRow, 0, len(lootRows))
	for _, def := range lootRows {
		row := lootRow{Key: def.key, Label: def.label, Status: "blank"}
		for _, f := range fs {
			if containsAnyFold(f.text, def.kws...) {
				row.Status = "done"
				row.Why = "已确认"
				row.Evidence = append(row.Evidence, candoEvidence{Kind: "finding", ID: f.id, Title: f.title})
			}
		}
		for _, c := range caps {
			text := c.Item + " " + c.Can
			if !containsAnyFold(text, def.kws...) {
				continue
			}
			if row.Hint == "" {
				row.Hint = c.NextIfApproved
			}
			if row.Status == "blank" && c.Confidence == "observed" {
				row.Status = "tried"
				row.Why = "试过，还没成功"
			}
		}
		if row.Status == "blank" {
			for _, rs := range running {
				if containsAnyFold(rs, def.kws...) {
					row.Status = "run"
					row.Why = "worker 正在试"
					break
				}
			}
		}
		if row.Status != "blank" {
			if len(row.Evidence) > 3 {
				row.Evidence = row.Evidence[:3]
			}
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Status != rows[j].Status {
			return rows[i].Status == "done"
		}
		return rows[i].Key < rows[j].Key
	})

	done := 0
	for _, row := range rows {
		if row.Status == "done" {
			done++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"goal":     t.Goal,
		"done":     done,
		"total":    len(rows),
		"rows":     rows,
		"findings": len(fs),
	})
}

func containsAnyFold(hay string, needles ...string) bool {
	up := strings.ToUpper(hay)
	for _, n := range needles {
		if n != "" && strings.Contains(up, strings.ToUpper(n)) {
			return true
		}
	}
	return false
}
