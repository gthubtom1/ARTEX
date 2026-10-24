package server

import "testing"

// 战果未读（P0-b 胶囊红点）的判据：只看已拿到（done）的行，
// 用该行证据 id 的最大值跟 loot_seen 游标比；游标为零 = 全都算新。
func TestCountUnreadLoot(t *testing.T) {
	rows := []lootRow{
		{Key: "admin", Status: "done", Evidence: []candoEvidence{{ID: 3}, {ID: 9}}},
		{Key: "db", Status: "done", Evidence: []candoEvidence{{ID: 2}}},
		{Key: "rce", Status: "run", Evidence: []candoEvidence{{ID: 11}}}, // 正在试，不算战果
		{Key: "data", Status: "done"},                                    // done 但没证据 id，不能凭状态诈红点
		{Key: "fileread", Status: "na", Evidence: []candoEvidence{{ID: 7}}},
	}
	cases := []struct {
		name string
		seen int64
		want int64
	}{
		{"从没看过 -> 全部算新", 0, 2},
		{"游标 2 -> 只剩 admin(9)", 2, 1},
		{"游标 9 -> 都看过了", 9, 0},
		{"游标超前（换过库/回滚）-> 不出现负数", 99, 0},
	}
	for _, c := range cases {
		if got := countUnreadLoot(rows, c.seen); got != c.want {
			t.Fatalf("%s：countUnreadLoot(seen=%d)=%d，期望 %d", c.name, c.seen, got, c.want)
		}
	}
	if got := countUnreadLoot(nil, 0); got != 0 {
		t.Fatalf("空战果表应为 0，得到 %d", got)
	}
}
