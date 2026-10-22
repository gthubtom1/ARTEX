import io

p = 'docs/进度台账.md'
s = open(p, encoding='utf-8').read()

old = '''| **P2-1 MEMORY 接力** | 每段 run 结束写「意图/结果/下一步」，下一段 run 注入 system prompt | ✅ | `internal/relay`（5 单测）+ `agent/relay_hook.go` + `agent/worker.go` |
'''
new = '''| **P2-1 MEMORY 接力** | 每段 run 结束写「意图/结果/下一步」，下一段 run 注入 system prompt | ✅ | `internal/relay`（5 单测）+ `agent/relay_hook.go` + `agent/worker.go` |
| **P2-2 GateChain** | 同锚点同方向合并 + 负空间/阶段/ROI/未测资产加权；不拒绝（只合并） | ✅ | `internal/gatechain`（6 单测）+ `rank_intents` 工具 |
| **P2-3 stall 检测器** | 无产出轮数/事实速度/重复度/层级缺口/前端集中度/预算燃烧 | ✅ | `internal/stall`（2 单测） |
| **P2-4 adviser** | 诊断包 → 可执行 hint/steer/kill（≤3 条、无 add_intent、误报沉默） | ✅ | `internal/adviser`（4 单测）+ `server/progress.go` 每轮规划后自动跑 |
| **P3-1 能力卡** | 13 组 91 项清单 + 派生(finding/fact) + 白话渲染 + 覆盖度自检 + DB 幂等合并 | ✅ | `internal/capability`（7 单测）+ `record_capability`/`list_capabilities` |
| **P3-2 三门校验** | grounding/negation/interrogation → verified/inferred/rejected + 证明级别降档 | ✅ | `internal/verify`（6 单测）+ `verify_claim` 工具 + 能力卡入库前强制过门 |
| **P3-3 自动派生** | 每轮规划后从 finding 派生能力卡（幂等，只补新 item） | ✅ | `agent.SyncCapabilities` + `server.progressHook` |
| **P4-1 凭据库** | AES-256-GCM + HMAC 指纹去重 + 复用计划（ssh/mysql/smb/winrm/redis…） | ✅ | `internal/creds`（4 单测）+ `cred_*` 工具 |
| **P4-2 立足点池** | 会话跨 worker 登记/租借/过期接手/清理 | ✅ | `internal/access`（3 单测）+ `session_*` 工具 |
| **P4-3 提权决策** | LinPEAS/WinPEAS → GTFOBins 排序，内核漏洞永远最后 | ✅ | `internal/privesc`（3 单测）+ `privesc_plan` |
| **P4-4 横移/路由** | ssh/smb/winrm/mssql/mysql/redis/rdp 方案 + 跳板选路 | ✅ | `internal/lateral`（3 单测）+ `lateral_plan`/`route_plan` |
| **P5-1 持有台账** | assets_owned 登记/复查/stale/lost/cleaned/导出 + 只读复查命令 | ✅ | `internal/ledger`（6 单测）+ `owned_*` 工具 + 到期提醒 hint |
| **M2c 适配器工具** | nmap/httpx/nuclei 结构化 host tools（参数白名单、原始输出落盘） | ✅ | `agent/adapter_tools.go` + `TestCheckTargetRejectsInjection` |
| **P6-API 只读面板** | capabilities / owned / owned export / progress / p2 audit 五个接口 | ✅ | `server/p2_api.go` |
'''
assert old in s
s = s.replace(old, new)

old2 = '''go test ./internal/relay/ -v                     # 5/5 PASS
# 全量：go test ./internal/... ./db/... ./guard/... ./agent/... → 15 个包 ok
# go build ./... → exit 0
bash -n scripts/accept_p0.sh                    # OK'''
new2 = '''go test ./internal/relay/ -v                     # 5/5 PASS
go test ./internal/gatechain/ -v                # 6/6 PASS
go test ./internal/stall/ ./internal/adviser/    # 2 + 4 PASS
go test ./internal/capability/ ./internal/verify/ # 7 + 6 PASS
go test ./internal/creds/ ./internal/access/     # 4 + 3 PASS
go test ./internal/privesc/ ./internal/lateral/  # 3 + 3 PASS
go test ./internal/ledger/ -v                    # 6/6 PASS
# 全量：go test ./internal/... ./db/... ./guard/... ./agent/... → 全部 ok
# go build ./... → exit 0
bash -n scripts/accept_p0.sh                    # OK
bash scripts/accept_p2.sh                       # build + vet + 全量无库单测'''
assert old2 in s
s = s.replace(old2, new2)

old3 = '''## 下一步（按序）

1. adapter 注册为 host tools（nmap/httpx/nuclei 三个 typed 工具，agent 不再拼 shell 字符串）
3. P2 GateChain（去重 + 优先级，不拒绝）+ adviser + MEMORY 接力
4. P3 能力卡 + 三门校验 + 决策队列
5. P4/P5 shell 池 + 凭据 + 提权 + 横移 + 台账'''
new3 = '''## 下一步（按序）

1. **P6 前端 8 个页面**（API 已就绪：`/api/capabilities`、`/api/owned`、`/api/owned/export`、
   `/api/progress`、`/api/p2/audit`）——任务/菜单/台账/知识/环境/网络/审计/报告。
2. **决策队列**（P3 剩余）：能力卡 status=open 的三档按钮（只拿证据/继续/换目标）落库 + 前端。
3. **LLM 版顾问**：`internal/adviser.Prompt()` 已给提示词，需独立 LLM profile（强模型）接上；
   当前默认确定性规则（零 token、可离线测）。
4. **复查执行器**：台账现在只写 hint 提醒；可做成定时任务自动跑 `ledger.PlanChecks` 的 nc/curl。
5. **intercept 自动裁决**（方案 §十 reviewer 模式）——目前仍是人工/规则。
6. **P7**：注入层、知识库 MCP、多模型分工、回放看板。'''
assert old3 in s
s = s.replace(old3, new3)
open(p, 'w', encoding='utf-8').write(s)
print('ok')
