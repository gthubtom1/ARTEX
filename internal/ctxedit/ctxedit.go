// Package ctxedit 实现 tool_result 的「上下文编辑」（context editing）。
//
// 长任务里工具结果会不断累积，是仅次于工具 schema 的第二大上下文开销。
// 本包提供纯函数式的裁剪能力：保留最近 N 条 tool_result 完整，把更早的大块
// 结果压成「首尾保留 + 省略标记」或一行摘要（保留 URL / 命令 / 状态码 / 首行）。
//
// 设计约束：
//   - 纯函数：不修改入参、无全局可变状态，可并发调用；
//   - 零依赖：只使用标准库；
//   - 未超预算时完全不动（Changed=false），调用方可以每轮无脑调用；
//   - 所有截断都按 rune 边界进行，输出始终是合法 UTF-8。
package ctxedit

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	defaultKeepRecent    = 6
	defaultHeadBytes     = 400
	defaultTailBytes     = 400
	defaultMaxTotalBytes = 120_000
	defaultMinKeepLines  = 3

	// 整段摘要模式的预算（字节）。
	defaultSummaryLimit = 512
	// 摘要里每个条目（URL / 命令行 / 状态码 / 首行）的最大字节数。
	summaryItemMaxBytes = 200
	// 摘要里最多收集几个 URL。
	maxSummaryURLs = 3

	// 为保证 MinKeepLines 行完整，允许 Head/Tail 在基准字节数之外扩展的幅度。
	headExtendFactor = 4
	headExtendSlack  = 1024
)

// Policy 是裁剪策略。
//
// 零值 Policy{} 表示全部使用默认值（见 DefaultPolicy）。
// 一旦显式设置了任意字段，未设置（0）的 HeadBytes/TailBytes 就表示「不保留首尾」，
// 即整段替换为 Summarize 的一行摘要；负值同样按 0 处理。
type Policy struct {
	KeepRecent    int // 最近 N 条 tool_result 原样保留（默认 6）
	HeadBytes     int // 更早的保留开头多少字节（默认 400）
	TailBytes     int // 再保留结尾多少字节（默认 400）；开头+结尾=0 表示整段换成一行摘要
	MaxTotalBytes int // 全部 tool_result 总字节上限；未超限则完全不动（默认 120_000）
	MinKeepLines  int // Head/Tail 至少保留几行（默认 3）
}

// DefaultPolicy 返回默认裁剪策略。
func DefaultPolicy() Policy {
	return Policy{
		KeepRecent:    defaultKeepRecent,
		HeadBytes:     defaultHeadBytes,
		TailBytes:     defaultTailBytes,
		MaxTotalBytes: defaultMaxTotalBytes,
		MinKeepLines:  defaultMinKeepLines,
	}
}

// withDefaults 在 Edit 内部做策略归一化。注意：只有完全零值的 Policy 才会
// 获得默认 HeadBytes/TailBytes；否则显式的 0 表示「整段摘要」模式。
func (p Policy) withDefaults() Policy {
	if p == (Policy{}) {
		return DefaultPolicy()
	}
	if p.KeepRecent <= 0 {
		p.KeepRecent = defaultKeepRecent
	}
	if p.MaxTotalBytes <= 0 {
		p.MaxTotalBytes = defaultMaxTotalBytes
	}
	if p.MinKeepLines <= 0 {
		p.MinKeepLines = defaultMinKeepLines
	}
	if p.HeadBytes < 0 {
		p.HeadBytes = 0
	}
	if p.TailBytes < 0 {
		p.TailBytes = 0
	}
	return p
}

// Result 是一次裁剪的结果。
type Result struct {
	Changed     bool
	BeforeBytes int
	AfterBytes  int
	EditedCount int // 被压缩的块数
}

// Edit 对一组「工具结果文本」就地计划裁剪，返回新切片与统计。
//
// 入参 texts 中每条对应一个 tool_result 的文本；返回切片长度与入参一致。
// 总字节未超过策略上限时原样返回（Changed=false），不做任何改写。
// 入参不会被修改，返回值与入参不共享底层数组。
func Edit(texts []string, p Policy) ([]string, Result) {
	p = p.withDefaults()

	before := 0
	for _, t := range texts {
		before += len(t)
	}

	res := Result{BeforeBytes: before, AfterBytes: before}

	// 快路径：未超限（或没有输入）时完全不动。
	if len(texts) == 0 || before <= p.MaxTotalBytes {
		out := make([]string, len(texts))
		copy(out, texts)
		return out, res
	}

	out := make([]string, len(texts))
	copy(out, texts)

	keep := p.KeepRecent
	if keep < 0 {
		keep = 0
	}
	if keep > len(texts) {
		keep = len(texts)
	}
	cut := len(texts) - keep

	edited := 0
	after := 0
	for i, t := range out {
		if i < cut {
			c := compressOne(t, p)
			if len(c) < len(t) {
				out[i] = c
				t = c
				edited++
			}
		}
		after += len(t)
	}

	res.Changed = edited > 0
	res.EditedCount = edited
	res.AfterBytes = after
	return out, res
}

// compressOne 压缩单条结果；只有确实变短时调用方才会采用。
func compressOne(s string, p Policy) string {
	if s == "" {
		return s
	}
	if p.HeadBytes+p.TailBytes <= 0 {
		return Summarize(s, defaultSummaryLimit)
	}
	return compressHeadTail(s, p)
}

// compressHeadTail 保留前 HeadBytes + 省略标记 + 后 TailBytes。
func compressHeadTail(s string, p Policy) string {
	n := len(s)

	// 整段都落在保留窗口内，没有压缩收益，交给调用方保持原样。
	if p.HeadBytes >= n || p.TailBytes >= n {
		return s
	}

	headEnd := prefixCut(s, p.HeadBytes)
	tailStart := suffixCut(s, p.TailBytes)

	maxHead := p.HeadBytes*headExtendFactor + headExtendSlack
	headEnd = extendHead(s, headEnd, p.MinKeepLines, maxHead)

	minTail := n - (p.TailBytes*headExtendFactor + headExtendSlack)
	if minTail < 0 {
		minTail = 0
	}
	tailStart = extendTail(s, tailStart, p.MinKeepLines, minTail)

	// 首尾窗口重叠：改用一行摘要，避免输出反而更长或语义混乱。
	if headEnd >= tailStart {
		return Summarize(s, defaultSummaryLimit)
	}

	omitted := tailStart - headEnd
	return s[:headEnd] +
		"\n…（已省略 " + strconv.Itoa(omitted) + " 字节）…\n" +
		s[tailStart:]
}

// extendHead 在不超过 maxEnd 的前提下，把 head 扩展到至少包含 minLines 个换行。
func extendHead(s string, end, minLines, maxEnd int) int {
	if end <= 0 || end >= len(s) || minLines <= 0 {
		return end
	}
	cnt := strings.Count(s[:end], "\n")
	if cnt >= minLines {
		return end
	}
	pos := end
	for cnt < minLines {
		j := strings.IndexByte(s[pos:], '\n')
		if j < 0 {
			if len(s) <= maxEnd {
				return len(s)
			}
			return end
		}
		next := pos + j + 1
		if next > maxEnd {
			return end
		}
		pos = next
		cnt++
	}
	return pos
}

// extendTail 在不超过 minStart 的前提下，把 tail 向前扩展到至少包含 minLines 个换行。
func extendTail(s string, start, minLines, minStart int) int {
	if start <= 0 || start >= len(s) || minLines <= 0 {
		return start
	}
	cnt := strings.Count(s[start:], "\n")
	if cnt >= minLines {
		return start
	}
	pos := start
	for cnt < minLines {
		j := strings.LastIndexByte(s[:pos], '\n')
		if j < 0 {
			if minStart <= 0 {
				return 0
			}
			return start
		}
		if j+1 < minStart {
			return start
		}
		pos = j + 1
		cnt++
	}
	return pos
}

// prefixCut 返回不超过 max 字节、且落在 rune 起始处的切点。
func prefixCut(s string, max int) int {
	if max <= 0 {
		return 0
	}
	if max >= len(s) {
		return len(s)
	}
	i := max
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

// suffixCut 返回后缀起点，使 s[start:] 不超过 n 字节且是合法 UTF-8。
func suffixCut(s string, n int) int {
	if n <= 0 {
		return len(s)
	}
	if n >= len(s) {
		return 0
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return i
}

// truncateBytes 按 rune 边界把 s 截到不超过 limit 字节。
// limit 小于首个 rune 时会返回首个 rune（保证不返回空串）。
func truncateBytes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	i := prefixCut(s, limit)
	if i > 0 {
		return s[:i]
	}
	_, size := utf8.DecodeRuneInString(s)
	if size > 0 && size <= len(s) {
		return s[:size]
	}
	return ""
}

// Summarize 是给「整段被替换」场景用的一行摘要：从原文里抽出最能代表这次调用的几样东西，
// 顺序：URL(http/https) → 命令行(以 $ 或 > 开头的行) → 状态码(HTTP/1.x NNN) → 首行。
//
// 每个条目最多 summaryItemMaxBytes 字节并去重，整体不超过 limit 字节（limit<=0 时用默认值）。
// 抽不出任何内容时退化为「首行 + 字节数」，绝不返回空串。
func Summarize(text string, limit int) string {
	if limit <= 0 {
		limit = defaultSummaryLimit
	}
	itemMax := summaryItemMaxBytes
	if itemMax > limit {
		itemMax = limit
	}

	var items []string
	seen := make(map[string]bool, 4)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		s = truncateBytes(s, itemMax)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		items = append(items, s)
	}

	for _, u := range findURLs(text, maxSummaryURLs) {
		add(u)
	}
	if cmd := findCommandLine(text); cmd != "" {
		add(cmd)
	}
	if st := findHTTPStatus(text); st != "" {
		add(st)
	}
	if first := firstNonEmptyLine(text); first != "" {
		add(first)
	}

	if len(items) == 0 {
		line := firstNonEmptyLine(text)
		if line == "" {
			line = "（空结果）"
		}
		return truncateBytes(line+" | "+strconv.Itoa(len(text))+" bytes", limit)
	}

	out := truncateBytes(strings.Join(items, " | "), limit)
	if out != "" {
		return out
	}
	// limit 极小时的兜底：至少返回一个 rune，绝不返回空串。
	if len(text) > 0 {
		_, size := utf8.DecodeRuneInString(text)
		if size > 0 && size <= len(text) {
			return text[:size]
		}
	}
	return "（空结果）"
}

// findURLs 按出现顺序抽取最多 max 个 http/https URL。
func findURLs(text string, max int) []string {
	var out []string
	for i := 0; i < len(text) && len(out) < max; {
		j := strings.Index(text[i:], "http")
		if j < 0 {
			break
		}
		start := i + j
		rest := text[start:]
		if !strings.HasPrefix(rest, "http://") && !strings.HasPrefix(rest, "https://") {
			i = start + len("http")
			continue
		}
		end := start
		for end < len(text) && isURLByte(text[end]) {
			end++
		}
		for end > start && (text[end-1] == '.' || text[end-1] == ',') {
			end--
		}
		if end-start > len("https://") {
			out = append(out, text[start:end])
		}
		if end <= start {
			end = start + len("http")
		}
		i = end
	}
	return out
}

// isURLByte 报告 b 是否可出现在摘要里的 URL 中。非 ASCII 字节一律视为分隔符，
// 避免把紧跟 URL 的中文标点吞进来。
func isURLByte(b byte) bool {
	if b <= ' ' || b >= utf8.RuneSelf {
		return false
	}
	switch b {
	case '"', '\'', '<', '>', '`', '(', ')', '[', ']', '{', '}', ',', ';', '|', '\\':
		return false
	}
	return true
}

// findCommandLine 返回第一条以 $ 或 > 开头的非空行。
func findCommandLine(text string) string {
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		if len(t) < 2 {
			continue
		}
		if t[0] == '$' || t[0] == '>' {
			return t
		}
	}
	return ""
}

// findHTTPStatus 返回第一个形如 HTTP/1.1 403 的状态码片段。
func findHTTPStatus(text string) string {
	for i := 0; i < len(text); {
		j := strings.Index(text[i:], "HTTP/")
		if j < 0 {
			return ""
		}
		start := i + j
		k := start + len("HTTP/")
		vs := k
		for k < len(text) && (isDigit(text[k]) || text[k] == '.') {
			k++
		}
		if k == vs {
			i = start + len("HTTP/")
			continue
		}
		version := text[vs:k]
		m := k
		for m < len(text) && (text[m] == ' ' || text[m] == '\t') {
			m++
		}
		if m+3 <= len(text) && isDigit(text[m]) && isDigit(text[m+1]) && isDigit(text[m+2]) {
			return "HTTP/" + version + " " + text[m:m+3]
		}
		i = k
	}
	return ""
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// firstNonEmptyLine 返回第一个非空行（已 TrimSpace）。
func firstNonEmptyLine(text string) string {
	for _, ln := range strings.Split(text, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return ""
}

// countLines 统计行数：空串为 0；有内容时按换行符计数，最后一行无换行也算一行。
func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// LooksLikeBigBlob 判断一条结果是否「值得优先压缩」：超过 size 字节且行数 > lines。
// 负的 size/lines 按 0 处理。
func LooksLikeBigBlob(text string, size, lines int) bool {
	if size < 0 {
		size = 0
	}
	if lines < 0 {
		lines = 0
	}
	return len(text) > size && countLines(text) > lines
}
