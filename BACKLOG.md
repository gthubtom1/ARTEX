# BACKLOG（唯一待办）

> 由 `doc-hygiene` 遗留盘点产生。**一条遗留只能落一个分类**；仍需要的都在这张表里，别处不再散落待办。
> 更新纪律：做完就在「状态」写 ✅ + 证据；放弃就写 ❌ + 原因。空表 = 项目无待办。

| id | 事项 | 来源 | 仍需要的理由 | 验收判据 | 优先级 | 状态 |
|---|---|---|---|---|---|---|
| B-01 | 在「系统 → Agent」把 **审查官** 绑到最强模型 | `docs/14` §0、本次审计 | 现在"跟随任务/全局"，任务 pin 到便宜模型时审查官跟着降智 | Agent 页审查官「默认模型」≠ 跟随；跑一次 review，`review_runs.model` 不是 `rule` | P0 | ⬜ |
| B-02 | 把 planner / mainagent 也绑强模型，worker 保持便宜档 | `docs/12` §三 | 现在所有角色同一个脑子，方向错一次最贵 | `llm_profiles` 里至少两个不同 profile 被 `agents.llm_profile_id` 引用 | P0 | ⬜ |
| B-03 | 用一个**真实新任务**跑两轮，判断审查官建议值不值得留 | `docs/15` §5（退出条件） | 它是否保留取决于"介入后 3 轮 fact velocity 是否上升"；目前只有 1 个构造任务的数据 | 新任务里 `review_runs` 至少 2 条 `level=action`，且 `outcome` 中 effective 不为 0 | P0 | ⬜ |
| B-04 | `git push`（本地 680+ commit 未推） | 本次审计 | 换机/协作/备份都依赖远端 | `git status` 显示 "up to date with origin/main" | P1 | ⬜ |
| B-05 | `install.sh` / `update.sh` 改走新路线 | deploy 审计结论 §3-1 | 两者仍 `docker compose pull`，而默认镜像是本地构建 → `set -e` 直接中断 | 干净机器跑 `install.sh` 能起到 8787；或改为转调 `deploy/bootstrap.sh` | P1 | ⬜ |
| B-06 | 锁死 ASM 扫描节点版本 | deploy 审计结论 §3-3 | `ASM_SCAN_TAG=latest` 会随上游漂移 | `docker-compose.yml` 里两个 asm 镜像都用 digest 或明确 tag | P2 | ⬜ |
| B-07 | 战果清单补「还没试 / 不适用」两态 + 「标为不适用」 | `docs/12` §二 | 目前没证据的行不显示；想"标记这条与我无关"没入口 | 能把一行标成 na 并持久化，刷新后仍为 na | P2 | ⬜ |
| B-08 | 战果「✦ 补这个站特有的项」前端按钮 | `docs/12` §二 | 后端 `/loot/suggest` 已通，前端无入口 | 点按钮后列表追加 `AI 推断·待确认` 行且不计入 done | P2 | ⬜ |
| B-09 | 命中率做成页面 | `docs/12` §三 | 现在只有 `/api/system/hitrate` 接口；判断"哪些机制是空表"要手跑 SQL | `/system/bench` 或新页能看 16 表数字 | P2 | ⬜ |
| B-10 | 在干净机器上实跑一次 `deploy/bootstrap.sh --with-asm` | deploy 审计结论 | 静态审查 + 语法全过，但**没真跑过**；"一条命令装两套"还没被证明 | 新机器上 `bash deploy/status.sh` 输出 `STATUS PASS`，且 wire 后 MCP 工具数 >0 | P1 | ⬜ |

---

## 不做（已放弃，留证）

| 事项 | 原因 |
|---|---|
| H2 流量绑定 / H6 URL 路由 | 引擎拿不到真实流量；`routes` 表是网络路由不是 URL（`docs/00b` §7） |
| E01 L3 硬约束（阻断版） | intent 无 stage 字段，硬阻断会让任务停摆 → 已改度量版（`db.AssertReconUsedTool`） |
| 敏感数据打码 | 用户明确不做 |
| 新建任务表单只留「模板 + 网址」 | 主用法是对话派活，不简化表单 |
| 为"还没试"的行强行补数据 | 没证据的行不显示，是设计不是缺陷 |

---

## 待确认（需 owner 一句话）

| 事项 | 需要谁回答什么 |
|---|---|
| `docs/06 冻结表` 作废条目的措辞（"作废（用户改口）" vs "临时例外"） | 用户确认一次即可落定 |
| 是否把 `Dockerfile`（预编译路线）也补 `jwt.key → data/jwt.key` 软链 | 只有切到该路线才需要，问 owner 是否保留这条路 |
