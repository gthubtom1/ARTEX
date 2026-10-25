package agent

import (
	"context"
	"time"
)

// TaskClock carries a task's absolute deadline into a worker/planner run so the run
// can clamp its own wall-clock budget to the task's remaining time and pick the
// right wrap-up words (per-run vs task-timeout). Attached to the run ctx by the
// engine. Zero value = no task-level timeout (behaves exactly as before).
type TaskClock struct {
	DeadlineUnix int64 // absolute deadline (unix seconds); 0 = no task timeout
	Final        bool  // coordinator-driven FINAL planner round (task ending now)
}

type taskClockKey struct{}

// WithTaskClock attaches a TaskClock to ctx for the run.
func WithTaskClock(ctx context.Context, tc TaskClock) context.Context {
	return context.WithValue(ctx, taskClockKey{}, tc)
}

// taskClockFrom reads the TaskClock (zero value if none attached).
func taskClockFrom(ctx context.Context) TaskClock {
	if v, ok := ctx.Value(taskClockKey{}).(TaskClock); ok {
		return v
	}
	return TaskClock{}
}

// clampMaxDuration folds a task deadline into a run's own wall-clock budget.
//   - ownBudget = the agent's own run_seconds (0 = unlimited).
//   - Returns eff = the MaxDuration to use (floored at 1s so we never pass ≤0, which
//     harness reads as "unlimited"), and clamped = whether the TASK deadline is the
//     binding constraint (remaining ≤ ownBudget, or ownBudget unlimited). When there
//     is no deadline, returns (ownBudget, false) unchanged.
func clampMaxDuration(deadlineUnix int64, ownBudget time.Duration) (eff time.Duration, clamped bool) {
	if deadlineUnix <= 0 {
		return ownBudget, false
	}
	remaining := time.Until(time.Unix(deadlineUnix, 0))
	if remaining < time.Second {
		remaining = time.Second // max(1, …): never pass ≤0 (harness treats 0 as unlimited)
	}
	// clamped when the task deadline binds this run's time: remaining ≤ own budget,
	// or the agent has no own time budget (then remaining always binds).
	clamped = ownBudget <= 0 || remaining <= ownBudget
	eff = remaining
	if ownBudget > 0 && ownBudget < remaining {
		eff = ownBudget
	}
	return eff, clamped
}

// clampIntentBudget 是"单条意图的墙钟预算"：最多吃掉任务剩余时间的 1/3。
//
// 背景（2026-10-25 外部审查 P0-01，docs/PLANS/2026-10-25-artex-找洞能力审查.md）：
// 实测过"一条意图吃满整个任务窗口"—— 20 分钟的任务里，第一条意图跑 18 分钟没出结果，
// 剩下的时间连第二条方向都来不及试。找洞靠的是"多方向快速试错"，不是"一条路跑到黑"。
//
// 规则：eff = min(自己的 run_seconds, 任务剩余时间/3, 但不能少于 90 秒)。
// 90 秒地板是为了不把正常的一次验证打断（一次 sqlmap --batch 往往就要几十秒）。
// 没有任务 deadline 时行为与以前完全一致。
func clampIntentBudget(deadlineUnix int64, ownBudget time.Duration) (time.Duration, bool) {
	eff, clamped := clampMaxDuration(deadlineUnix, ownBudget)
	if deadlineUnix <= 0 {
		return eff, clamped
	}
	share := time.Until(time.Unix(deadlineUnix, 0)) / 3
	if share < 90*time.Second {
		share = 90 * time.Second
	}
	if eff <= 0 || share < eff {
		return share, true
	}
	return eff, clamped
}
