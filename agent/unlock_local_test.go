package agent

import (
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

// TestUnlockLocalDeferred 锁定 2026-10-24 实测到的死锁：
// 宿主传了非 nil 的 UnlockSet（MCP 场景）时，SDK 不会用 DeferredTools 播种解锁集，
// agent 自带的 deferred 工具（get_worker_output / nmap_scan …）就全被锁死，
// 模型调用时收到「is not unlocked in this session」，然后反复重试白烧 token。
func TestUnlockLocalDeferred(t *testing.T) {
	// 模拟 server 侧：只为 MCP 名字建的解锁集
	unlock := actool.NewUnlockSet("mcp__browser__navigate")
	def := DeferredInfo{Unlock: unlock}

	names := []string{"get_worker_output", "get_worker_trace", "nmap_scan"}
	if unlock.Has("get_worker_output") {
		t.Fatal("前置条件错：本来应该是锁着的")
	}
	unlockLocalDeferred(def, names)
	for _, n := range names {
		if !unlock.Has(n) {
			t.Fatalf("%s 解锁后仍不可调用（就是线上那个 bug）", n)
		}
	}
	if !unlock.Has("mcp__browser__navigate") {
		t.Fatal("不能影响已有的 MCP 解锁项")
	}

	// 没有 UnlockSet 时（无 MCP）不应 panic —— 那时 SDK 自己会播种。
	unlockLocalDeferred(DeferredInfo{}, names)
	// 空名单也不应 panic。
	unlockLocalDeferred(DeferredInfo{Unlock: unlock}, nil)
}
