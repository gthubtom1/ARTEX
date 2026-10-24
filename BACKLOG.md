# BACKLOG（唯一待办）

> 遗留四分类后的单一待办。**做完写 ✅ + 证据；放弃写 ❌ + 原因。空表 = 无待办。**
> 模型选择类属于「配置建议」，不是待办（见文末）。

| id | 事项 | 谁做 | 不做会怎样 | 验收判据 | 状态 |
|---|---|---|---|---|---|
| B-03 | 用真实新任务跑两轮，判断审查官值不值得留 | **你**（只有你能判断建议对不对） | 审查官是留是降级没有证据；不影响其它功能 | 新任务里 ≥2 条 `level=action`，且 `outcome=effective` 不为 0 | ⬜ |
| B-04 | `git push` | 你（或授权我做） | 本机硬盘坏了全没 | `git status` 显示 up to date with origin/main | ✅（2026-10-23 `ea43e73..0ede296`，683 commit 已同步） |
| B-05 | `install.sh` / `update.sh` 改走新路线 | **我**（纯代码） | 用 `install.sh` 的新机器会失败（用 `deploy/bootstrap.sh` 不受影响） | 干净机器跑 `install.sh` 能起到 8787 | ✅（代码 + stub 双分支验证；真机验收并入 B-10） |
| B-06 | 锁死 ASM 扫描节点版本（`latest` → 固定 tag/digest） | **我** | 上游发新版可能拉到不兼容的扫描节点 | `docker-compose.yml` 里两个 asm 镜像都不是 `latest` | ✅ |
| B-07 | 战果清单「还没试 / 不适用」两态 + 标为不适用 | 可选（你要求再做） | 现有使用不受影响；没证据的行现在本来就不显示 | 能把一行标 na 并持久化，刷新后仍 na | ✅（0011 + `/loot/state` + 详情区两个按钮；Go 单测 4 个） |
| B-08 | 战果「✦ 补这个站特有的项」前端按钮 | 可选 | 后端能力在，只是没入口 | 点按钮追加 `AI 推断·待确认` 行且不计入 done | ✅（本来就已落地，本次只是核对，见证据） |
| B-09 | 命中率做成页面 | 可选 | 只是排查不便，一条 SQL 也能看 | 页面上能看 16 表数字 | ✅（`/system/hitrate` + 侧边栏「命中率」） |
| B-10 | 干净机器实跑 `deploy/bootstrap.sh --with-asm` | **我**（需能跑 docker 的机器） | “一条命令装两套”只是静态通过，没被真证 | 新机器 `status.sh` 输出 `STATUS PASS` 且 MCP 工具数 >0 | ⚠ 一半：stub 全流程 PASS（`scripts/stub-bootstrap.sh`），**真机未跑**（本机无 docker） |

### 发版（2026-10-23，B-11）
- `v0.7.4-p9` 已发布：`artex-windows-amd64.zip`（稳定名，供 `部署.bat` 无脑下载）+ `artex-v0.7.4-p9-windows-amd64.zip` + `SHA256SUMS`。
- `build.sh` 打包补齐 kb/部署.bat/启动.bat/三份文档，无 zip 时用 python 兜底；`部署.bat` 下载失败且本机有 Go+Node 时自动本地构建。
- CI：`release.yml` 只跑正式 semver（带 `-` 的标签跳过），`demo.yml` 需 `DEMO_ENABLED=true` 才跑——不再因缺 secrets 留红。

**现在只剩 2 件需要人**：B-03（判断审查官留不留，不做也照样用）、B-10（一台能跑 docker 的干净机器，一条命令即可收尾）。

---

## 已完成证据（2026-10-23）

### B-04 推送

```
git push origin main    # ea43e73..0ede296（683 commit）
git status -sb          # ## main...origin/main（无 ahead）
git rev-list --count HEAD / origin/main   # 683 / 683
```

### B-05 镜像路线分流

- 默认 `ARTEX_IMAGE=artex-fork`（或未设）→ `docker compose build`，**不 pull**；只有显式设了远程 registry 镜像才 pull。可执行行：`install.sh:190`、`update.sh:59`，都在 remote 分支内。
- `install.sh` 补 `ARTEX_PASSWORD` / `ARTEX_CRED_KEY`（缺失才生成，已有值不覆盖）；启动后按 `/api/auth/status`（200/401/403）等就绪，再调 `/api/auth/init`（`install.sh:65`），已设过返回 403 则保留原密码。
- 验证：`bash -n` 通过；临时目录用 stub docker/curl 实跑两条分支——本地：`compose build` + `auth/init` 200；远程：`compose pull`；重复跑 `.env` 密钥不变。

### B-06 扫描节点锁版本

- `docker-compose.yml`：`asm-scan: image: autumn27/scopesentry-scan:${ASM_SCAN_TAG:-1.9.1}`；`scope-sentry: image: autumn27/scopesentry:${ASM_TAG:-1.9.4}`。
- `.env.example` / `deploy/lib.sh` / `deploy/status.sh` 默认同步为 1.9.1；`ensure_asm_env` 会把旧 `.env` 里遗留的 `ASM_SCAN_TAG=latest` 迁成 1.9.1（用户显式固定值不覆盖）。
- 1.9.1 未核实镜像存在（本机无 docker 无法 inspect），注释已写明可用 `ASM_SCAN_TAG` 覆盖；要完全可复现用 `image@sha256:<digest>`，获取命令 `docker buildx imagetools inspect autumn27/scopesentry-scan:<tag>`。
- 验证：PyYAML 解析 compose，两个 asm 镜像均非 latest；全仓 `image: ...latest` 只剩 bench/fixtures（非安装路线）。

### B-07 战果两态（本次新增）

- 迁移 `db/migrations/0011_loot_row_state.sql`：`loot_row_states(task_id,row_key,state,updated_at)`，主键 `(task_id,row_key)`。
- 后端：`POST /api/tasks/{id}/loot/state`（`state` = `todo|na|""`，空 = 取消）；`lootRowsForTask` 只在**规则没算出证据**时套用标记（证据优先），`na` 不计入 `total`，新增 `todo`/`na` 计数；「还没试」行有独立下一步模板 `lootTryPrompt`（不假设已有入口），`na` 行不给下一步。
- 前端：`loot-panel.tsx` 详情区「标为还没试 / 标为不适用 / 取消标记」，写库后重拉（状态永远由后端算）。
- 验证：`go test ./server -run 'TestLootApplyState|TestLootStateRowKey|TestLootTryPrompt|TestLootPlainState'` 全 PASS；`npx tsc --noEmit` 通过；提交 `0ede296`。
- 未验：真 PG 上的迁移与 UPSERT（本机无数据库），下次起服务看日志 `migration applied: 0011_loot_row_state.sql`。

### B-08 「✦ 补这个站特有的项」

- 前端按钮在 `loot-panel.tsx` 工具栏（`onClick={suggest}`），调 `lootapi.suggest` → `POST /api/tasks/{id}/loot/suggest`；后端 `server/loot_ai.go` `lootSuggest`，每条必须有真实 finding id（`lootValidateSuggestions`），追加行 `status="blank"` 显示「AI 推断·待确认」，不计入 done/total。

### B-09 命中率页面

- 新增 `web/src/app/(main)/system/hitrate/page.tsx`；侧边栏 `系统 → 命中率`；`hitrateapi.get(task)`（`web/src/lib/api.ts`）。
- 内容：有数据机制数 / 空表数 / 写入行数合计、16 表逐行（0 行灰显「空表」）、工具调用分布条、复制 JSON。任务下拉默认取 active。
- 验证：`npx tsc --noEmit` 通过。

### B-10 stub 全流程（真机待跑）

`bash scripts/stub-bootstrap.sh` → `STUB_RESULT PASS`。它把 docker/curl/jq/uname/id/stat 全部 stub 掉，在临时目录跑两遍：

1. `bootstrap.sh --with-asm`：退出 0、`STATUS PASS`、`wire.sh` 接通 2 个工具、`ASM_ENABLED=1` 落盘、compose 收到 `--profile asm`、**全程没有 pull**；
2. `bootstrap.sh --no-asm`：退出 0、`STATUS PASS`、`ASM_ENABLED=0`、`.env` 密码不变（幂等）、compose 不再带 `--profile asm`。

它顺手抓出两个真 bug（都已修）：

- `deploy/bootstrap.sh:41` `TARGET_USER="${SUDO_USER:-$USER}"` 在 `set -u` 且 `USER` 未定义（systemd / CI / 容器）时直接崩 → 改为 `${SUDO_USER:-${USER:-$(id -un || echo root)}}`；
- `deploy/status.sh` 迁移判据写死 `0010`，加了 `0011` 之后体检永远 FAIL → 改为读最新迁移号比较（`10#` 防八进制）。

**真机验收（待你给一台能跑 docker 的机器，一条命令）**：

```bash
git clone <本仓库> artex && cd artex && sudo bash deploy/bootstrap.sh --with-asm
bash deploy/status.sh      # 期望 STATUS PASS + 「MCP 数据源 enabled，N 个工具」N>0
```

---

---

## 完工判据（2026-10-24 新增，唯一标准）

**「完成」= 在真实任务里留下可观察的痕迹 + 有可复算的判据 + 失败时界面显形。**
过去把"代码写完/build 过/单测过"当完成，导致审查官"做完"了却没人看出它没在工作。
逐功能判据见 **`docs/20-完工方案.md` §3**（实测：7 个机制有数据、6 个为 0 待定性、1 个未验证）。

四个阶段：A 可信（值不值得打卡 / 审查官 cites 校验 / 报告首页）→ B 阶段门 →
C 六个 0 行机制定性（定性不了就删）→ D 交付（重新发版 / 清备份 / 文档同步 / 真机验收）。

**阶段门真机触发尝试（2026-10-24，负结果，如实记录）**

建 recon 任务（`https://axcheat.com`，180s）→ 经 `POST /api/chat?task=11` 明确要求
"让 worker 用 sqlmap 跑注入检测" → 轮询 120s。

结果：**StageGate 拦截 0 条**。该任务 tool_use 全是只读侦察（curl 枚举 author/order、
passive DNS、robots.txt），模型没有尝试 sqlmap —— 主 Agent 按"只读侦察"的阶段目标把
那条指令改写成了侦察动作。

判断：
- 阶段门**接线正确**（单测 8 拦 / 7 放；`settings: task.stage.<id>` 实测落库）；
- "模型主动尝试利用"未被触发，所以**首条真机拦截记录仍为空**；
- 这不是缺陷，是模型遵守了阶段约束。判据保留：
  `select count(*) from task_error_events where tool='StageGate'` > 0。

自检任务 11 已删除；库里恢复 task 6/7 两个真实任务；`settings` 无 `task.stage.%` 残留。


## 配置建议（**不是待办**，你随时改）

模型选择是你的决定，不是代码缺陷，不阻塞任何功能（不绑也能用，只是全用同一个模型）。

| 建议 | 怎么改 | 为什么 |
|---|---|---|
| 审查官用最强模型 | 系统 → Agent → 审查官 → 默认模型 | 它一次判断决定还要不要继续烧钱，跟便宜模型会降智 |
| planner / mainagent 用强模型，worker 用便宜档 | 同上，逐个角色绑 | 方向错一次最贵；worker 干粗活不需要贵模型 |

---

## 已放弃（留证，不再讨论）

| 事项 | 原因 |
|---|---|
| H2 流量绑定 / H6 URL 路由 | 引擎拿不到真实流量；`routes` 表是网络路由不是 URL |
| E01 L3 硬约束（阻断版） | intent 无 stage 字段，硬阻断会让任务停摆 → 已改度量版 |
| 敏感数据打码 | 用户明确不做 |
| 新建任务表单只留「模板 + 网址」 | 主用法是对话派活 |
| 给「还没试」的行强行补数据 | 没证据的行不显示是设计 |

---

## 待确认（需 owner 一句话）

| 事项 | 需要你回答什么 |
|---|---|
| `docs/06 冻结表` 作废条目措辞 | 「作废（用户改口）」这个说法行不行 |
| `Dockerfile`（预编译路线）是否保留 | 保留的话要补 `jwt.key → data/jwt.key` 软链 |
