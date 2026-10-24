package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// 业务动作执行通道（P2）。
//
// 用户要的形态（docs/22 §三）：战况卡列出"我现在能做到的现实后果"，
// 但**永远不自动执行**；他在主对话里说「帮我把它数据库里的订单表数据拉出来」，
// 那就是授权（沿用 0.11.7 的对话即授权），做完要有证据可查。
//
// 所以这里不做"执行器"，只做三件事：
//  1. 登记：人一发话，按关键词落一条工单（say_it 存原话 = 授权凭据）；
//  2. 收口：该动作对应的战果行出现**新证据**（base_ref 之后）→ 自动标 done 并写上证据；
//  3. 可查：GET /actions 与 /brief 都能看到状态、证据 id、失败原因。
//
// 纪律：state 只有 requested/done/failed/aborted；`brief.actions[].state` 仍恒为
// "idle"（= 永不自动执行），前端看的是新增的 run_state。两者不能混。

type actionDef struct {
	Class    string
	Title    string
	Need     string
	SayIt    string
	Rows     []string // 用哪几个战果行判"做完了"
	Evidence string   // 留证要求（给人看：做完该留下什么）
}

// actionDefs 是唯一事实源：brief 的动作列表、对话解析、证据收口都用它。
var actionDefs = []actionDef{
	{
		Class: "data_export", Title: "按条件导出数据（订单/用户/卡密）",
		Need: "注入点或数据库权限", SayIt: "帮我把它数据库里的订单表数据拉出来，先各取几条看看",
		Rows: []string{"db", "data"}, Evidence: "行数 + 样本 3 条（脱敏）",
	},
	{
		Class: "account_read", Title: "看后台的用户名/密码或哈希",
		Need: "后台管理员或数据库", SayIt: "把后台的用户列表和密码（或哈希）给我看看",
		Rows: []string{"admin", "cred"}, Evidence: "脱敏后的账号列表",
	},
	{
		Class: "account_create", Title: "开一个管理员账号",
		Need: "后台或命令执行", SayIt: "用管理员权限帮我开一个后台账号，我登录看看",
		Rows: []string{"admin"}, Evidence: "创建请求 + 登录成功截图",
	},
	{
		Class: "money_adjust", Title: "给指定账号加钱/改余额",
		Need: "后台或写库权限", SayIt: "给这个账号加 100，把改前改后都记下来",
		Rows: []string{"admin", "db"}, Evidence: "改前改后的余额记录",
	},
	{
		Class: "money_approve", Title: "把提款/订单审批通过",
		Need: "后台", SayIt: "在后台把这笔提款审批通过",
		Rows: []string{"admin"}, Evidence: "审批请求 + 状态变化",
	},
	{
		Class: "deface", Title: "改首页 / 挂马 / 换收款地址",
		Need: "上传或写入权限", SayIt: "把它首页的收款地址换成我的，先备份原文件",
		Rows: []string{"filewrite"}, Evidence: "前后对比（原文件已备份）",
	},
	{
		Class: "persist", Title: "留账号/计划任务（持久化）",
		Need: "命令执行", SayIt: "留一个可用的后台账号，任务结束前删掉",
		Rows: []string{"rce"}, Evidence: "命令 + 输出 + 清理方式",
	},
}

func actionDefByClass(cls string) (actionDef, bool) {
	for _, d := range actionDefs {
		if d.Class == cls {
			return d, true
		}
	}
	return actionDef{}, false
}

type actionRun struct {
	ID          int64  `json:"id"`
	Class       string `json:"action_class"`
	SayIt       string `json:"say_it"`
	State       string `json:"state"` // requested|done|failed|aborted
	BaseRef     int64  `json:"base_ref"`
	Evidence    string `json:"evidence"`
	EvidenceRef int64  `json:"evidence_ref"`
	Note        string `json:"note"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func (s *Server) registerActionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/actions", s.taskActionRuns)
	mux.HandleFunc("POST /api/tasks/{id}/actions", s.taskActionCreate)
	mux.HandleFunc("POST /api/tasks/{id}/actions/{run}/result", s.taskActionResult)
}

// getActionRuns 读这个任务的工单；没库/没表一律返回空（工单是增强项，不能拖垮战况卡）。
func (s *Server) getActionRuns(ctx context.Context, taskID int64) []actionRun {
	out := []actionRun{}
	if s.m == nil || s.m.pg == nil || taskID == 0 {
		return out
	}
	rows, err := s.m.pg.QueryContext(ctx, `SELECT id,action_class,say_it,state,base_ref,evidence,evidence_ref,note,
		to_char(created_at,'MM-DD HH24:MI'), to_char(updated_at,'MM-DD HH24:MI')
		FROM action_runs WHERE task_id=$1 ORDER BY created_at DESC LIMIT 50`, taskID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r actionRun
		if rows.Scan(&r.ID, &r.Class, &r.SayIt, &r.State, &r.BaseRef, &r.Evidence, &r.EvidenceRef, &r.Note,
			&r.CreatedAt, &r.UpdatedAt) == nil {
			out = append(out, r)
		}
	}
	return out
}

// latestRunsByClass 每类动作只取最新一条，供 /brief 拼状态用。
func latestRunsByClass(runs []actionRun) map[string]actionRun {
	out := map[string]actionRun{}
	for _, r := range runs { // runs 已按时间倒序
		if _, ok := out[r.Class]; !ok {
			out[r.Class] = r
		}
	}
	return out
}

func (s *Server) taskActionRuns(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	// 读之前先做一次收口：战果已经出现的，工单自动标 done（确定性推导，不调模型）。
	s.closeActionsByEvidence(r.Context(), t, taskID)
	writeJSON(w, http.StatusOK, map[string]any{"runs": s.getActionRuns(r.Context(), taskID)})
}

// taskActionCreate 人发话后登记工单。body: {"class":"data_export","say_it":"原话"}
// 只登记，不执行：这里没有任何"跑命令"的路径。
func (s *Server) taskActionCreate(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	if s.m == nil || s.m.pg == nil {
		writeErr(w, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	var body struct {
		Class string `json:"class"`
		SayIt string `json:"say_it"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "class 必填")
		return
	}
	def, ok := actionDefByClass(strings.TrimSpace(body.Class))
	if !ok {
		writeErr(w, http.StatusBadRequest, "class 不认识")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	sayIt := strings.TrimSpace(body.SayIt)
	if sayIt == "" {
		sayIt = def.SayIt
	}
	base := s.maxEvidenceRef(t)
	run, err := s.upsertActionRun(r.Context(), taskID, def.Class, sayIt, base)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "登记失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run})
}

// upsertActionRun 同一类动作已有进行中的工单就更新原话（幂等），否则新建。
func (s *Server) upsertActionRun(ctx context.Context, taskID int64, class, sayIt string, base int64) (actionRun, error) {
	var r actionRun
	err := s.m.pg.QueryRowContext(ctx, `INSERT INTO action_runs(task_id,action_class,say_it,state,base_ref)
		VALUES($1,$2,$3,'requested',$4)
		ON CONFLICT (task_id,action_class) WHERE state='requested'
		DO UPDATE SET say_it=EXCLUDED.say_it, updated_at=now()
		RETURNING id,action_class,say_it,state,base_ref,evidence,evidence_ref,note,
			to_char(created_at,'MM-DD HH24:MI'), to_char(updated_at,'MM-DD HH24:MI')`,
		taskID, class, sayIt, base).
		Scan(&r.ID, &r.Class, &r.SayIt, &r.State, &r.BaseRef, &r.Evidence, &r.EvidenceRef, &r.Note, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// taskActionResult 回报结果：done（附证据）/ failed（附原因）/ aborted。
// body: {"state":"done","evidence":"...","evidence_ref":123,"note":"..."}
func (s *Server) taskActionResult(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	if s.m == nil || s.m.pg == nil {
		writeErr(w, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	runID, _ := strconv.ParseInt(r.PathValue("run"), 10, 64)
	var body struct {
		State       string `json:"state"`
		Evidence    string `json:"evidence"`
		EvidenceRef int64  `json:"evidence_ref"`
		Note        string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "state 必填")
		return
	}
	state := strings.TrimSpace(body.State)
	if state != "done" && state != "failed" && state != "aborted" {
		writeErr(w, http.StatusBadRequest, "state 只能是 done / failed / aborted")
		return
	}
	// done 必须带证据：没证据的"做完了"就是没做（P2 的留证纪律）。
	if state == "done" && strings.TrimSpace(body.Evidence) == "" && body.EvidenceRef <= 0 {
		writeErr(w, http.StatusBadRequest, "标记完成要带 evidence 或 evidence_ref")
		return
	}
	tag, err := s.m.pg.ExecContext(r.Context(), `UPDATE action_runs
		SET state=$3, evidence=$4, evidence_ref=$5, note=$6, updated_at=now()
		WHERE task_id=$1 AND id=$2`, taskID, runID, state, body.Evidence, body.EvidenceRef, body.Note)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "工单不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": state})
}

// maxEvidenceRef 当前任务里最大的证据 id（用作工单的 base_ref）。
func (s *Server) maxEvidenceRef(t *Task) int64 {
	var max int64
	if t == nil {
		return 0
	}
	for _, f := range s.lootFindings(t) {
		if f.id > max {
			max = f.id
		}
	}
	return max
}

// closeActionsByEvidence：把 requested 工单在"该类动作对应的战果行出现新证据"后自动标 done。
// 这是确定性推导（新证据的 id > 登记时的 base_ref），不调模型、不猜。
func (s *Server) closeActionsByEvidence(ctx context.Context, t *Task, taskID int64) {
	if s.m == nil || s.m.pg == nil || t == nil || taskID == 0 {
		return
	}
	runs := s.getActionRuns(ctx, taskID)
	open := []actionRun{}
	for _, r := range runs {
		if r.State == "requested" {
			open = append(open, r)
		}
	}
	if len(open) == 0 {
		return
	}
	fs := s.lootFindings(t)
	rows := s.lootRowsForTask(ctx, t, fs)
	for _, r := range open {
		def, ok := actionDefByClass(r.Class)
		if !ok {
			continue
		}
		done, evidence, ref := actionClosure(def, rows, r.BaseRef)
		if !done {
			continue
		}
		if _, err := s.m.pg.ExecContext(ctx, `UPDATE action_runs
			SET state='done', evidence=$3, evidence_ref=$4, note='战果出现后自动收口', updated_at=now()
			WHERE task_id=$1 AND id=$2 AND state='requested'`, taskID, r.ID, evidence, ref); err != nil {
			continue
		}
	}
}

// actionClosure 是收口判据（纯函数，可单测）：
// 该动作映射的战果行里，是否存在**证据 id 大于 base_ref** 的已拿到行。
func actionClosure(def actionDef, rows []lootRow, baseRef int64) (bool, string, int64) {
	var bestRef int64
	var bestLabel string
	var bestIDs []int64
	for _, row := range rows {
		if row.Status != "done" {
			continue
		}
		want := false
		for _, k := range def.Rows {
			if row.Key == k {
				want = true
				break
			}
		}
		if !want {
			continue
		}
		var maxID int64
		var ids []int64
		for _, e := range row.Evidence {
			if e.ID > maxID {
				maxID = e.ID
			}
			ids = append(ids, e.ID)
		}
		if maxID <= baseRef || maxID <= bestRef {
			continue
		}
		bestRef = maxID
		bestLabel = row.Label
		bestIDs = ids
	}
	if bestRef == 0 {
		return false, "", 0
	}
	ids := make([]string, 0, len(bestIDs))
	for _, id := range bestIDs {
		ids = append(ids, "#"+strconv.FormatInt(id, 10))
	}
	return true, "战果行「" + bestLabel + "」已拿到，证据 " + strings.Join(ids, " "), bestRef
}

// maybeActionFromChat：人发话时按关键词登记工单（与 maybeGrantFromChat 并存：
// 一个记"允许这个方向"，一个记"具体要做哪件事 + 原话"）。
func (s *Server) maybeActionFromChat(ctx context.Context, t *Task, msg string) {
	if s == nil || t == nil || s.m == nil || s.m.pg == nil {
		return
	}
	cls := actionClassFromChat(msg)
	if cls == "" {
		return
	}
	taskID, err := strconv.ParseInt(t.ID, 10, 64)
	if err != nil || taskID <= 0 {
		return
	}
	if _, err := s.upsertActionRun(ctx, taskID, cls, strings.TrimSpace(msg), s.maxEvidenceRef(t)); err != nil {
		return
	}
	def, _ := actionDefByClass(cls)
	s.appendReviewActivity(ctx, taskID, "已登记动作工单：「"+def.Title+"」（原话已留档，做完要留证："+def.Evidence+"）")
}

// actionClassFromChat 把一句人话映射到动作类别（纯函数，可单测）。
// 先排否定句与"只看不做"，再按最长特征匹配，避免"看看"被当成"要看密码"。
func actionClassFromChat(msg string) string {
	text := strings.ToLower(strings.TrimSpace(msg))
	if text == "" {
		return ""
	}
	for _, neg := range []string{"不要", "别", "禁止", "不用", "先不", "不允许", "不准", "算了"} {
		if strings.Contains(text, neg) {
			return ""
		}
	}
	type rule struct {
		cls  string
		keys []string
	}
	// 顺序即优先级：越具体越靠前（"开个账号" 不能被 "看用户列表" 抢走）。
	rules := []rule{
		{"account_create", []string{"开一个后台账号", "开个账号", "开管理员", "加个管理员", "新建管理员", "创建一个账号"}},
		{"money_adjust", []string{"加钱", "改余额", "加 100", "加100", "充值", "改金额"}},
		{"money_approve", []string{"审批通过", "通过提款", "把提款", "审核通过"}},
		{"deface", []string{"改首页", "挂马", "换收款", "收款地址", "换首页"}},
		{"persist", []string{"留账号", "留一个账号", "留个账号", "留一个", "留个", "后门", "计划任务", "持久化"}},
		{"data_export", []string{"订单表", "拉数据", "拉出来", "导出", "拖库", "取几条", "数据拉"}},
		{"account_read", []string{"用户列表", "密码", "哈希", "hash", "账号列表", "看用户名"}},
	}
	for _, r := range rules {
		for _, k := range r.keys {
			if strings.Contains(text, strings.ToLower(k)) {
				return r.cls
			}
		}
	}
	return ""
}

// briefActions 给战况卡拼动作列表：定义来自 actionDefs，状态来自最新工单。
// state 恒为 "idle"（永不自动执行），人要求过没有看 run_state。
func (s *Server) briefActions(ctx context.Context, taskID int64) []briefAction {
	runs := latestRunsByClass(s.getActionRuns(ctx, taskID))
	out := make([]briefAction, 0, len(actionDefs))
	for _, d := range actionDefs {
		item := briefAction{
			Class: d.Class, Title: d.Title, State: "idle",
			Need: d.Need, SayIt: d.SayIt, EvidenceNeed: d.Evidence, RunState: "none",
		}
		if r, ok := runs[d.Class]; ok {
			item.RunState = r.State
			item.RunID = r.ID
			item.RunEvidence = r.Evidence
			item.RunSayIt = r.SayIt
		}
		out = append(out, item)
	}
	return out
}
