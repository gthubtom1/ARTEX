package agent

import (
	"encoding/json"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/relay"
	"github.com/Autumn-27/norma/harness"
)

// 本文件把 relay（三段式接力便条）接进 worker：
//   - 启动时读 MEMORY.md 注入 system prompt（跨 worker 交接）；
//   - 结束时把本次结果压成一块写回（下次从这里接着干）。
// 与 transcript 的分工：transcript 管同一 intent 续跑，MEMORY 管跨 worker/跨会话。

// intentSummaryText 从意图 payload 里取一句话摘要。
func intentSummaryText(intent *db.Node) string {
	if intent == nil {
		return ""
	}
	var p map[string]any
	if json.Unmarshal(intent.Payload, &p) == nil {
		if s, ok := p["summary"].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// terminalState 把 run 结果映射成接力便条里的状态词。
func terminalState(reason harness.TerminalReason, err error) string {
	switch {
	case err != nil:
		return "blocked"
	case reason == harness.ReasonMaxTurns, reason == harness.ReasonTimeout:
		return "exhausted"
	}
	return "done"
}

// nextHint 给出下一段该做什么的默认建议（正向表述，不含禁止类措辞）。
func nextHint(state string) string {
	switch state {
	case "blocked":
		return "先看这次的原始报错属于哪类（目标侧防护 / 本机环境 / 参数问题），换个等价做法再试一次"
	case "exhausted":
		return "这条方向还没做完，换一个载体或换个入口继续，不要原样重跑"
	}
	return "看本次新产生的 fact，挑最接近目标的那条继续深入"
}

// memoryBlock 组装本段便条文本（便于测试）。
func memoryBlock(intent *db.Node, reason harness.TerminalReason, err error) string {
	st := terminalState(reason, err)
	return relay.Block(intent.ID, intentSummaryText(intent), st, nextHint(st))
}
