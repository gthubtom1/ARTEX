package server

import (
	"testing"
	"time"
)

// 闸门2（投入产出）判据必须"只认证据"，且不能因为一次波动就把有进展的任务停掉。
// 三条边界：30 分钟只警告、60 分钟建议收工、120 分钟 + 人 60 分钟没说话才自动暂停。
func TestStallDecision(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	if got := stallDecision(time.Time{}, time.Time{}, now); got.Level != "ok" {
		t.Fatalf("没有证据时间时应 ok，得到 %+v", got)
	}
	if got := stallDecision(now.Add(-10*time.Minute), now.Add(-2*time.Minute), now); got.Level != "ok" || got.Minutes != 10 {
		t.Fatalf("10 分钟没进展应 ok/10，得到 %+v", got)
	}
	if got := stallDecision(now.Add(-31*time.Minute), now, now); got.Level != "warn" {
		t.Fatalf("31 分钟应 warn，得到 %+v", got)
	}
	if got := stallDecision(now.Add(-61*time.Minute), now, now); got.Level != "stop" || got.Advice == "" {
		t.Fatalf("61 分钟应 stop 且给建议，得到 %+v", got)
	}

	if shouldAutoPause(now.Add(-121*time.Minute), now.Add(-61*time.Minute), now) != true {
		t.Fatal("2 小时没进展 + 人 1 小时没说话 → 应自动暂停（省钱）")
	}
	if shouldAutoPause(now.Add(-121*time.Minute), now.Add(-5*time.Minute), now) {
		t.Fatal("人刚说过话（5 分钟前）时不许自动暂停：会打断用户")
	}
	if shouldAutoPause(now.Add(-59*time.Minute), now.Add(-120*time.Minute), now) {
		t.Fatal("不到 2 小时不许自动暂停")
	}
}
