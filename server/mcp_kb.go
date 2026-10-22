package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	artexkb "github.com/Autumn-27/artex/internal/kb"
)

// 本文件提供最小可用的 MCP 端点（JSON-RPC 2.0 over HTTP），把知识库暴露给外部 agent：
//
//	POST /mcp/kb   initialize / tools/list / tools/call
//	GET  /mcp/kb   能力说明（人类可读）
//
// 只读：仅 kb_search / kb_read 两个工具，不执行任何命令。

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

func (s *Server) registerMCPRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /mcp/kb", s.mcpKB)
	mux.HandleFunc("GET /mcp/kb", s.mcpKBInfo)
}

func (s *Server) mcpKBInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"server":    "artex-kb",
		"transport": "json-rpc-2.0 over HTTP POST",
		"tools": []map[string]string{
			{"name": "kb_search", "description": "在知识库中按文件名/正文搜索"},
			{"name": "kb_read", "description": "读取知识库中的一个相对路径"},
		},
	})
}

func (s *Server) kbRoot() string {
	if s.settings != nil {
		if v := strings.TrimSpace(s.settings.String("kb.path", "kb")); v != "" {
			return v
		}
	}
	return "kb"
}

func (s *Server) mcpKB(w http.ResponseWriter, r *http.Request) {
	var req mcpRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, mcpResponse{JSONRPC: "2.0", Error: &mcpError{Code: -32700, Message: "parse error"}})
		return
	}
	reply := mcpResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		reply.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "artex-kb", "version": "1"},
		}
	case "notifications/initialized", "initialized":
		w.WriteHeader(http.StatusNoContent)
		return
	case "tools/list":
		reply.Result = map[string]any{"tools": []map[string]any{
			{
				"name":        "kb_search",
				"description": "在 ARTEX 知识库（kb/，含战术/能力清单/指纹/案例）中按关键词搜索。",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string", "description": "关键词（中文/英文均可）"},
						"limit": map[string]any{"type": "integer", "description": "最多返回条数（默认 10）"},
					},
					"required": []string{"query"},
				},
			},
			{
				"name":        "kb_read",
				"description": "读取知识库中的一个相对路径（如 tactics/sqli.md）。",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string", "description": "相对 kb/ 的路径"},
					},
					"required": []string{"path"},
				},
			},
		}}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			reply.Error = &mcpError{Code: -32602, Message: "invalid params"}
			break
		}
		text, err := s.mcpKBCall(r, p.Name, p.Arguments)
		if err != nil {
			reply.Error = &mcpError{Code: -32000, Message: err.Error()}
			break
		}
		reply.Result = map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": false,
		}
	default:
		reply.Error = &mcpError{Code: -32601, Message: "method not found: " + req.Method}
	}
	writeJSON(w, http.StatusOK, reply)
}

func (s *Server) mcpKBCall(r *http.Request, name string, args json.RawMessage) (string, error) {
	switch name {
	case "kb_search":
		var a struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		_ = json.Unmarshal(args, &a)
		if a.Limit <= 0 {
			a.Limit = 10
		}
		hits, err := artexkb.Search(s.kbRoot(), a.Query, artexkb.Options{Limit: a.Limit})
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(map[string]any{"query": a.Query, "count": len(hits), "hits": hits})
		return string(b), nil
	case "kb_read":
		var a struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(args, &a)
		return artexkb.Read(s.kbRoot(), a.Path, 256*1024)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}
