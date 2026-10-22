package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/audit"
	"github.com/Autumn-27/artex/internal/capability"
	"github.com/Autumn-27/artex/internal/decision"
	"github.com/Autumn-27/artex/internal/recheck"
)

// registerP2Routes 挂 P2~P5 的只读面板接口（能力卡 / 持有台账 / 进度诊断 / 审计）。
// 前端页面（P6）直接消费这些 JSON；没有 s.p2 时返回明确的空数据而不是 500。
func (s *Server) registerP2Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/capabilities", s.getCapabilities)
	mux.HandleFunc("POST /api/capabilities/{id}/decide", s.decideCapability)
	mux.HandleFunc("POST /api/capabilities/decide-batch", s.decideCapabilitiesBatch)
	mux.HandleFunc("GET /api/owned", s.getOwned)
	mux.HandleFunc("GET /api/owned/export", s.exportOwned)
	mux.HandleFunc("POST /api/owned/recheck", s.runRecheck)
	mux.HandleFunc("GET /api/progress", s.getProgress)
	mux.HandleFunc("GET /api/p2/audit", s.getAuditLog)
	mux.HandleFunc("GET /api/p2/verifier", s.getVerifierReviews)
}

func taskIDParam(r *http.Request) int64 {
	n, _ := strconv.ParseInt(r.URL.Query().Get("task_id"), 10, 64)
	return n
}

// loadCapabilityCards 读某任务的能力卡（taskID=0 → 全部），并用清单补齐白话/星级。
func (s *Server) loadCapabilityCards(ctx context.Context, taskID int64) ([]capability.Card, error) {
	if s.p2 == nil || s.p2.DB == nil {
		return nil, nil
	}
	rows, err := s.p2.DB.QueryContext(ctx,
		`SELECT id, COALESCE(task_id,0), grp, item, can, surface, evidence, proof_level, confidence,
		 impact, risk, next_if_approved, COALESCE(array_to_json(source_node_ids)::text,'[]'), status
		 FROM capabilities WHERE ($1 = 0 OR task_id = $1) ORDER BY grp, item`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []capability.Card{}
	for rows.Next() {
		var c capability.Card
		var nodeIDsJSON string
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Group, &c.Item, &c.Can, &c.Surface, &c.Evidence,
			&c.ProofLevel, &c.Confidence, &c.Impact, &c.Risk, &c.NextIfApproved, &nodeIDsJSON, &c.Status); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(nodeIDsJSON), &c.SourceNodeIDs)
		if s.p2.Tax != nil {
			if tx, ok := s.p2.Tax.Get(c.Item); ok {
				c.Plain, c.Value = tx.Plain, tx.Value
			}
		}
		cards = append(cards, c)
	}
	return cards, rows.Err()
}

func (s *Server) getCapabilities(w http.ResponseWriter, r *http.Request) {
	cards, err := s.loadCapabilityCards(r.Context(), taskIDParam(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if cards == nil {
		cards = []capability.Card{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cards":    cards,
		"coverage": capability.Coverage(cards),
		"plain":    capability.RenderPlain(cards, nil),
	})
}

// decideCapability 是决策面板的写接口：三档按钮 → 能力卡状态 + 规划者 hint。
func (s *Server) decideCapability(w http.ResponseWriter, r *http.Request) {
	if s.p2 == nil || s.p2.DB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "决策队列需要 PostgreSQL"})
		return
	}
	capID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || capID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "非法 cap id"})
		return
	}
	var body struct {
		Action string `json:"action"`
		Note   string `json:"note"`
		TaskID int64  `json:"task_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体不是合法 JSON"})
		return
	}
	if body.TaskID == 0 {
		_ = s.p2.DB.QueryRowContext(r.Context(), `SELECT COALESCE(task_id,0) FROM capabilities WHERE id = $1`, capID).Scan(&body.TaskID)
	}
	var ts *db.ExplorationStore
	if body.TaskID > 0 {
		if t, ok := s.m.Task(strconv.FormatInt(body.TaskID, 10)); ok && t != nil {
			ts = t.Store
		}
	}
	v, err := s.p2.DecideCapability(r.Context(), ts, capID, body.Action, body.Note, "human")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if s.auditLog != nil {
		s.auditLog.Log(r.Context(), audit.Entry{TaskID: body.TaskID, Actor: "human", Tool: "decide_capability",
			Target: v.Hint, Action: string(v.Action), Result: v.Status})
	}
	if ts != nil && body.TaskID > 0 {
		if t, ok := s.m.Task(strconv.FormatInt(body.TaskID, 10)); ok && t != nil {
			s.engine.emitActivity(t, db.Activity{Worker: "human", Kind: "text",
				Summary: "裁决：" + string(v.Action) + " → " + v.Hint})
			t.Notify()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"verdict": v})
}

// decideCapabilitiesBatch 是批量裁决：逐条复用 DecideCapability，单条失败不影响其它。
// 返回 {results:[{cap_id,status,hint}], errors:[]}。
func (s *Server) decideCapabilitiesBatch(w http.ResponseWriter, r *http.Request) {
	if s.p2 == nil || s.p2.DB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "决策队列需要 PostgreSQL"})
		return
	}
	var body struct {
		IDs    []int64 `json:"ids"`
		Action string  `json:"action"`
		Note   string  `json:"note"`
		TaskID int64   `json:"task_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体不是合法 JSON"})
		return
	}
	if len(body.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ids 必填"})
		return
	}
	action, err := decision.Parse(body.Action)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ids, err := decision.NormalizeBatch(body.IDs, action)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var ts *db.ExplorationStore
	if body.TaskID > 0 {
		if t, ok := s.m.Task(strconv.FormatInt(body.TaskID, 10)); ok && t != nil {
			ts = t.Store
		}
	}
	type result struct {
		CapID  int64  `json:"cap_id"`
		Status string `json:"status"`
		Hint   string `json:"hint"`
	}
	results := make([]result, 0, len(ids))
	errs := []string{}
	for _, id := range ids {
		v, derr := s.p2.DecideCapability(r.Context(), ts, id, string(action), body.Note, "human")
		if derr != nil {
			errs = append(errs, fmt.Sprintf("cap %d: %v", id, derr))
			continue
		}
		results = append(results, result{CapID: v.CapID, Status: v.Status, Hint: v.Hint})
	}
	if s.auditLog != nil {
		s.auditLog.Log(r.Context(), audit.Entry{TaskID: body.TaskID, Actor: "human", Tool: "decide_capability_batch",
			Target: fmt.Sprintf("%d 张能力卡", len(ids)), Action: string(action),
			Result: fmt.Sprintf("ok=%d, err=%d", len(results), len(errs))})
	}
	if ts != nil && body.TaskID > 0 && len(results) > 0 {
		if t, ok := s.m.Task(strconv.FormatInt(body.TaskID, 10)); ok && t != nil {
			s.engine.emitActivity(t, db.Activity{Worker: "human", Kind: "text",
				Summary: fmt.Sprintf("批量裁决：%s → %d 张能力卡", action, len(results))})
			t.Notify()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "errors": errs})
}

func (s *Server) getOwned(w http.ResponseWriter, r *http.Request) {
	if s.p2 == nil {
		writeJSON(w, http.StatusOK, map[string]any{"assets": []any{}, "stats": map[string]int{}})
		return
	}
	status := r.URL.Query().Get("status")
	list, err := s.p2.Ledger.List(r.Context(), status, 200)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	stats, _ := s.p2.Ledger.Stats(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"assets": list, "stats": stats})
}

// runRecheck 手动触发一轮持有复查（零基础用户点一下"现在查一遍"）。
func (s *Server) runRecheck(w http.ResponseWriter, r *http.Request) {
	if s.p2 == nil || s.p2.Ledger == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "台账未装配"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	runner := recheck.Runner{Ledger: s.p2.Ledger, MaxBatch: 20, Timeout: 10 * time.Second}
	res, err := runner.RunOnce(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	stats, _ := s.p2.Ledger.Stats(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"result": res, "stats": stats})
}

func (s *Server) exportOwned(w http.ResponseWriter, r *http.Request) {
	if s.p2 == nil {
		writeJSON(w, http.StatusOK, map[string]any{"count": 0, "assets": []any{}})
		return
	}
	b, err := s.p2.Ledger.ExportJSON(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="artex-ledger.json"`)
	_, _ = w.Write(b)
}

func (s *Server) getProgress(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("task_id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id required"})
		return
	}
	t, ok := s.m.Task(id)
	if !ok || t == nil || t.Store == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	numID, _ := strconv.ParseInt(t.ID, 10, 64)
	tsx := agent.NewToolSet(t.Store, "adviser")
	tsx.SetTaskID(numID)
	tsx.SetGoal(t.Goal)
	if assets := s.m.Assets(); assets != nil {
		tsx.SetAssetStore(assets, assets.Companies())
	}
	rep, diag, _, err := tsx.DiagnoseProgress(r.Context(), false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stall": rep, "diagnosis": diag})
}

func (s *Server) getAuditLog(w http.ResponseWriter, r *http.Request) {
	if s.p2 == nil || s.p2.DB == nil {
		writeJSON(w, http.StatusOK, map[string]any{"entries": []any{}})
		return
	}
	taskID := taskIDParam(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.p2.DB.QueryContext(r.Context(),
		`SELECT id, ts, task_id, actor, tool, target, action, result, err, args_digest
		 FROM audit_log WHERE ($1 = 0 OR task_id = $1) ORDER BY id DESC LIMIT $2`, taskID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	type entry struct {
		ID     int64  `json:"id"`
		TS     string `json:"ts"`
		TaskID int64  `json:"task_id"`
		Actor  string `json:"actor"`
		Tool   string `json:"tool"`
		Target string `json:"target"`
		Action string `json:"action"`
		Result string `json:"result"`
		Err    string `json:"err,omitempty"`
		Digest string `json:"args_digest,omitempty"`
	}
	out := []entry{}
	for rows.Next() {
		var e entry
		var ts any
		if err := rows.Scan(&e.ID, &ts, &e.TaskID, &e.Actor, &e.Tool, &e.Target, &e.Action, &e.Result, &e.Err, &e.Digest); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if b, err := json.Marshal(ts); err == nil {
			e.TS = string(b)
		}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

// negativesFromTask 从探索事实里挑出"负结论"（试过但没成立的方向），
// 这些结论和"能做什么"一样值钱：下次省时间。
func (s *Server) negativesFromTask(t *Task) []string {
	if t == nil || t.Store == nil {
		return nil
	}
	facts, err := t.Store.ListByKind(db.KindFact, 200)
	if err != nil {
		return nil
	}
	markers := []string{"无差异", "未成立", "不可利用", "没有回显", "closed", "失败", "不存在", "未发现"}
	out := []string{}
	seen := map[string]bool{}
	for _, f := range facts {
		var p map[string]any
		if json.Unmarshal(f.Payload, &p) != nil {
			continue
		}
		txt := ""
		for _, k := range []string{"summary", "text", "detail"} {
			if v, ok := p[k].(string); ok {
				txt += " " + v
			}
		}
		low := strings.ToLower(txt)
		hit := false
		for _, m := range markers {
			if strings.Contains(low, m) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		txt = strings.Join(strings.Fields(txt), " ")
		if len([]rune(txt)) > 140 {
			txt = string([]rune(txt)[:140]) + "…"
		}
		if txt == "" || seen[txt] {
			continue
		}
		seen[txt] = true
		out = append(out, txt)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

// getVerifierReviews 返回独立复核员对 finding 的判定（evidence_timeline kind='verifier'）。
func (s *Server) getVerifierReviews(w http.ResponseWriter, r *http.Request) {
	if s.p2 == nil || s.p2.DB == nil {
		writeJSON(w, http.StatusOK, map[string]any{"reviews": []any{}})
		return
	}
	taskID := taskIDParam(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.p2.DB.QueryContext(r.Context(),
		`SELECT COALESCE(finding_id,0), ts, content_ref, meta FROM evidence_timeline
		 WHERE kind = 'verifier' AND ($1 = 0 OR task_id = $1) ORDER BY id DESC LIMIT $2`, taskID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var findingID int64
		var ts any
		var ref, meta string
		if rows.Scan(&findingID, &ts, &ref, &meta) != nil {
			continue
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(meta), &m)
		out = append(out, map[string]any{"finding_id": findingID, "ts": tsString(ts), "content_ref": ref, "review": m})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": out})
}
