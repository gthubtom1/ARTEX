package kb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seed(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("tactics/sqli.md", "---\ntitle: SQL 注入\n---\n\n# SQL 注入\n\n布尔盲注：' and 1=1 -- -\n时间盲注：sleep(5)\n")
	must("capabilities/taxonomy.md", "# 能力清单\n\n组 1 数据：数据库结构+规模\n")
	must("inbox/skip.md", "secret token")
	return dir
}

func TestSearchFindsBodyAndName(t *testing.T) {
	dir := seed(t)
	hits, err := Search(dir, "盲注", Options{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Path != "tactics/sqli.md" {
		t.Fatalf("body search: %+v", hits)
	}
	if hits[0].Title != "SQL 注入" || !strings.Contains(hits[0].Snippet, "盲注") {
		t.Fatalf("hit detail: %+v", hits[0])
	}
	hits, _ = Search(dir, "taxonomy", Options{Limit: 5})
	if len(hits) != 1 || hits[0].Path != "capabilities/taxonomy.md" {
		t.Fatalf("name search: %+v", hits)
	}
}

func TestSearchSkipsInbox(t *testing.T) {
	dir := seed(t)
	hits, _ := Search(dir, "secret", Options{Limit: 5})
	if len(hits) != 0 {
		t.Fatalf("inbox must be skipped: %+v", hits)
	}
}

func TestEmptyQueryListsFiles(t *testing.T) {
	dir := seed(t)
	hits, _ := Search(dir, "", Options{Limit: 10})
	if len(hits) != 2 {
		t.Fatalf("want 2 md files, got %+v", hits)
	}
}

func TestReadBlocksTraversal(t *testing.T) {
	dir := seed(t)
	if _, err := Read(dir, "../etc/passwd", 1024); err == nil {
		t.Fatal("traversal must be rejected")
	}
	if _, err := Read(dir, "/etc/passwd", 1024); err == nil {
		t.Fatal("absolute path must be rejected")
	}
	s, err := Read(dir, "tactics/sqli.md", 1024)
	if err != nil || !strings.Contains(s, "布尔盲注") {
		t.Fatalf("read: %q %v", s, err)
	}
}

func TestSearchLimit(t *testing.T) {
	dir := seed(t)
	hits, _ := Search(dir, "", Options{Limit: 1})
	if len(hits) != 1 {
		t.Fatalf("limit not applied: %d", len(hits))
	}
}
