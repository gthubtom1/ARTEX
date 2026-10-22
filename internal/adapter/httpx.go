package adapter

import (
	"encoding/json"
	"fmt"
	"strings"
)

const httpxTool = "httpx"

// httpxParser 解析 `httpx -json` 的 JSONL 输出。
type httpxParser struct{}

// HttpxJSONL 返回 httpx JSONL 解析器。
func HttpxJSONL() Parser { return httpxParser{} }

func (httpxParser) Name() string { return httpxTool }

type httpxRecord struct {
	URL           string          `json:"url"`
	Status        int             `json:"status"`
	Title         string          `json:"title"`
	Tech          json.RawMessage `json:"tech"`
	CDN           json.RawMessage `json:"cdn"`
	Webserver     string          `json:"webserver"`
	ContentLength int             `json:"content_length"`
}

func (httpxParser) Parse(stdout, stderr string) (Result, error) {
	res := Result{Tool: httpxTool, OK: false, Items: []Item{}}
	if err := checkSize(httpxTool, stdout); err != nil {
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

		var rec httpxRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			// CLI 偶发脏行（进度条、截断、日志混入）：跳过，不算失败。
			continue
		}
		res.Items = append(res.Items, Item{
			Kind: "web",
			Data: map[string]any{
				"url":            rec.URL,
				"status":         rec.Status,
				"title":          rec.Title,
				"tech":           parseStringList(rec.Tech),
				"cdn":            parseCDNBool(rec.CDN),
				"webserver":      rec.Webserver,
				"content_length": rec.ContentLength,
			},
		})
	}

	if total > 0 && len(res.Items) == 0 {
		err := fmt.Errorf("%s: %d 行非空输出均不是合法 JSON", httpxTool, total)
		res.Err = err.Error()
		return res, err
	}
	res.OK = true
	return res, nil
}

// normalizeLine 去掉行尾 \r、首部 BOM 和首尾空白。
func normalizeLine(raw string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(raw, "\r"), "\ufeff"))
}

// parseStringList 兼容 `["a","b"]`、`"a"` 和缺省值。
func parseStringList(raw json.RawMessage) []string {
	out := []string{}
	if len(raw) == 0 {
		return out
	}
	if string(raw) == "null" {
		return out
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		if list == nil {
			return out
		}
		return list
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if single != "" {
			out = append(out, single)
		}
		return out
	}
	return out
}

// parseCDNBool 兼容 `true`、`"cloudflare"`、`"false"` 等写法。
func parseCDNBool(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.ToLower(strings.TrimSpace(s))
		return s != "" && s != "false" && s != "0" && s != "no"
	}
	return false
}
