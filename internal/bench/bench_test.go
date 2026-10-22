package bench

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildRunKey(t *testing.T) {
	now := time.Date(2026, 10, 22, 15, 30, 45, 0, time.UTC)
	cases := []struct {
		set, model, want string
	}{
		{"basic", "deepseek-v4.1-flash", "20261022-1530-basic-deepseek-v4.1-flash"},
		{"smoke", "deepseek v4.1/flash", "20261022-1530-smoke-deepseek-v4.1-flash"},
		{"basic", "claude   sonnet -- 4", "20261022-1530-basic-claude-sonnet-4"},
		{"Smoke_1", "qwen2.5-72b", "20261022-1530-Smoke_1-qwen2.5-72b"},
		{"", "", "20261022-1530-default-unknown"},
		{"a/b/../c", strings.Repeat("m", 40), "20261022-1530-a-b-c-" + strings.Repeat("m", 32)},
	}
	for _, c := range cases {
		if got := BuildRunKey(now, c.set, c.model); got != c.want {
			t.Fatalf("BuildRunKey(%q,%q)=%q want %q", c.set, c.model, got, c.want)
		}
	}

	// run_key 必须能直接当结果文件名用。
	key := BuildRunKey(now, "basic", "deepseek v4.1/flash") + ".json"
	if _, err := SafeRunKey(key); err != nil {
		t.Fatalf("BuildRunKey 产物无法通过 SafeRunKey: %v", err)
	}
}

func TestSafeRunKey(t *testing.T) {
	good := []string{
		"20261022-1530-basic-flash.json",
		"run.json",
		"a_b.c-d.json",
	}
	for _, name := range good {
		if got, err := SafeRunKey(name); err != nil || got != name {
			t.Fatalf("SafeRunKey(%q)=(%q,%v), want 原样返回", name, got, err)
		}
	}

	bad := []string{
		"",
		"run",
		"run.json.bak",
		"run.JSON",
		"../../etc/passwd",
		"..%2fpasswd.json",
		"a/../b.json",
		`a\b.json`,
		"/abs/run.json",
		"run key.json",
		"run.jsonx",
		"..json",
		"run..json",
	}
	for _, name := range bad {
		if _, err := SafeRunKey(name); err == nil {
			t.Fatalf("SafeRunKey(%q) 应报错", name)
		}
	}
}

func TestSummarize(t *testing.T) {
	r := Run{
		RunKey: "k", Total: 3, Passed: 2,
		Cases: []CaseResult{
			{Key: "a", Passed: true, DurationMS: 1000, FirstFindingMS: 600, Outcome: OutcomePassed},
			{Key: "b", Passed: false, DurationMS: 2000, FirstFindingMS: 0, Outcome: OutcomeFailed},
			{Key: "c", Passed: true, DurationMS: 3000, FirstFindingMS: 400, Outcome: OutcomePassed},
		},
	}
	s := Summarize(r)
	if s.PassRate != 2.0/3.0 {
		t.Fatalf("PassRate=%v", s.PassRate)
	}
	if s.AvgDurationMS != 2000 {
		t.Fatalf("AvgDurationMS=%d", s.AvgDurationMS)
	}
	// 只对“出了 finding”的用例求均值：(600+400)/2。
	if s.AvgFirstFindingMS != 500 {
		t.Fatalf("AvgFirstFindingMS=%d", s.AvgFirstFindingMS)
	}
	if s.DeltaPassRate != 0 || s.DeltaDurationMS != 0 {
		t.Fatalf("Summarize 不应带环比: %+v", s)
	}

	empty := Summarize(Run{})
	if empty.PassRate != 0 || empty.AvgDurationMS != 0 || empty.AvgFirstFindingMS != 0 {
		t.Fatalf("空 Run 汇总应为零值: %+v", empty)
	}
}

func TestDelta(t *testing.T) {
	prev := Run{
		RunKey: "p", Total: 2, Passed: 1,
		Cases: []CaseResult{
			{Key: "a", Passed: true, DurationMS: 1000, FirstFindingMS: 100, Outcome: OutcomePassed},
			{Key: "b", Passed: false, DurationMS: 3000, Outcome: OutcomeFailed},
		},
	}
	cur := Run{
		RunKey: "c", Total: 2, Passed: 2,
		Cases: []CaseResult{
			{Key: "a", Passed: true, DurationMS: 1000, FirstFindingMS: 200, Outcome: OutcomePassed},
			{Key: "b", Passed: true, DurationMS: 1000, FirstFindingMS: 200, Outcome: OutcomePassed},
		},
	}
	s := Delta(prev, cur)
	if s.PassRate != 1 {
		t.Fatalf("PassRate=%v", s.PassRate)
	}
	if s.DeltaPassRate != 50 {
		t.Fatalf("DeltaPassRate=%v want 50pp", s.DeltaPassRate)
	}
	if s.DeltaDurationMS != -1000 {
		t.Fatalf("DeltaDurationMS=%d want -1000", s.DeltaDurationMS)
	}

	// 没有上一次 → 只给绝对值，环比为 0。
	first := Delta(Run{}, cur)
	if first.PassRate != 1 || first.DeltaPassRate != 0 || first.DeltaDurationMS != 0 {
		t.Fatalf("首次跑批环比应为 0: %+v", first)
	}
}

func validRun() Run {
	return Run{
		RunKey: "20261022-1530-smoke-flash", SetName: "smoke", ModelProfile: "flash",
		StartedAt: "2026-10-22T15:30:00+08:00",
		Total:     2, Passed: 1, DurationMS: 4000, TotalTokens: 1234,
		Cases: []CaseResult{
			{Key: "sqli-01", Category: "sqli", Passed: true, DurationMS: 1000, FirstFindingMS: 400, Tokens: 100, TaskID: "1", Outcome: OutcomePassed},
			{Key: "cmdi-01", Category: "cmdi", Passed: false, DurationMS: 3000, Tokens: 200, TaskID: "2", Outcome: OutcomeFailed},
		},
	}
}

func TestVerify(t *testing.T) {
	if err := Verify(validRun()); err != nil {
		t.Fatalf("valid run 应通过: %v", err)
	}

	bad := []struct {
		name string
		mut  func(*Run)
	}{
		{"空 run_key", func(r *Run) { r.RunKey = "" }},
		{"passed>total", func(r *Run) { r.Passed = 3 }},
		{"cases 数量不符", func(r *Run) { r.Cases = r.Cases[:1] }},
		{"outcome 非法", func(r *Run) { r.Cases[0].Outcome = "ok" }},
		{"passed 标记矛盾", func(r *Run) { r.Cases[0].Passed = false }},
		{"负数时延", func(r *Run) { r.Cases[1].DurationMS = -1 }},
		{"total 为负", func(r *Run) { r.Total = -1 }},
	}
	for _, b := range bad {
		r := validRun()
		b.mut(&r)
		if err := Verify(r); err == nil {
			t.Fatalf("%s: 应报错", b.name)
		}
	}

	// Total=0 时允许空批次（跑批被 --limit 0 掐掉）。
	if err := Verify(Run{RunKey: "r.json"}); err != nil {
		t.Fatalf("空批次应通过: %v", err)
	}
}

func TestParseCaseYAMLAndMatch(t *testing.T) {
	dir := filepath.Join("..", "..", "bench", "cases")
	c, err := LoadCaseFile(filepath.Join(dir, "sqli-01.yaml"))
	if err != nil {
		t.Fatalf("LoadCaseFile: %v", err)
	}
	if c.Key != "sqli-01" || c.Category != "sqli" || c.TimeoutS <= 0 {
		t.Fatalf("解析结果不对: %+v", c)
	}
	if c.Pass.FindingSurfaceRegex == "" || len(c.Pass.FindingVulnclass) == 0 || c.Pass.FindingEvidenceRegex == "" {
		t.Fatalf("sqli-01 的 pass 规则不完整: %+v", c.Pass)
	}

	// 正确命中的证据（surface 用真实 DVWA 入口）。
	hit := Finding{
		VulnClass: "SQL-Injection",
		Surface:   "http://127.0.0.1:18081/vulnerabilities/sqli/?id=1",
		Evidence:  "id=1' AND 1=1 -- 返回正常，id=1' AND 1=2 -- 返回空 → 布尔盲注",
	}
	if !c.Match(hit) {
		t.Fatalf("应命中: %+v", hit)
	}

	// 误报：只有“疑似”结论，没有可复现证据。
	miss := []Finding{
		{VulnClass: "SQL-Injection", Surface: hit.Surface, Evidence: "页面报错，可能存在 SQL 注入"},
		{VulnClass: "xss", Surface: hit.Surface, Evidence: hit.Evidence},
		{VulnClass: "SQL-Injection", Surface: "http://127.0.0.1:18081/login.php", Evidence: hit.Evidence},
	}
	for i, f := range miss {
		if c.Match(f) {
			t.Fatalf("误报 #%d 不应命中: %+v", i, f)
		}
	}

	ok, first := c.MatchAny(miss)
	if ok || first != nil {
		t.Fatalf("MatchAny 不应命中")
	}
	ok, first = c.MatchAny(append([]Finding{{VulnClass: "xss"}}, hit))
	if !ok || first == nil || first.VulnClass != hit.VulnClass {
		t.Fatalf("MatchAny 应返回第一条命中: ok=%v first=%+v", ok, first)
	}
}

func TestParseCaseYAMLBadInput(t *testing.T) {
	if _, err := ParseCaseYAML([]byte("::: not yaml :::")); err == nil {
		t.Fatalf("非法 YAML 应报错")
	}
	if _, err := ParseCaseYAML([]byte("name: 没有 key\n")); err == nil {
		t.Fatalf("缺 key 应报错")
	}
}

func TestMatchRuleSemantics(t *testing.T) {
	// 全空 → false（没有判据不算通过）。
	var empty Case
	if empty.Match(Finding{VulnClass: "anything", Surface: "x", Evidence: "y"}) {
		t.Fatalf("空规则必须返回 false")
	}

	// 只配 vulnclass（非 AND 多条件）→ 大小写不敏感的子串匹配。
	onlyClass := Case{Pass: PassRule{FindingVulnclass: []string{"idor", "broken-object"}}}
	if !onlyClass.Match(Finding{VulnClass: "Broken-Object-Level Authorization (IDOR)"}) {
		t.Fatalf("vulnclass 子串匹配失败")
	}
	if onlyClass.Match(Finding{VulnClass: "csrf"}) {
		t.Fatalf("vulnclass 不匹配却通过")
	}

	// 坏正则 → 视为不匹配，不 panic。
	badRe := Case{Pass: PassRule{FindingSurfaceRegex: "("}}
	if badRe.Match(Finding{Surface: "anything"}) {
		t.Fatalf("坏正则必须按不匹配处理")
	}

	// AND 语义：vulnclass 命中但 evidence 不命中 → false。
	both := Case{Pass: PassRule{
		FindingVulnclass:     []string{"ssrf"},
		FindingSurfaceRegex:  `(?i)domain=`,
		FindingEvidenceRegex: `(?i)(127\.0\.0\.1|169\.254\.169\.254)`,
	}}
	if !both.Match(Finding{VulnClass: "SSRF", Surface: "/?domain=x", Evidence: "回显 127.0.0.1:65412 首页"}) {
		t.Fatalf("AND 全满足应命中")
	}
	if both.Match(Finding{VulnClass: "SSRF", Surface: "/?domain=x", Evidence: "看起来可以访问内网"}) {
		t.Fatalf("evidence 不满足却通过")
	}
	if both.Match(Finding{VulnClass: "SSRF", Surface: "/?id=1", Evidence: "127.0.0.1"}) {
		t.Fatalf("surface 不满足却通过")
	}
}

func TestAllCaseFilesParse(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "bench", "cases", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 10 {
		t.Fatalf("应有 10 个用例文件，实际 %d: %v", len(paths), paths)
	}
	seen := map[string]bool{}
	for _, p := range paths {
		c, err := LoadCaseFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if seen[c.Key] {
			t.Fatalf("重复 key: %s", c.Key)
		}
		seen[c.Key] = true
		if c.Name == "" || c.Goal == "" || c.URL == "" {
			t.Fatalf("%s: name/goal/url 不能为空: %+v", p, c)
		}
		if !strings.HasPrefix(c.URL, "http://127.0.0.1:1808") {
			t.Fatalf("%s: url 必须指向本地靶场 127.0.0.1:1808x，实际 %q", p, c.URL)
		}
		if c.TimeoutS <= 0 || c.TimeoutS > 900 {
			t.Fatalf("%s: timeout_s=%d 不合理", p, c.TimeoutS)
		}
	}
}
