package agent

import (
	"testing"
	"time"
)

// P0-01：单条意图不许吃满整个任务窗口。
func TestClampIntentBudget(t *testing.T) {
	now := time.Now()
	// 无 deadline：行为不变
	if eff, _ := clampIntentBudget(0, 600*time.Second); eff != 600*time.Second {
		t.Fatalf("无 deadline 时应保持自己的预算，得到 %v", eff)
	}
	// 剩 30 分钟、自己 1200s → 夹到 1/3 = 600s
	eff, clamped := clampIntentBudget(now.Add(30*time.Minute).Unix(), 1200*time.Second)
	if !clamped || eff > 610*time.Second || eff < 590*time.Second {
		t.Fatalf("30 分钟任务、1200s 预算 → 期望约 600s，得到 %v (clamped=%v)", eff, clamped)
	}
	// 剩 3 分钟、自己 1200s → 1/3=60s，但地板 90s
	eff2, _ := clampIntentBudget(now.Add(3*time.Minute).Unix(), 1200*time.Second)
	if eff2 < 85*time.Second {
		t.Fatalf("地板应 ≥90s，得到 %v", eff2)
	}
	// 自己预算本来就小 → 用自己那个
	eff3, _ := clampIntentBudget(now.Add(60*time.Minute).Unix(), 100*time.Second)
	if eff3 != 100*time.Second {
		t.Fatalf("自己预算更小时应保留，得到 %v", eff3)
	}
	// deadline 已经过去 → 不小于 1s（harness 把 0 当不限）
	eff4, _ := clampIntentBudget(now.Add(-time.Hour).Unix(), 600*time.Second)
	if eff4 < time.Second {
		t.Fatalf("过期 deadline 也必须 ≥1s，得到 %v", eff4)
	}
}
