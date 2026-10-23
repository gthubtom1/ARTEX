# CARD-000 骨架（迁移 + 路由占位 + 前端接口 + TABS 一行）

> ⚠️ **已完成（2026-10-23）；其中「TABS 加一行 cando」已被回退**：任务详情现在**没有** `能做到什么` tab，
> 入口改为会话右栏的「战果」面板。本卡仅作历史记录，不要再照着加 tab。

> 先读：`docs/06-判定标准-冻结.md`、`docs/07-开发流水线.md` §2
> 依赖：无 ｜ 独占共享文件，**必须先于 B01/C01 完成**

## ① 目标
一次性把 B01（地图）与 C01（复盘官）需要的**共享改动**做完：两张表、两组路由占位、两个前端 api 函数、TABS 加一行。之后两个功能各自只写新文件，零冲突。

## ② 为什么
B01 与 C01 都要改 `server/server.go`（路由注册）和 `db/migrations`。并行子代理会撞车。先把共享文件改完，再并行。

## ③ 必读事实（不要重新推理）
1. 迁移目录：`db/migrations/`，最新是 `0004_p8.sql`，启动时自动执行（`db/migrate`）。
2. 路由注册集中在 `server/server.go` 里 `HandleFunc("/api/tasks/...")` 一段（同类路由相邻）。
3. 前端 api 封装在 `web/src/lib/api.ts`，按 `p2api` / `p8api` 分组，同一文件既有 `get/post` helper。
4. TABS 在 `web/src/app/(main)/function/tasks/detail/page.tsx` 的 `const TABS`，「发现」条目之后插入。
5. 表结构以 `docs/08` §5 与 `docs/09` §5 的 DDL 为准，**逐字照抄**。

## ④ 改哪
- 新增：`db/migrations/0005_cando_review.sql`
- 修改：`server/server.go`（只加路由注册 + 每个 handler 返回 501 占位）
- 修改：`web/src/lib/api.ts`（加 `cando(taskId)` / `reviewLatest(taskId)` / `reviewRun(taskId)` 等 6 个函数，指向上面路由）
- 修改：`web/src/app/(main)/function/tasks/detail/page.tsx`（`const TABS` 加一行 `{ value: "cando", label: "能做到什么" }`，插在「发现」之后）—— ⚠️ **已回退（2026-10-23）：该行已删除，当前 TABS 共 11 项）**

## ⑤ 怎么改
迁移文件：把 `docs/08` §5 的 `cando_annotations` + `docs/09` §5 的 `review_notes` / `review_runs` / `review_feedback` 三段 DDL 原样拼入（全部 `CREATE TABLE IF NOT EXISTS` + 索引 `IF NOT EXISTS`）。

路由占位（6 条，与详设路径一致）：
```
GET  /api/tasks/{id}/cando                 -> 501 {"error":"not implemented"}
POST /api/tasks/{id}/cando/annotate        -> 501
GET  /api/tasks/{id}/cando/annotate        -> 501
POST /api/tasks/{id}/review                -> 501
GET  /api/tasks/{id}/review/latest         -> 501
POST /api/tasks/{id}/review/{run}/apply    -> 501
POST /api/tasks/{id}/review/{run}/rebut    -> 501
GET  /api/tasks/{id}/review/notes          -> 501
```
占位 handler 统一叫 `notImplemented(w)`，写在同文件底部。

前端 api：只加函数签名与路径，**不写渲染逻辑**。

## ⑥ 白名单
```whitelist
db/migrations/0005_cando_review.sql
server/server.go
server/cando.go
server/review.go
web/src/lib/api.ts
web/src/app/(main)/function/tasks/detail/page.tsx
docs/00b-卡片线-进度与任务卡.md
docs/06-任务卡/CARD-000.md
docs/06-任务卡/CARD-A01.md
```

## ⑦ 禁止
```forbidden
web/src/app/(main)/function/capabilities/page.tsx
report/report.go
agent/tools.go
server/adviser_llm.go
```

## ⑧ 验收
```
go build ./...
go test ./server/ ./db/ -count=1
# 迁移在真库上跑一遍（VM 内）：
sudo -u postgres psql -d artex -c "\dt cando_annotations" -c "\dt review_runs" -c "\dt review_notes" -c "\dt review_feedback"
# 期望：4 张表都存在
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8787/api/tasks/2/cando   # 期望 501
```
前端：`cd web && npx tsc --noEmit` 必须 exit 0。

## ⑨ 已知坑
1. 迁移必须幂等（`IF NOT EXISTS`），否则重启即报错。
2. `server/server.go` 的 `{id}` 路由用的是同一种解析方式，照抄相邻几条，别自己写 mux。
3. 前端 `api.ts` 里 `get/post` 的泛型签名要与现有函数一致，否则 `tsc` 报错。
4. TABS 顺序：插在「发现」之后、「任务情报」之前；顺序错会导致 tab 高亮错位（用 `value` 匹配，不用索引）。

## ⑩ 停止条件
- 若 `0004_p8.sql` 之后已有其它未提交迁移 → 停下回报，编号改为实际下一个。
- 若路由注册不是集中在一处（分散多个文件）→ 停下回报，附上位置。

## ⑪ 完成回执
`git diff --stat`、三条验收命令输出、`\dt` 四张表结果、`tsc` 结果。
