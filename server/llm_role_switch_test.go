package server

import (
	"context"
	"testing"

	"github.com/Autumn-27/artex/internal/settings"
	"github.com/Autumn-27/artex/internal/stall"
)

// D2 决策：adviser / verifier 留代码、默认关（真靶场 25 分钟：adviser 1 次、
// verifier 0 次；三门校验已拦住幻觉）。这两条单测钉住三件事：
//  1. 出厂默认必须是 false；
//  2. settings 为 nil（未初始化）也必须按关处理，不能"读不到就放开"；
//  3. 关着时 verifier 连冷却门禁/DB 查询都不碰 —— 即零额外 LLM 调用。

func TestLLMRoleDefaultsOff(t *testing.T) {
	for _, k := range []string{"adviser.enabled", "verifier.enabled"} {
		v, ok := settings.Defaults[k]
		if !ok {
			t.Fatalf("settings.Defaults 缺少 %q（出厂默认关必须显式声明）", k)
		}
		if v != "false" {
			t.Fatalf("%q 默认值 = %q，期望 false", k, v)
		}
	}

	// 未初始化 settings（s.settings == nil）按关处理。
	empty := &Server{}
	if empty.adviserLLMEnabled() {
		t.Fatal("settings 未初始化时 adviser 必须按关处理")
	}
	if empty.verifierLLMEnabled() {
		t.Fatal("settings 未初始化时 verifier 必须按关处理")
	}

	// 默认 Store（未写入任何值）也必须是关。
	st := settings.New(nil)
	s := &Server{settings: st}
	if s.adviserLLMEnabled() {
		t.Fatal("新装默认 adviser 必须关")
	}
	if s.verifierLLMEnabled() {
		t.Fatal("新装默认 verifier 必须关")
	}
}

func TestLLMRoleSwitchReadsSetting(t *testing.T) {
	st := settings.New(nil)
	s := &Server{settings: st}
	ctx := context.Background()

	if err := st.Set(ctx, "adviser.enabled", "true"); err != nil {
		t.Fatalf("set adviser.enabled: %v", err)
	}
	if err := st.Set(ctx, "verifier.enabled", "true"); err != nil {
		t.Fatalf("set verifier.enabled: %v", err)
	}
	if !s.adviserLLMEnabled() || !s.verifierLLMEnabled() {
		t.Fatal("写入 true 后两个开关都应生效")
	}

	if err := st.Set(ctx, "adviser.enabled", "false"); err != nil {
		t.Fatalf("set adviser.enabled: %v", err)
	}
	if err := st.Set(ctx, "verifier.enabled", "false"); err != nil {
		t.Fatalf("set verifier.enabled: %v", err)
	}
	if s.adviserLLMEnabled() || s.verifierLLMEnabled() {
		t.Fatal("写入 false 后两个开关都应关闭")
	}
}

// 调用点的判据（停顿命中 + 开关）本身也要能被单测钉住：默认关时即使停顿命中
// 也不能去调 tryLLMAdviser，否则"默认关"就是假的。
func TestAdviserCallSiteRequiresSwitch(t *testing.T) {
	hit := stall.Report{Stuck: []string{"repeat_tool"}}
	miss := stall.Report{}

	s := &Server{settings: settings.New(nil)}
	if s.shouldConsultLLMAdviser(hit) {
		t.Fatal("默认关：停顿命中也不得调 LLM 顾问")
	}
	if s.shouldConsultLLMAdviser(miss) {
		t.Fatal("无停顿不应调 LLM 顾问")
	}

	if err := s.settings.Set(context.Background(), "adviser.enabled", "true"); err != nil {
		t.Fatalf("set adviser.enabled: %v", err)
	}
	if !s.shouldConsultLLMAdviser(hit) {
		t.Fatal("开启后：停顿命中应允许调 LLM 顾问")
	}
	if s.shouldConsultLLMAdviser(miss) {
		t.Fatal("开启后：无停顿仍不应调 LLM 顾问")
	}
}

// 关着时 verifierPass 必须在"碰任何东西"之前返回：冷却门禁不记时间、DB 不查。
// 开着时（这里 p2/Store 为 nil，所以仍返回 0）会走到冷却门禁 —— 用它区分
// "被开关拦住" 和 "开关没生效"。
func TestVerifierPassDisabledSkipsBeforeAnyWork(t *testing.T) {
	st := settings.New(nil)
	s := &Server{settings: st}
	ctx := context.Background()
	task := &Task{ID: "42"}

	if n := s.verifierPass(ctx, task); n != 0 {
		t.Fatalf("关着时 verifierPass 应返回 0，得到 %d", n)
	}
	if len(s.verifierCooldown.last) != 0 {
		t.Fatalf("关着时不应进入冷却门禁/DB 查询，冷却表已有 %d 条", len(s.verifierCooldown.last))
	}

	if err := st.Set(ctx, "verifier.enabled", "true"); err != nil {
		t.Fatalf("set verifier.enabled: %v", err)
	}
	if n := s.verifierPass(ctx, task); n != 0 { // 缺 p2/Store，仍应 0
		t.Fatalf("无 p2 时 verifierPass 应返回 0，得到 %d", n)
	}
	if _, ok := s.verifierCooldown.last["42"]; !ok {
		t.Fatal("开着时应走到冷却门禁（说明开关确实被读取）")
	}
}
