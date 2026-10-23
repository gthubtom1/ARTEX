package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 经验层（docs/14 §7-1）：KB 检索必须能命中、最多 2 条、Title 用文件名、
// Snippet 取命中行前后 2 行；目录不存在时整段跳过（返回 nil，不报错）。
func TestBuildKBHitsResolvesSnippetAroundHit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tactics"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# SQL 优先级\n\n第一行\n第二行\n这里讲 MSSQL 盲注 的优先级判断\n下面一行\n再下面一行\n"
	if err := os.WriteFile(filepath.Join(root, "tactics", "sqli.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := buildKBHits(root, []string{"盲注"})
	if len(hits) != 1 {
		t.Fatalf("应命中 1 条，得到 %d: %+v", len(hits), hits)
	}
	h := hits[0]
	if h.Title != "sqli.md" {
		t.Fatalf("Title 应为文件名，得到 %q", h.Title)
	}
	if h.Query != "盲注" {
		t.Fatalf("Query=%q", h.Query)
	}
	if !strings.Contains(h.Snippet, "盲注") {
		t.Fatalf("Snippet 应包含命中行：%q", h.Snippet)
	}
	// 前后 2 行 → 命中行前两行（第一行/第二行）都应在，再往前的标题行不应在。
	if !strings.Contains(h.Snippet, "第二行") || !strings.Contains(h.Snippet, "下面一行") {
		t.Fatalf("Snippet 应包含命中行前后 2 行：%q", h.Snippet)
	}
	if strings.Contains(h.Snippet, "SQL 优先级") {
		t.Fatalf("Snippet 不该超出前后 2 行：%q", h.Snippet)
	}
}

// 多查询词、多文件 → 全局最多 2 条，且不重复同一文件。
func TestBuildKBHitsCapsAtTwo(t *testing.T) {
	root := t.TempDir()
	for i, name := range []string{"a.md", "b.md", "c.md"} {
		body := strings.Repeat("填充行\n", i) + "含 XSS 的说明\n"
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hits := buildKBHits(root, []string{"XSS", "xss"})
	if len(hits) != kbMaxHits {
		t.Fatalf("最多 %d 条，得到 %d", kbMaxHits, len(hits))
	}
	if hits[0].Title == hits[1].Title {
		t.Fatalf("同一文件不应重复命中：%+v", hits)
	}
}

// KB 目录不存在 → 静默跳过，绝不报错、绝不阻塞。
func TestBuildKBHitsMissingRootIsSilent(t *testing.T) {
	if hits := buildKBHits(filepath.Join(t.TempDir(), "nope"), []string{"sqli"}); hits != nil {
		t.Fatalf("目录不存在应返回 nil，得到 %+v", hits)
	}
	if hits := buildKBHits("", nil); hits != nil {
		t.Fatalf("没有查询词应返回 nil，得到 %+v", hits)
	}
}

// 查询词：最频繁的非我方故障错误类 + 最近漏洞类型，最多 2 个。
func TestKBQueriesPrefersTargetSideAndVulnclass(t *testing.T) {
	errs := map[string]int{"self_error": 9, "target_blocked": 3, "env_error": 5}
	got := kbQueries(errs, []string{"sqli", "idor"})
	if len(got) != 2 || got[0] != "target_blocked" || got[1] != "sqli" {
		t.Fatalf("查询词不对：%+v", got)
	}
	if q := kbQueries(map[string]int{"self_error": 9}, nil); len(q) != 0 {
		t.Fatalf("我方故障不应拿来检索经验：%+v", q)
	}
}

// propose_intent 永远不自动执行，且必须带依据（cites 非空 + why 非空）。
func TestDecideProposeIntentNeverAuto(t *testing.T) {
	v := Verdict{
		Verdict:     "continue",
		Body:        "手里有一条读取能力，可以换个方向。",
		Cites:       []Cite{{Kind: "finding", ID: 31}},
		Actions:     []Action{{Kind: "propose_intent", Why: "读 applicationHost.config 找可写目录"}},
		Expectation: "3 轮内出现涉及可写目录的新事实",
	}
	out := Decide(baseIn(v))
	if out.Drop != "" {
		t.Fatalf("有依据的提议不该被丢弃：%s（%v）", out.Drop, out.Notes)
	}
	if len(out.AutoApplied) != 0 {
		t.Fatalf("propose_intent 绝不能自动执行：%+v", out.AutoApplied)
	}
	if !out.NeedHuman {
		t.Fatal("propose_intent 必须交给人/主 Agent 核对")
	}
	if len(out.Actions) != 1 || out.Actions[0].Kind != "propose_intent" {
		t.Fatalf("提议应保留：%+v", out.Actions)
	}
	if out.Level != LevelAction {
		t.Fatalf("有动作应走 B 级投递，level=%s", out.Level)
	}
	if !NeedHumanFor("propose_intent") || !NeedHumanFor("set_next_stage") || NeedHumanFor("retry_intent") {
		t.Fatal("NeedHumanFor 判定不对")
	}
}

// 没依据（没 cites / 没 why）的提议必须被剔除。
func TestDecideDropsUncitedProposal(t *testing.T) {
	v := Verdict{
		Verdict: "pivot",
		Body:    "换方向。",
		Actions: []Action{{Kind: "propose_intent", Why: "凭感觉"}},
	}
	out := Decide(baseIn(v))
	if len(out.Actions) != 0 {
		t.Fatalf("无依据的提议必须剔除：%+v", out.Actions)
	}

	v.Cites = []Cite{{Kind: "finding", ID: 31}}
	v.Actions = []Action{{Kind: "propose_intent"}}
	if out := Decide(baseIn(v)); len(out.Actions) != 0 {
		t.Fatalf("没写 why 的提议必须剔除：%+v", out.Actions)
	}
}

// ack 超时的分流：kill/retry → 可自动；propose/set_next_stage → 只建议。
func TestEscalatePlanSplits(t *testing.T) {
	actions := []Action{
		{Kind: "retry_intent", Intent: 70},
		{Kind: "propose_intent", Why: "换方向"},
		{Kind: "set_next_stage", Stage: "exploit"},
		{Kind: "kill_intent", Intent: 71},
	}
	auto, suggest := EscalatePlan(actions)
	if len(auto) != 2 || len(suggest) != 2 {
		t.Fatalf("auto=%+v suggest=%+v", auto, suggest)
	}
	for _, a := range suggest {
		if a.Kind != "propose_intent" && a.Kind != "set_next_stage" {
			t.Fatalf("需人确认的动作不该进 auto：%+v", a)
		}
	}
}

// Parse 必须保留 propose_intent（docs/14 §3）。
func TestParseKeepsProposeIntent(t *testing.T) {
	raw := `{"verdict":"pivot","body":"换一条路","cites":[{"kind":"finding","id":31}],` +
		`"actions":[{"kind":"propose_intent","why":"读配置文件找可写目录"}],"confidence":"medium"}`
	v, err := Parse(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(v.Actions) != 1 || v.Actions[0].Kind != "propose_intent" {
		t.Fatalf("propose_intent 必须保留：%+v", v.Actions)
	}
}
