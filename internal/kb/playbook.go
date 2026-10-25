// Package kb 的"打法"层：把 kb/tactics/*.md 解析成结构化 playbook，供 agent 工具按形状检索。
//
// 为什么要结构化（2026-10-25 外部审查 P0-02，docs/PLANS/2026-10-25-artex-找洞能力审查.md）：
// kb/tactics 里有 6 篇 428 行"怎么验一个洞"的打法（判定标准 / 不算命中 / 检查顺序不许跳级），
// 但当时**零接线** —— 只是给人看的散文，worker 从来读不到。模型靠自由发挥，找洞慢且容易漏步。
//
// 这个文件只做三件事：读文件、解析三段（判定标准 / 检查顺序 / 最小验证）、按形状匹配。
// 不调模型、不写库、同输入同输出。
package kb

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Playbook 是一篇打法的结构化形态。
type Playbook struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Tags     []string `json:"tags"`
	Criteria []string `json:"criteria"` // 算命中的判据（bullet）
	NotHit   []string `json:"not_hit"`  // 不算命中（最常见的误判）
	Steps    []string `json:"steps"`    // 检查顺序（不许跳级）
	MinProof string   `json:"min_proof"`
}

// shapeKeywords 把"攻击面形状"映射到 tactic id。刻意保守：命中越多排越前。
var shapeKeywords = map[string][]string{
	"tactics/sqli": {
		"sql", "注入", "?id=", "id=", "uid=", "q=", "search=", "keyword", "order by",
		"login", "doLogin", "select", "参数", "param",
	},
	"tactics/access-control": {
		"idor", "越权", "未授权", "user_id", "userId", "account", "accountId", "order_id",
		"orderId", "uid", "api/", "/api", "枚举", "多租户", "role",
	},
	"tactics/upload-to-rce": {
		"upload", "上传", "multipart", "filename", ".php", ".jsp", "attachment", "avatar", "文件",
	},
	"tactics/data-access-proof": {
		"数据", "样本", "记录", "表", "select", "dump", "交易", "订单", "用户",
	},
	"tactics/lateral-pivot": {
		"内网", "横向", "smb", "445", "ssh", "rdp", "3389", "跳板", "隧道",
	},
	"tactics/post-exploit-foothold": {
		"shell", "rce", "命令执行", "代码执行", "webshell", "反序列化", "ssti",
	},
}

// LoadPlaybooks 读 dir/tactics/*.md 并解析。目录不存在时返回空切片（不报错：
// 运行时可能没带 kb 目录，工具该降级而不是让 worker 崩）。
func LoadPlaybooks(root string) []Playbook {
	dir := filepath.Join(root, "tactics")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]Playbook, 0, len(entries))
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		pb := parsePlaybook(string(b))
		if pb.ID == "" {
			pb.ID = "tactics/" + strings.TrimSuffix(n, ".md")
		}
		out = append(out, pb)
	}
	return out
}

// parsePlaybook 从 md 文本里抽 frontmatter(id/tags) + 三个段落。
func parsePlaybook(md string) Playbook {
	var pb Playbook
	lines := strings.Split(md, "\n")
	inFront := false
	section := ""
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			if !inFront && pb.ID == "" && len(pb.Tags) == 0 {
				inFront = true
				continue
			}
			if inFront {
				inFront = false
			}
			continue
		}
		if inFront {
			k, v, ok := strings.Cut(trimmed, ":")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			switch k {
			case "id":
				pb.ID = v
			case "tags":
				v = strings.Trim(v, "[]")
				for _, t := range strings.Split(v, ",") {
					if t = strings.TrimSpace(t); t != "" {
						pb.Tags = append(pb.Tags, t)
					}
				}
			}
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			if pb.Title == "" {
				pb.Title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			}
			section = ""
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			h := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			switch {
			case strings.Contains(h, "判定标准"):
				section = "criteria"
			case strings.Contains(h, "检查顺序"):
				section = "steps"
			case strings.Contains(h, "最小验证"):
				section = "proof"
			default:
				section = ""
			}
			continue
		}
		if trimmed == "" {
			continue
		}
		switch section {
		case "criteria":
			// 只收"算命中"那一段的 bullet；遇到"不算命中"就切走
			if strings.Contains(trimmed, "不算命中") || strings.Contains(trimmed, "不算") {
				section = "not_hit"
				continue
			}
			if item := bulletText(trimmed); item != "" {
				pb.Criteria = append(pb.Criteria, item)
			}
		case "not_hit":
			if item := bulletText(trimmed); item != "" {
				pb.NotHit = append(pb.NotHit, item)
			}
		case "steps":
			if item := bulletText(trimmed); item != "" {
				pb.Steps = append(pb.Steps, item)
			}
		case "proof":
			if pb.MinProof == "" {
				pb.MinProof = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			}
		}
	}
	return pb
}

// bulletText 去掉列表符号/序号，只留正文。
func bulletText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimPrefix(s, "*")
	s = strings.TrimSpace(s)
	// "1. xxx" / "1) xxx"
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i < len(s) && (s[i] == '.' || s[i] == ')') {
		s = strings.TrimSpace(s[i+1:])
	}
	return s
}

// MatchPlaybooks 按"攻击面形状"匹配打法，最多 limit 篇（按命中词数排序，稳定）。
// 一个都没命中时返回最通用的两篇（sqli + access-control），避免"查了但没有"。
func MatchPlaybooks(shape string, pbs []Playbook, limit int) []Playbook {
	if limit <= 0 {
		limit = 3
	}
	if len(pbs) == 0 {
		return nil
	}
	low := strings.ToLower(shape)
	type scored struct {
		pb    Playbook
		score int
	}
	all := make([]scored, 0, len(pbs))
	for _, pb := range pbs {
		kws := shapeKeywords[pb.ID]
		score := 0
		for _, kw := range kws {
			if strings.Contains(low, strings.ToLower(kw)) {
				score++
			}
		}
		all = append(all, scored{pb: pb, score: score})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].pb.ID < all[j].pb.ID
	})
	out := make([]Playbook, 0, limit)
	for _, s := range all {
		if s.score <= 0 {
			continue
		}
		out = append(out, s.pb)
		if len(out) >= limit {
			return out
		}
	}
	if len(out) == 0 {
		// 没识别出形状：给两篇最通用的（注入与越权覆盖大多数入口）
		for _, s := range all {
			if s.pb.ID == "tactics/sqli" || s.pb.ID == "tactics/access-control" {
				out = append(out, s.pb)
				if len(out) >= 2 {
					break
				}
			}
		}
	}
	return out
}

// Render 把一篇打法压成给模型看的短文本（硬限长度：不许把 kb 变成上下文炸弹）。
func (p Playbook) Render(maxSteps, maxCriteria int) string {
	if maxSteps <= 0 {
		maxSteps = 8
	}
	if maxCriteria <= 0 {
		maxCriteria = 4
	}
	var b strings.Builder
	fmt.Fprintf(&b, "【%s】%s\n", p.ID, p.Title)
	if len(p.Criteria) > 0 {
		b.WriteString("算命中：\n")
		for i, c := range p.Criteria {
			if i >= maxCriteria {
				b.WriteString("- …（更多见 kb " + p.ID + "）\n")
				break
			}
			b.WriteString("- " + clip(c, 110) + "\n")
		}
	}
	if len(p.NotHit) > 0 {
		b.WriteString("不算命中（常见误判）：\n")
		for i, c := range p.NotHit {
			if i >= 3 {
				break
			}
			b.WriteString("- " + clip(c, 90) + "\n")
		}
	}
	if len(p.Steps) > 0 {
		b.WriteString("检查顺序（不许跳级）：\n")
		for i, s := range p.Steps {
			if i >= maxSteps {
				break
			}
			fmt.Fprintf(&b, "%d. %s\n", i+1, clip(s, 120))
		}
	}
	if p.MinProof != "" {
		b.WriteString("最小验证：" + clip(p.MinProof, 160) + "\n")
	}
	return b.String()
}

// clip 按"字"截断（中文按 rune 算），避免把一句话砍成半个字。
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
