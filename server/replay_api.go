package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/internal/egress"
	"github.com/Autumn-27/artex/internal/envfilter"
	"github.com/Autumn-27/artex/internal/replay"
	"github.com/Autumn-27/norma/transcript"
)

// 回放看板（轻量版）：对比两个任务的「输入成本 / 产出 / 效率」，用于验证一次改动
// 到底让任务变快了还是变贵了。不重跑 transcript，只读已落库的图与 token 统计。

type replayMetrics struct {
	TaskID            string  `json:"task_id"`
	Name              string  `json:"name"`
	Status            string  `json:"status"`
	Intents           int     `json:"intents"`
	Facts             int     `json:"facts"`
	Findings          int     `json:"findings"`
	Hints             int     `json:"hints"`
	InputTokens       int     `json:"input_tokens"`
	OutputTokens      int     `json:"output_tokens"`
	DurationSec       int64   `json:"duration_seconds"`
	FindingsPerIntent float64 `json:"findings_per_intent"`
	TokensPerFinding  float64 `json:"tokens_per_finding"`
	FactsPerIntent    float64 `json:"facts_per_intent"`
}

func (s *Server) registerReplayRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/replay/compare", s.replayCompare)
	mux.HandleFunc("GET /api/replay/sessions", s.replaySessions)
	mux.HandleFunc("GET /api/replay/session", s.replaySession)
	mux.HandleFunc("POST /api/replay/rerun", s.replayRerun)
}

// transcriptDir 返回会话日志目录（norma transcript 写在 <dataDir>/transcripts）。
func (s *Server) transcriptDir() string {
	if s.m == nil {
		return ""
	}
	return filepath.Join(s.m.dir, "transcripts")
}

// replaySessions 列出可回放的会话文件。
func (s *Server) replaySessions(w http.ResponseWriter, r *http.Request) {
	dir := s.transcriptDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}, "dir": dir})
		return
	}
	type sess struct {
		ID      string `json:"id"`
		Size    int64  `json:"size"`
		ModTime string `json:"mod_time"`
	}
	out := []sess{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		out = append(out, sess{ID: strings.TrimSuffix(e.Name(), ".jsonl"), Size: info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	if len(out) > 100 {
		out = out[:100]
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out, "dir": dir})
}

// loadReplayDoc 读取并解析一个会话（做路径校验）。
func (s *Server) loadReplayDoc(session string) (replay.Doc, error) {
	var d replay.Doc
	id := strings.TrimSpace(session)
	if id == "" || strings.ContainsAny(id, `/\..`) {
		return d, fmt.Errorf("非法 session id")
	}
	dir := s.transcriptDir()
	if dir == "" {
		return d, fmt.Errorf("transcript 目录不可用")
	}
	recs, err := transcript.NewStore(dir).Load(transcript.NewStore(dir).MainPath(id))
	if err != nil {
		return d, err
	}
	if len(recs) == 0 {
		return d, fmt.Errorf("会话不存在或为空")
	}
	return replay.Parse(recs), nil
}

// replaySession 返回会话摘要与步骤（结果截断，避免一次吐几 MB）。
func (s *Server) replaySession(w http.ResponseWriter, r *http.Request) {
	d, err := s.loadReplayDoc(r.URL.Query().Get("session"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	type stepView struct {
		Index    int    `json:"index"`
		Tool     string `json:"tool"`
		Kind     string `json:"kind"`
		Command  string `json:"command"`
		Result   string `json:"result"`
		IsError  bool   `json:"is_error"`
		ReadOnly bool   `json:"read_only"`
		TS       string `json:"ts,omitempty"`
	}
	steps := make([]stepView, 0, len(d.Steps))
	for _, st := range d.Steps {
		steps = append(steps, stepView{Index: st.Index, Tool: st.Tool, Kind: replay.Kind(st), Command: st.Command,
			Result: clipLine(st.Result, 800), IsError: st.IsError, ReadOnly: replay.ReadOnly(st), TS: st.TS})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session": d.SessionID, "turns": d.Turns, "tokens_in": d.TokensIn, "tokens_out": d.TokensOut,
		"started_at": d.StartedAt, "ended_at": d.EndedAt,
		"steps": steps, "tool_counts": replay.ToolCounts(d),
	})
}

// shellReplayExec 执行只读回放命令（命令已过 replay.ReadOnly 白名单与危险词过滤）。
type shellReplayExec struct{ timeout time.Duration }

func (x shellReplayExec) Exec(ctx context.Context, command string) (string, error) {
	if err := egress.Guard(ctx); err != nil {
		return "", fmt.Errorf("出口未就绪，拒绝回放：%w", err)
	}
	c, cancel := context.WithTimeout(ctx, x.timeout)
	defer cancel()
	cmd := exec.CommandContext(c, "sh", "-c", command)
	if len(egress.ProxyEnv) > 0 {
		cmd.Env = append(envfilter.Strip(os.Environ()), egress.ProxyEnv...)
	}
	out, err := cmd.CombinedOutput()
	if len(out) > 200*1024 {
		out = out[:200*1024]
	}
	return string(out), err
}

// replayRerun 只读重放：只跑白名单命令，默认 10 步、单步 20s。
func (s *Server) replayRerun(w http.ResponseWriter, r *http.Request) {
	d, err := s.loadReplayDoc(r.URL.Query().Get("session"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10
	}
	cmps := replay.ReplayReadOnly(r.Context(), d, shellReplayExec{timeout: 20 * time.Second}, limit)
	writeJSON(w, http.StatusOK, map[string]any{
		"summary": replay.Summary(d, cmps), "comparisons": cmps,
	})
}

func (s *Server) replayMetrics(ctx context.Context, t *Task) (replayMetrics, error) {
	m := replayMetrics{TaskID: t.ID, Name: t.Name, Status: t.Status}
	if m.Name == "" {
		m.Name = t.Description
	}
	stats, err := t.Store.Stats()
	if err != nil {
		return m, err
	}
	m.Intents = stats["intent"]
	m.Facts = stats["fact"]
	m.Findings = stats["finding"]
	m.Hints = stats["hint"]
	if tok, terr := t.Store.TokenTotal(); terr == nil {
		m.InputTokens = tok.InputTokens
		m.OutputTokens = tok.OutputTokens
	}
	_ = ctx
	if t.CompletedAt > 0 && t.CreatedAt > 0 {
		m.DurationSec = t.CompletedAt - t.CreatedAt
	}
	m.FindingsPerIntent = ratio(m.Findings, m.Intents)
	m.FactsPerIntent = ratio(m.Facts, m.Intents)
	if m.Findings > 0 {
		m.TokensPerFinding = float64(m.InputTokens+m.OutputTokens) / float64(m.Findings)
	}
	return m, nil
}

func ratio(a, b int) float64 {
	if b <= 0 {
		return 0
	}
	return math.Round(float64(a)/float64(b)*1000) / 1000
}

func (s *Server) replayCompare(w http.ResponseWriter, r *http.Request) {
	aID := r.URL.Query().Get("a")
	bID := r.URL.Query().Get("b")
	if aID == "" || bID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "需要 a 与 b 两个任务 id"})
		return
	}
	ta, okA := s.m.Task(aID)
	tb, okB := s.m.Task(bID)
	if !okA || !okB || ta == nil || tb == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "任务不存在"})
		return
	}
	ma, err := s.replayMetrics(r.Context(), ta)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	mb, err := s.replayMetrics(r.Context(), tb)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"a": ma,
		"b": mb,
		"delta": map[string]any{
			"intents":             mb.Intents - ma.Intents,
			"facts":               mb.Facts - ma.Facts,
			"findings":            mb.Findings - ma.Findings,
			"tokens":              (mb.InputTokens + mb.OutputTokens) - (ma.InputTokens + ma.OutputTokens),
			"duration_seconds":    mb.DurationSec - ma.DurationSec,
			"findings_per_intent": math.Round((mb.FindingsPerIntent-ma.FindingsPerIntent)*1000) / 1000,
		},
		"summary": fmt.Sprintf("B 相比 A：意图 %+d，事实 %+d，发现 %+d，token %+d，时长 %+ds",
			mb.Intents-ma.Intents, mb.Facts-ma.Facts, mb.Findings-ma.Findings,
			(mb.InputTokens+mb.OutputTokens)-(ma.InputTokens+ma.OutputTokens), mb.DurationSec-ma.DurationSec),
	})
}
