package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/config"
	"github.com/Autumn-27/artex/internal/bench"
)

// P8-1：基准跑分的只读面板 + 结果登记。
//
// 数据分两层，刻意不把明细塞进 DB：
//   - 明细：bench/results/<run_key>.json（脚本 scripts/bench/run.py 写，人工可读、可进 git）
//   - 摘要：bench_runs 表（幂等 UPSERT，供列表/环比查询）
//
// 为什么不做成"库里存全量"：一次跑批的 cases 明细包含每题的证据片段，属于可再生成数据，
// 进库只会让备份变大；而"哪次跑批通过率多少"这种一行摘要才是需要被查询的。

// benchRoot 解析 bench/ 根目录。
//
// 为什么要一串候选：打包后二进制在 dist/ 下，而 bench/ 在仓库根（与 skills/、kb/ 同级）。
// 只认 <exe目录>/bench 会让"dist 里跑起来"看不到用例。顺序：
//  1. ARTEX_BENCH_DIR（显式指定，直接当 results 目录用）
//  2. <exe目录>/bench
//  3. <exe目录>/../bench（dist/artex.exe → 仓库根/bench）
//  4. 当前工作目录/bench
//
// 选第一个"真的存在 cases 或 results 子目录"的；都不存在时用 <exe目录>/bench（会自建）。
func benchRoot() string {
	if v := strings.TrimSpace(os.Getenv("ARTEX_BENCH_DIR")); v != "" {
		return filepath.Dir(v) // 传进来的按 results 目录理解
	}
	base := config.BaseDir()
	cands := []string{
		filepath.Join(base, "bench"),
		filepath.Join(base, "..", "bench"),
		filepath.Join(".", "bench"),
	}
	for _, c := range cands {
		if st, err := os.Stat(filepath.Join(c, "cases")); err == nil && st.IsDir() {
			if abs, aerr := filepath.Abs(c); aerr == nil {
				return abs
			}
			return c
		}
	}
	for _, c := range cands {
		if st, err := os.Stat(filepath.Join(c, "results")); err == nil && st.IsDir() {
			return c
		}
	}
	if abs, err := filepath.Abs(cands[0]); err == nil {
		return abs
	}
	return cands[0]
}

// benchResultsDir 是结果目录；ARTEX_BENCH_DIR 显式指定时直接用它。
func benchResultsDir() string {
	if v := strings.TrimSpace(os.Getenv("ARTEX_BENCH_DIR")); v != "" {
		return v
	}
	return filepath.Join(benchRoot(), "results")
}

// benchCasesDir 是用例目录。
func benchCasesDir() string {
	return filepath.Join(benchRoot(), "cases")
}

func (s *Server) registerBenchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/bench/ingest", s.benchIngest)
	mux.HandleFunc("GET /api/bench/runs", s.benchListRuns)
	mux.HandleFunc("GET /api/bench/run", s.benchGetRun)
	mux.HandleFunc("GET /api/bench/cases", s.benchListCases)
}

// benchIngest 幂等登记一次跑批的摘要。
func (s *Server) benchIngest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RunKey       string `json:"run_key"`
		SetName      string `json:"set_name"`
		ModelProfile string `json:"model_profile"`
		Total        int    `json:"total"`
		Passed       int    `json:"passed"`
		DurationMS   int64  `json:"duration_ms"`
		TotalTokens  int64  `json:"total_tokens"`
		Notes        string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	key, err := bench.SafeRunKey(req.RunKey)
	if err != nil {
		writeErr(w, 400, "run_key 不合法："+err.Error())
		return
	}
	if req.Total < 0 || req.Passed < 0 || req.Passed > req.Total {
		writeErr(w, 400, "passed 不能大于 total")
		return
	}
	if s.p2 == nil || s.p2.DB == nil {
		writeErr(w, 503, "基准登记需要 PostgreSQL")
		return
	}
	_, err = s.p2.DB.ExecContext(r.Context(),
		`INSERT INTO bench_runs (run_key, set_name, model_profile, total, passed, duration_ms, total_tokens, notes)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (run_key) DO UPDATE SET
		   set_name = EXCLUDED.set_name, model_profile = EXCLUDED.model_profile,
		   total = EXCLUDED.total, passed = EXCLUDED.passed,
		   duration_ms = EXCLUDED.duration_ms, total_tokens = EXCLUDED.total_tokens,
		   notes = EXCLUDED.notes`,
		key, req.SetName, req.ModelProfile, req.Total, req.Passed, req.DurationMS, req.TotalTokens, req.Notes)
	if err != nil {
		writeErr(w, 500, "登记失败："+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"run_key": key, "registered": true})
}

// benchRunRow 是列表里的一行（带环比）。
type benchRunRow struct {
	RunKey        string  `json:"run_key"`
	SetName       string  `json:"set_name"`
	ModelProfile  string  `json:"model_profile"`
	Total         int     `json:"total"`
	Passed        int     `json:"passed"`
	PassRate      float64 `json:"pass_rate"`
	DurationMS    int64   `json:"duration_ms"`
	TotalTokens   int64   `json:"total_tokens"`
	Notes         string  `json:"notes"`
	CreatedAt     string  `json:"created_at"`
	DeltaPassRate float64 `json:"delta_pass_rate"`
	DeltaDuration int64   `json:"delta_duration_ms"`
	PrevRunKey    string  `json:"prev_run_key,omitempty"`
}

// benchListRuns 返回最近 50 次跑批，并对"同 set + 同模型"的相邻两次算环比。
func (s *Server) benchListRuns(w http.ResponseWriter, r *http.Request) {
	out := struct {
		Runs       []benchRunRow `json:"runs"`
		ResultsDir string        `json:"results_dir"`
	}{Runs: []benchRunRow{}, ResultsDir: benchResultsDir()}

	if s.p2 == nil || s.p2.DB == nil {
		writeJSON(w, 200, out)
		return
	}
	rows, err := s.p2.DB.QueryContext(r.Context(),
		`SELECT run_key, set_name, model_profile, total, passed, duration_ms, total_tokens,
		        COALESCE(notes,''), created_at
		   FROM bench_runs ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		writeErr(w, 500, "读取基准失败："+err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var row benchRunRow
		var created time.Time
		if err := rows.Scan(&row.RunKey, &row.SetName, &row.ModelProfile, &row.Total, &row.Passed,
			&row.DurationMS, &row.TotalTokens, &row.Notes, &created); err != nil {
			continue
		}
		if row.Total > 0 {
			row.PassRate = float64(row.Passed) / float64(row.Total)
		}
		row.CreatedAt = created.Format(time.RFC3339)
		out.Runs = append(out.Runs, row)
	}
	// 环比：按 (set, model) 找"时间上更早的最近一次"。
	for i := range out.Runs {
		cur := &out.Runs[i]
		for j := i + 1; j < len(out.Runs); j++ {
			prev := out.Runs[j]
			if prev.SetName != cur.SetName || prev.ModelProfile != cur.ModelProfile {
				continue
			}
			cur.DeltaPassRate = cur.PassRate - prev.PassRate
			cur.DeltaDuration = cur.DurationMS - prev.DurationMS
			cur.PrevRunKey = prev.RunKey
			break
		}
	}
	writeJSON(w, 200, out)
}

// benchGetRun 读一次跑批的明细（路径穿越防护靠 bench.SafeRunKey）。
func (s *Server) benchGetRun(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	name, err := bench.SafeRunKey(key)
	if err != nil {
		writeErr(w, 400, "key 不合法："+err.Error())
		return
	}
	path := filepath.Join(benchResultsDir(), name)
	raw, err := os.ReadFile(path)
	if err != nil {
		writeErr(w, 404, "结果文件不存在："+name)
		return
	}
	var run bench.Run
	if err := json.Unmarshal(raw, &run); err != nil {
		writeErr(w, 500, "结果文件损坏："+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"run":     run,
		"summary": bench.Summarize(run),
	})
}

// benchListCases 列出当前可用的用例（读 bench/cases/*.yaml 的头部字段）。
func (s *Server) benchListCases(w http.ResponseWriter, r *http.Request) {
	dir := benchCasesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, 200, map[string]any{"cases": []any{}, "dir": dir, "detail": "用例目录不可读：" + err.Error()})
		return
	}
	type caseRow struct {
		Key      string `json:"key"`
		Name     string `json:"name"`
		Category string `json:"category"`
		URL      string `json:"url"`
		TimeoutS int    `json:"timeout_s"`
	}
	var out []caseRow
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		c, err := bench.LoadCaseFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, caseRow{Key: c.Key, Name: c.Name, Category: c.Category, URL: c.URL, TimeoutS: c.TimeoutS})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	writeJSON(w, 200, map[string]any{"cases": out, "dir": dir, "count": len(out),
		"hint": "跑批：python scripts/bench/run.py --set smoke --profile <llm_profile_id>"})
}

// benchIntParam 是给后续扩展留的小工具（避免各 handler 重复写 ParseInt）。
func benchIntParam(r *http.Request, name string, def int) int {
	if v := strings.TrimSpace(r.URL.Query().Get(name)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
