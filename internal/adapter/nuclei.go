package adapter

import (
	"encoding/json"
	"fmt"
	"strings"
)

const nucleiTool = "nuclei"

// nucleiParser 解析 `nuclei -jsonl` 的 JSONL 输出。
type nucleiParser struct{}

// NucleiJSONL 返回 nuclei JSONL 解析器。
func NucleiJSONL() Parser { return nucleiParser{} }

func (nucleiParser) Name() string { return nucleiTool }

type nucleiRecord struct {
	TemplateID  string     `json:"template-id"`
	Info        nucleiInfo `json:"info"`
	MatchedAt   string     `json:"matched-at"`
	Type        string     `json:"type"`
	CurlCommand string     `json:"curl-command"`
	Host        string     `json:"host"`
}

type nucleiInfo struct {
	Name     string `json:"name"`
	Severity string `json:"severity"`
}

func (nucleiParser) Parse(stdout, stderr string) (Result, error) {
	res := Result{Tool: nucleiTool, OK: false, Items: []Item{}}
	if err := checkSize(nucleiTool, stdout); err != nil {
		res.Err = err.Error()
		return res, err
	}

	total := 0
	for _, raw := range strings.Split(stdout, "\n") {
		line := normalizeLine(raw)
		if line == "" {
			continue
		}
		total++

		var rec nucleiRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			// 跳过脏行，保持与 httpx 一致的行为。
			continue
		}
		res.Items = append(res.Items, Item{
			Kind: "finding",
			Data: map[string]any{
				"template_id": rec.TemplateID,
				"name":        rec.Info.Name,
				"severity":    strings.ToLower(strings.TrimSpace(rec.Info.Severity)),
				"matched_at":  rec.MatchedAt,
				"type":        rec.Type,
				"curl":        rec.CurlCommand,
				"host":        rec.Host,
			},
		})
	}

	if total > 0 && len(res.Items) == 0 {
		err := fmt.Errorf("%s: %d 行非空输出均不是合法 JSON", nucleiTool, total)
		res.Err = err.Error()
		return res, err
	}
	res.OK = true
	return res, nil
}
