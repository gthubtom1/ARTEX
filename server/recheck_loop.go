package server

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/recheck"
)

// recheckLoop 周期跑「持有资产复查」：到期 → 只读探测（nc/curl）→ 写回 asset_checks。
// 默认 60 分钟一轮（settings: asset.recheck_minutes），批次 10，单条 10s。
// 结果只记活动日志与台账状态，不做任何写操作。
func (s *Server) recheckLoop(ctx context.Context) {
	if s.p2 == nil || s.p2.Ledger == nil {
		return
	}
	interval := time.Hour
	if s.settings != nil {
		if m := s.settings.Int("asset.recheck_minutes", 60); m > 0 {
			interval = time.Duration(m) * time.Minute
		}
	}
	runner := &recheck.Runner{
		Ledger:   s.p2.Ledger,
		MaxBatch: 10,
		Timeout:  10 * time.Second,
		OnEvent: func(e recheck.Event) {
			if e.TaskID <= 0 {
				return
			}
			t, ok := s.m.Task(strconv.FormatInt(e.TaskID, 10))
			if !ok || t == nil {
				return
			}
			state := "存活"
			if !e.OK {
				state = "失效/变更"
			}
			s.engine.emitActivity(t, db.Activity{Worker: "system", Kind: "text",
				Summary: fmt.Sprintf("持有复查：owned#%d %s %s（%s）", e.OwnedID, e.Kind, state, clipLine(e.Detail, 120))})
		},
	}
	// 首次延迟 30s 起跑：让应用先完成启动与迁移。
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		res, err := runner.RunOnce(ctx)
		if err != nil {
			log.Printf("[recheck] run failed (ignored): %v", err)
		} else if res.Checked+res.Skipped > 0 {
			log.Printf("[recheck] checked=%d ok=%d failed=%d skipped=%d", res.Checked, res.OK, res.Failed, res.Skipped)
		}
		timer.Reset(interval)
	}
}

func clipLine(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
