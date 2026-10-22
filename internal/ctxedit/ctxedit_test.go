package ctxedit

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func sumBytes(texts []string) int {
	n := 0
	for _, t := range texts {
		n += len(t)
	}
	return n
}

func TestEditUnderLimitUnchanged(t *testing.T) {
	texts := []string{
		"alpha",
		"中文结果 https://example.com/x",
		strings.Repeat("x", 500),
	}
	p := Policy{MaxTotalBytes: 10_000}

	out, res := Edit(texts, p)
	if res.Changed {
		t.Fatalf("Changed = true, want false")
	}
	if res.EditedCount != 0 {
		t.Fatalf("EditedCount = %d, want 0", res.EditedCount)
	}
	if res.BeforeBytes != res.AfterBytes {
		t.Fatalf("BeforeBytes=%d AfterBytes=%d, want equal", res.BeforeBytes, res.AfterBytes)
	}
	if !reflect.DeepEqual(out, texts) {
		t.Fatalf("output changed:\n got=%q\nwant=%q", out, texts)
	}

	// 返回值不能与入参共享底层数组。
	out[0] = "mutated"
	if texts[0] != "alpha" {
		t.Fatalf("Edit mutated input slice")
	}
}

func TestEditZeroPolicyIsUnderDefaultLimit(t *testing.T) {
	texts := []string{"a", "b", strings.Repeat("z", 1000)}
	out, res := Edit(texts, Policy{})
	if res.Changed || res.EditedCount != 0 {
		t.Fatalf("zero Policy on small input changed: %+v", res)
	}
	if !reflect.DeepEqual(out, texts) {
		t.Fatalf("output changed")
	}
}

func TestEditOverLimitKeepsRecent(t *testing.T) {
	const total = 12
	texts := make([]string, total)
	for i := range texts {
		texts[i] = fmt.Sprintf(
			"RESULT-%02d https://example.com/scan/%d\n%s\nTAIL-%02d",
			i, i,
			strings.Repeat(fmt.Sprintf("line-%d data data data\n", i), 200),
			i,
		)
	}
	p := Policy{
		KeepRecent:    3,
		HeadBytes:     200,
		TailBytes:     200,
		MaxTotalBytes: 20_000,
		MinKeepLines:  3,
	}

	before := sumBytes(texts)
	out, res := Edit(texts, p)

	if !res.Changed {
		t.Fatalf("Changed = false, want true")
	}
	if len(out) != total {
		t.Fatalf("len(out) = %d, want %d", len(out), total)
	}
	if res.BeforeBytes != before {
		t.Fatalf("BeforeBytes = %d, want %d", res.BeforeBytes, before)
	}
	if res.AfterBytes >= res.BeforeBytes {
		t.Fatalf("AfterBytes = %d, want < BeforeBytes = %d", res.AfterBytes, res.BeforeBytes)
	}
	if res.EditedCount != total-3 {
		t.Fatalf("EditedCount = %d, want %d", res.EditedCount, total-3)
	}

	// 最近 3 条必须一字不差。
	for i := total - 3; i < total; i++ {
		if out[i] != texts[i] {
			t.Fatalf("recent block %d was modified", i)
		}
	}

	// 更早的块被压缩，但关键字段仍可检索。
	for i := 0; i < total-3; i++ {
		if len(out[i]) >= len(texts[i]) {
			t.Fatalf("block %d not compressed: %d >= %d", i, len(out[i]), len(texts[i]))
		}
		if !strings.Contains(out[i], "已省略") {
			t.Fatalf("block %d missing omission marker: %q", i, out[i])
		}
		if !strings.HasPrefix(out[i], fmt.Sprintf("RESULT-%02d", i)) {
			t.Fatalf("block %d missing head marker: %q", i, out[i])
		}
		if !strings.Contains(out[i], fmt.Sprintf("TAIL-%02d", i)) {
			t.Fatalf("block %d missing tail marker: %q", i, out[i])
		}
		if !utf8.ValidString(out[i]) {
			t.Fatalf("block %d is not valid UTF-8", i)
		}
	}
}

func TestEditUTF8Boundaries(t *testing.T) {
	block := strings.Repeat("中文测试结果ABC", 500)
	texts := []string{block, block, block}
	p := Policy{
		KeepRecent:    1,
		HeadBytes:     101, // 故意落在汉字中间
		TailBytes:     97,  // 故意落在汉字中间
		MaxTotalBytes: 1000,
		MinKeepLines:  3,
	}

	out, res := Edit(texts, p)
	if !res.Changed || res.EditedCount != 2 {
		t.Fatalf("unexpected result: %+v", res)
	}
	for i, s := range out {
		if !utf8.ValidString(s) {
			t.Fatalf("output %d is not valid UTF-8", i)
		}
	}
	if !strings.Contains(out[0], "已省略") {
		t.Fatalf("head/tail marker missing: %q", out[0])
	}
	if out[2] != texts[2] {
		t.Fatalf("recent block was modified")
	}
	if res.AfterBytes >= res.BeforeBytes {
		t.Fatalf("AfterBytes = %d, want < %d", res.AfterBytes, res.BeforeBytes)
	}
}

func TestEditHeadTailMode(t *testing.T) {
	blob := strings.Repeat("A", 5000)
	texts := []string{blob, blob, blob}
	p := Policy{
		KeepRecent:    1,
		HeadBytes:     100,
		TailBytes:     50,
		MaxTotalBytes: 1000,
		MinKeepLines:  3,
	}

	out, _ := Edit(texts, p)
	if !strings.HasPrefix(out[0], strings.Repeat("A", 100)) {
		t.Fatalf("head not preserved: %q", out[0])
	}
	if !strings.HasSuffix(out[0], strings.Repeat("A", 50)) {
		t.Fatalf("tail not preserved: %q", out[0])
	}
	if !strings.Contains(out[0], "已省略") {
		t.Fatalf("omission marker missing: %q", out[0])
	}
	if want := 5000 - 100 - 50; !strings.Contains(out[0], fmt.Sprintf("已省略 %d 字节", want)) {
		t.Fatalf("omitted bytes wrong, want %d: %q", want, out[0])
	}
}

func TestEditSummarizeMode(t *testing.T) {
	blob := "HTTP/1.1 403 Forbidden\n" +
		"$ nmap -sV 10.0.0.1\n" +
		"https://example.com/a\n" +
		strings.Repeat("noise line\n", 1000)
	texts := []string{blob, blob, blob}
	p := Policy{
		KeepRecent:    1,
		HeadBytes:     0,
		TailBytes:     0,
		MaxTotalBytes: 1000,
		MinKeepLines:  3,
	}

	out, res := Edit(texts, p)
	if !res.Changed || res.EditedCount != 2 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if strings.Contains(out[0], "已省略") {
		t.Fatalf("summarize mode should not use head/tail marker: %q", out[0])
	}
	if strings.Contains(out[0], "\n") {
		t.Fatalf("summary should be one line: %q", out[0])
	}
	for _, want := range []string{"https://example.com/a", "nmap", "HTTP/1.1 403"} {
		if !strings.Contains(out[0], want) {
			t.Fatalf("summary missing %q: %q", want, out[0])
		}
	}
	if len(out[0]) >= len(texts[0]) {
		t.Fatalf("summary did not shrink: %d >= %d", len(out[0]), len(texts[0]))
	}
	if !utf8.ValidString(out[0]) {
		t.Fatalf("summary is not valid UTF-8")
	}
	if out[2] != texts[2] {
		t.Fatalf("recent block was modified")
	}
}

func TestZeroPolicyUsesDefaults(t *testing.T) {
	texts := make([]string, 10)
	for i := range texts {
		texts[i] = fmt.Sprintf("block-%d\n%s", i, strings.Repeat("data ", 3000))
	}

	out, res := Edit(texts, Policy{})
	if !res.Changed {
		t.Fatalf("Changed = false, want true")
	}
	// 默认 KeepRecent=6，前 4 条被压缩。
	if res.EditedCount != 4 {
		t.Fatalf("EditedCount = %d, want 4", res.EditedCount)
	}
	for i := 4; i < 10; i++ {
		if out[i] != texts[i] {
			t.Fatalf("recent block %d was modified", i)
		}
	}
	if !strings.Contains(out[0], "已省略") {
		t.Fatalf("default policy should use head/tail mode: %q", out[0])
	}
}

func TestSummarize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"url", "scan done\nsee https://example.com/path?q=1 for details", "https://example.com/path?q=1"},
		{"command", "$ nmap -sV 10.0.0.1\nopen ports", "$ nmap -sV 10.0.0.1"},
		{"command-gt", "> curl http://10.0.0.1/\nok", "> curl http://10.0.0.1/"},
		{"status", "HTTP/1.1 403 Forbidden\nbody", "HTTP/1.1 403"},
		{"status2", "HTTP/2 200 OK", "HTTP/2 200"},
		{"plain", "hello world\nsecond line", "hello world"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Summarize(tc.in, 200)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("Summarize(%q) = %q, want substring %q", tc.in, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("not valid UTF-8: %q", got)
			}
		})
	}

	t.Run("order", func(t *testing.T) {
		in := "BEGIN LINE\n$ nmap -sV 10.0.0.1\nHTTP/1.1 500 Internal\nhttps://example.com/x"
		got := Summarize(in, 500)
		url := strings.Index(got, "https://example.com/x")
		cmd := strings.Index(got, "$ nmap")
		status := strings.Index(got, "HTTP/1.1 500")
		first := strings.Index(got, "BEGIN LINE")
		if url < 0 || cmd < 0 || status < 0 || first < 0 {
			t.Fatalf("missing parts: %q", got)
		}
		if !(url < cmd && cmd < status && status < first) {
			t.Fatalf("wrong order (url=%d cmd=%d status=%d first=%d): %q", url, cmd, status, first, got)
		}
	})

	t.Run("limit", func(t *testing.T) {
		got := Summarize(strings.Repeat("中文 ", 1000), 30)
		if got == "" {
			t.Fatal("empty result")
		}
		if len(got) > 30 {
			t.Fatalf("len = %d, want <= 30", len(got))
		}
		if !utf8.ValidString(got) {
			t.Fatalf("not valid UTF-8: %q", got)
		}
	})

	t.Run("item-cap", func(t *testing.T) {
		in := "https://example.com/" + strings.Repeat("a", 500)
		got := Summarize(in, 1000)
		if len(got) > summaryItemMaxBytes {
			t.Fatalf("len = %d, want <= %d", len(got), summaryItemMaxBytes)
		}
	})

	t.Run("empty", func(t *testing.T) {
		got := Summarize("", 100)
		if got == "" {
			t.Fatal("Summarize(\"\") must not be empty")
		}
		if !strings.Contains(got, "bytes") {
			t.Fatalf("fallback should mention bytes: %q", got)
		}
	})

	t.Run("whitespace-fallback", func(t *testing.T) {
		got := Summarize("   \n\t\n", 100)
		if got == "" || !strings.Contains(got, "bytes") {
			t.Fatalf("unexpected fallback: %q", got)
		}
	})

	t.Run("tiny-limit", func(t *testing.T) {
		got := Summarize("中文", 1)
		if got == "" {
			t.Fatal("must not be empty")
		}
		if !utf8.ValidString(got) {
			t.Fatalf("not valid UTF-8: %q", got)
		}
	})
}

func TestLooksLikeBigBlob(t *testing.T) {
	blob := strings.Repeat("line\n", 100) // 500 字节，100 行
	if !LooksLikeBigBlob(blob, 100, 50) {
		t.Fatal("expected big blob")
	}
	if LooksLikeBigBlob("short", 100, 50) {
		t.Fatal("short text must not be a big blob")
	}
	if LooksLikeBigBlob(blob, len(blob), 50) {
		t.Fatal("size is exclusive boundary, want false")
	}
	if LooksLikeBigBlob(blob, 100, 100) {
		t.Fatal("lines is exclusive boundary, want false")
	}
	if LooksLikeBigBlob("", 0, 0) {
		t.Fatal("empty text must not be a big blob")
	}
	if !LooksLikeBigBlob("a\nb", -1, -1) {
		t.Fatal("negative thresholds should behave like zero, want true")
	}
}

// TestDemo200KB 演示真实规模下的压缩比：44 条 tool_result，总计约 200KB。
func TestDemo200KB(t *testing.T) {
	const blocks = 44
	texts := make([]string, blocks)
	for i := range texts {
		var b strings.Builder
		fmt.Fprintf(&b, "$ nmap -sV -p- 10.0.%d.%d\n", i/256, i%256)
		fmt.Fprintf(&b, "Starting Nmap 7.94 ( https://nmap.org )\n")
		fmt.Fprintf(&b, "Nmap scan report for 10.0.%d.%d (https://example.com/host/%d)\n", i/256, i%256, i)
		for j := 0; j < 43; j++ {
			fmt.Fprintf(&b, "%d/tcp open service-%d syn-ack ttl 64 banner=%s\n",
				j, j, strings.Repeat("x", 55))
		}
		fmt.Fprintf(&b, "HTTP/1.1 401 Unauthorized at https://10.0.%d.%d/admin\n", i/256, i%256)
		texts[i] = b.String()
	}

	before := sumBytes(texts)
	_, res := Edit(texts, DefaultPolicy())
	ratio := float64(res.AfterBytes) / float64(res.BeforeBytes) * 100
	t.Logf("blocks=%d before=%d bytes after=%d bytes ratio=%.2f%% edited=%d",
		blocks, res.BeforeBytes, res.AfterBytes, ratio, res.EditedCount)

	if before <= 120_000 {
		t.Fatalf("demo input too small: %d bytes", before)
	}
	if !res.Changed || res.EditedCount != blocks-defaultKeepRecent {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.AfterBytes >= res.BeforeBytes {
		t.Fatalf("AfterBytes = %d, want < %d", res.AfterBytes, res.BeforeBytes)
	}
}
