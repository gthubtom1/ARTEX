# CARD-C01 复盘官（A2：常驻 + 触发 + 笔记 + 可反驳）

> 先读：`docs/06-判定标准-冻结.md`、**`docs/09-详设-C01-复盘官.md`（全文，13 项，唯一规格来源）**
> 依赖：CARD-000 完成 ｜ 可与 CARD-B01 并行

## ① 目标
在会话 tab 加胶囊开关；开启后左栏常驻「◈ 复盘官」，按条件/定时/手动触发，读**结构化快照 + 自己的笔记**判断整条方向，产出人话结论卡（投递到它自己会话 + 主 Agent 会话只读卡 + 顶部监工条一行），用户可采纳/反驳，采纳有回执与效果账本。

## ② 为什么
`adviser`（LLM 顾问）只把纠偏写成 hint 节点给 planner，**人在界面零可见**（实测 45 分钟不可见）；`verifier` 只判单条证据真假；`stall` 只出数字不出人话与动作。

## ③ 必读事实（不要重新推理）
1. 复用 `agentKey=adviser`（不新建 agent 行）：模型在「系统 → Agent」页绑定，走既有优先级链。
2. **顾问提示词是代码常量** `adviserSystemPrompt`（`server/adviser_llm.go
server/intercept.go`），Agent 页那份对它**不生效** —— 本卡必须改成「先读 `agent_prompts` 当前版本，取不到回退常量」，否则用户改了以为生效。
3. 冷却器已有：`adviserGate`（`server/adviser_llm.go
server/intercept.go`），按任务冷却，可直接复用。
4. 规则版兜底已有：`internal/adviser`（`Diagnose`）+ `internal/stall`，模型失败时用它们出「规则版」结论。
5. `pause` 会打断流式输出并留 `aborted_streaming` 坏账（实测 3 条）→「停下来聊」用**软停**（不再派新意图），**不许调 pause**。
6. 表已由 CARD-000 建好（`review_notes` / `review_runs` / `review_feedback`）。

## ④ 改哪
- 新增：`server/review.go`（8 个 handler：review 提交/latest/runs/apply/rebut/notes）
- 新增：`internal/review/snapshot.go`（快照生成，**不含对话/thinking/推理**）
- 新增：`internal/review/notes.go`（笔记读写 + 乐观锁 version）
- 新增：`web/src/app/(main)/function/tasks/detail/_tabs/review-panel.tsx`（胶囊 + 左栏行 + 结论卡）
- 修改：`server/server.go`（把 501 占位换成真实 handler，仅那 5–8 条）
- 修改：`web/src/app/(main)/function/tasks/detail/_tabs/sessions-tab.tsx`（挂胶囊、左栏一行、右栏渲染）

## ⑤ 怎么改
按 `docs/09`：
- §7 快照 11 字段逐条实现；**硬规则：快照里不出现对话、thinking、worker 推理**
- §5.1 笔记固定六行 ≤400 token；写冲突基于最新版本重写
- §4 两个状态机（介入 6 态 / 结论 5 态）
- §8 失败态：45s 超时 / 429 / 5xx → `failed` + 规则版结论，不弹红字
- §10 权限三档；`apply` **只允许** `kill_intent` / `retry_intent` / `set_next_stage`；**永不**改证据、改目标、收工
- §12 G3 六步验收

## ⑥ 白名单
```whitelist
server/review.go
server/server.go
server/adviser_llm.go
server/intercept.go
internal/review/snapshot.go
internal/review/notes.go
web/src/app/(main)/function/tasks/detail/_tabs/review-panel.tsx
web/src/app/(main)/function/tasks/detail/_tabs/sessions-tab.tsx
web/src/lib/api.ts
docs/06-任务卡/CARD-C01.md
```

## ⑦ 禁止
```forbidden
web/src/app/(main)/function/capabilities/page.tsx
report/report.go
agent/tools.go
db/migrations/0005_cando_review.sql
server/cando.go
web/src/app/(main)/function/tasks/detail/_tabs/cando-tab.tsx
```

## ⑧ 验收
```
go build ./... && go vet ./server/ ./internal/review/ && go test ./server/ ./internal/review/ -count=1
cd web && npx tsc --noEmit
bash scripts/gate/diff-whitelist.sh docs/06-任务卡/CARD-C01.md
```
G3（截图 + 时间戳，见 `docs/09` §12）：
开启胶囊（见"常驻"徽标）→ 手动看一次（≤45s 出卡，含置信度与 cites 数）→ 点「按它说的做」→ 30s 内被标死的 intent 变 `stopped` → 点「反驳」→ 30s 内改口且必须含「旧结论 + 推翻证据」→ 关掉后再等 10 分钟 `llm_usage` 无新增 → 断网时出「规则版」而非空白。

## ⑨ 已知坑
1. **不许新建 agent 行**；只在 `tierAgents` 之外复用现有 `adviser` 绑定。
2. 评审调用必须写 `llm_usage.worker='adviser'`，否则成本归因失真（现有 42 条空值）。
3. 并发评审会让笔记互相覆盖 → 互斥锁 + 版本号，冲突重写不静默覆盖。
4. 结论卡的证据引用必须是真实 id（`intent_id`/`finding_id`），不许写「某条注入」这种描述。
5. 主 Agent 会话里的投递卡是**只读**，点击才转成动作，不许自动执行。
6. 关掉开关后必须真的不跑：触发处先查 `enabled`，并验证 `llm_usage` 不增。

## ⑩ 停止条件
- 同一快照两次评审 verdict 相反（continue ↔ wrapup）→ 停下回报：快照信息不足，需重设字段而不是加次数。
- 模型 JSON 不可解析率 >20% → 停下回报，改「先人话后结构」双段输出。
- `negativesFromTask` / `assets_owned` 结构与预期不符 → 停下回报。

## ⑪ 完成回执
`git diff --stat`、三条验收命令输出、G3 六步截图路径、`review_runs` 条数与 `review_notes.version`、关掉后 `llm_usage` 增量为 0 的查询结果。
