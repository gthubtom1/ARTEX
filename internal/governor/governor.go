// Package governor 是资源治理层：限制并发、单命令超时、输出上限，避免把 Kali VM 打崩。
//
// 设计纪律：拒绝是**有界排队**而非报错——超过并发就等，超时就返回明确原因，
// 输出超限就截断（保留头部）而不是失败。
package governor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Limits 是治理参数（默认值见 Default）。
type Limits struct {
	MaxParallel int           // 同时执行的外部命令数（默认 3）
	CmdTimeout  time.Duration // 单命令超时（默认 300s）
	OutputCap   int           // 输出上限（字节，默认 2MB）；超出截断
	MaxRate     int           // 保留字段（每秒请求上限，0=不限）
}

// Default 是出厂设置。
func Default() Limits {
	return Limits{MaxParallel: 3, CmdTimeout: 300 * time.Second, OutputCap: 2 << 20}
}

// Governor 用信号量控制并发。
type Governor struct {
	lim Limits
	sem chan struct{}
}

func New(l Limits) *Governor {
	d := Default()
	if l.MaxParallel <= 0 {
		l.MaxParallel = d.MaxParallel
	}
	if l.CmdTimeout <= 0 {
		l.CmdTimeout = d.CmdTimeout
	}
	if l.OutputCap <= 0 {
		l.OutputCap = d.OutputCap
	}
	return &Governor{lim: l, sem: make(chan struct{}, l.MaxParallel)}
}

// Limits 返回当前参数。
func (g *Governor) Limits() Limits { return g.lim }

// Acquire 获取一个执行槽（阻塞直到有空位或 ctx 取消）。
func (g *Governor) Acquire(ctx context.Context) (release func(), err error) {
	select {
	case g.sem <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-g.sem }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ErrTimeout 表示该命令因超时被中止。
var ErrTimeout = errors.New("governor: command timed out")

// Run 在治理约束下执行 fn：
//   - 先排队拿槽；
//   - 套上 CmdTimeout；
//   - 输出超过 OutputCap 时截断并标记（不报错）。
//
// fn 收到的 ctx 已带超时；它必须尊重 ctx。
func (g *Governor) Run(ctx context.Context, fn func(context.Context) ([]byte, error)) (out []byte, truncated bool, err error) {
	release, err := g.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	defer release()

	c, cancel := context.WithTimeout(ctx, g.lim.CmdTimeout)
	defer cancel()

	out, err = fn(c)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(c.Err(), context.DeadlineExceeded) {
			return out, false, fmt.Errorf("%w (limit %s)", ErrTimeout, g.lim.CmdTimeout)
		}
		return out, false, err
	}
	if len(out) > g.lim.OutputCap {
		return out[:g.lim.OutputCap], true, nil
	}
	return out, false, nil
}

// InFlight 返回当前占用的槽数（观测用）。
func (g *Governor) InFlight() int { return len(g.sem) }
