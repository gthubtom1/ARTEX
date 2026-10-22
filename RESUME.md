# 接手说明（新会话从这里开始）

> 换会话/换模型/上下文压缩后用这一页续接。**不需要旧对话 ID**——进度全在磁盘上。

## 项目路径

```
C:\Users\toti\pi-cwd\20261020\artex-分析\mirror
```

## 新会话第一句话（直接复制粘贴）

```
读 RESUME.md 和 docs/进度台账.md，按里面的"下一步"继续做 ARTEX 二开，不要问我问题，直接推进。
目标：把它做成"给一句话就能自主推进、有证据、能长期持有资产"的渗透平台（零基础可用）。
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
| P1 | netguard（出口验 IP/切节点/fail-closed） | ✅ 7 测试 |
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
| 运行时 | 真 PG18 冒烟：capabilities/owned/verifier/report/knowledge 200；复查 verified；死代理 fail-closed paused | ✅ |
| 全量测试 | `ARTEX_PG_DSN=... go test ./... -count=1` exit 0（sed/python 平台 skip） | ✅ |
| 外部技能 | 236 技能分层：138 MIT 知识层（kb_search）+ 8 精选执行层（worker/pentest）+ 98 无许可排除 | ✅ |

## 验证（新会话开工前先跑一遍，确认交接没坏）

```bash
cd /c/Users/toti/pi-cwd/20261020/artex-分析/mirror
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
internal/settings audit errclass idem snapshot dbiface netguard env adapter oast provision governor relay
agent/relay_hook.go（MEMORY 接力）  agent/assembly.go（幂等+快照包装）
guard/guard.go（审计 sink + 工具存在性钩子）
server/server.go（wireP0：settings/audit/idem/snapshot/netguard/env）
server/engine.go（启动前出口校验 fail-closed + errclass 分类日志）
fixtures/docker-compose.yml        靶场（DVWA/JuiceShop/VulHub，全绑 127.0.0.1）
scripts/accept_p0.sh               五项验收
docs/最终方案-v1.md                唯一总纲（冲突以它为准）
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

## 唯一需要人做的一次性动作

起 PostgreSQL，把 `ARTEX_PG_DSN` 指过去（例：`postgres://artex:pass@127.0.0.1:5432/artex?sslmode=disable`），
然后 `bash scripts/accept_p0.sh` 应输出"通过 5 / 失败 0"。迁移会在应用首次启动时自动执行。
