# CARD-B01「能做到什么」tab（画布）

> 先读：`docs/06-判定标准-冻结.md`、**`docs/08-详设-B01-能做到什么.md`（全文，13 项，是唯一规格来源）**
> 依赖：CARD-000 完成 ｜ 可与 CARD-C01 并行

## ① 目标
在任务详情新增第 12 个 tab「能做到什么」：画布展示 入口→能做到→拿到了什么→下一步 / 卡住了 / 已排除，四色体检 + 已拿下标记 + 决策选项 + 复制/下发指令 + 按需 AI 细化（按钮触发，后台跑，结果回填）。

## ② 为什么
`发现` 只回答"有几条洞"；`能力菜单` 是跨任务、按能力分组的商业裁决，结构上拿不到 `exploration_nodes`，画不出链路；`探索链路` 是"它做了什么"。三者都不回答"我手里有什么 + 下一步派什么活"。

## ③ 必读事实（不要重新推理）
1. 图库已存在：`@xyflow/react ^12.3.0`，**不要引入新图库**；布局可照抄 `web/src/components/exploration-graph.tsx` 的 barycenter 段。
2. TABS 已有 `cando` 项（CARD-000 加的），本卡只实现组件与后端。
3. 状态判定只用这三个源：`exploration_nodes.state`、`activity`（最近 fact 时间）、`task_error_events.class`。
4. 负结论用现成的 `s.negativesFromTask(t)`（报告在用），**不许自己查**。
5. 下发走 `POST /api/tasks/{id}/hint`（CARD-000 未建则该卡负责补；实现时把 `agent/tools.go:1743 addHint` 的落库逻辑抽成 `server.addHintForTask` 共用，**不许复制写库代码**）。
6. 所有规格细节（状态机、字段映射、失败态、成本、验收）以 `docs/08` 为准，**本卡不重复**。

## ④ 改哪
- 新增：`server/cando.go`（GET cando / POST annotate / GET annotate）
- 新增：`web/src/app/(main)/function/tasks/detail/_tabs/cando-tab.tsx`
- 修改：`server/cando.go`（CARD-000 的 3 条 501 占位换成真实 handler，并加 `/cando/send`）
- 修改：`web/src/app/(main)/function/tasks/detail/page.tsx`（import + `<TabsContent value="cando">` 挂载）

## ⑤ 怎么改
按 `docs/08`：
- §5 DDL 已由 CARD-000 建表；本卡只读写
- §7 字段来源映射表逐行实现，**映射不出来显示「未确认」，不许猜**
- §4 状态机（6 节点态 + 5 按钮态）逐条实现
- §9 成本：annotate 缓存键 `(task_id,node_key,evidence_hash)`，命中直接返回；每任务 ≤20 次；同任务并发 1、排队 1
- §10：含不可逆动词（改数据/写文件/删/上传）时「直接发给它」置灰

## ⑥ 白名单
```whitelist
server/cando.go
server/loot.go
server/loot_ai.go
server/loot_extract_test.go
server/server.go
db/migrations/0007_loot_explain.sql
web/src/app/(main)/function/tasks/detail/_tabs/loot-panel.tsx
web/src/app/(main)/function/tasks/detail/_tabs/cando-tab.tsx
web/src/app/(main)/function/tasks/detail/page.tsx
web/src/lib/api.ts
docs/08-详设-B01-能做到什么.md
docs/12-功能台账.md
docs/06-任务卡/CARD-B01.md
```

## ⑦ 禁止
```forbidden
web/src/app/(main)/function/capabilities/page.tsx
report/report.go
agent/tools.go
db/migrations/0005_cando_review.sql
server/review.go
web/src/app/(main)/function/tasks/detail/_tabs/sessions-tab.tsx
```

## ⑧ 验收
```
go build ./... && go vet ./server/ && go test ./server/ -count=1
cd web && npx tsc --noEmit
bash scripts/gate/diff-whitelist.sh docs/06-任务卡/CARD-B01.md
```
G3（必须交截图 + 时间戳，步骤见 `docs/08` §12）：
打开 tab → 3 秒内说出「能不能进系统」→ 点节点看清单 → 复制指令粘进会话发送 → 点「让它详细分析」→ 切走再回来看到内容替换（带 ✦ 与时间）→ 停 PG 后刷新显示"数据取不到 + 重试"。

## ⑨ 已知坑（照 `docs/08` §13，另加）
1. **`tasks.status` 从不写 `running`**，判断"在跑"用 `activity` 最近时间。
2. endpoint 的 `label` 可能重复 → lane 用 `label+id` 去重。
3. `capabilities.impact` 目前几乎全空 → B1 模板必须有降级，不能等 B2。
4. React Flow 的 Handle 要 `opacity-0`（照抄探索链路），否则卡片边缘出现小圆点。
5. `evidence_hash` 用 finding/fact id 排序后 sha256，**不含时间**（含时间缓存永远失效）。

## ⑩ 停止条件
- `capabilities.proof_level` 无法区分「已拿下」→ 停下回报，改用 `findings.status='confirmed'`。
- `negativesFromTask` 返回结构与预期不符 → 停下回报。
- endpoint 与 finding 的关联缺失超过半数 → 停下回报，lane 归并规则需重定。

## ⑪ 完成回执
`git diff --stat`、四条验收命令输出、G3 六步截图路径与秒数、`cando_annotations` 行数。
