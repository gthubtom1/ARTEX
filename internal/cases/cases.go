// Package cases persists positive and negative pentest case cards as Markdown
// files. Writes are content-addressed (sha256 prefix) so re-collecting the same
// evidence never duplicates a card.
package cases

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Card is one durable case note.
type Card struct {
	ID         string
	Title      string
	Target     string
	VulnClass  string
	ProofLevel string
	Confidence string
	Summary    string
	Evidence   string
	Negative   bool
	Tags       []string
}

// Store writes cards into Dir.
type Store struct {
	Dir string
}

type canonicalCard struct {
	Title      string   `json:"title"`
	Target     string   `json:"target"`
	VulnClass  string   `json:"vuln_class"`
	ProofLevel string   `json:"proof_level"`
	Confidence string   `json:"confidence"`
	Summary    string   `json:"summary"`
	Evidence   string   `json:"evidence"`
	Negative   bool     `json:"negative"`
	Tags       []string `json:"tags"`
}

// Write persists card and returns the case file path. created=false means an
// identical card (same content key) already existed and was left untouched.
func (s Store) Write(card Card) (path string, created bool, err error) {
	dir := strings.TrimSpace(s.Dir)
	if dir == "" {
		return "", false, fmt.Errorf("cases: empty Dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, fmt.Errorf("cases: mkdir: %w", err)
	}

	tags := append([]string(nil), card.Tags...)
	sort.Strings(tags)
	key := contentKey(card, tags)

	// Dedupe across dates as well: the same evidence collected tomorrow is still
	// the same case card.
	matches, _ := filepath.Glob(filepath.Join(dir, "*-"+key+".md"))
	if len(matches) > 0 {
		sort.Strings(matches)
		return matches[0], false, nil
	}

	now := time.Now().UTC()
	path = filepath.Join(dir, now.Format("2006-01-02")+"-"+key+".md")
	body := render(card, tags, key, now)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return path, false, nil
		}
		return "", false, fmt.Errorf("cases: create: %w", err)
	}
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		return "", false, fmt.Errorf("cases: write: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", false, fmt.Errorf("cases: close: %w", err)
	}
	return path, true, nil
}

// List returns up to limit case file names, newest first. Filenames (not full
// paths) are returned so the API can expose a stable, portable listing.
func (s Store) List(limit int) ([]string, error) {
	dir := strings.TrimSpace(s.Dir)
	if dir == "" {
		return []string{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("cases: list: %w", err)
	}
	type item struct {
		name    string
		updated time.Time
		mod     time.Time
	}
	items := make([]item, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, item{name: e.Name(), updated: frontMatterUpdated(full), mod: info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].updated, items[j].updated
		if a.IsZero() {
			a = items[i].mod
		}
		if b.IsZero() {
			b = items[j].mod
		}
		if !a.Equal(b) {
			return a.After(b)
		}
		return items[i].name > items[j].name
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.name)
	}
	return out, nil
}

func contentKey(card Card, tags []string) string {
	payload, _ := json.Marshal(canonicalCard{
		Title:      strings.TrimSpace(card.Title),
		Target:     strings.TrimSpace(card.Target),
		VulnClass:  strings.TrimSpace(card.VulnClass),
		ProofLevel: strings.TrimSpace(card.ProofLevel),
		Confidence: strings.TrimSpace(card.Confidence),
		Summary:    strings.TrimSpace(card.Summary),
		Evidence:   strings.TrimSpace(card.Evidence),
		Negative:   card.Negative,
		Tags:       tags,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])[:12]
}

func render(card Card, tags []string, key string, now time.Time) string {
	id := strings.TrimSpace(card.ID)
	if id == "" {
		id = key
	}
	kind := "positive"
	if card.Negative {
		kind = "negative"
	}
	title := strings.TrimSpace(card.Title)
	if title == "" {
		title = "未命名案例"
	}
	summary := strings.TrimSpace(card.Summary)
	if summary == "" {
		summary = "（未注明）"
	}
	evidence := strings.TrimSpace(card.Evidence)
	if evidence == "" {
		evidence = "（未注明）"
	}
	tagLine := make([]string, 0, len(tags))
	for _, tag := range tags {
		tagLine = append(tagLine, strconv.Quote(strings.TrimSpace(tag)))
	}

	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %s\n", yamlScalar(id))
	fmt.Fprintf(&b, "type: %s\n", kind)
	fmt.Fprintf(&b, "negative: %t\n", card.Negative)
	fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(tagLine, ", "))
	fmt.Fprintf(&b, "updated: %s\n", now.Format(time.RFC3339Nano))
	fmt.Fprintf(&b, "target: %s\n", yamlScalar(strings.TrimSpace(card.Target)))
	fmt.Fprintf(&b, "vuln_class: %s\n", yamlScalar(strings.TrimSpace(card.VulnClass)))
	fmt.Fprintf(&b, "proof_level: %s\n", yamlScalar(strings.TrimSpace(card.ProofLevel)))
	fmt.Fprintf(&b, "confidence: %s\n", yamlScalar(strings.TrimSpace(card.Confidence)))
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", title)
	b.WriteString("## 摘要\n\n")
	b.WriteString(summary)
	b.WriteString("\n\n## 证据\n\n")
	b.WriteString(evidence)
	b.WriteString("\n\n## 负结论\n\n")
	if card.Negative {
		b.WriteString("该方向**未成立/不可利用**，作为负案例保留，避免后续重复投入：\n\n")
		b.WriteString(summary)
		b.WriteString("\n")
	} else {
		b.WriteString("非负案例：该结论为正向发现（或已成立的利用路径）。\n")
	}
	return b.String()
}

func yamlScalar(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ":#[]{}\n\r\t\"'") || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "?") {
		return strconv.Quote(s)
	}
	return s
}

func frontMatterUpdated(path string) time.Time {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	buf := make([]byte, 2048)
	n, _ := f.Read(buf)
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "updated:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "updated:"))
			v = strings.Trim(v, `"'`)
			if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return ts
			}
		}
	}
	return time.Time{}
}

// Hit 是一条案例命中。
type Hit struct {
	Path    string `json:"path"`
	Title   string `json:"title,omitempty"`
	Snippet string `json:"snippet"`
	Score   int    `json:"score"`
}

// Search 在案例目录里按文件名/正文关键词检索（只用小写子串，足够快且可解释）。
// 供 planner 在派新方向前"查旧账"：同指纹的负案例能直接省掉一次死路。
func Search(dir, query string, limit int) ([]Hit, error) {
	if limit <= 0 {
		limit = 3
	}
	q := strings.ToLower(strings.TrimSpace(query))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	hits := make([]Hit, 0, limit)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		if len(hits) >= limit && q == "" {
			break
		}
		b, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil || len(b) > 256*1024 {
			continue
		}
		text := string(b)
		score := 0
		if q != "" && strings.Contains(strings.ToLower(e.Name()), q) {
			score += 5
		}
		snippet := ""
		if q != "" {
			if i := strings.Index(strings.ToLower(text), q); i >= 0 {
				start := i - 60
				if start < 0 {
					start = 0
				}
				end := i + 140
				if end > len(text) {
					end = len(text)
				}
				snippet = strings.Join(strings.Fields(text[start:end]), " ")
				score += 3
			}
		} else {
			snippet = firstLine(text)
			score = 1
		}
		if snippet == "" {
			continue
		}
		hits = append(hits, Hit{Path: e.Name(), Title: caseTitle(text), Snippet: snippet, Score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func caseTitle(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
		if strings.HasPrefix(line, "title:") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "title:")), `"'`)
		}
	}
	return ""
}

func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "---") {
			return line
		}
	}
	return ""
}
