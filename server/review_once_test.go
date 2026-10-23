package server

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

// 审查官（agentKey=adviser）的落库路径必须是真的：
//   - review_runs / review_notes 表存在（迁移 0005 已应用）；
//   - 模型不可用时不阻断，走规则版并写 model='rule'；
//   - Agent 页的提示词能从库里读出来（agentPromptOrConst 不回落、不吞编辑）。
//
// 这条测试同时守住「会话里加了个胶囊但点了没反应」那个坑：insertReviewRun 以前
// 忽略错误，表不存在时会静默产出 id=0 的假成功。
func TestReviewOncePersistsRuleVerdict(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("database unavailable (%v)", err)
	}
	if m.pg == nil {
		t.Skip("postgres unavailable")
	}
	t.Cleanup(func() { m.Close() })

	task, err := m.CreateTask("审查官落库测试", "验证审查官能把结论写进 review_runs", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	tid, _ := strconv.ParseInt(task.ID, 10, 64)
	t.Cleanup(func() {
		m.pg.Exec(`DELETE FROM review_feedback WHERE run_id IN (SELECT id FROM review_runs WHERE task_id=$1)`, tid)
		m.pg.Exec(`DELETE FROM review_runs WHERE task_id=$1`, tid)
		m.pg.Exec(`DELETE FROM review_notes WHERE task_id=$1`, tid)
		m.pg.Exec(`DELETE FROM task_archives WHERE task_id=$1`, tid)
		m.pg.DeleteTask(tid)
	})

	td := t.TempDir()
	s := newTestServer(t, m, td, td, td)

	// 没有可用的 LLM 配置时 collectText 必失败 → 必须回落规则版而不是报错。
	runID, err := s.reviewOnce(context.Background(), task.ID, tid, "cond", "")
	if err != nil {
		t.Fatalf("reviewOnce: %v", err)
	}
	if runID <= 0 {
		t.Fatal("runID = 0：insertReviewRun 静默失败（review_runs 表或权限有问题）")
	}

	var status, verdict, body, model string
	if err := m.pg.QueryRow(`SELECT status, COALESCE(verdict,''), COALESCE(body,''), COALESCE(model,'')
		FROM review_runs WHERE id=$1`, runID).Scan(&status, &verdict, &body, &model); err != nil {
		t.Fatal(err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want done（结论没落库就永远停在 running）", status)
	}
	if strings.TrimSpace(body) == "" || strings.TrimSpace(verdict) == "" {
		t.Fatalf("结论为空：verdict=%q body=%q", verdict, body)
	}
	if model != "rule" {
		t.Fatalf("model = %q, want rule（无 LLM 时必须走规则版兜底）", model)
	}

	// 笔记：规则版也要积累（review_notes 是审查官唯一的长期记忆）。
	var notes string
	if err := m.pg.QueryRow(`SELECT body FROM review_notes WHERE task_id=$1`, tid).Scan(&notes); err != nil {
		t.Fatalf("review_notes 没有落记录：%v", err)
	}
	if strings.TrimSpace(notes) == "" {
		t.Fatal("review_notes.body 为空")
	}

	// 闭环字段必须真的落库（docs/13 §7）：level / reason_class / dedupe_key / seen。
	var level, reasonClass, dedupe string
	if err := m.pg.QueryRow(`SELECT COALESCE(level,''), COALESCE(reason_class,''), COALESCE(dedupe_key,'')
		FROM review_runs WHERE id=$1`, runID).Scan(&level, &reasonClass, &dedupe); err != nil {
		t.Fatal(err)
	}
	if level != "read" && level != "action" {
		t.Fatalf("level=%q，必须是 read|action（分级投递）", level)
	}
	// 未读红点：新结论 seen_at 为空 → 前端能看到红点；标记已读后必须清零。
	var unseen int
	if err := m.pg.QueryRow(`SELECT count(*) FROM review_runs WHERE task_id=$1 AND status='done' AND seen_at IS NULL`, tid).Scan(&unseen); err != nil {
		t.Fatal(err)
	}
	if unseen == 0 {
		t.Fatal("新结论应为未读（未读=红点依据）")
	}
	if _, err := m.pg.Exec(`UPDATE review_runs SET seen_at=now() WHERE task_id=$1 AND status='done' AND seen_at IS NULL`, tid); err != nil {
		t.Fatal(err)
	}
	if err := m.pg.QueryRow(`SELECT count(*) FROM review_runs WHERE task_id=$1 AND status='done' AND seen_at IS NULL`, tid).Scan(&unseen); err != nil || unseen != 0 {
		t.Fatalf("标记已读后未读应为 0，得到 %d (%v)", unseen, err)
	}

	// 提示词单一来源：库里播种的审查官提示词必须被 agentPromptOrConst 读到，
	// 而不是每次都回落到代码常量（否则 Agent 页的编辑等于没改）。
	got := s.agentPromptOrConst("adviser", "FALLBACK")
	if got == "FALLBACK" {
		t.Fatal("审查官提示词没有从 agent_prompts 读出：Agent 页编辑不生效（回落常量）")
	}
	if strings.Contains(got, "{{") {
		t.Fatal("审查官提示词含未渲染占位符：两条调用路径都会被整体回落")
	}
	if !strings.Contains(got, "审查官") {
		t.Fatalf("审查官提示词主体不对（前 40 字）：%q", firstRunes(got, 40))
	}
}

func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
