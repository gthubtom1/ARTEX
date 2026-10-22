// Package recheck 是「持有资产复查执行器」：把台账到期的资产跑一遍只读探测
// （nc / curl），把结果写回 asset_checks，并让上层记活动日志。
//
// 纪律：
//   - 只跑 ledger.PlanChecks 生成的只读命令，不接受外部传入的任意命令；
//   - 命令串由台账字段拼装，PlanChecks 已做字符白名单（脏数据 → manual，不执行）；
//   - 单条超时 10s，整轮有批次上限，绝不把机器拖死。
package recheck

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Autumn-27/artex/internal/egress"
	"github.com/Autumn-27/artex/internal/ledger"
)

// Executor 执行一条只读命令，返回输出。便于测试注入假执行器。
type Executor interface {
	Run(ctx context.Context, command string, timeout time.Duration) (string, error)
}

// ShellExecutor 用 /bin/sh 执行（命令只来自 PlanChecks）。
type ShellExecutor struct{}

func (ShellExecutor) Run(ctx context.Context, command string, timeout time.Duration) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("空命令")
	}
	if err := egress.Guard(ctx); err != nil {
		return "", fmt.Errorf("出口未就绪，拒绝复查：%w", err)
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(c, "sh", "-c", command)
	if len(egress.ProxyEnv) > 0 {
		cmd.Env = append(os.Environ(), egress.ProxyEnv...)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Event 是一条复查结果（上层用来记活动日志）。
type Event struct {
	OwnedID int64  `json:"owned_id"`
	TaskID  int64  `json:"task_id"`
	Kind    string `json:"kind"`
	Command string `json:"command"`
	OK      bool   `json:"ok"`
	Detail  string `json:"detail"`
}

// Result 是一轮复查的统计。
type Result struct {
	Checked int `json:"checked"`
	OK      int `json:"ok"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// Runner 跑一轮到期复查。
type Runner struct {
	Ledger   *ledger.Store
	Exec     Executor
	Timeout  time.Duration
	MaxBatch int
	OnEvent  func(Event)
	// Now 可注入（测试用）；nil = time.Now。
	Now func() time.Time
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// RunOnce 处理一批到期资产。返回统计与第一个致命错误（单条失败不算致命）。
func (r *Runner) RunOnce(ctx context.Context) (Result, error) {
	var res Result
	if r == nil || r.Ledger == nil {
		return res, fmt.Errorf("recheck: ledger 未装配")
	}
	exec := r.Exec
	if exec == nil {
		exec = ShellExecutor{}
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	batch := r.MaxBatch
	if batch <= 0 {
		batch = 10
	}
	due, err := r.Ledger.Due(ctx, r.now().UTC(), batch)
	if err != nil {
		return res, fmt.Errorf("recheck: due: %w", err)
	}
	specs := ledger.PlanChecks(due)
	taskByOwned := map[int64]int64{}
	for _, o := range due {
		taskByOwned[o.ID] = o.FirstTaskID
	}
	for _, sp := range specs {
		if strings.TrimSpace(sp.Command) == "" {
			res.Skipped++
			if r.OnEvent != nil {
				r.OnEvent(Event{OwnedID: sp.OwnedID, TaskID: taskByOwned[sp.OwnedID], Kind: sp.Kind,
					OK: false, Detail: sp.Expect})
			}
			continue
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		out, xerr := exec.Run(ctx, sp.Command, timeout)
		res.Checked++
		ok := xerr == nil
		detail := strings.TrimSpace(out)
		if sp.Kind == "http" {
			// curl -w '%{http_code}'：000 / 空 = 连不上；5xx 也算"变了"。
			code := strings.TrimSpace(out)
			ok = (strings.HasPrefix(code, "2") || strings.HasPrefix(code, "3")) && xerr == nil
			detail = "http_code=" + code
		}
		if xerr != nil && detail == "" {
			detail = xerr.Error()
		}
		if len([]rune(detail)) > 300 {
			detail = string([]rune(detail)[:300]) + "…"
		}
		if cErr := r.Ledger.RecordCheck(ctx, ledger.Check{
			OwnedID: sp.OwnedID, Kind: sp.Kind, OK: ok, Detail: detail, TS: r.now().UTC(),
		}); cErr != nil {
			return res, fmt.Errorf("recheck: record owned %d: %w", sp.OwnedID, cErr)
		}
		if ok {
			res.OK++
		} else {
			res.Failed++
		}
		if r.OnEvent != nil {
			r.OnEvent(Event{OwnedID: sp.OwnedID, TaskID: taskByOwned[sp.OwnedID], Kind: sp.Kind,
				Command: sp.Command, OK: ok, Detail: detail})
		}
	}
	return res, nil
}
