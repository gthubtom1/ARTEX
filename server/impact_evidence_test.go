package server

import (
	"strings"
	"testing"

	"github.com/Autumn-27/artex/internal/impact"
)

// 真机 #19（2026-10-24，E4 数据访问）的 evidence 原文，砍到关键几行。
// 校准原则：收紧判据不能把**真结果**也砍掉 —— 这份必须过。
const realEvidence19 = `{"by": "work#2", "poc": "【基线】GET https://demo.testfire.net/ 200 (9227B, title=Altoro Mutual)；GET /login.jsp 200 (8360B)，表单 action=\"doLogin\"。
【对照组】POST /doLogin  body: uid=admin&passw=wrongpass123
 -> HTTP/1.1 302 Found ; Location: login.jsp
【攻击】POST /doLogin  body: uid=admin'--&passw=x
 -> HTTP/1.1 302 Found ; Location: /bank/main.jsp
    Set-Cookie: JSESSIONID=851DC3C73D3B434CB4899549FCD2988A
【会话验证，只读】GET /bank/main.jsp  Cookie: JSESSIONID=851DC3C7...
 -> HTTP/1.1 200 (6195B) ; 页面含 \"Hello Admin User\"
【仅读交易样本，单账户 800000，最多 3 条】
GET /bank/transaction.jsp?accountId=800000 (同一会话) -> 200，流水表 3 条样本：
 1) txid=24782 | 2026-10-24 18:31 | account=800000 | Withdrawal | -$7777.77
 2) txid=24754 | 2026-10-24 18:24 | account=800000 | Withdrawal | -$7777.77
 3) txid=24753 | 2026-10-24 18:22 | account=800001 | Deposit    | $7777.77"}`

// P0-01：真结果的证据必须仍然被判成"真的读到了数据"。
func TestEvidenceProofKeepsRealRun(t *testing.T) {
	p := parseEvidence(realEvidence19)
	if !p.roundTrip() {
		t.Fatalf("真机 #19 证据应满足往返（请求+响应），得到 %+v", p)
	}
	if p.Records < 2 {
		t.Fatalf("真机 #19 有 3 行流水样本，应识别出 ≥2 行，得到 %d", p.Records)
	}
	if !p.provesData() {
		t.Fatalf("真机 #19 应被判成读到了业务数据，得到 %+v", p)
	}
}

// P0-01：只是"风险"（漏洞名写了 RCE，但没有命令回显/没有响应）不许点亮等级。
func TestEvidenceProofRejectsKeywordOnly(t *testing.T) {
	cases := []string{
		"",
		"命令执行（未验证）",
		"SQL 注入，虽然没取数据但应该能拖库",
		"摘要里写着：疑似可以读到用户数据样本",
	}
	for _, ev := range cases {
		p := parseEvidence(ev)
		if p.provesCommand() {
			t.Fatalf("没有命令回显却判成能执行命令：%q → %+v", ev, p)
		}
		if p.provesData() {
			t.Fatalf("没有数据行却判成读到数据：%q → %+v", ev, p)
		}
	}
}

// P0-02：能力不许跨洞复用 —— 一条洞读到数据，不能让另一条只提到 SQL 的洞也"已成立"。
func TestStandingGotBelongsToProducingFinding(t *testing.T) {
	reasoning := lootFinding{id: 1, title: "SQL 注入（报错回显，未取数）", text: "sql 注入 报错回显"}
	produced := lootFinding{id: 2, title: "订单接口越权（取到 3 条流水）", text: "越权 idor 订单"}
	rows := []lootRow{{Key: "data", Label: "数据拉取（用户/订单/卡密）", Status: "done"}}

	// 任务级：确实读到了数据
	global := impact.Stats{Findings: 2, DataSamples: 1}

	// 漏洞 1 自己没产出数据 → 只能是"差一步"
	local1 := global
	local1.DataSamples = 0 // impactScan.byFinding[1].DataSamples == 0
	if got := findingStanding(reasoning, rows, local1); got.Standing != "one_step" || got.Gap == "" {
		t.Fatalf("没产出数据的洞不该标已成立，得到 %+v", got)
	}
	// 漏洞 2 就是产出者 → 已成立
	local2 := global
	if got := findingStanding(produced, rows, local2); got.Standing != "got" {
		t.Fatalf("产出数据的洞应标已成立，得到 %+v", got)
	}
}

// P0-01 端到端：同一条 SQL 注入，证据从"空"变成"真样本"，等级必须从 E0 变 E4。
// 这里不连库，直接走"解析 + 统计"这条纯逻辑：与 impactScan 用的是同一对函数。
func TestImpactLevelFollowsParsableEvidence(t *testing.T) {
	st := impact.Stats{Findings: 1}
	// ① 只有漏洞名，没有证据 → 不许抬到 E3/E4
	ev := "SQL 注入（未验证）"
	if p := parseEvidence(ev); p.provesData() {
		st.DataSamples++
	}
	if lv, _ := impact.Compute(st); lv >= impact.E3 {
		t.Fatalf("无证据的 SQL 注入不该到 E3+，得到 %v", lv)
	}
	// ② 补上真证据 → 到 E4
	if p := parseEvidence(realEvidence19); p.provesData() {
		st.DataSamples++
	}
	lv, basis := impact.Compute(st)
	if lv != impact.E4 {
		t.Fatalf("有真样本证据应为 E4，得到 %v（%s）", lv, basis)
	}
}

// 回归（2026-10-25 真机抓到的坑）：证据从 jsonb 取出来是转义文本（字面 \n），
// 必须先还原再解析。这里用"库里的真实形态"再跑一遍同一条判据。
func TestEvidenceProofHandlesJSONEscapedText(t *testing.T) {
	escaped := strings.ReplaceAll(realEvidence19, "\n", `\n`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	p := parseEvidence(escaped)
	if !p.provesData() {
		t.Fatalf("库里转义形态的证据也必须判成读到数据，得到 %+v", p)
	}
}
