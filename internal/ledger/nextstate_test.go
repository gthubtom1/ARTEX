package ledger

import (
	"testing"
	"time"
)

// fixedNow 是状态机测试的基准时间，避免用 time.Now() 造成不稳定。
var fixedNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func TestTransitionTable(t *testing.T) {
	day := 24 * time.Hour
	// 每个子测试默认：已连续失败 1 次、7 天前验证过、MaxFails=3、StaleAfter=7d。
	base := TransitionInput{
		Fails:        1,
		LastVerified: fixedNow.Add(-7 * day),
		Now:          fixedNow,
		StaleAfter:   7 * day,
		MaxFails:     3,
	}
	in := func(cur OwnedStatus, ok bool) TransitionInput {
		i := base
		i.Current = cur
		i.OK = ok
		return i
	}

	tests := []struct {
		name      string
		in        TransitionInput
		wantNext  OwnedStatus
		wantFails int
		wantTouch bool
	}{
		{"owned ok → verified", in(StatusOwned, true), StatusVerified, 0, true},
		{"owned fail → stale", in(StatusOwned, false), StatusStale, 2, false},
		{"verified ok → verified", in(StatusVerified, true), StatusVerified, 0, true},
		{"verified fail → stale", in(StatusVerified, false), StatusStale, 2, false},
		{"stale ok → verified", in(StatusStale, true), StatusVerified, 0, true},
		{"stale fail → stale（未到上限）", in(StatusStale, false), StatusStale, 2, false},
		{"lost ok → lost（幂等）", in(StatusLost, true), StatusLost, 1, false},
		{"lost fail → lost（幂等）", in(StatusLost, false), StatusLost, 1, false},
		{"cleaned ok → cleaned（幂等）", in(StatusCleaned, true), StatusCleaned, 1, false},
		{"cleaned fail → cleaned（幂等）", in(StatusCleaned, false), StatusCleaned, 1, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Transition(tc.in)
			if got.Next != tc.wantNext {
				t.Fatalf("Next = %q, want %q", got.Next, tc.wantNext)
			}
			if got.Fails != tc.wantFails {
				t.Fatalf("Fails = %d, want %d", got.Fails, tc.wantFails)
			}
			if got.TouchVerify != tc.wantTouch {
				t.Fatalf("TouchVerify = %v, want %v", got.TouchVerify, tc.wantTouch)
			}
		})
	}
}

func TestTransitionStaleToLostAtMaxFails(t *testing.T) {
	in := TransitionInput{
		Current:    StatusStale,
		OK:         false,
		Fails:      2,
		Now:        fixedNow,
		MaxFails:   3,
		StaleAfter: DefaultStaleAfter,
	}
	got := Transition(in)
	if got.Next != StatusLost || got.Fails != 3 {
		t.Fatalf("fails=2 → 第 3 次失败应 lost/3，得到 %q/%d", got.Next, got.Fails)
	}
	// 到 lost 之后继续失败：不再累加、保持 lost。
	again := Transition(TransitionInput{Current: got.Next, OK: false, Fails: got.Fails, Now: fixedNow, MaxFails: 3})
	if again.Next != StatusLost || again.Fails != 3 {
		t.Fatalf("lost 应幂等，得到 %q/%d", again.Next, again.Fails)
	}
}

// 从 owned 出发连续失败 3 次应恰好走到 lost（验收判据 1）。
func TestTransitionThreeFailsFromOwned(t *testing.T) {
	cur := OwnedStatus("")
	fails := 0
	var got TransitionResult
	for i := 1; i <= 3; i++ {
		got = Transition(TransitionInput{
			Current:      cur,
			OK:           false,
			Fails:        fails,
			LastVerified: fixedNow.Add(-30 * 24 * time.Hour),
			Now:          fixedNow.Add(time.Duration(i) * time.Hour),
			StaleAfter:   7 * 24 * time.Hour,
			MaxFails:     3,
		})
		cur, fails = got.Next, got.Fails
		if i < 3 && got.Next != StatusStale {
			t.Fatalf("第 %d 次失败应 stale，得到 %q", i, got.Next)
		}
	}
	if got.Next != StatusLost || got.Fails != 3 {
		t.Fatalf("三次失败应 lost/3，得到 %q/%d", got.Next, got.Fails)
	}
}

func TestTransitionDefaults(t *testing.T) {
	// MaxFails = 0 → 3
	got := Transition(TransitionInput{Current: StatusStale, OK: false, Fails: 2, Now: fixedNow})
	if got.Next != StatusLost {
		t.Fatalf("MaxFails 默认应 3，fails=2→3 应 lost，得到 %q", got.Next)
	}
	// MaxFails = -5 → 3
	got = Transition(TransitionInput{Current: StatusStale, OK: false, Fails: 2, Now: fixedNow, MaxFails: -5})
	if got.Next != StatusLost {
		t.Fatalf("MaxFails<=0 应按 3，得到 %q", got.Next)
	}
	// MaxFails 显式给 5 → 第 3 次失败仍是 stale
	got = Transition(TransitionInput{Current: StatusStale, OK: false, Fails: 2, Now: fixedNow, MaxFails: 5})
	if got.Next != StatusStale || got.Fails != 3 {
		t.Fatalf("MaxFails=5 时 fails=3 应 stale，得到 %q/%d", got.Next, got.Fails)
	}

	// StaleAfter 默认 7 天：不填（0）与显式 7d 行为一致。
	now := fixedNow
	age := now.Add(-8 * 24 * time.Hour)
	if !StaleByAge(TransitionInput{Current: StatusVerified, LastVerified: age, Now: now}) {
		t.Fatal("StaleAfter 默认应 7 天：8 天未验证应 stale")
	}
	if StaleByAge(TransitionInput{Current: StatusVerified, LastVerified: now.Add(-6 * 24 * time.Hour), Now: now}) {
		t.Fatal("6 天未验证不应 stale")
	}
	// StaleAfter <= 0 等价于默认值
	if StaleByAge(TransitionInput{Current: StatusVerified, LastVerified: age, Now: now, StaleAfter: -time.Hour}) != true {
		t.Fatal("StaleAfter<=0 应按 7 天")
	}
	if DefaultStaleAfter != 7*24*time.Hour {
		t.Fatalf("DefaultStaleAfter = %v", DefaultStaleAfter)
	}
	if DefaultMaxFails != 3 {
		t.Fatalf("DefaultMaxFails = %d", DefaultMaxFails)
	}
}

func TestTransitionEmptyCurrentIsOwned(t *testing.T) {
	ok := Transition(TransitionInput{Current: "", OK: true, Fails: 4, Now: fixedNow})
	if ok.Next != StatusVerified || ok.Fails != 0 || !ok.TouchVerify {
		t.Fatalf("空串 + ok 应 verified/0/touch，得到 %+v", ok)
	}
	fail := Transition(TransitionInput{Current: "", OK: false, Fails: 0, Now: fixedNow})
	if fail.Next != StatusStale || fail.Fails != 1 {
		t.Fatalf("空串 + fail 应 stale/1，得到 %+v", fail)
	}
}

func TestTransitionIdempotentTerminalStates(t *testing.T) {
	for _, cur := range []OwnedStatus{StatusLost, StatusCleaned} {
		first := Transition(TransitionInput{Current: cur, OK: false, Fails: 7, Now: fixedNow})
		second := Transition(TransitionInput{Current: cur, OK: false, Fails: 7, Now: fixedNow})
		if first != second {
			t.Fatalf("%q 应幂等：%+v vs %+v", cur, first, second)
		}
		if first.Next != cur || first.Fails != 7 || first.TouchVerify {
			t.Fatalf("%q 应原样返回，得到 %+v", cur, first)
		}
	}
}

// 「owned 失败但已超 staleAfter」仍按 owned 失败 → stale 走（不因超时变成别的）。
func TestTransitionOwnedFailIgnoresAge(t *testing.T) {
	got := Transition(TransitionInput{
		Current:      StatusOwned,
		OK:           false,
		Fails:        0,
		LastVerified: fixedNow.Add(-30 * 24 * time.Hour),
		Now:          fixedNow,
		StaleAfter:   7 * 24 * time.Hour,
		MaxFails:     3,
	})
	if got.Next != StatusStale || got.Fails != 1 || got.TouchVerify {
		t.Fatalf("owned+fail 应 stale/1/false，得到 %+v", got)
	}
}

func TestStaleByAge(t *testing.T) {
	now := fixedNow
	day := 24 * time.Hour

	positives := []struct {
		name string
		in   TransitionInput
	}{
		{"verified 8 天未验", TransitionInput{Current: StatusVerified, LastVerified: now.Add(-8 * day), Now: now}},
		{"verified 自定义阈值 1h", TransitionInput{Current: StatusVerified, LastVerified: now.Add(-2 * time.Hour), Now: now, StaleAfter: time.Hour}},
	}
	for _, tc := range positives {
		t.Run("true/"+tc.name, func(t *testing.T) {
			if !StaleByAge(tc.in) {
				t.Fatalf("应判 stale：%+v", tc.in)
			}
		})
	}

	negatives := []struct {
		name string
		in   TransitionInput
	}{
		{"owned 即使很久", TransitionInput{Current: StatusOwned, LastVerified: now.Add(-100 * day), Now: now}},
		{"空串按 owned", TransitionInput{Current: "", LastVerified: now.Add(-100 * day), Now: now}},
		{"stale 不再判", TransitionInput{Current: StatusStale, LastVerified: now.Add(-100 * day), Now: now}},
		{"lost 不再判", TransitionInput{Current: StatusLost, LastVerified: now.Add(-100 * day), Now: now}},
		{"cleaned 不再判", TransitionInput{Current: StatusCleaned, LastVerified: now.Add(-100 * day), Now: now}},
		{"从未验证过", TransitionInput{Current: StatusVerified, LastVerified: time.Time{}, Now: now}},
		{"刚好等于阈值（不 >）", TransitionInput{Current: StatusVerified, LastVerified: now.Add(-7 * day), Now: now}},
		{"刚刚验证过", TransitionInput{Current: StatusVerified, LastVerified: now, Now: now}},
	}
	for _, tc := range negatives {
		t.Run("false/"+tc.name, func(t *testing.T) {
			if StaleByAge(tc.in) {
				t.Fatalf("不应判 stale：%+v", tc.in)
			}
		})
	}
}

func TestShouldRecheck(t *testing.T) {
	tests := map[OwnedStatus]bool{
		"":             true,
		StatusOwned:    true,
		StatusVerified: true,
		StatusStale:    true,
		StatusLost:     false,
		StatusCleaned:  false,
		"bogus":        false,
	}
	if len(tests) != 7 {
		t.Fatalf("枚举不完整：%d", len(tests))
	}
	for s, want := range tests {
		name := string(s)
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			if got := ShouldRecheck(s); got != want {
				t.Fatalf("ShouldRecheck(%q) = %v, want %v", s, got, want)
			}
		})
	}
}

// 常量与 DB 里的 status 列取值必须一字不差。
func TestOwnedStatusConstants(t *testing.T) {
	want := map[OwnedStatus]string{
		StatusOwned:    "owned",
		StatusVerified: "verified",
		StatusStale:    "stale",
		StatusLost:     "lost",
		StatusCleaned:  "cleaned",
	}
	for got, w := range want {
		if string(got) != w {
			t.Fatalf("常量 = %q, want %q", got, w)
		}
	}
}
