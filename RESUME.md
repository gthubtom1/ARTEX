# 接手说明（新会话从这里开始）

> **先读这三份，再决定做什么：**
> - `docs/00-总纲v2-唯一事实源.md` —— 唯一事实源（32 个机制的真实状态 + 14 条不可返工决定 + 12 处冲突裁决 + 作废清单）
> - `docs/01-零基础实战判定.md` —— 现在能打赢什么/打不赢什么 + 为什么
> - **`docs/02-P8执行手册.md` —— 施工图**（P8-0～P8-9，每条到文件/函数/表/伪代码/验收判据；含接力协议、执行顺序、回归清单、进度账）
>
> 其余设计文档已降级为历史资料（份数以 `docs/README.md` 索引为准），**不要从头读一遍**。
>
> 零基础使用/操作流程（点哪里、说什么话、四种目标分支）：见根目录 **`使用教程.md`**。

> 换会话/换模型/上下文压缩后用这一页续接。**不需要旧对话 ID**——进度全在磁盘上。

## 2026-10-23 卡片线（B01/C01/D01/E01）—— 最新一页

> 这一页比下面的 P8 状态新。**接手先看这里，再看 P8。**

| 卡 | 做了什么 | 现在在哪看 |
|---|---|---|
| A01 | 报告改用「独立漏洞」计数 | `/api/report` 摘要行 |
| 000 | `0005_cando_review.sql` 四表 + 路由占位 + api | —（TABS 那一行已回退） |
| **B01** | 删掉「能做到什么」tab；改为**会话右侧「战果清单」**（done/run/tried 三态、可拖拽详情、复制下一步指令、AI explain + suggest） | 任务详情 → 会话 → 右栏「战果」胶囊 |
| **C01** | **审查官**全链路：条件/定时触发、三档权限、自检五问、8 条防抖、90s ack、10 分钟观察窗 outcome、`propose_intent`、KB 经验 + 死路台账、提示词版本化重播、错误按 `intent_id` 归属 | 任务详情 → 会话 → 右栏「审查官」胶囊 |
| D01 | 生产端钩子 H1/H3/H4/H5（H2/H6 跳过） | `db/hooks.go` |
| E01 | L1 提示 + L2 工具改道 + L3 度量版 + 命中率 16 表**接口** | `guard/guard.go`、`GET /api/system/hitrate` |
| 另 | 任务模板 5 个种子、常用语（三个输入框）、会话头部模型胶囊改可切下拉 | `db/seed_templates.go`、`components/quick-phrases.tsx`、`sessions-tab.tsx` |

**状态一律以 `docs/12-功能台账.md` 为准**；交互协议看 `docs/13`/`14`/`15`；入口/并行切分看 `docs/00b-卡片线-进度与任务卡.md`。

2026-10-23 收口（B-05/06/07/08/09/B-10 stub 全做完）：

- 战果清单五态齐：`todo`（还没试）/ `na`（不适用）由人标，写 `loot_row_states`（`0011`），`POST /api/tasks/{id}/loot/state`；证据优先，`na` 不计入分母。
- 命中率看板有页面了：侧边栏 **系统 → 命中率**（`/system/hitrate`），16 表 0 行高亮 + 工具分布。
- 两套系统的**批量接口**：`triage/score.py`（打分/聚簇）+ `triage/dispatch.sh`（按 quickstart 派单），说明在 `triage/README.md`。
- `install.sh` / `update.sh` 按镜像来源分流（本地 build / 远程 pull），ASM 扫描节点锁 `1.9.1`。
- `scripts/stub-bootstrap.sh` 在没有 docker 的机器上把 bootstrap 全流程跑一遍（`STUB_RESULT PASS`）；它抓出并修掉了两个真 bug：`USER` 未定义时 `set -u` 直接崩、`status.sh` 迁移判据写死 `0010`。
- `docs/08-详设` 已作废。

---

## P8 状态（2026-10-22，v0.6.0-p8）

**代码层面 P8-0～P8-9 全部落地**，成品 `dist/artex.exe`（55MB，前端内嵌）。
逐格进度与证据在 `docs/02-P8执行手册.md` §9（39 格已勾，10 格需要外部环境）。

已完成（可复核）：

```
P8-0  db/migrations/0004_p8.sql + internal/errev（五类错误事件 + 4 个写入点）
P8-1  internal/bench + bench/cases(10 题) + bench/compose.yml + scripts/bench/run.py + /system/bench 页面
P8-3  internal/findinggate 三档闸 + agent/finding_gate.go（接进 report_finding）
      ★ internal/verify 新增 trace 判定：只有版本号/CVE 断言一律 rejected（原来会判 verified）
P8-4  POST /api/tasks/quickstart + GET /api/egress/check + 首页 QuickStartCard
P8-5  internal/ledger/nextstate（3 次失败→lost）+ frozen_asset_ids 落库与消费
P8-6  netguard.VerifyCached + guard.EgressCheck（Bash/WebFetch 也要过出口闸）
P8-7  creds.LoadOrCreateKey + envfilter（凭据密钥不再进子进程）+ /system/onboard 向导
P8-8  task_error_events + /api/tasks/{id}/errors + 任务详情顶部聚合条
P8-9  internal/llmmodel 三档 + /api/llm/tier-suggest
P8-2  内置默认人设 + 每请求现取 + 主对话/对话页全覆盖
```

**必须由人补的一次性验证**（写这份时的状态；多数已于 2026-10-22/23 在真 PG18 + 真靶场上验过，仍缺的看 `docs/02` §9 的最后两格）：

| 要验什么 | 怎么验 |
|---|---|
| 0004 迁移在真 PG 上执行 | 起 PG → 启动 artex → 看日志无迁移错误 |
| 3 个靶标（10 个用例复用） + 基线跑批 | `docker compose -f bench/compose.yml up -d` → `python scripts/bench/run.py --set smoke` |
| 贴 URL 3 分钟出第一张卡 | 首页 QuickStartCard 贴 `http://127.0.0.1:18083/`（DSVW） |
| 台账 3 次失败 → lost | 关掉靶标端口，等复查 3 轮 |
| 换目标后不再派单 | 能力菜单点"换目标"，看 planner 是否被拒 |
| 删 cred.key 重启旧凭据仍可解 | 删 `data/cred.key` 前先存一条凭据 |
| 拔代理 → 任务页出现"环境 ×N" | `interrupt` 代理后跑任务 |

验证命令（每次提交前）：

```bash
go build ./... && go vet ./agent/... ./server/... ./guard/... ./internal/...
go test ./agent/ ./guard/ ./internal/... -count=1
cd web && npx tsc --noEmit
python scripts/audit_links.py          # 必须 AUDIT_RESULT PASS
bash scripts/package_local.sh v0.6.0-p8
```

---

## 项目路径

```
C:\Users\toti\pi-cwd\20261020\artex-分析\mirror
```

## 新会话第一句话（直接复制粘贴）

```
读 docs/00-总纲v2-唯一事实源.md 、 docs/02-P8执行手册.md §9 进度账，按第一个 `[ ]` 往下做，不要问我问题，直接推进。
每完成一个子任务就在 §9 勾一行并写证据；每 20 分钟写一次 checkpoint。
每改一条功能，必须贴 P8-1 的基准对比，否则不许说“优化了”。
```

然后（需要 goal 模式时）先执行 `/goal-list` → `/goal-focus <id>`，把目标聚焦到：
**"完成 ARTEX 二开 P2–P6：GateChain/adviser、能力卡+三门校验、shell池+凭据+横移、台账复查、前端页面；每步 go build + go test 全绿。"**

## 当前进度（截至本文件更新时间）

| 阶段 | 内容 | 状态 |
|---|---|---|
| P0 | 13 张表 + assets 扩列 + 迁移 ledger（启动自动执行） | ✅ |
| P0 | settings / audit / errclass / idem / snapshot / dbiface | ✅ |
| P0 | 本地靶场 compose + 五项验收脚本 | ✅ |
| P0.5 | 接线：审计入 guard、幂等+快照包住写工具、错误分类进 engine | ✅ |
| P1 | netguard（出口验 IP）→ **P9 改为纯自检：Clash 切节点与 fail-closed 已删除** | ✅ 7 测试（P1 时） |
| P1 | env（五层工具发现 + 指纹 + 缓存） | ✅ 6 测试 |
| P1 | adapter（nmap/httpx/nuclei 结构化解析） | ✅ 10 测试 |
| P1 | OAST（HTTP + DNS 带外） | ✅ 4 测试 |
| P1 | provision（安装计划 + 降级链） | ✅ 5 测试 |
| P1 | governor（并发/超时/输出上限） | ✅ 4 测试 |
| P2 | MEMORY 三段式接力（读写已接进 worker） | ✅ 5 测试 |
| P2 | GateChain（去重+优先级，不拒绝） | ✅ 6 测试 |
| P2 | stall 检测器 + adviser（只 hint/steer/kill，每轮规划后自动跑） | ✅ 2 + 4 测试 |
| P3 | 能力卡（13 组 91 项，派生自 finding，白话渲染 + 覆盖度） | ✅ 7 测试 |
| P3 | 三门校验（grounding/negation/interrogation） | ✅ 6 测试 |
| P4 | 凭据库（AES-GCM + 指纹 + 复用计划） | ✅ 4 测试 |
| P4 | shell 池（跨 worker 会话租借） | ✅ 3 测试 |
| P4 | 提权决策（GTFOBins 排序）+ 横移/路由 | ✅ 3 + 3 测试 |
| P5 | assets_owned 台账 + 复查计划 + 战后清理 | ✅ 6 测试 |
| — | adapter 注册为 typed host tools（nmap/httpx/nuclei） | ✅ |
| P6 | 只读面板 API（capabilities/owned/export/progress/p2 audit/environment/network/knowledge/replay） | ✅ |
| P6 | 8 个前端页面（任务/菜单/台账/知识/环境/网络/审计/报告）+ 回放看板 | ✅ tsc + next build |
| P3 | 决策队列（三档裁决 + 落库 + hint + API/工具） | ✅ 6 测试 |
| P5 | 复查执行器（到期只读 nc/curl + 写回 + 活动日志） | ✅ 6 测试 |
| P7 | 注入层（拒答重定向 + 执行上下文块 + on-miss provider 装饰器 + 预算） | ✅ 11 + 6 测试 |
| P7 | 知识库 MCP（/mcp/kb：kb_search / kb_read） | ✅ 5 + 3 测试 |
| P7 | 多模型分工（adviser 顾问 + verifier 专判 + planner/worker/retester 独立绑定） | ✅ 4 测试 |
| P7 | 回放看板（两任务成本/产出对比 + transcript 真回放） | ✅ 5 测试 |
| M15 | 数据定位（表/行数/敏感列，D1–D4）+ 数据页 | ✅ |
| M12 | 凭据收割（配置/历史/env/云元数据 → 加密库） | ✅ |
| M18 | intercept 自动裁决（off/reviewer/allow_all，规则 ask 也覆盖） | ✅ |
| L5 | 案例自动沉淀（正/负 → kb/cases，sha 去重） | ✅ |
| M21 | verifier 联动降档 + 决策批量裁决 | ✅ |
| L3/L4 | 提权/横移/清理执行面（策略校验 + 自动登记台账/会话） | ✅ 5 测试 |
| 打包 | `scripts/package_local.sh` → `dist/artex(.exe)`（前端内嵌，替代需要 rsync 的 build.sh） | ✅ |
| 减法 | 4 次 0 产出方向代码层冻结；continue 预算到点自动停 | ✅ |
| 案例消费 | planner 每轮检索 kb/cases 注入正/负案例 | ✅ |
| 立足点 | raw shell（nc+哨兵）+ HTTP Basic 复用 | ✅ |
| 运行时 | 真 PG18 冒烟：capabilities/owned/verifier/report/knowledge 200；复查 verified；（P9 前）死代理 fail-closed paused —— P9 后改为只显形不阻断 | ✅ |
| 全量测试 | `ARTEX_PG_DSN=... go test ./... -count=1` exit 0（sed/python 平台 skip） | ✅ |
| 外部技能 | 236 技能分层：138 MIT 知识层（kb_search）+ 8 精选执行层（worker/pentest）+ 98 无许可排除 | ✅ |

## 验证（新会话开工前先跑一遍，确认交接没坏）

```bash
cd <仓库根目录>
go build ./...
go test ./internal/... ./db/... ./guard/... ./agent/...
```

期望：build 退出码 0；测试 **全部 ok**（P2~P5 新增 10 个包）。
一键：`bash scripts/accept_p2.sh`（build + vet + 全量无库单测）
      `bash scripts/accept_p6.sh`（前端 tsc + next build + 9 页面存在性 + 新包单测）

## P6–P7 新增（详见 `docs/P6-P7-实现说明.md`）

```
internal/decision    三档裁决（evidence/continue/switch）        decide_capability / POST /api/capabilities/{id}/decide
internal/recheck     到期台账只读复查（nc/curl）                   POST /api/owned/recheck + 后台 recheckLoop
internal/inject      拒答检测 + 可换词重定向 + token 预算          agent/inject_hook.go（Stop hook）
internal/kb          知识库检索/读取（路径穿越防护）                POST /mcp/kb（MCP）+ GET /api/knowledge
server/adviser_llm   LLM 顾问（agentKey=adviser，10 分钟冷却）     失败自动回落确定性规则
server/replay_api    两任务成本/产出对比                          GET /api/replay/compare + /system/replay
web 新页面           能力菜单/台账/知识/报告/审计/环境/网络/回放
```

前端依赖：本机 `NODE_ENV=production` 时 `npm ci` 会跳过 devDeps；
正确命令 `NODE_ENV=development npm ci --include=dev --ignore-scripts`。

## 实机验收（DSVW，2026-10-22）

- 靶场：GitHub `stamparm/DSVW`，`http://127.0.0.1:65412`（单文件 Python；Juice Shop 因 sqlite3 原生模块在 Node 24/22 构建失败且本机无 docker 未用）。
- 模型：`openai` 兼容端点 cpa / `deepseek-v4.1-flash`（运行时 env 注入，不入库）。
- 首次确认战果（任务创建 → 首个 finding）：**81.9s**（#1）、**56.5s**（#2）、**52.1s**（#4）；总时长 92.3s / 64.8s / 85.8s。
- 任务 #4：finding#5 路径穿越/任意文件读取（high，含 win.ini 与 dsvw.py 源码 PoC）；2 张能力卡（4.1 命令执行 P3、5.1 任意文件读取 P3）；`assets_owned#1` 127.0.0.1:65412 webshell status=verified；`/api/report?task=4` 含「能力菜单」与「持有资产台账」。
- 证据文件：`docs/实机验收-DSVW.md`（由真实 DB 数据生成：计时/finding 证据/能力卡/台账/复查记录）。
- 全量测试：`ARTEX_PG_DSN=… go test ./... -count=1 -p 1` → exit 0（并行跑曾出现一次 PG 40P01 死锁 flake；单包复跑绿）。

## 本轮新增（P2–P5，详见 `docs/P2-P5-实现说明.md`）

```
internal/gatechain   意图去重+优先级（不拒绝，只合并）        rank_intents
internal/stall       停顿检测（轮数/速度/重复度/层缺口/预算）  diagnose_progress
internal/adviser     顾问 hint/steer/kill（确定性规则 + Prompt）
internal/capability  13 组清单 + 派生 + 白话渲染 + 覆盖度      record_capability / list_capabilities
internal/verify      三门校验 → verified/inferred/rejected    verify_claim
internal/creds       AES-GCM + 指纹去重 + 复用计划             cred_add/list/reuse_plan/verify
internal/access      跨 worker 会话池                          session_register/lease/list/release
internal/privesc     LinPEAS/WinPEAS → GTFOBins                privesc_plan
internal/lateral     横移方案 + 跳板选路                       lateral_plan / route_plan
internal/ledger      assets_owned 台账/复查/清理/导出          owned_register/list/due/check/cleanup/export
agent/p2.go          工具注册 + DiagnoseProgress + SyncCapabilities
agent/adapter_tools.go  nmap_scan / httpx_probe / nuclei_scan（结构化 + 原始输出落盘）
server/progress.go   每轮规划后自动顾问纠偏 + 能力卡派生 + 台账到期提醒
server/p2_api.go     GET /api/capabilities|owned|owned/export|progress|p2/audit
```

凭据密钥：`.env.example` 的 `ARTEX_CRED_KEY`（`openssl rand -hex 32`；留空=每次启动一次性密钥）。

## 已落地的目录（别重复造）

```
db/migrations/0001_p0_schema.sql   13 张表（幂等）+ db/migrations.go（ledger runner）
internal/settings audit errclass idem snapshot dbiface netguard env adapter oast provision relay
（`internal/governor` 已 `git rm`，别再去引用）
agent/relay_hook.go（MEMORY 接力）  agent/assembly.go（幂等+快照包装）
guard/guard.go（审计 sink + 工具存在性钩子）
server/server.go（wireP0：settings/audit/idem/snapshot/netguard/env）
server/engine.go（errclass 分类日志；出口校验的 fail-closed 已被 P9 移除，改为启动前自检 + 只告警）
fixtures/docker-compose.yml        靶场（DVWA/JuiceShop/VulHub，全绑 127.0.0.1）
scripts/accept_p0.sh               五项验收
docs/00-总纲v2-唯一事实源.md      唯一事实源（机制状态 / 决定 / 作废清单，冲突以它为准）
docs/最终方案-v1.md                历史总纲（已收编，仅留一页摘要）
docs/进度台账.md                   活文档：完成情况 + 设计决定 + 待验证项
```

## 8 条不可返工的设计决定（改前必读）

1. **零权限门**：不做开关/分级；提示词不出现"禁止"；目标清单与出口只放在**网络层**（模型看不见）。
2. **默认全开 + 可回滚**：写文件/改配置前快照。
3. 迁移走 `schema_migrations` ledger；文件内 `BEGIN/COMMIT` 会被 runner 剥离。
4. `local_tools_cache`（不叫 `tools_cache`，避免与 `mcp_tools_cache`/`tools` 混淆）。
5. `creds_vault` 只存加密秘密；`task_intelligence(kind=credential)` 保留叙述性情报。
6. 交付物表（capabilities/assumptions/evidence_timeline）任务外键 `SET NULL`。
7. `audit_log` 无外键、只追加。
8. 幂等键 = `keyFor(task,intent,step,tool,input)`；重复写回返回"已存在"而不是报错。

## 环境前置（不是“只做一次”，是每次换环境都要满足）

起 PostgreSQL，把 `ARTEX_PG_DSN` 指过去（例：`postgres://artex:pass@127.0.0.1:5432/artex?sslmode=disable`），
然后 `bash scripts/accept_p0.sh` 应输出"通过 5 / 失败 0"。迁移会在应用首次启动时自动执行（目前 0001–0010）。
另外宿主层必须能出网；任务不再因出口失败 `paused`（P9 后只自检，见 `docs/00-总纲v2` §7 第 4 项）。
