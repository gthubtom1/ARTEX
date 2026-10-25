package server

import (
	"regexp"
	"strings"
)

// 证据解析（P0-01）：等级 E3/E4 **不许**再由"漏洞名里出现 rce / 摘要里出现 样本"这类文字决定。
//
// 背景（外部产品审查 2026-10-25，`docs/PLANS/2026-10-25-artex-产品审查.md` P0-01）：
// 旧判定 `strings.Contains(vulnclass,"rce") → Shells++`、`strings.Contains(evidence,"样本") → DataSamples++`
// 会让"只是风险"的洞被算成"真的打通了"，用户拿这份结论去做商业判断。
//
// 现在改成三段**可解析**判据（纯字符串解析，同输入同输出，不调模型）：
//  1. 有往返：出现请求（HTTP 动词 + 路径/URL）且出现响应（HTTP/1.x + 状态码）
//  2. 有数据行：≥2 行"结构化记录"（`|` 分隔多字段），且正文里出现数据类词
//  3. 有命令回显：uid=0(... / root:x:0:0: / Microsoft Windows [Version / nt authority\system
//
// 校准依据是真机跑出来的两份证据（都真的打通了）：
//   - #19（2026-10-24，E4）：`GET https://… 200`、`-> HTTP/1.1 302`、3 行 `1) txid=24782 | …`；
//   - #20（2026-10-25，E4）：`GET /bank/transaction.jsp` + `-> HTTP/1.1 200` + 3 行
//     `24928 | 2026-10-24 19:20 | 800000 | …`，且写的是"表格样本"而不是"条样本"。
//
// 两次踩坑（都是真机才发现，读代码/mock 测不出来）：
//   - jsonb 取出来的 evidence 是**转义文本**（换行是字面 `\n`），不还原就按行解析全落空 → normalizeEvidence；
//   - 请求行常写在 `【攻击】` / `"poc": "` 这类前缀后面，不能只认行首 → 动词+路径/URL 匹配。

var (
	// 请求：HTTP 动词 + 路径或 URL（不要求行首，能认 【攻击】POST /doLogin 这种写法）
	evReqRe = regexp.MustCompile(`(?i)\b(?:GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s+(?:https?://|/)[\w./:?=&%#{}$\-@\[\]]*`)
	// 响应：`HTTP/1.1 302 Found`
	evRespRe = regexp.MustCompile(`(?i)HTTP/1\.[01]\s+[1-5]\d\d`)
	// 命令回显特征
	// 命令回显特征：只认"执行了命令"才会出现的东西。
	// 注意不能把 /etc/passwd 的内容（root:x:0:0:）算进来 —— 那是文件读取，不是命令执行。
	evCmdRe = regexp.MustCompile(`(?i)uid=\d+\(|microsoft windows \[version|nt authority\system|volume serial number`)
	// 结构化记录行：`1) txid=24782 | … | -$7777.77` / `24928 | 2026-10-24 19:20 | 800000 | …`
	evRecordRe = regexp.MustCompile(`(?i)(^\s*\d+[).]?\s+.*\|.*\|)|\b\w{2,12}=(?:[^|\s]{1,40})\s*\|\s*\w{2,14}=`)
	// 数据类词。它只是三道判据之一，必须与"往返 + ≥2 行结构化记录"同时成立，
	// 所以可以放宽到"样本/记录/流水/表格"这类常见说法（#20 写的是"表格样本"）。
	evDataWordRe = regexp.MustCompile(`(?i)条样本|样本|条记录|记录数|记录|行数据|流水|表格|字段|rows? returned|sample rows|record count|transactions?`)
)

// evidenceProof 是一段证据文本的"可证明到什么"解析结果。
type evidenceProof struct {
	Req       bool // 有请求
	Resp      bool // 有响应
	Records   int  // 结构化记录行数
	DataWord  bool // 有数据类词
	CmdOutput bool // 有命令回显
}

// normalizeEvidence 把"从库里取出来的 JSON 文本"还原成可读多行文本。
// 只在确实含反斜杠时才做替换，避免污染正常文本。
func normalizeEvidence(ev string) string {
	if !strings.Contains(ev, `\`) {
		return ev
	}
	out := strings.ReplaceAll(ev, `\r\n`, "\n")
	out = strings.ReplaceAll(out, `\n`, "\n")
	out = strings.ReplaceAll(out, `\t`, "\t")
	out = strings.ReplaceAll(out, `\"`, `"`)
	out = strings.ReplaceAll(out, `\/`, "/")
	return out
}

// parseEvidence 解析证据文本。空文本 → 全 false（这是关键：没证据就什么都没证明）。
func parseEvidence(ev string) evidenceProof {
	var p evidenceProof
	ev = normalizeEvidence(ev)
	if strings.TrimSpace(ev) == "" {
		return p
	}
	p.Req = evReqRe.MatchString(ev)
	p.Resp = evRespRe.MatchString(ev)
	p.DataWord = evDataWordRe.MatchString(ev)
	p.CmdOutput = evCmdRe.MatchString(ev)
	if p.Resp {
		for _, line := range strings.Split(ev, "\n") {
			if evRecordRe.MatchString(line) {
				p.Records++
			}
		}
	}
	return p
}

// roundTrip：请求 + 响应都出现过 —— "这一步真的发生过"的最低要求。
func (p evidenceProof) roundTrip() bool { return p.Req && p.Resp }

// provesCommand：能证明"命令真的执行了"。出现命令回显特征即算（回显本身就是执行结果）。
func (p evidenceProof) provesCommand() bool { return p.CmdOutput }

// provesSession：能证明"真的进了（后台）会话"。
//
// 真机 #22（2026-10-25）：worker 用 admin'-- 拿到后台会话、页面出现 "Hello Admin User"，
// 但等级仍显示 E0 —— 因为 AppControl 只从 fact 文本里正则匹配"登录/后台 + 成功"，
// 而它把结论写成了事实（"取得 Admin 会话"）+ 证据块。等级低估和虚高一样糟：
// 用户会以为"什么都没拿到"。
//
// 判据：请求/响应往返 + 出现"已登录/后台页"的特征词（必须是响应里的现象，不是模型自述）。
func (p evidenceProof) provesSession(ev string) bool {
	if !p.roundTrip() {
		return false
	}
	low := strings.ToLower(ev)
	for _, k := range []string{
		"hello admin", "sign off", "已处于登录态", "登录成功", "已登录", "已进入后台",
		"管理后台", "后台首页", "dashboard", "administration", "logout", "退出登录",
	} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

// provesData：能证明"真的读到了业务数据"。
// 要求：往返 + ≥2 行结构化记录 + 数据类词。只有"登录成功了"或"摘要里写了样本"都不算。
func (p evidenceProof) provesData() bool {
	return p.roundTrip() && p.Records >= 2 && p.DataWord
}
