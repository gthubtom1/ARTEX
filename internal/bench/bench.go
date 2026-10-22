// Package bench 是「内置基准跑分」的纯逻辑层：结果结构、run_key 规则、
// 汇总/环比、结果自检，以及用例 YAML 的解析与判定。
//
// 纪律：本包不 import server / agent / db，也不做任何 HTTP 或文件系统写操作。
// 跑批脚本（scripts/bench/run.py）写 bench/results/<run_key>.json，后端只读不写，
// 两边必须用**同一套 schema 与同一套判定语义**（见 bench/results/README.md）。
package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Outcome 是单个用例的终态。枚举外的值一律视为结果无效（见 Verify）。
const (
	// OutcomePassed 命中了 pass 规则。
	OutcomePassed = "passed"
	// OutcomeFailed 任务正常结束，但没有命中 pass 规则。
	OutcomeFailed = "failed"
	// OutcomeTimeout 任务/跑批超时，没拿到可用结论。
	OutcomeTimeout = "timeout"
	// OutcomeError 平台侧错误（建任务失败、5xx、解析失败等）。
	OutcomeError = "error"
)

// CaseResult 是单个用例的一次跑批结果。JSON 字段名与
// bench/results/README.md 里写死的 schema 一致，不允许改名。
type CaseResult struct {
	Key            string `json:"key"`
	Category       string `json:"category"`
	Passed         bool   `json:"passed"`
	DurationMS     int64  `json:"duration_ms"`
	FirstFindingMS int64  `json:"first_finding_ms"`
	Tokens         int64  `json:"tokens"`
	TaskID         string `json:"task_id"`
	Outcome        string `json:"outcome"` // passed|failed|timeout|error
	Note           string `json:"note"`
}

// Run 是一次完整跑批的结果。run_key 是主键，也是结果文件名。
type Run struct {
	RunKey       string       `json:"run_key"`
	SetName      string       `json:"set_name"`
	ModelProfile string       `json:"model_profile"`
	StartedAt    string       `json:"started_at"`
	Total        int          `json:"total"`
	Passed       int          `json:"passed"`
	DurationMS   int64        `json:"duration_ms"`
	TotalTokens  int64        `json:"total_tokens"`
	Cases        []CaseResult `json:"cases"`
}

// Summary 是一次跑批的汇总 + 与上一次同 set+model 的环比。
// PassRate 是 0..1 的比例；DeltaPassRate 是**百分点**（pp），DeltaDurationMS 是毫秒。
type Summary struct {
	PassRate          float64 `json:"pass_rate"`
	AvgDurationMS     int64   `json:"avg_duration_ms"`
	AvgFirstFindingMS int64   `json:"avg_first_finding_ms"`
	DeltaPassRate     float64 `json:"delta_pass_rate"`
	DeltaDurationMS   int64   `json:"delta_duration_ms"`
}

var (
	// reUnsafeSlug 是 run_key 里唯一允许的字符集。
	reUnsafeSlug = regexp.MustCompile(`[^A-Za-z0-9._-]`)
	// reDashRun 用于折叠连续 '-'。
	reDashRun = regexp.MustCompile(`-+`)
	// reRunKey 是 SafeRunKey 的白名单：必须整串只含安全字符且以 .json 结尾。
	reRunKey = regexp.MustCompile(`^[A-Za-z0-9._-]+\.json$`)
)

// slug 把任意字符串压成 run_key 允许的片段：非 [A-Za-z0-9._-] → '-'，
// ".." → '-'（保证 run_key 永远不会命中 SafeRunKey 的路径穿越黑名单），
// 连续 '-' 折叠，去首尾 '-'，再截断到 max 个字符（截断后再去一次尾部 '-'）。
func slug(s string, max int) string {
	s = strings.TrimSpace(s)
	s = reUnsafeSlug.ReplaceAllString(s, "-")
	s = strings.ReplaceAll(s, "..", "-")
	s = reDashRun.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if max > 0 && len(s) > max {
		s = strings.Trim(s[:max], "-")
	}
	return s
}

// BuildRunKey 生成 run_key：YYYYMMDD-HHMM-<set>-<modelslug>。
// model 的非法字符替换为 '-'、连续 '-' 折叠、截断到 32 字符；set 同样做净化
// （上限 24 字符）——这样 run_key 一定能通过 SafeRunKey 的字符白名单。
func BuildRunKey(now time.Time, set, model string) string {
	setSlug := slug(set, 24)
	if setSlug == "" {
		setSlug = "default"
	}
	modelSlug := slug(model, 32)
	if modelSlug == "" {
		modelSlug = "unknown"
	}
	return fmt.Sprintf("%s-%s-%s", now.Format("20060102-1504"), setSlug, modelSlug)
}

// SafeRunKey 校验结果文件名（路径穿越防护）：只接受
// ^[A-Za-z0-9._-]+\.json$，并额外拒绝任何含 ".." 的名字。
// 调用方拿到的名字可以直接 filepath.Join(resultsDir, name)。
func SafeRunKey(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("bench: run_key 为空")
	}
	if strings.Contains(name, "..") {
		return "", fmt.Errorf("bench: run_key 不允许包含 \"..\": %q", name)
	}
	if !reRunKey.MatchString(name) {
		return "", fmt.Errorf("bench: run_key 必须是 ^[A-Za-z0-9._-]+\\.json$: %q", name)
	}
	if len(name) > 128 {
		return "", fmt.Errorf("bench: run_key 过长（%d 字符）", len(name))
	}
	// 防御性：即使白名单通过，也不允许名字里带路径分隔符（windows 下 '\' 也在白名单外）。
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("bench: run_key 不允许包含路径分隔符: %q", name)
	}
	return name, nil
}

// Summarize 计算单次跑批的汇总（不含环比）。AvgFirstFindingMS 只对
// FirstFindingMS > 0 的用例求均值 —— 没出 finding 的用例不该被当成“0ms 出洞”。
func Summarize(r Run) Summary {
	var s Summary
	n := len(r.Cases)
	if r.Total > 0 {
		s.PassRate = float64(r.Passed) / float64(r.Total)
	} else if n > 0 {
		s.PassRate = float64(r.Passed) / float64(n)
	}
	if n == 0 {
		return s
	}
	var durSum int64
	var firstSum int64
	var firstN int64
	for _, c := range r.Cases {
		durSum += c.DurationMS
		if c.FirstFindingMS > 0 {
			firstSum += c.FirstFindingMS
			firstN++
		}
	}
	s.AvgDurationMS = durSum / int64(n)
	if firstN > 0 {
		s.AvgFirstFindingMS = firstSum / firstN
	}
	return s
}

// Delta 计算 cur 相对 prev 的环比：通过率差（百分点）与平均耗时差（毫秒）。
// prev 为空（Total=0 且 Cases 为空）时只返回 cur 的绝对值，Delta* = 0。
func Delta(prev, cur Run) Summary {
	s := Summarize(cur)
	if prev.Total == 0 && len(prev.Cases) == 0 {
		return s
	}
	p := Summarize(prev)
	s.DeltaPassRate = (s.PassRate - p.PassRate) * 100
	s.DeltaDurationMS = s.AvgDurationMS - p.AvgDurationMS
	return s
}

// Verify 自检一次跑批结果的结构一致性。跑批脚本落盘前、后端 ingest 前都该调一次。
func Verify(r Run) error {
	if strings.TrimSpace(r.RunKey) == "" {
		return fmt.Errorf("bench: run_key 为空")
	}
	if r.Total < 0 || r.Passed < 0 {
		return fmt.Errorf("bench: total/passed 不能为负（total=%d passed=%d）", r.Total, r.Passed)
	}
	if r.Passed > r.Total {
		return fmt.Errorf("bench: passed(%d) > total(%d)", r.Passed, r.Total)
	}
	if r.Total > 0 && len(r.Cases) != r.Total {
		return fmt.Errorf("bench: len(cases)=%d 与 total=%d 不一致", len(r.Cases), r.Total)
	}
	if len(r.Cases) > 0 && r.Passed > len(r.Cases) {
		return fmt.Errorf("bench: passed(%d) > len(cases)=%d", r.Passed, len(r.Cases))
	}
	if r.DurationMS < 0 || r.TotalTokens < 0 {
		return fmt.Errorf("bench: duration_ms/total_tokens 不能为负")
	}
	for i, c := range r.Cases {
		if strings.TrimSpace(c.Key) == "" {
			return fmt.Errorf("bench: cases[%d].key 为空", i)
		}
		switch c.Outcome {
		case OutcomePassed, OutcomeFailed, OutcomeTimeout, OutcomeError:
		default:
			return fmt.Errorf("bench: cases[%d](%s) outcome 非法: %q", i, c.Key, c.Outcome)
		}
		if c.Outcome == OutcomePassed && !c.Passed {
			return fmt.Errorf("bench: cases[%d](%s) outcome=passed 但 passed=false", i, c.Key)
		}
		if c.DurationMS < 0 || c.FirstFindingMS < 0 || c.Tokens < 0 {
			return fmt.Errorf("bench: cases[%d](%s) 时延/token 不能为负", i, c.Key)
		}
	}
	return nil
}

// Case 是一道基准题，对应 bench/cases/<key>.yaml。
type Case struct {
	Key      string   `yaml:"key"`
	Name     string   `yaml:"name"`
	Category string   `yaml:"category"`
	URL      string   `yaml:"url"`
	Goal     string   `yaml:"goal"`
	TimeoutS int      `yaml:"timeout_s"`
	Pass     PassRule `yaml:"pass"`
}

// PassRule 是判定规则。所有**非空**条件必须同时满足（AND 语义）；全空 → 永不通过。
// 这样写得越具体越难过，避免“证据写得松 → 误报算通过”。
type PassRule struct {
	FindingVulnclass     []string `yaml:"finding_vulnclass"`
	FindingSurfaceRegex  string   `yaml:"finding_surface_regex"`
	FindingEvidenceRegex string   `yaml:"finding_evidence_regex"`
}

// Finding 是判定时从平台 finding 里抽出的三要素。
type Finding struct {
	VulnClass string
	Surface   string
	Evidence  string
}

// ParseCaseYAML 解析一道题。yaml 库用仓库已有的 gopkg.in/yaml.v3。
func ParseCaseYAML(b []byte) (Case, error) {
	var c Case
	if err := yaml.Unmarshal(b, &c); err != nil {
		return Case{}, fmt.Errorf("bench: 用例 YAML 解析失败: %w", err)
	}
	c.Key = strings.TrimSpace(c.Key)
	if c.Key == "" {
		return Case{}, fmt.Errorf("bench: 用例缺少 key")
	}
	c.Category = strings.TrimSpace(c.Category)
	c.URL = strings.TrimSpace(c.URL)
	return c, nil
}

// LoadCaseFile 读一个用例文件（方便后端/脚本做一致性自检）。
func LoadCaseFile(path string) (Case, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Case{}, err
	}
	c, err := ParseCaseYAML(b)
	if err != nil {
		return Case{}, fmt.Errorf("%s: %w", path, err)
	}
	if base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)); base != c.Key {
		return Case{}, fmt.Errorf("%s: 文件名 %q 与 key %q 不一致", path, base, c.Key)
	}
	return c, nil
}

// MatchAny 返回第一个命中的 finding（没命中返回 false, nil）。
func (c Case) MatchAny(fs []Finding) (bool, *Finding) {
	for i := range fs {
		if c.Match(fs[i]) {
			return true, &fs[i]
		}
	}
	return false, nil
}

// Match 判定一条 finding 是否满足该用例的 pass 规则。
//
//   - finding_vulnclass 非空 → VulnClass 必须**大小写不敏感地包含**其中任一项；
//   - finding_surface_regex 非空 → 正则必须在 Surface 里命中；
//   - finding_evidence_regex 非空 → 正则必须在 Evidence 里命中；
//   - 所有非空条件全部满足才为 true；全空 → false（没有规则不等于通过）。
//
// 正则编译失败按“不匹配”处理（不 panic），这样坏 YAML 只会让用例失败，不会炸掉批次。
func (c Case) Match(f Finding) bool {
	rule := c.Pass
	if len(rule.FindingVulnclass) == 0 &&
		strings.TrimSpace(rule.FindingSurfaceRegex) == "" &&
		strings.TrimSpace(rule.FindingEvidenceRegex) == "" {
		return false
	}

	if len(rule.FindingVulnclass) > 0 {
		got := strings.ToLower(strings.TrimSpace(f.VulnClass))
		if got == "" {
			return false
		}
		hit := false
		for _, want := range rule.FindingVulnclass {
			want = strings.ToLower(strings.TrimSpace(want))
			if want == "" {
				continue
			}
			if strings.Contains(got, want) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}

	if p := strings.TrimSpace(rule.FindingSurfaceRegex); p != "" {
		re, err := regexp.Compile(p)
		if err != nil || !re.MatchString(f.Surface) {
			return false
		}
	}

	if p := strings.TrimSpace(rule.FindingEvidenceRegex); p != "" {
		re, err := regexp.Compile(p)
		if err != nil || !re.MatchString(f.Evidence) {
			return false
		}
	}
	return true
}
