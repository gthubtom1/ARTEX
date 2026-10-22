package agent

import (
	"context"
	"testing"
)

// 真库守门：gateStateSQL 只要有一个列名/表名写错，查询就报错 -> gateState 返回 nil ->
// 工具门控静默 fail-open（全部工具照挂，优化归零），而且 build / vet / 普通单测全绿。
// 2026-10-22 就是这样把一个拼错的列名（tasks.source_task_ids，实际在 task_archives 上）
// 带进了主干，靠 R3 真跑才抓到。这条测试是唯一能挡住这类回归的东西。
func TestGateStateSQLRunsOnRealDB(t *testing.T) {
	d := p2PGDB(t)
	taskID := p2PGTask(t, d)

	ts := NewToolSet(nil, "test")
	ts.SetP2(&P2{DB: d})
	ts.SetTaskID(taskID)

	st := ts.gateState()
	if st == nil {
		t.Fatal("gateState 返回 nil —— gateStateSQL 在真库上报错了（某个列名/表名不存在）。" +
			"后果是工具门控静默 fail-open，P9-1 完全失效")
	}
	if st.HasOwned {
		t.Fatal("新任务不该有台账状态")
	}

	// 造一条"来源任务"关系：应能读出 HasOwned（继承型台账）
	other := p2PGTask(t, d)
	if _, err := d.Exec(`INSERT INTO task_relations (task_id, source_task_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, taskID, other); err != nil {
		t.Skipf("task_relations 写入失败（老库无该表？）: %v", err)
	}
	st = ts.gateState()
	if st == nil {
		t.Fatal("插入 task_relations 后 gateState 仍返回 nil")
	}
	if !st.HasOwned {
		t.Fatal("有来源任务时应判为 HasOwned（继承型台账），实际 false")
	}
	_ = context.Background()
}
