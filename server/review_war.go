package server

import (
	"context"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/internal/impact"
	"github.com/Autumn-27/artex/internal/review"
)

// warBriefForReview 把战况卡那份数据（docs/23 三层模型）喂给审查官。
//
// 为什么必须喂：审查官要判断"方向还成不成立"，而它原来只看意图/漏洞/事实列表 ——
// 漏洞很多但没有一个 got（换不到任何现实后果）时，它会误判成"进展不错"。
// 这里有意识地用与 /brief **同一套口径**（impactStats + lootRows + stallDecision），
// 否则审查官看到的战况会和用户屏幕上看到的不一样，反而更糟。
func (s *Server) warBriefForReview(ctx context.Context, t *Task, taskID int64) *review.WarBrief {
	if s == nil || t == nil || taskID <= 0 {
		return nil
	}
	st := s.impactStats(ctx, t, taskID)
	lv, basis := impact.Compute(st)
	fs := s.lootFindings(t)
	rows := s.lootRowsForTask(ctx, t, fs)

	// 三段式计数：与 briefCan 同口径（含能力佐证）
	standing := map[string]int{"got": 0, "one_step": 0, "no_gain": 0}
	firstGap := ""
	for _, f := range fs {
		info := findingStanding(f, rows, st)
		standing[info.Standing]++
		if info.Standing == "one_step" && firstGap == "" {
			firstGap = info.Gap
		}
	}

	has := []string{}
	if st.OwnedApp > 0 {
		has = append(has, "后台管理员会话")
	}
	if st.Shells > 0 {
		has = append(has, "命令执行")
	}
	if st.DataSamples > 0 {
		has = append(has, "业务数据样本")
	}
	if st.Creds > 0 {
		has = append(has, "凭据（未验证）")
	}

	doable := []string{}
	for _, d := range actionDefs {
		if actionDoable(d, st) {
			doable = append(doable, d.Class)
		}
	}

	stall := ""
	if taskIsRunning(t.Status) && !t.Paused {
		w := stallDecision(s.taskLastProgress(ctx, taskID), s.taskLastHuman(ctx, taskID), time.Now())
		stall = w.Level
		if w.Minutes >= 0 {
			stall += "（" + strconv.Itoa(w.Minutes) + " 分钟没有新证据）"
		}
	} else {
		stall = "任务未在运行"
	}

	open := 0
	for _, r := range s.getActionRuns(ctx, taskID) {
		if r.State == "requested" {
			open++
		}
	}

	return &review.WarBrief{
		Level: lv.String(), Basis: basis, Has: has,
		CanStanding: standing, Doable: doable, Stall: stall, OpenActions: open, FirstGap: firstGap,
	}
}
