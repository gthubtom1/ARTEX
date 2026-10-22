package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/internal/settings"
)

func mcpTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tactics"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tactics", "sqli.md"), []byte("# SQL 注入\n布尔盲注 payload: ' and 1=1 -- -\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := settings.New(nil)
	if err := st.Set(context.Background(), "kb.path", dir); err != nil {
		t.Fatal(err)
	}
	return &Server{settings: st}
}

func doMCP(t *testing.T, s *Server, body string) mcpResponse {
	t.Helper()
	req := httptest.NewRequest("POST", "/mcp/kb", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.mcpKB(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var resp mcpResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	return resp
}

func TestMCPInitializeAndList(t *testing.T) {
	s := mcpTestServer(t)
	resp := doMCP(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}
	m := resp.Result.(map[string]any)
	if m["protocolVersion"] == "" || m["serverInfo"] == nil {
		t.Fatalf("initialize result: %+v", m)
	}
	resp = doMCP(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := resp.Result.(map[string]any)["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("want 2 tools, got %d", len(tools))
	}
}

func TestMCPToolCallSearchAndRead(t *testing.T) {
	s := mcpTestServer(t)
	resp := doMCP(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"kb_search","arguments":{"query":"盲注"}}}`)
	if resp.Error != nil {
		t.Fatalf("search error: %+v", resp.Error)
	}
	res := resp.Result.(map[string]any)
	content := res["content"].([]any)[0].(map[string]any)
	text := content["text"].(string)
	if !strings.Contains(text, "tactics/sqli.md") {
		t.Fatalf("search text: %s", text)
	}

	resp = doMCP(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"kb_read","arguments":{"path":"tactics/sqli.md"}}}`)
	text = resp.Result.(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "布尔盲注") {
		t.Fatalf("read text: %s", text)
	}
}

func TestMCPRejectsTraversalAndUnknown(t *testing.T) {
	s := mcpTestServer(t)
	resp := doMCP(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"kb_read","arguments":{"path":"../../etc/passwd"}}}`)
	if resp.Error == nil {
		t.Fatal("traversal must error")
	}
	resp = doMCP(t, s, `{"jsonrpc":"2.0","id":6,"method":"nope"}`)
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("unknown method: %+v", resp.Error)
	}
}
