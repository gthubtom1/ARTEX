# CARD-D01 生产端自动写入钩子（引擎写，不靠模型调工具）

> ⚠️ **本卡已超范围交付（2026-10-23）**：当时写的“本卡不做 H3/H5”已不成立——H3（能力级别→P2）
> 和 H5（立足点→`assets_owned`）现在都在 `db/hooks.go` 里，并已接线（`agent/tools.go:1390-1396`）。
> 现行状态：H1 ✅ / H3 ✅ / H4 ✅ / H5 ✅；H2、H6 **明确跳过**。

> 先读：`docs/00-§7`、`docs/06-判定标准-冻结.md` §二-3
> 依赖：无（独立于 B/C）

## ① 目标
把 `evidence_timeline` / `assumptions` 两张空表接上：由引擎在 `record_fact` / `report_finding` 落库后直接写，不依赖模型记得调工具。

## ② 为什么
实测 45 分钟：`record_capability` / `owned_register` / `route_resolve` / `record_assumption` / `bind_finding_traffic` **各 0 次**；`evidence_timeline`、`assumptions` 各 0 行。模型在真实任务里只 Bash。

## ③ 必读事实
1. 表结构以 VM 实查为准：`evidence_timeline(task_id,finding_id,node_id,kind,content_ref,sha256,actor,meta)`；`assumptions(task_id,text,status,evidence_node,created_round,resolved_round)`。
2. `routes` 表是**网络路由**（cidr / via_owned / hops），**不是 URL 路由** —— 不要往里面塞 url+param（这条曾被我误判，已纠正）。
3. 能力卡证明级别升级 ~~需要 `vulnclass → taxonomy item` 的映射，**目前不存在** → 本卡不做 H3~~ → **已做**：`db.ProofKeyword`（关键词映射）+ `BumpCapabilityProof`（P1→P2/observed，只升不降）。
4. ~~`assets_owned` 需要立足点判定（shell 回显/写文件成功），引擎侧没有现成信号 → 本卡不做 H5~~ → **已做**：`db.IsFootholdSignal` + `RegisterFoothold`（幂等 `asset_key='asset:<id>'`）。

## ④ 改哪
- 新增：`db/hooks.go`（`NoteEvidence` / `NoteAssumptionIfNegative` / `ProofKeyword` / `IsFootholdSignal` / `BumpCapabilityProof` / `RegisterFoothold` / `AssertReconUsedTool`）
- 修改：`agent/tools.go`（`record_fact` 与 `report_finding` 各一处 1–3 行）

## ⑤ 怎么改
- `NoteEvidence`：按 `(task_id, sha256, kind)` 幂等插入；失败只返回 error，调用方吞掉。
- `NoteAssumptionIfNegative`：事实摘要命中否定词表（无差异/未成立/不可利用/没有回显/不支持/不存在/未发现/未启用/已关闭/拒绝/失败）时，按 `(task_id,text)` 幂等写入 assumptions。
- 调用点：`recordFact` 的 `ids[i]=id` 之后；`addFinding` 的 `t.writes.Findings++` 之后。

## ⑥ 白名单
```whitelist
db/hooks.go
agent/tools.go
docs/06-任务卡/CARD-D01.md
```

## ⑦ 禁止
```forbidden
web/src/app/(main)/function/capabilities/page.tsx
report/report.go
server/review.go
server/cando.go
```

## ⑧ 验收
```
go build ./... && go vet ./db/ ./agent/ && go test ./db/ -count=1
bash scripts/gate/diff-whitelist.sh docs/06-任务卡/CARD-D01.md
```
G5（需要一次真实任务）：跑完后
```
select count(*) from evidence_timeline where task_id=N;   -- 应 ≥ findings 数
select count(*) from assumptions where task_id=N;          -- 应 ≥1（有否定结论时）
```

## ⑨ 已知坑
1. `agent/tools.go` 里有两处 `ids[i] = id`（addIntent 与 recordFact），**必须用 `if intelligenceEnabled()` 作为锚点**，否则插错函数（本次已踩）。
2. 钩子必须在主流程成功之后调用，且吞掉错误 —— 不许因为审计写失败而让 `record_fact` 报错。
3. 幂等键不含时间，否则每条都重复。

## ⑩ 停止条件
若 `evidence_timeline` 被 verifier 占用同一 `(task_id,kind)` 语义 → 停下回报，改用 `kind='fact'` 前缀区分。

## ⑪ 完成回执
diff、三条命令输出、G5 两条 SQL 结果（或说明需要新任务才有数据）。
