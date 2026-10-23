# CARD-B01「能做到什么」tab（画布）

> ⚠️ **本卡已交付，但交付形态与本卡描述不一致；卡片正文作废。**
> 「任务详情新 tab + 画布」（`_tabs/cando-tab.tsx`）**已删除**，实际落地为**会话右侧第三个面板「战果」**
> （`_tabs/loot-panel.tsx` + `server/loot.go`/`loot_ai.go`，迁移 `0007_loot_explain.sql`）。
> 现状与缺口看 `docs/12-功能台账.md` §二；`docs/08-详设` 已标作废。
> 卡内白名单里的 `cando-tab.tsx` 已不存在；`sessions-tab.tsx` 也不再是禁止修改文件（三个面板互斥就在它里面）。

> 先读：`docs/06-判定标准-冻结.md`、**`docs/12-功能台账.md` §二（现状与缺口，唯一依据）**；
> `docs/08-详设-B01-能做到什么.md` 已标作废，只可当“字段从哪张表来”的参考，**不要按它的界面实现**。
> 依赖：CARD-000 完成 ｜ 可与 CARD-C01 并行

## ① 目标

~~在任务详情新增第 12 个 tab「能做到什么」~~ → **实际交付**：会话右侧同一个方框里的第三个面板「战果」（战果清单表格，done/run/tried 三态 + 详情层 + 复制下一步指令 + AI explain/suggest）。

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
- 新增：`server/cando.go`（GET cando / POST annotate / GET annotate；后续又加了 `/cando/send`）
- 新增：`server/loot.go` + `server/loot_ai.go`（战果清单 / `/loot/explain` / `/loot/suggest`）+ 单测
- 新增：`web/src/app/(main)/function/tasks/detail/_tabs/loot-panel.tsx`（**当前真实入口**）
- ~~新增 `_tabs/cando-tab.tsx`~~ → 已删除，不要再建
- 修改：`web/src/app/(main)/function/tasks/detail/_tabs/sessions-tab.tsx`（胶囊 + 三面板互斥切换）

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
server/loot_prompt_test.go
server/server.go
db/migrations/0007_loot_explain.sql
web/src/app/(main)/function/tasks/detail/_tabs/loot-panel.tsx
# web/src/app/(main)/function/tasks/detail/_tabs/cando-tab.tsx   ← 已删除（2026-10-23），不要再建
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
