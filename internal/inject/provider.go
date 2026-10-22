package inject

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"iter"
	"strings"
	"sync/atomic"

	"github.com/Autumn-27/norma/llm"
)

// Injector 是 llm.Provider 的装饰器，实现「按需注入（on-miss）」：
// 每次请求前检查系统提示/消息里是否还有注入指纹（Marker），
// 没有就补一段执行上下文。压缩、截断、跨会话续跑把块丢掉后会自动补回。
//
// 为什么在这里做：prompt 只在 run 开始时拼一次，中途 compaction 会把它压掉；
// 只有挂在 provider 层才能在"每次真正请求模型"时判断。
type Injector struct {
	inner    llm.Provider
	mode     Mode
	goal     func() string
	maxChars int

	injected atomic.Int64 // 补注入次数
	chars    atomic.Int64 // 累计补注入字符数
	// Custom 是用户人设/破甲词；PersonaEveryTurn=true 时每轮都带，否则只在上下文里缺失时补。
	Custom           string
	PersonaEveryTurn bool
}

// NewInjector 包装 provider；inner 为 nil 时 Stream/Complete 返回空实现。
func NewInjector(inner llm.Provider, mode Mode, goal func() string, maxChars int) *Injector {
	if maxChars <= 0 {
		maxChars = 900
	}
	return &Injector{inner: inner, mode: mode, goal: goal, maxChars: maxChars}
}

// Stats 返回（补注入次数，累计字符数），用于活动流/审计。
func (p *Injector) Stats() (int, int) {
	if p == nil {
		return 0, 0
	}
	return int(p.injected.Load()), int(p.chars.Load())
}

// Patch 返回补齐了注入块的请求（导出版本便于测试/复用）。
func (p *Injector) Patch(req llm.CompletionRequest) llm.CompletionRequest {
	if p == nil {
		return req
	}
	needCtx := p.mode.ShouldCheckRequest() && !requestHasMarker(req)
	pm := PersonaMarker(p.Custom)
	needPersona := strings.TrimSpace(p.Custom) != "" &&
		(p.PersonaEveryTurn || !requestHasText(req, pm))
	if !needCtx && !needPersona {
		return req
	}
	goal := ""
	if p.goal != nil {
		goal = p.goal()
	}
	block := ""
	if needCtx {
		block = PromptBlock(goal, p.maxChars)
	}
	if needPersona {
		if block != "" {
			block += string(rune(10)) + string(rune(10))
		}
		block += PersonaBlock(p.Custom)
	}
	if block == "" {
		return req
	}
	out := req
	out.System = append(append([]string{}, req.System...), block)
	p.injected.Add(1)
	p.chars.Add(int64(len([]rune(block))))
	return out
}

func (p *Injector) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return p.inner.Stream(ctx, p.Patch(req))
}

func (p *Injector) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	return p.inner.Complete(ctx, p.Patch(req))
}

// requestHasMarker 检查系统提示与全部消息里是否还有指纹。
// PersonaBlock 生成带指纹的人设块（原样全文，不截断）。
func PersonaBlock(custom string) string {
	custom = strings.TrimSpace(custom)
	if custom == "" {
		return ""
	}
	return PersonaMarker(custom) + string(rune(10)) + "【人设 / 执行约定（用户提供，原样生效）】" + string(rune(10)) + custom
}

// PersonaMarker 是人设指纹（用于判断当前上下文里还有没有人设）。
func PersonaMarker(custom string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(custom)))
	return "[[ARTEX-PERSONA:" + hex.EncodeToString(sum[:6]) + "]]"
}

func requestHasText(req llm.CompletionRequest, text string) bool {
	if text == "" {
		return false
	}
	for _, s := range req.System {
		if strings.Contains(s, text) {
			return true
		}
	}
	for _, m := range req.Messages {
		for _, c := range m.Content {
			if c.Type == llm.BlockText && strings.Contains(c.Text, text) {
				return true
			}
		}
	}
	return false
}

func requestHasMarker(req llm.CompletionRequest) bool {
	for _, s := range req.System {
		if HasMarker(s) {
			return true
		}
	}
	for _, m := range req.Messages {
		for _, c := range m.Content {
			if c.Type == llm.BlockText && HasMarker(c.Text) {
				return true
			}
		}
	}
	return false
}
