package server

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/internal/adviser"
	"github.com/Autumn-27/artex/internal/stall"
)

// 本文件是「LLM 版顾问」：用独立 agentKey=adviser 的模型配置（可在 Agent 页绑强模型），
// 把停顿诊断包交给模型，输出结构化纠偏；失败/超时/输出不可解析时自动回落到确定性规则。
//
// 成本控制：每个任务默认 10 分钟最多一次 LLM 顾问调用，且只在检测器命中时触发。

const adviserSystemPrompt = `你是这次渗透任务的"资深负责人"。你不是规划者，也不是执行者：
在团队跑偏时把它拉回来，只说能立刻执行的话。绝不自己派意图、绝不自己跑命令。
每条 hint 必须带具体资产/端口/接口/参数 + 为什么 + 做到什么算成功；没有真问题就输出 0 条 hint。
严格输出 JSON：{"diagnosis":"","evidence":[],"pivot":{"from":"","to":"","why":""},"hints":[],"steer":[{"intent_id":0,"message":""}],"kill":[],"confidence":"high|medium|low"}`

type adviserGate struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (g *adviserGate) allow(taskID string, every time.Duration) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.last == nil {
		g.last = map[string]time.Time{}
	}
	if t, ok := g.last[taskID]; ok && time.Since(t) < every {
		return false
	}
	g.last[taskID] = time.Now()
	return true
}

// tryLLMAdviser 调一次强模型顾问；返回 (诊断, 是否成功)。
func (s *Server) tryLLMAdviser(ctx context.Context, t *Task, rep stall.Report,
	led adviser.Ledger, neg adviser.NegativeSpace) (adviser.Diagnosis, bool) {
	if s.m == nil || s.m.PG() == nil {
		return adviser.Diagnosis{}, false
	}
	if !s.adviserCooldown.allow(t.ID, 10*time.Minute) {
		return adviser.Diagnosis{}, false
	}
	rt := &taskLLMRuntime{s: s, taskID: t.ID, agentKey: "adviser"}
	c, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	sys := s.agentPromptOrConst("adviser", adviserSystemPrompt)
	raw, err := streamCollectText(c, rt, sys, adviser.Prompt(t.Goal, rep, led, neg))
	if err != nil {
		return adviser.Diagnosis{}, false
	}
	d, err := adviser.ParseJSON(raw)
	if err != nil {
		return adviser.Diagnosis{}, false
	}
	if len(d.Hints) == 0 && len(d.Steer) == 0 && len(d.Kill) == 0 {
		return adviser.Diagnosis{}, false // 模型认为没事 → 交给规则判断
	}
	if strings.TrimSpace(d.Diagnosis) == "" {
		d.Diagnosis = "模型诊断（无摘要）"
	}
	return d, true
}
