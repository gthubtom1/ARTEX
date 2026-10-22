package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/cases"
)

// registerCaseRoutes wires the case-card collection/listing endpoints.
func (s *Server) registerCaseRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/cases/collect", s.collectTaskCases)
	mux.HandleFunc("GET /api/cases", s.listCaseFiles)
}

var negativeCaseMarkers = []string{"无差异", "未成立", "不成立", "不可利用", "closed", "失败"}

// collectTaskCases distills a task's exploration graph into durable case cards:
// findings become positive cases; facts whose payload records a dead end become
// negative cases. At most 10 of each kind are collected per call.
func (s *Server) collectTaskCases(w http.ResponseWriter, r *http.Request) {
	rawTask := strings.TrimSpace(r.URL.Query().Get("task"))
	taskID, err := strconv.ParseInt(rawTask, 10, 64)
	if err != nil || taskID <= 0 {
		writeErr(w, http.StatusBadRequest, "task 必须是正整数任务 ID")
		return
	}
	res, err := s.collectCases(r.Context(), taskID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// collectCases 是案例沉淀的核心（handler 与任务完成钩子共用）。
func (s *Server) collectCases(ctx context.Context, taskID int64) (map[string]any, error) {
	if s.m == nil || s.m.pg == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	task, err := s.m.pg.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("任务不存在")
	}
	store := s.m.pg.Exploration(task.ExplorationID)
	findings, err := store.ListByKind(db.KindFinding, 60)
	if err != nil {
		return nil, err
	}
	facts, err := store.ListByKind(db.KindFact, 300)
	if err != nil {
		return nil, err
	}

	dir := s.casesDir()
	cs := cases.Store{Dir: dir}
	files := make([]string, 0, 20)
	positive, negative, created, deduped := 0, 0, 0, 0
	var writeErrs []string

	writeCard := func(card cases.Card) {
		path, isNew, err := cs.Write(card)
		if err != nil {
			writeErrs = append(writeErrs, err.Error())
			return
		}
		files = append(files, filepath.Base(path))
		if isNew {
			created++
		} else {
			deduped++
		}
	}

	for _, n := range findings {
		if positive >= 10 {
			break
		}
		writeCard(nodeCaseCard(taskID, n, false))
		positive++
	}
	for _, n := range facts {
		if negative >= 10 {
			break
		}
		if !payloadHasNegativeMarker(n.Payload) {
			continue
		}
		writeCard(nodeCaseCard(taskID, n, true))
		negative++
	}

	resp := map[string]any{
		"task":      taskID,
		"dir":       dir,
		"positive":  positive,
		"negative":  negative,
		"collected": positive + negative,
		"created":   created,
		"deduped":   deduped,
		"files":     dedupeStrings(files),
	}
	if len(writeErrs) > 0 {
		resp["errors"] = writeErrs
	}
	return resp, nil
}

// listCaseFiles returns the most recent case-card filenames.
func (s *Server) listCaseFiles(w http.ResponseWriter, r *http.Request) {
	limit := atoiDefault(r.URL.Query().Get("limit"), 20)
	if limit < 1 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	dir := s.casesDir()
	files, err := (cases.Store{Dir: dir}).List(limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if files == nil {
		files = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "count": len(files), "dir": dir})
}

func (s *Server) casesDir() string {
	root := "kb"
	if s.settings != nil {
		if v := strings.TrimSpace(s.settings.String("kb.path", "kb")); v != "" {
			root = v
		}
	}
	return filepath.Join(root, "cases")
}

func payloadHasNegativeMarker(raw json.RawMessage) bool {
	text := strings.ToLower(string(raw))
	for _, marker := range negativeCaseMarkers {
		if strings.Contains(text, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func nodeCaseCard(taskID int64, n *db.Node, negative bool) cases.Card {
	var payload map[string]any
	_ = json.Unmarshal(n.Payload, &payload)

	title := payloadText(payload, "name", "title", "summary", "text", "goal")
	summary := payloadText(payload, "summary", "text", "description", "conclusion", "finding")
	if summary == "" {
		summary = title
	}
	if title == "" {
		title = fmt.Sprintf("%s #%d", n.Kind, n.ID)
	}
	evidence := payloadText(payload, "evidence", "proof", "detail", "output", "result", "response")
	if evidence == "" {
		evidence = truncateText(string(n.Payload), 4000)
	}
	vulnClass := payloadText(payload, "vulnclass", "vuln_class", "vulnerability", "class")
	if negative && vulnClass == "" {
		vulnClass = "negative"
	}
	tags := payloadTags(payload, "tags", "labels")
	tags = append(tags, "case", n.Kind)
	if negative {
		tags = append(tags, "negative")
	} else {
		tags = append(tags, "positive")
	}
	if vulnClass != "" {
		tags = append(tags, vulnClass)
	}
	if severity := payloadText(payload, "severity"); severity != "" {
		tags = append(tags, "severity:"+severity)
	}

	return cases.Card{
		ID:         fmt.Sprintf("task-%d-%s-%d", taskID, n.Kind, n.ID),
		Title:      title,
		Target:     payloadText(payload, "target", "host", "url", "uri", "ip", "domain", "asset", "endpoint"),
		VulnClass:  vulnClass,
		ProofLevel: payloadText(payload, "proof_level", "proof", "level"),
		Confidence: payloadText(payload, "confidence", "certainty"),
		Summary:    summary,
		Evidence:   evidence,
		Negative:   negative,
		Tags:       dedupeStrings(tags),
	}
}

func payloadText(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		v, ok := payload[key]
		if !ok {
			continue
		}
		if s := valueText(v); s != "" {
			return s
		}
	}
	return ""
}

func valueText(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			if s := valueText(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		for _, key := range []string{"host", "target", "url", "uri", "name", "value", "ip", "domain"} {
			if s := valueText(x[key]); s != "" {
				return s
			}
		}
		raw, _ := json.Marshal(x)
		return string(raw)
	case nil:
		return ""
	default:
		raw, _ := json.Marshal(x)
		return string(raw)
	}
}

func payloadTags(payload map[string]any, keys ...string) []string {
	for _, key := range keys {
		switch v := payload[key].(type) {
		case []any:
			out := make([]string, 0, len(v))
			for _, item := range v {
				if s := valueText(item); s != "" {
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				return out
			}
		case string:
			out := make([]string, 0, 4)
			for _, part := range strings.Split(v, ",") {
				if part = strings.TrimSpace(part); part != "" {
					out = append(out, part)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func truncateText(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= maxRunes {
		return s
	}
	return string(rs[:maxRunes]) + "…"
}
