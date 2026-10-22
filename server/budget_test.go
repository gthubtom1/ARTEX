package server

import (
	"testing"
	"time"
)

func TestMatchesBudgetTarget(t *testing.T) {
	if !matchesBudgetTarget("对 /api/user?id= 做越权遍历", "越权遍历", "/api/user?id=") {
		t.Fatal("surface/can match expected")
	}
	if matchesBudgetTarget("枚举子域名", "越权遍历", "/api/user?id=") {
		t.Fatal("unrelated intent must not be killed")
	}
	if matchesBudgetTarget("", "x", "y") {
		t.Fatal("empty summary must not match")
	}
}

func TestBudgetExpired(t *testing.T) {
	now := time.Now()
	if !budgetExpired(now, now.Add(-16*time.Minute), 15*time.Minute, 0, 20) {
		t.Fatal("minutes budget should expire")
	}
	if budgetExpired(now, now.Add(-5*time.Minute), 15*time.Minute, 0, 20) {
		t.Fatal("minutes budget should not expire yet")
	}
	if !budgetExpired(now, now.Add(-1*time.Minute), 15*time.Minute, 20, 20) {
		t.Fatal("rounds budget should expire")
	}
	if budgetExpired(now, time.Time{}, 15*time.Minute, 99, 20) {
		t.Fatal("未开始的预算不熔断")
	}
}
