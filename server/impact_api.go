package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/impact"
)

var (
	grantCacheMu sync.Mutex
	grantCache   = map[string]grantCacheEntry{}
)

// 即时授权（just-in-time authorization）—— 对应 2026-10-24 用户的原话：
// 「串联漏洞和线索才是渗透本质……它那时候会问我，让我明确授权」。
//
// 设计要点（不要预先写死边界）：
//  1. 系统只算"现在到哪一级 + 下一步能做什么"，把「能得到什么 / 我会执行什么 / 风险」摆给人；
//  2. 授权粒度 = (任务, 动作类别)，由人在卡片上当场点；拒绝也记录，避免反复打扰；
//  3. 未授权的动作在执行前被 guard 拦下，并写一条 policy_blocked(GrantGate) —— 失败显形。

type impactView struct {
	Level      int           `json:"level"`
	LevelLabel string        `json:"level_label"`
	Basis      string        `json:"basis"`
	Stats      impact.Stats  `json:"stats"`
	Steps      []impact.Step `json:"steps"`
	Grants     []grantView   `json:"grants"`
}

type grantView struct {
	Class     string `json:"class"`
	Granted   bool   `json:"granted"`
	DecidedAt string `json:"decided_at"`
	Note      string `json:"note"`
}

func (s *Server) registerImpactRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/{id}/impact", s.taskImpact)
	mux.HandleFunc("POST /api/tasks/{id}/grant", s.taskGrant)
}

func (s *Server) taskImpact(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	st := s.impactStats(r.Context(), t, taskID)
	lv, basis := impact.Compute(st)
	writeJSON(w, http.StatusOK, impactView{
		Level:      int(lv),
		LevelLabel: lv.String(),
		Basis:      basis,
		Stats:      st,
		Steps:      impact.Propose(lv, st),
		Grants:     s.taskGrants(r.Context(), taskID),
	})
}

func (s *Server) taskGrant(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok || t == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	var body struct {
		Class   string `json:"class"`
		Granted bool   `json:"granted"`
		Note    string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "class / granted 必填")
		return
	}
	cls := strings.TrimSpace(body.Class)
	if !validActionClass(cls) {
		writeErr(w, http.StatusBadRequest, "class 只能是 file_write / data_dump / persist / lateral")
		return
	}
	taskID, _ := strconv.ParseInt(t.ID, 10, 64)
	if _, err := s.m.pg.Exec(`INSERT INTO task_grants(task_id,action_class,granted,note,decided_at)
		VALUES($1,$2,$3,$4,now())
		ON CONFLICT (task_id,action_class) DO UPDATE SET granted=EXCLUDED.granted, note=EXCLUDED.note, decided_at=now()`,
		taskID, cls, body.Granted, strings.TrimSpace(body.Note)); err != nil {
		writeErr(w, http.StatusInternalServerError, "授权记录失败")
		return
	}
	s.clearGrantCache(taskID)
	who := "拒绝"
	if body.Granted {
		who = "允许"
	}
	if s.engine != nil && t.Store != nil {
		s.engine.emitActivity(t, db.Activity{Worker: "human", Kind: "text",
			Summary: "用户" + who + "本任务执行「" + classCN(cls) + "」"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "class": cls, "granted": body.Granted})
}

func validActionClass(c string) bool {
	switch impact.ActionClass(c) {
	case impact.ClassFileWrite, impact.ClassDataDump, impact.ClassPersist, impact.ClassLateral:
		return true
	}
	return false
}

func classCN(c string) string {
	switch c {
	case "file_write":
		return "上传/写文件（webshell、落地验证文件）"
	case "data_dump":
		return "批量取数据"
	case "persist":
		return "留可控账号/计划任务"
	case "lateral":
		return "向其它主机发起连接"
	}
	return c
}

// ── 授权缓存（guard 每次 Bash 都会问，不能每次都查库）──
type grantCacheEntry struct {
	granted bool
	at      time.Time
}

func (s *Server) clearGrantCache(taskID int64) {
	grantCacheMu.Lock()
	delete(grantCache, grantCacheKey(taskID, ""))
	for k := range grantCache {
		if strings.HasPrefix(k, strconv.FormatInt(taskID, 10)+"|") {
			delete(grantCache, k)
		}
	}
	grantCacheMu.Unlock()
}

func grantCacheKey(taskID int64, class string) string {
	return strconv.FormatInt(taskID, 10) + "|" + class
}

// grantAllowed 供 guard 注入使用：某任务是否已被授权做某类动作。
func (s *Server) grantAllowed(taskID int64, class string) bool {
	k := grantCacheKey(taskID, class)
	grantCacheMu.Lock()
	if e, ok := grantCache[k]; ok && time.Since(e.at) < 30*time.Second {
		grantCacheMu.Unlock()
		return e.granted
	}
	grantCacheMu.Unlock()

	granted := false
	if s.m != nil && s.m.pg != nil && taskID > 0 {
		var v bool
		if err := s.m.pg.QueryRow(`SELECT granted FROM task_grants WHERE task_id=$1 AND action_class=$2`,
			taskID, class).Scan(&v); err == nil {
			granted = v
		}
	}
	grantCacheMu.Lock()
	grantCache[k] = grantCacheEntry{granted: granted, at: time.Now()}
	grantCacheMu.Unlock()
	return granted
}

func (s *Server) taskGrants(ctx context.Context, taskID int64) []grantView {
	out := []grantView{}
	if s.m == nil || s.m.pg == nil || taskID == 0 {
		return out
	}
	rows, err := s.m.pg.QueryContext(ctx,
		`SELECT action_class, granted, to_char(decided_at,'YYYY-MM-DD HH24:MI'), note
		   FROM task_grants WHERE task_id=$1 ORDER BY action_class`, taskID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var g grantView
		if rows.Scan(&g.Class, &g.Granted, &g.DecidedAt, &g.Note) == nil {
			out = append(out, g)
		}
	}
	return out
}

// impactStats 从既有表算等级需要的计数，**不新增采集**（避免两套真相）。
func (s *Server) impactStats(ctx context.Context, t *Task, taskID int64) impact.Stats {
	var st impact.Stats
	if s.m == nil || s.m.pg == nil {
		return st
	}
	// 漏洞：图里的 finding 节点（并按文本粗分"命令执行/数据"类，保守判定）
	// 只看 vulnclass/name 判"命令执行"，再看 evidence 里有没有"真的读到了数据"的痕迹判 E4。
	// 教训（2026-10-24 实测）：一开始把 summary 也算进去，一个 XSS 因为摘要里出现"用户枚举/数据"
	// 就被算成 E4 数据访问 —— 等级虚高比不显示更糟，用户会以为已经拿到数据。
	rows, err := s.m.pg.QueryContext(ctx, `SELECT coalesce(payload->>'vulnclass',''), coalesce(payload->>'name',''),
		coalesce(payload->>'evidence'::text,'')
		FROM exploration_nodes n JOIN tasks t ON t.exploration_id=n.exploration_id
		WHERE t.id=$1 AND n.kind='finding'`, taskID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var vc, nm, sm string
			if rows.Scan(&vc, &nm, &sm) != nil {
				continue
			}
			st.Findings++
			kind := strings.ToLower(vc + " " + nm)
			ev := strings.ToLower(sm) // 这里 sm 实际装的是 evidence 文本
			if strings.Contains(kind, "rce") || strings.Contains(kind, "命令执行") || strings.Contains(kind, "代码执行") ||
				strings.Contains(kind, "webshell") || strings.Contains(kind, "反序列化") {
				st.Shells++
			}
			// E4 要求"证据里真的出现数据样本"，只认这几类证据词，避免 XSS 之类被抬高。
			dataVuln := strings.Contains(kind, "sql") || strings.Contains(kind, "注入") ||
				strings.Contains(kind, "敏感信息") || strings.Contains(kind, "数据泄露") || strings.Contains(kind, "越权")
			dataProof := strings.Contains(ev, "样本") || strings.Contains(ev, "条记录") || strings.Contains(ev, "rows") ||
				strings.Contains(ev, "select ") || strings.Contains(ev, "返回值")
			if dataVuln && dataProof {
				st.DataSamples++
			}
		}
	}
	_ = s.m.pg.QueryRowContext(ctx, `SELECT count(*) FROM creds_vault WHERE task_id=$1`, taskID).Scan(&st.Creds)
	_ = s.m.pg.QueryRowContext(ctx, `SELECT count(*) FROM assets_owned WHERE first_task_id=$1`, taskID).Scan(&st.OwnedHosts)
	_ = s.m.pg.QueryRowContext(ctx, `SELECT count(*) FROM assets_owned WHERE first_task_id=$1
		AND lower(privilege) IN ('root','system','admin')`, taskID).Scan(&st.OwnedRoot)
	if st.Shells > 0 && st.DataSamples == 0 {
		st.DataSamples = 0 // 只按"有数据类证据"算，不被 RCE 顺带抬高
	}
	log.Printf("[impact] task %d stats: findings=%d creds=%d shells=%d data=%d owned=%d root=%d",
		taskID, st.Findings, st.Creds, st.Shells, st.DataSamples, st.OwnedHosts, st.OwnedRoot)
	return st
}
