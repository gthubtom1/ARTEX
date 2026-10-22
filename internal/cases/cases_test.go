package cases

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreWriteCreatesMarkdownCard(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	card := Card{
		ID:         "case-1",
		Title:      "SQL injection in login",
		Target:     "example.com",
		VulnClass:  "sqli",
		ProofLevel: "exploited",
		Confidence: "high",
		Summary:    "Boolean-based SQLi allows authentication bypass.",
		Evidence:   "GET /login?id=1' OR '1'='1 -> 200 admin dashboard",
		Tags:       []string{"web", "sqli"},
	}
	path, created, err := st.Write(card)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first write should create a card")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"id: case-1", "type: positive", "tags: [\"sqli\", \"web\"]",
		"## 摘要", "## 证据", "## 负结论", "SQL injection in login",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("card missing %q:\n%s", want, text)
		}
	}
	if _, err := time.Parse("2006-01-02", filepath.Base(path)[:10]); err != nil {
		t.Fatalf("filename has no date prefix: %s", path)
	}
}

func TestStoreWriteDedupesSameContent(t *testing.T) {
	dir := t.TempDir()
	st := Store{Dir: dir}
	card := Card{
		Title:    "Open SSH with default creds",
		Target:   "10.10.10.10",
		Summary:  "root/root works",
		Evidence: "ssh root@10.10.10.10",
		Tags:     []string{"creds", "ssh"},
	}
	first, created, err := st.Write(card)
	if err != nil || !created {
		t.Fatalf("first write: path=%s created=%v err=%v", first, created, err)
	}
	// Same content, different transient ID and tag order: still the same card.
	card.ID = "different-id"
	card.Tags = []string{"ssh", "creds"}
	second, created, err := st.Write(card)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("duplicate content must not create a second card")
	}
	if first != second {
		t.Fatalf("dedupe returned %s, want %s", second, first)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d files, want 1", len(entries))
	}
}

func TestStoreListNewestFirstAndLimit(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	var paths []string
	for i, title := range []string{"first", "second", "third"} {
		path, created, err := st.Write(Card{Title: title, Summary: title, Evidence: title})
		if err != nil || !created {
			t.Fatalf("write %d: path=%s created=%v err=%v", i, path, created, err)
		}
		paths = append(paths, path)
		time.Sleep(2 * time.Millisecond)
	}
	got, err := st.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d files, want 3: %v", len(got), got)
	}
	if got[0] != filepath.Base(paths[2]) {
		t.Fatalf("newest file first, got %v (paths=%v)", got, paths)
	}
	limited, err := st.List(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 || limited[0] != filepath.Base(paths[2]) {
		t.Fatalf("limit/order wrong: %v", limited)
	}
}

func TestStoreWritesNegativeCaseMarker(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	path, created, err := st.Write(Card{
		Title:    "SSRF via image URL",
		Target:   "example.com",
		Summary:  "无差异：内网地址与公网地址响应一致，不可利用。",
		Evidence: "curl -s 'https://example.com/fetch?url=http://127.0.0.1' -> same body",
		Negative: true,
		Tags:     []string{"ssrf"},
	})
	if err != nil || !created {
		t.Fatalf("write: path=%s created=%v err=%v", path, created, err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"type: negative", "negative: true", "## 负结论", "未成立/不可利用"} {
		if !strings.Contains(text, want) {
			t.Fatalf("negative card missing %q:\n%s", want, text)
		}
	}
}

func TestStoreListMissingDirEmpty(t *testing.T) {
	st := Store{Dir: filepath.Join(t.TempDir(), "missing")}
	got, err := st.List(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

func TestSearchFindsNegativeCase(t *testing.T) {
	dir := t.TempDir()
	cs := Store{Dir: dir}
	if _, _, err := cs.Write(Card{Title: "登录页注入未成立", Target: "a.example.com", VulnClass: "sqli", Negative: true,
		Summary: "6 种 payload 无差异", Evidence: "布尔/时间/报错均无差异", Tags: []string{"sqli"}}); err != nil {
		t.Fatal(err)
	}
	hits, err := Search(dir, "sqli", 3)
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%+v err=%v", hits, err)
	}
	if !strings.Contains(hits[0].Snippet, "无差异") && !strings.Contains(hits[0].Title, "注入") {
		t.Fatalf("snippet: %+v", hits[0])
	}
	if hits, _ := Search(dir, "不存在的关键词", 3); len(hits) != 0 {
		t.Fatalf("unexpected hits: %+v", hits)
	}
}
