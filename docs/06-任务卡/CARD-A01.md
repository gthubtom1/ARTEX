# CARD-A01 报告改用「独立漏洞」计数

> 先读：`docs/06-判定标准-冻结.md` §二-8 ｜ 纯 bug 修复（按 `docs/07` §1 可跳过 G0/G1）

## ① 目标
报告 Markdown 摘要行改用 `db.DistinctFindingStats`（独立漏洞），与页面数字对齐；查不到时回退原始条数。

## ② 为什么
页面显示「3 个独立漏洞 / 3 条上报」，报告写「确认发现：4 个」——同一份数据两个数字，交付时会被问住。

## ③ 必读事实
1. 统计函数已存在：`db/finding_cases.go:473 DistinctFindingStats(taskID string)`。
2. 页面早就用它：`server/server.go` 列表 DTO 的 `DistinctFindings`。
3. 报告入口：`server/server.go` 的 `getReport`，此前喂 `len(ListByKind(KindFinding))` 的原始条数。

## ④ 改哪
- `report/report.go`：`Input` 加 `Distinct *db.FindingDistinctStats`；摘要行分支。
- `server/server.go`：新增 `distinctStats(s, taskID)` 并传入 `Input.Distinct`。

## ⑤ 怎么改
见实际 diff（28 行）。`Distinct == nil` 时输出与旧版完全一致（保证向后兼容）。

## ⑥ 白名单
```whitelist
report/report.go
server/server.go
docs/06-任务卡/CARD-A01.md
```

## ⑦ 禁止
```forbidden
web/src/app/(main)/function/capabilities/page.tsx
agent/tools.go
```

## ⑧ 验收
```
go build ./... && go test ./report/ -count=1 && go vet ./report/ ./server/
```
G3（待装机）：`curl -s 'http://127.0.0.1:8787/api/report?task=2' | grep 确认发现` → 期望「3 个独立漏洞（原始上报 4 条）」。

## ⑨ 已知坑
1. `t.ID` 是 string，`DistinctFindingStats` 也收 string —— 不要 ParseInt。
2. 报告后面还拼了 `CapabilitySection`，改摘要别挤掉这段。
3. `s.m.PG()` 可能为 nil —— 必须回退，不许 panic。

## ⑩ 停止条件
若 `DistinctFindingStats.Total` 语义不是「独立漏洞数」→ 停下回报，不改它的 SQL。

## ⑪ 完成回执
diff 行数、三条命令输出、装机后 grep 结果。
