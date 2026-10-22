package ledger

import "time"

// OwnedStatus 是持有台账的状态枚举（与 assets_owned.status 列的取值一致）。
//
// 它是 string 的具名类型，所以和 Owned.Status（string）之间需要显式转换：
//
//	OwnedStatus(o.Status) / string(res.Next)
type OwnedStatus string

const (
	StatusOwned    OwnedStatus = "owned"    // 刚打下来，还没复查过
	StatusVerified OwnedStatus = "verified" // 最近一次复查成功
	StatusStale    OwnedStatus = "stale"    // 复查失败，但还没到 lost
	StatusLost     OwnedStatus = "lost"     // 连续失败达到上限，视为彻底丢失
	StatusCleaned  OwnedStatus = "cleaned"  // 战后主动清理
)

// 状态机默认值（P8-5 表格之外的两条兜底规则）。
const (
	// DefaultMaxFails 是连续失败几次算 lost（P8-5 文档口径：3 次）。
	DefaultMaxFails = 3
	// DefaultStaleAfter 是多久没验证算 stale（P8-5 文档口径：7 天）。
	DefaultStaleAfter = 7 * 24 * time.Hour
)

// TransitionInput 是一次复查对状态机的影响。
type TransitionInput struct {
	Current      OwnedStatus   // 当前状态（空串视为 owned）
	OK           bool          // 本次复查是否成功
	Fails        int           // 已连续失败次数（本次之前）
	LastVerified time.Time     // 上次验证成功时间（零值表示从未验证过）
	Now          time.Time     // 本次复查时间
	StaleAfter   time.Duration // 多久没验证算 stale（<=0 时按 7 天）
	MaxFails     int           // 连续失败几次算 lost（<=0 时按 3）
}

// TransitionResult 是状态机输出。
type TransitionResult struct {
	Next        OwnedStatus
	Fails       int  // 新连续失败次数（成功时归零）
	TouchVerify bool // 是否需要更新 last_verified_at = Now
}

// normalize 把零值补成默认口径。
func (in TransitionInput) normalize() TransitionInput {
	if in.Current == "" {
		in.Current = StatusOwned
	}
	if in.MaxFails <= 0 {
		in.MaxFails = DefaultMaxFails
	}
	if in.StaleAfter <= 0 {
		in.StaleAfter = DefaultStaleAfter
	}
	return in
}

// Transition 按 P8-5 表格推进状态。
//
//	owned    + ok          → verified, fails=0, touch
//	owned    + fail        → stale,    fails+1
//	verified + ok          → verified, fails=0, touch
//	verified + fail        → stale,    fails+1
//	stale    + ok          → verified, fails=0, touch
//	stale    + fail        → fails+1；>= MaxFails 则 lost，否则 stale
//	lost     + 任意        → lost（fails 不变）
//	cleaned  + 任意        → cleaned（fails 不变）
//
// 「太久没验」（now-last_verified > StaleAfter）**不在这里改状态**，见 StaleByAge：
// 那是查询侧（owned_due）的判定，不该在写路径上顺手改行。
func Transition(in TransitionInput) TransitionResult {
	in = in.normalize()
	fails := in.Fails
	if fails < 0 {
		fails = 0
	}

	// 终态优先：lost / cleaned 对任何事件都保持不变（包括复查成功）。
	switch in.Current {
	case StatusLost:
		// 已判死：不再变化，失败计数也不再累加（保留 last known）。
		return TransitionResult{Next: StatusLost, Fails: fails}
	case StatusCleaned:
		// 已清理：保持原状，不需要再验证。
		return TransitionResult{Next: StatusCleaned, Fails: fails}
	}

	if in.OK {
		// 成功后一律回到 verified 并清零连续失败。
		return TransitionResult{Next: StatusVerified, Fails: 0, TouchVerify: true}
	}

	// owned / verified / stale + 失败
	fails++
	if in.Current == StatusStale && fails >= in.MaxFails {
		return TransitionResult{Next: StatusLost, Fails: fails}
	}
	return TransitionResult{Next: StatusStale, Fails: fails}
}

// ShouldRecheck 判断该状态是否还应该进复查队列。
//
//	owned / verified / stale / 空串 → true
//	lost / cleaned                 → false
//	其它未知状态                   → false（DB 有 CHECK 约束，正常不会出现）
func ShouldRecheck(s OwnedStatus) bool {
	switch s {
	case "", StatusOwned, StatusVerified, StatusStale:
		return true
	case StatusLost, StatusCleaned:
		return false
	}
	return false
}

// StaleByAge 判定「太久没验证」：LastVerified 非零、已超过 StaleAfter、且当前是 verified。
//
// 专供 owned_due / 列表查询侧使用：命中时把展示状态标成 stale，**不改行**。
// Current 为空串按 owned 处理（从未验证过的不算「太久没验」，它压根没验过）。
func StaleByAge(in TransitionInput) bool {
	in = in.normalize()
	if in.Current != StatusVerified {
		return false
	}
	if in.LastVerified.IsZero() {
		return false
	}
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	return in.Now.Sub(in.LastVerified) > in.StaleAfter
}
