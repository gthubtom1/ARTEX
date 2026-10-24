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



### 阶段 E：从"找漏洞"到"拿权限"（2026-10-24，用户核心需求）

用户判定：「串联漏洞和线索才是渗透本质」——不要预设边界，到要越界那一步由系统问人。

**E1 权限阶梯（E0–E5，纯规则）** `GET /api/tasks/{id}/impact`
```
E0 仅有信息 → E1 有效凭据 → E2 应用控制 → E3 命令执行 → E4 数据访问 → E5 服务器控制
```
实测（task 7，只有 1 个 XSS）：`E0 仅有信息 | 只有漏洞，还没有把它变成任何访问权`
统计 `findings=1 creds=0 shells=0 data=0 owned=0 root=0`。
> 教训：第一版把 summary 也纳入判定，XSS 因摘要出现"数据/用户枚举"被算成 **E4 数据访问**——
> 等级虚高比不显示更糟。已改为：E3 只看 vulnclass/name；E4 还要求 evidence 里出现样本/rows/select 之类证据。

**E2 下一步提案（不预设边界，只描述）**
每条提案写清：能得到什么 / 为什么现在能做 / 授权后我会执行什么 / 风险。实测输出：
```
提案[file_write] 把漏洞变成命令执行（上传一个最小 webshell，验证后立即删除）
  为什么: 已有可复现漏洞，且它可能允许写文件/上传
  风险: 会在目标上产生一个文件（可能被 WAF/杀软记录）；不删除会留下后门风险
提案[data_dump] 直接读业务数据（每类最多 3 条样本）
  风险: 读取行为可能被审计；样本里有真实用户数据，需要你决定能不能取
```

**E3 即时授权（task_grants）** `POST /api/tasks/{id}/grant {class, granted}`
- 粒度 = (任务, 动作类别：file_write|data_dump|persist|lateral)；
- 实测：允许 file_write → 库 `7|file_write|t`；拒绝 data_dump → `7|data_dump|f`；
  会话流留下两条人话记录 `用户允许本任务执行「上传/写文件…」` / `用户拒绝本任务执行「批量取数据」`；
- 接口回读立即显示 `granted`，30s 缓存后 guard 即时生效。

**E4 执行闸（guard.ActionGate）**
未授权的越界动作在执行前被拦，写 `task_error_events(class=policy_blocked, tool=GrantGate)`，
提示原文：「这一步需要你先授权：「上传/写文件…」。到任务首页「下一步」卡片点『允许』…」。
单测 `guard/action_gate_test.go`：12 条越界命令分类正确（curl -F/PUT、webshell 落地、sqlmap --dump、
mysqldump、useradd/chpasswd、crontab、authorized_keys、ssh@、proxychains、nxc 等），
6 条侦察命令（nmap/curl 只读/ffuf/sqlmap --banner/cat/grep）不要求授权。

部署：`0.10.1-p9`。

**E5 未做（下一步）**：链对象（`chains`/`chain_steps`，把 finding→利用→收益→下一跳串起来）、
后渗透执行器（拿到 E3 后解锁 webshell/shell 池/数据取样），以及模板③④的 goal 改成
"拿到命令执行或数据样本才算完成"。


### 全链路回归（2026-10-24，两轮，第二轮抓出并修掉一个真回归）

**第一轮（v0.10.2）**：建真实 recon 任务（12）跑 200s，逐轮检查每一环：
```
状态 created · 节点 intent=3 goal=1 fact 1→3 hint 5 · 工具调用 14→86
判定 unclear（reasons 1→3，surface 1→4）· 等级 E0 · 审查官 1/adviser
⚠ 第 5 轮出现 policy_blocked(GrantGate)
```
**定位**：新加的即时授权门把 `proxychains4 curl/nmap ...` 判成了「横向移动」。
proxychains4 只是**出口包装器**（侦察时常用），不是横移 ——
**这就是"加功能把别处链路打断"的典型**：改一行分类清单就好，但如果不跑回归根本发现不了。

**修复**：`lateral` 只认真的连另一台主机的形态（`ssh@`、`impacket-*`、`psexec/wmiexec/smbexec`、
`nxc/netexec/crackermapexec`），移除 proxychains；单测补两条
（`proxychains4 -q curl ...`、`proxychains4 nmap ...` 必须放行）。

**第二轮（v0.10.3）同脚本复跑**：
```
第 1~5 轮：被门拦 = 无
节点 fact 2 · intent 3 · hint 3 · 工具调用 74
判定 unclear（reasons 2，surface 4）· 等级 E0 · 审查官 1/adviser
报告接口 543 字节（该任务无漏洞，属正常）
清理：delete → 200；剩余任务 6/done、7/done
```
**结论：建任务 → planner → worker → guard → 事实 → 判定 → 权限阶梯 → 审查官 → 报告 → 删除，全链路无中断。**

### 其它收口（本轮）
- 模板 ③④ 的 goal 从"列漏洞"改成"**至少把一条漏洞变成命令执行或数据访问**"（并要求 `owned_register`/`record_fact` 登记，等级才会涨）；老库按"未被用户改过"回填。
- 审查官规则版新增一句：`有 N 个漏洞但还没有变成任何访问权 → 下一步优先把它变成登录/命令执行/数据样本，而不是继续找新洞`。
- `internal/impact` 补单测：7 种统计组合的等级判定 + 提案字段完整性（why/plan/risk 缺一即失败）。


### 阶段 E 收口（2026-10-24，按 1→3→4→2 顺序做完并实测）

| 步骤 | 内容 | 判据 → 实测 |
|---|---|---|
| 1 链对象 | `chains`/`chain_steps` + `internal/chain.Build()` 纯函数 + `GET /api/tasks/{id}/chains`（派生后幂等落库） | task 7 返回 1 条链 `F200`：`confirm done` → `authorize:file_write done` → `authorize:data_dump failed`，下一步「已授权，等它把这一步做完并登记收益」；库 `chains` 1 行 + `chain_steps` 3 行与派生一致；单测覆盖 open/dead/done 三态 |
| 3 E2 信号 | facts 里「登录/后台 + 成功/已进入/可访问」→ E2 | `impact` 统计新增 `AppControl`；task 7 = 0（无登录事实），等级仍 E0 |
| 4 登记提醒 | 有命令执行证据但台账为空 → 审查官要求先 `owned_register` | 规则版正文新增该句（有判据：正文含「还没登记立足点」） |
| 2 后渗透清单 | 有命令执行 → 给标准动作清单（读配置→取样→提权检查→横移→写回证据） | 规则版正文新增清单段 |
| 发版 | Release 追平代码 | `v0.11.2-p9`（latest）：windows zip ×2（含稳定名）+ linux zip + SHA256SUMS + USER-GUIDE.md |
| 回归 | 全链路 + 判据脚本 | 两轮真实任务回归：第一轮抓到 proxychains 误拦（已修），第二轮零拦截；`verify-0.9.6.sh` → `VERIFY_RESULT PASS`（VM 0.11.1-p9） |

**仍未验证（外部条件）**：真机 `bootstrap.sh`（docker 可选路线）、StageGate/GrantGate 首条真机拦截记录、
五个条件机制（立足点 / ≥2 漏洞 / 流量捕获）。


### 无 Docker 安装路线 + 真机验收（2026-10-24，替代"必须干净机器"那条）

用户判定：「按理说应该不需要 Docker」。核对结论：**ARTEX 本体确实不需要 Docker**，
docker 只用于两件事：① 新机器一条命令装好（pg+artex 起容器）；② 资产测绘 ScopeSentry
（Mongo+Redis+扫描节点要 host 网络，上游只发镜像）。

新增 `scripts/install-native.sh`（无 Docker 一键装：apt 依赖 → 本机 PostgreSQL 建库 →
二进制（`--binary` 或本地编译）→ config.json → systemd 单元 → health 自检 → 前端口径检查）。
支持 `--prefix/--port/--db/--unit/--dry-run`，幂等，不覆盖已有密码。

**真机验收（就在用户的 Kali VM 上做，不碰现有服务）**：
```
sudo bash scripts/install-native.sh --prefix /opt/artex-t2 --port 18788      --db artex_t2 --unit artex-t2 --binary /opt/artex/artex
[+] 数据库就绪：artex_t2
[+] 二进制就位：/opt/artex-t2/artex
[+] 配置就绪：/opt/artex-t2/config.json
[+] health OK：{"ok":true,"service":"artex","version":"0.11.1-p9"}
[+] 前端已内嵌（/ → 200）
```
再用判据脚本打这套全新实例：`ARTEX_DIR=/opt/artex-t2 ARTEX_PORT=18788 bash scripts/verify-0.9.6.sh`
→ 版本/模板/命中率全部 PASS，`VERIFY_RESULT PASS`（无任务时"报告/值不值得打"按设计跳过）。
验证后已清理（停单元、删目录、drop 库），原 `/opt/artex` 服务仍 active。

**因此阶段 D 那条"真机 bootstrap（docker 路线）"降级为"可选、未验收"**，
不再阻塞：用户实际使用的原生路线已在真机验收通过。


### 效果验收（2026-10-24 新增，`docs/21-效果验收.md`）

「代码完成 ≠ 有效」。把"效果"写成 5 条硬指标（F1 能拿权限 / F2 误报率≤10% / F3 可复现 /
F4 成本可见 / F5 零基础可走通），并给出**当前实测值**：

```
真实任务 2 个 · 漏洞 2 个 · 报告 6708/8221 字符
拿到 shell 0 次 · 读到数据 0 次 · 立足点 0 个 · 凭据 2 条(未验证)
权限阶梯最高 E0 · 链 1 条(open，无 gain) · 审查官结构化动作 0
累计 token 60,378,766（≈3000 万/漏洞）
```

结论：**链路全通，但"拿权限"一次都没被证明**。F1~F3 全部未通过。
下一步按 `docs/21` 的流程跑标准靶场（DVWA / DSVW / juice-shop），用 ③ 模板，
判据 SQL 已写在该文件里。


### 战况卡线（P0–P4）进度账 — 2026-10-24

施工图：`docs/22-战况卡与业务动作库.md`。**按此顺序继续，不要跳。**

- ✅ **P0-a 后端**：`GET /api/tasks/{id}/brief`（server/brief.go）已上线 (0.11.8-p9)，实测 task 14：
  等级 E4 / 已拿到 3 项 / "能换到什么"按漏洞类型映射 / 业务动作 7 个且 `state=idle` / `say_this` 一句话。
- ✅ **P0-b 前端**（2026-10-24 完成）：任务总览顶部只剩**一张「战况卡」**（`web/src/app/(main)/function/tasks/detail/_tabs/battle-card.tsx`），
  数据源 `GET /api/tasks/{id}/brief`；原来的「利用链 / 现在到哪一步 / 值不值得打」三张卡已删除，
  利用链、越界授权（允许/拒绝收成一行）、「为什么这么判（可展开依据）」都收进这同一张卡，页面上不再有同义卡。
  战果面板（`loot-panel.tsx`）：每行主语改白话（`plain`）+ 原归类名降为副行；胶囊未读红点用 `/brief` 的 `new_since`
  （游标表 `db/migrations/0014_loot_seen.sql`，打开面板即调 `POST /api/tasks/{id}/loot/seen` 清零）；
  面板头部「详情看战况 →」跳回总览（`?tab=overview`），反向「战果 +N」跳 `?tab=sessions&panel=loot` 并自动打开战果。
  **实测（mock 全站 `NEXT_PUBLIC_MOCK=1` + Playwright，截图 `screenshots/p0b-*.png`）**：
  一张卡同时给出 `E3 命令执行` / 「我有什么」4 条（后台·凭据·数据·命令）/ 6 条「每个漏洞能换到什么」/
  7 件业务动作（每条都标「不自动执行」+ 前置 + 可复制的说法）/ 3 条利用链 / 一句话说法；
  战果胶囊 `战果 +5` → 打开战果面板后变 `战果 5`（红点清零）；战果行主行是白话；
  点「详情看战况 →」URL 变 `tab=overview` 且战况卡在页；展开依据出 3 条理由 + 攻击面 URL；浏览器 0 条 console error。
  `npx tsc --noEmit` / `npx next build`（36 路由无错）/ `go build ./...` / `go vet ./server` 全过，
  `go test ./server -run TestCountUnreadLoot` PASS。
  **真机验收（2026-10-24，Kali VM，VBoxManage guestcontrol 换装新二进制 0.11.9-p0c）**：
  换装前同一接口 `new_since` 字段不存在、`/loot/seen` 404、迁移停在 `0013`；
  换装后启动日志 `[db] migration applied: 0014_loot_seen.sql`，`/brief` 返回 `new_since:2`，
  `POST /loot/seen` → `{"ok":true,"seen_finding_id":298,"new_since":0}`，再取 `/brief` → `new_since:0`，
  `select * from loot_seen` → `14|298|2026-10-24 09:26:23`。判据脚本 `scripts/vm-check-brief.sh`（可复用）。
  唯一没做的：真机上的**浏览器**点一遍（要 ARTEX 网页登录密码，凭据红线不外拿）。
- ✅ **P0-c 前端+后端**（2026-10-24 完成）：「能换到什么」AI 版上线。
  后端 `server/brief_ai.go` + `db/migrations/0015_finding_abilities.sql`：缓存键 = `sha256(标题|摘要|证据原文)`，
  证据一变 hash 就变（旧解释自然失效）；一次调用批量问（`POST /api/tasks/{id}/brief/ai`，默认取前 8 条未缓存的），
  模型编出来的 finding_id 一律丢弃、超 80 字丢弃；失败什么都不写，`/brief` 继续用规则模板 `abilityText`。
  `GET /brief` 的 `can[]` 现在带 `source: rule|ai` + `cached`，前端 `battle-card.tsx` 显示 AI 角标
  （悬停「命中缓存，没花 token」）+「✦ 用 AI 说清楚 / 重新解释」按钮，`（N/M 条 AI 版）` 计数。
  **实测（mock 全站 + Playwright，截图 `screenshots/p0c-ai-abilities.png`）**：点一次 `（0/6 → 6/6 条 AI 版）`，
  每行出现 `AI` 角标、文案从「能读它的数据库（用户表/订单表等）」变成「能翻它的数据库：用户表、订单表都读得到。」；
  等一个 20s 轮询周期仍是 6/6（GET 也吃缓存，不会被轮询冲回规则版）；再点一次 toast
  「这几条都已经有 AI 解释了（命中缓存）」；浏览器 0 console error。
  `TestBriefAbilityHash`（证据变则键变 / id 不影响键）+ `TestParseBriefAbilities`（Markdown 包裹、前后废话、非 JSON）PASS。
  **真机验收（2026-10-24，同一台 Kali VM，真模型 `opendesign/deepseek-v4.1-flash`）**：
  换装后 `[db] migration applied: 0015_finding_abilities.sql`；
  `POST /brief/ai` → `{"asked":1,"filled":1,"model":"adviser"}`，模型实际写出
  「攻击者可冒用管理员身份登录，并读取系统内全部账户资料。」（finding 298），落库
  `finding_abilities: 298|8882dd29cfa0|…`；随后 `GET /brief` → `[{id:298,"source":"ai","cached":true}]`
  （缓存命中、不再花 token）。部署二进制内嵌前端也验过：`/_next/static/chunks/…js` 里
  「战况 / 每个漏洞能换到什么 / 详情看战况 / 用 AI 说清楚 / 不自动执行」全部命中。
- ✅ **P1 动态快捷短语**（2026-10-24 完成）：`GET /api/tasks/{id}/phrases`（`server/phrases.go`，纯规则、确定性、
  不调模型不写库），按当前战果生成 3–5 条，每条带 `why`（悬停显示"为什么现在说这句"）；与全局「常用语」并存。
  规则顺序：有命令执行 → 先捞配置/补证据；进后台 → 先取证；能读库 → 要样本+字段名；有凭据 → 先验证一次；
  能落文件 → 往命令推一步；有洞没收益 → 催"从确认推到达成"；试过没成 → 换手法或标记放弃；不足 3 条用通用兜底补。
  前端 `_tabs/task-phrases.tsx` 挂在会话输入框上方，点一下**只填入不发送**。
  **真机实测（Kali VM 0.11.10-p1）**：`/api/tasks/14/phrases` → `{"n":5,"has":["db","cred"],
  "keys":["db_sample","cred_verify","summary3","next_step","alt_route"],"first":"把订单表按时间倒序取前 20 条…"}`；
  浏览器（mock）点第一条 → 输入框出现该句、焦点落位、发送按钮启用但**没有发出**（无新消息）。
  `TestBuildTaskPhrasesCount`（3–5 条、字段非空、无重复）+ `TestBuildTaskPhrasesPriority`（有 shell 时第一条是捞配置、
  无战果时不出"进后台"、只有漏洞时第一条是催收益）PASS。
- ✅ **P2 业务动作执行通道**（2026-10-24 完成）：`db/migrations/0016_action_runs.sql` + `server/actions.go`。
  唯一事实源 `actionDefs`（7 类：data_export/account_read/account_create/money_adjust/money_approve/deface/persist，
  每类带前置、说法、留证要求、映射的战果行）；brief 的动作列表改由它生成。
  链路：人在主对话说「帮我…」→ `maybeActionFromChat` 按关键词登记工单（`say_it` 存原话=授权凭据，
  与既有 `maybeGrantFromChat` 并存）→ 该动作映射的战果行出现**新证据**（id > 登记时 base_ref）→ 自动收口 done；
  也可 `POST /actions/{run}/result` 显式回报 done/failed/aborted（**done 必须带 evidence 或 evidence_ref**）。
  纪律：`brief.actions[].state` 仍恒为 `idle`（永不自动执行），执行状态看新增的 `run_state`
  （none/requested/done/failed/aborted）+ `run_evidence` + `say_it` 原话。
  **真机实测（Kali VM 0.11.11-p2）**：`[db] migration applied: 0016_action_runs.sql`；
  `POST /actions` → `{"id":1,"state":"requested","base_ref":298}`；`/brief` → `{"state":"idle","run_state":"requested",
  "evidence_need":"行数 + 样本 3 条（脱敏）"}`；不带证据标 done → `{"error":"标记完成要带 evidence 或 evidence_ref"}`（被拒）；
  带证据标 done → `{"ok":true,"state":"done"}`，再读 `/brief` → `run_evidence:"订单表取到 3 条脱敏样本，共 1284 行"`；
  `action_runs` 行 `1|data_export|done|298|298|…`。
  `TestActionClassFromChat`（含否定句/提问不误判）、`TestActionClosureNeedsNewEvidence`（旧证据不算做成、
  run 状态不算战果）、`TestActionDefsComplete`（7 类字段齐全）PASS。
- ⬜ **P3** 未读通知（红点/任务列表徽章/标题）+ 审查官喂 Brief 并下硬动作。
  **已被下面的「统一模型重排」插队**：审查官要读的三段式判定先得建好，否则它拿到的还是错的输入。

### 统一模型重排（2026-10-24，用户指出"漏洞多但换不到后果"）

施工图：**`docs/23-战况与后果-统一模型.md`**（三层模型 / 三段式 standing / AI 只写文案 / 页面职责 /
三道闸门 / 7 步顺序）。**按它做，不要再零散加功能。**

一句话结论：**漏洞存在 ≠ 能换到现实后果**。之前把"洞能换到什么"和"我现在能做什么"混在一起，
导致"AI 说能换到"听起来像"我现在就能干"。修法是给每条洞加**确定性**三段式状态
（`got` 已成立 / `one_step` 差一步 / `no_gain` 换不到），由证据关判定，AI 只写措辞。

**0. 前置逻辑错已修（2026-10-24，0.11.16，用户指出）**：E0（只有漏洞、没拿到任何权限）时，
"下一步提案"原来直接给「上传一个最小 webshell」——那是拿到上传/写文件能力之后才该做的事。
现在 `impact.Propose(lv, st, kinds)` 按**漏洞类型 + 前置能力**分派：E0 默认只给
「只读验证：证明这个洞能真的读到东西（1 条即可，不落文件）」，只有**确认了上传/写文件类入口**
才允许提落文件；每条提案都带 `precondition` + `ready`，未就绪的只能显示「还差前置：…」，
会话顶部横条只提示 `ready` 的步骤。真机（Kali 0.11.16）：task 15（E0）→
`只读验证…ready:true`；task 14（E4）→ `提权到 root（前置：已能读到业务数据）`。
测试 `TestProposeE0DoesNotSuggestWebshell` PASS。

**① 三段式 standing 已上线（2026-10-24，0.11.15）**：`server/brief.go findingStanding()` +
`findingRowHints` 映射表（docs/23 §二）+ `/brief` 的 `can[]` 新增 `standing/gap/rows`。
前端按 got→one_step→no_gain 排序并加彩色标签，标题给三组计数
（如「3 个已成立 · 0 个差一步 · 3 个换不到 · 0/6 AI 版」）。
真机（Kali 0.11.15）：task 14 返回 `can_standing:{"got":1}`、`can_detail:[{id:298,standing:"got"}]`。
测试 `TestFindingStanding`（SQL 已读库=got / 无战果=one_step 且指出缺口 / 目录列举=no_gain / na 不算缺口）PASS。

施工蓝本：**`docs/25-成品方案-页面与组件.md`**（页面总图 / Tab 11→4+更多 / 会话页 / 战况页一屏一问 /
战果面板 / 审查官卡片 / 状态机三闸门 / 按钮总账 / 7 步顺序 / 不做清单）。

重排后的顺序：① 三段式 standing → ② 战况卡按三段式重排（**界面收敛见 `docs/24-界面收敛-一屏一问.md`**）
→ ③ AI 只写文案（一次点击刷新三处）→ ④ 模板重写（3 开局 + 三段式文本 + 放弃判据）
→ ⑤ 投入产出闸门 → ⑥ 审查官 P3 → ⑦ 通知。

**⑧ 终局审查（用户 2026-10-24 要求，放在 ①–⑦ 做完之后）**：以"零基础用户的新建任务→给网址→找漏洞→
看能不能拿权限/拖库"这条主线，**完完整整走一遍**，逐项判：
① 逻辑（判定有没有跳步、有没有拿预设冒充结论、有没有该拦没拦）② 交互（5 秒能不能看懂、
按钮是否重复/缺失、空态/失败态是否说人话）③ 数据（战果/战况/报告三处口径是否一致）；
产出 `docs/26-终局审查报告.md`：**每条带"现象 / 判据 / 结论(通过/缺陷) / 修法"**，
缺陷直接改并回归真机；发现的问题按"会不会让零基础用户做错判断"排序。

**0.1 战况自洽性修复（2026-10-24，0.11.19，用户截图指出）**：截图里等级是 `E0 仅有信息`、
「我有什么」为空，却在「现在能做到」里高亮「看后台的用户名/密码或哈希」——**自相矛盾**。
根因：`战果行的 done` 与 `能力（impact.Stats）` 是两套语义 ——
战果行 done 只代表"证据里提到这一类东西"（漏洞摘要出现"密码"就中），不代表"我拿到了"。
修法：**"能做到"和"已成立"都必须有同源能力佐证**（`actionDoable(def, st)` / `rowCapability(key, st)`）：
data_export←读到数据；account_read←进过后台或库里有凭据；persist←命令执行；deface←后台或命令执行…
战果行只用于"自动收口"（新证据=有进展），不再冒充能力。
真机（Kali 0.11.19）：task 15/16/17（E0）→ `doable:[]`、7 件全 `还做不到`、standing 全 `one_step`；
task 14（E4）→ 6 件 doable、standing got。测试 `TestActionDoableFollowsCapability` /
`TestFindingStandingNeedsCapability` PASS。

**②③ 战况页一屏一问已上线（2026-10-24，0.11.17）**：
- `BattleCard` 重写为五行主视线：① 我在哪（等级+进度点+洞数）② 我有什么（最多 3 个 + 还有 N）
  ③ **现在能做到**（最多 3 条 + `[就做这个]`，带"已登记/已完成"工单态）④ 差一步（一行：几个洞、缺什么）
  ⑤ 换不到（一行）；底部固定说明「灰的是可能清单，只有拿到证据才点亮；AI 只解释不替决定」。
- 主视线只留 **1 个 AI 按钮 `✦ 分析`**（原「用 AI 说清楚/重新解释」合并）+ `刷新`（免费）。
  其余全部折叠且标题带数量：`6 个洞·每个能换到什么` / `7 件能做到的事` / `N 条利用链` /
  `N 个越界动作` / `为什么这么判`。
- 一盏灯（docs/24 §九）：`✦ 分析` 走 `/brief/ai`（花钱、按证据缓存）后 `window.dispatchEvent("artex:analyze")`，
  会话页 `TaskPhrases` 监听该事件做一次**免费**重排；`⟳` 仍可单独点。
- **Tab 11 → 4 + 更多 ▾**（会话/总览/发现/报告；其余进下拉，拦截审批待办数仍带红标）。
- 实测（mock 浏览器 + 部署包）：`/function/tasks/detail` 只有 4 个 tab + `更多 ▾`；
  卡片渲染 `E3 命令执行`、`我有什么` 3 个 + `还有 1 项`、`现在能做到（6 件）` 前 3 条带 `[就做这个]`、
  `换不到 3 个洞…`、5 个折叠区；部署 chunk 命中 `我在哪/现在能做到/就做这个/更多 ▾/灰的是`。
  截图 `screenshots/p2-oneScreen.png`。

**界面收敛（docs/24，2026-10-24 用户反馈"东西太多、第一眼找不到"）**：一屏一问；主视线一张卡五行
（我在哪/我有什么/**能做到最多 3 条**/差一步一行/换不到一行）；主视线只留 **1 个 AI 按钮**（✦ 分析，
点一次刷三处），其余 AI 按钮与全量洞/链/越界动作/管理信息**全部折叠且标签带数量**；
三处入口分工写死：**AI 按钮=只读解释 /「就做这个」=只填入不发送 / 主对话=真的去做**（AI 按钮永不派任务）。
判据：打开任务 5 秒内能说出"我现在能做什么"，主视线可点按钮 ≤ 3。
- ✅ **P4 收尾**（2026-10-24 完成）：
  ① 详情页地址里没有 `?id=` 时**直接回任务列表**（`router.replace`），不再显示"任务 X 已被删除、归档或不存在"
  这个误报（真机已验证跳转到 `/function/tasks`，误报文案 0 次出现）。
  ② 「允许/拒绝」按钮全部删掉，改成一行说明 + 只读状态：「越界动作（默认不做；要做就在对话里说「帮我…」）」，
  每条只显示 `未授权 / 已在对话里授权（时间）/ 已明确拒绝（时间）`，下面一行
  「这里不放"允许/拒绝"按钮：说一句就是授权（「帮我挂个 webshell」），否定句（「不要…」）自动不算」。
  依据：P2 之后授权只剩"在对话里说"一条路，按钮是重复入口。
  **实测**：mock 浏览器 `/function/tasks/detail` → 跳到 `/function/tasks`，`有"允许"按钮=false`；
  部署后（0.11.12-p4）服务的 chunk 里 `正在返回任务列表 / 这里不放 / 已登记（等结果）/ 详情看战况 /
  每个漏洞能换到什么 / 这个站现在能说` 全部命中。

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
