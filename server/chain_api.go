package server

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"github.com/Autumn-27/artex/internal/chain"
)

// 利用链（「串联漏洞和线索才是渗透本质」）。
//
// 链是**派生**的：由 findings + task_grants + 收益证据算出来，落库只为跨会话累计与统计。
// 每次读都会同步一次（幂等 upsert），所以不会有"视图算出来但库里没有"的两套真相。

type chainView struct {
	Key    string       `json:"key"`
	Title  string       `json:"title"`
	Status string       `json:"status"`
	Steps  []chain.Step `json:"steps"`
	Next   string       `json:"next"` // 下一步该干什么（人话）
}

func (s *Server) registerChainRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/chains", s.taskChains)
}

func (s *Server) taskChains(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	cs := s.syncChains(r.Context(), t, taskID)
	out := make([]chainView, 0, len(cs))
	for _, c := range cs {
		out = append(out, chainView{Key: c.Key, Title: c.Title, Status: c.Status, Steps: c.Steps, Next: nextHint(c)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"chains": out, "count": len(out)})
}

// nextHint 用链状态直接说"下一步"，避免用户自己读步骤表。
func nextHint(c chain.Chain) string {
	if c.Status == "done" {
		return "这条链已经拿到实际收益，可以继续往下（提权/横移）或收尾出报告"
	}
	for _, st := range c.Steps {
		if st.Kind == "authorize" && st.Status == "pending" {
			return "这条链卡在授权：到「下一步」卡片点允许，它才能继续"
		}
	}
	if c.Status == "dead" {
		return "这条链的越界动作都被你拒绝了，换一条漏洞或换手法"
	}
	return "已授权，等它把这一步做完并登记收益"
}

// syncChains 从既有表派生链并幂等落库（同一任务同一 key 只留一份）。
func (s *Server) syncChains(ctx context.Context, t *Task, taskID int64) []chain.Chain {
	findings := s.chainFindings(ctx, taskID)
	grants := s.chainGrants(ctx, taskID)
	gains := s.chainGains(ctx, taskID)
	cs := chain.Build(findings, grants, gains)

	if s.m == nil || s.m.pg == nil || len(cs) == 0 {
		return cs
	}
	for _, c := range cs {
		if _, err := s.m.pg.ExecContext(ctx, `INSERT INTO chains(task_id, chain_key, title, status)
			VALUES($1,$2,$3,$4)
			ON CONFLICT (task_id, chain_key) DO UPDATE SET title=EXCLUDED.title, status=EXCLUDED.status, updated_at=now()`,
			taskID, c.Key, c.Title, c.Status); err != nil {
			log.Printf("[chain] upsert chain %s/%s: %v", t.ID, c.Key, err)
			continue
		}
		for _, st := range c.Steps {
			if _, err := s.m.pg.ExecContext(ctx, `INSERT INTO chain_steps(task_id, chain_key, step_key, seq, kind, title, status, ref_id)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8)
				ON CONFLICT (task_id, chain_key, step_key) DO UPDATE
				SET seq=EXCLUDED.seq, title=EXCLUDED.title, status=EXCLUDED.status, ref_id=EXCLUDED.ref_id, updated_at=now()`,
				taskID, c.Key, st.Key, st.Seq, st.Kind, st.Title, st.Status, st.RefID); err != nil {
				log.Printf("[chain] upsert step %s/%s: %v", t.ID, st.Key, err)
			}
		}
	}
	return cs
}

func (s *Server) chainFindings(ctx context.Context, taskID int64) []chain.FindingRef {
	out := []chain.FindingRef{}
	if s.m == nil || s.m.pg == nil {
		return out
	}
	rows, err := s.m.pg.QueryContext(ctx, `SELECT n.id, coalesce(nullif(n.payload->>'name',''), coalesce(n.payload->>'vulnclass','未分类'))
		FROM exploration_nodes n JOIN tasks t ON t.exploration_id=n.exploration_id
		WHERE t.id=$1 AND n.kind='finding' ORDER BY n.id`, taskID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var title string
		if rows.Scan(&id, &title) == nil {
			out = append(out, chain.FindingRef{ID: id, Title: title})
		}
	}
	return out
}

func (s *Server) chainGrants(ctx context.Context, taskID int64) []chain.GrantRef {
	out := []chain.GrantRef{}
	gs := s.taskGrants(ctx, taskID)
	for _, g := range gs {
		out = append(out, chain.GrantRef{Class: g.Class, Granted: g.Granted})
	}
	return out
}

// chainGains 收集"真的拿到了"的证据：凭据 / 命令执行 / 数据样本 / 立足点。
func (s *Server) chainGains(ctx context.Context, taskID int64) []chain.GainRef {
	out := []chain.GainRef{}
	if s.m == nil || s.m.pg == nil {
		return out
	}
	var creds int64
	if err := s.m.pg.QueryRowContext(ctx, `SELECT count(*) FROM creds_vault WHERE task_id=$1`, taskID).Scan(&creds); err == nil && creds > 0 {
		out = append(out, chain.GainRef{Kind: "cred", Ref: creds})
	}
	var owned, root int64
	_ = s.m.pg.QueryRowContext(ctx, `SELECT count(*) FROM assets_owned WHERE first_task_id=$1`, taskID).Scan(&owned)
	if owned > 0 {
		out = append(out, chain.GainRef{Kind: "owned", Ref: owned})
	}
	st := s.impactStats(ctx, nil, taskID)
	if st.Shells > 0 {
		out = append(out, chain.GainRef{Kind: "shell", Ref: int64(st.Shells)})
	}
	if st.DataSamples > 0 {
		out = append(out, chain.GainRef{Kind: "data", Ref: int64(st.DataSamples)})
	}
	_ = root
	return out
}
