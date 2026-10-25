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
//  1. 有往返：请求行（GET/POST … ）与响应行（HTTP/1.x 2xx/3xx/4xx/5xx）都出现
//  2. 有数据行：≥2 行"结构化记录"（`|` 分隔 ≥3 字段），且正文里出现数据类词
//  3. 有命令回显：uid=0(... / root:x:0:0: / Microsoft Windows [Version / nt authority\system
//
// 校准依据是真机 #19 的证据（2026-10-24，E4 数据访问，evidence 1134 字节）：
// 它含 `GET https://… 200`、`-> HTTP/1.1 302 Found`、`GET /bank/transaction.jsp… -> 200`，
// 以及三行 `1) txid=24782 | … | -$7777.77` —— 三条判据都能过，所以收紧后不会误伤真结果。
//
// 二次教训（2026-10-25，换装 0.11.47 后 #19 从 E4 掉到 E0 才发现）：
// 证据在 jsonb 里取出来是**转义过的 JSON 文本**（换行是字面 `\n`），必须先还原再解析，
// 否则"按行找请求行/记录行"全部落空 —— 真证据被判成没证据。见 normalizeEvidence。

var (
	// 请求行：`GET /path HTTP/1.1` 或 `POST /doLogin  body: …`（真机 #19 里的写法）
	evReqRe = regexp.MustCompile(`(?mi)^\s*(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s+[\w./:?=&%#{}$\-@\[\]]+`)
	// 响应行：`HTTP/1.1 302 Found`
	evRespRe = regexp.MustCompile(`(?i)HTTP/1\.[01]\s+[1-5]\d\d`)
	// 命令回显特征
	evCmdRe = regexp.MustCompile(`(?i)uid=\d+\(|^root:[^:]*:0:0:|microsoft windows \[version|nt authority\\system|volume serial number`)
	// 结构化记录行：`1) txid=24782 | … | -$7777.77` / `id=3 | name=… | pass=…`
	evRecordRe = regexp.MustCompile(`(?i)(^\s*\d+[).]?\s+.*\|.*\|)|\b\w{2,12}=(?:[^|\s]{1,40})\s*\|\s*\w{2,14}=`)
	// 数据类词（避免把"登录成功"当数据）
	evDataWordRe = regexp.MustCompile(`(?i)条样本|条记录|条数据|行数据|记录数|rows? returned|sample rows|record count`)
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

// provesData：能证明"真的读到了业务数据"。
// 要求：往返 + ≥2 行结构化记录 + 数据类词。只有"登录成功了"或"摘要里写了样本"都不算。
func (p evidenceProof) provesData() bool {
	return p.roundTrip() && p.Records >= 2 && p.DataWord
}
