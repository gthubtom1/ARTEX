package agent

import (
	"context"
	"testing"
	"time"
)

// P8-5 的真库回归：决策队列点"换目标"后，冻结的资产必须真的落到 capabilities.frozen_asset_ids，
// 并且能被 ToolSet.frozenAssets() 读回来（add_intent 就是靠这个集合拦截派单）。
//
// 这条测试守两件事：
//   1. 写：DecideCapability(action=switch) 把 decision.Verdict.FreezeAssetIDs 落库；
//   2. 读：frozenAssets() 用 array_to_json(array_agg(...)) 把 PG 的 bigint[] 还原成集合
//      —— 这是仓库里反复踩过的坑（bigint[] 不能直接 Scan）。
func TestDecideSwitchPersistsFrozenAssetsPG(t *testing.T) {
	d := p2PGDB(t)
	taskID := p2PGTask(t, d)
	ctx := context.Background()

	const frozenAsset int64 = 4242
	var capID int64
	if err := d.QueryRow(`
		INSERT INTO capabilities (task_id, grp, item, can, surface, status, source_node_ids)
		VALUES ($1, 4, '4.1', '能执行命令', '/index.php', 'open', $2::bigint[])
		RETURNING id`, taskID, int64ArrayLiteral([]int64{frozenAsset})).Scan(&capID); err != nil {
		t.Fatalf("insert capability: %v", err)
	}

	p2, err := NewP2(d, "")
	if err != nil {
		t.Fatalf("new p2: %v", err)
	}
	ts := NewToolSet(nil, "test")
	ts.SetP2(p2)
	ts.SetTaskID(taskID)

	// 冻结前：集合为空，不会拦任何派单。
	if set := ts.frozenAssets(); set.Len() != 0 {
		t.Fatalf("冻结前集合应为空，实际 %v", set.IDs())
	}

	v, err := p2.DecideCapability(ctx, nil, capID, "switch", "换一台打", "test")
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if len(v.FreezeAssetIDs) == 0 {
		t.Fatal("switch 裁决应给出要冻结的资产 id")
	}

	// 落库校验（直接读列，确认是 bigint[] 而不是空）
	var raw string
	if err := d.QueryRow(`SELECT COALESCE(array_to_json(frozen_asset_ids)::text,'[]') FROM capabilities WHERE id=$1`, capID).Scan(&raw); err != nil {
		t.Fatalf("read frozen_asset_ids: %v", err)
	}
	if raw != "[4242]" && raw != "[4242,4242]" {
		t.Fatalf("frozen_asset_ids 落库内容异常：%s", raw)
	}

	// 读回校验：这就是 add_intent 里那道检查的数据来源。
	set := ts.frozenAssets()
	if !set.Has(frozenAsset) {
		t.Fatalf("frozenAssets 未命中 %d，实际 %v", frozenAsset, set.IDs())
	}
	if id, hit := set.Any([]int64{1, frozenAsset, 2}); !hit || id != frozenAsset {
		t.Fatalf("Any 应命中 %d，实际 id=%d hit=%v", frozenAsset, id, hit)
	}
	if set.Has(9999) {
		t.Fatal("未冻结的资产不应命中")
	}

	// 换个任务看：冻结是按任务隔离的，不能串到别的任务上。
	other := p2PGTask(t, d)
	otherTS := NewToolSet(nil, "test")
	otherTS.SetP2(p2)
	otherTS.SetTaskID(other)
	if n := otherTS.frozenAssets().Len(); n != 0 {
		t.Fatalf("冻结集合串到别的任务了：%v", otherTS.frozenAssets().IDs())
	}
	_ = time.Now()
}
