# AGENTS.md —— 这个仓库的 AI 入口（换机 / 接手 / 部署，先读这里）

> 给任何 AI 编程代理（Codex / Claude Code / OpenCode / pi / Cursor …）。
> **人在浏览器里点** → `使用教程.md`；**你要动手** → 这份 + `INSTALL-AI.md`。
> 本文件只做路由和硬规则，不复制细节；细节全部在引用到的那几份里。

---

## 0. 30 秒：用户想干什么，先读哪份

| 用户说的话 | 读哪份 | 第一条命令 |
|---|---|---|
| 装 / 部署 / 换电脑 / 新机器 / 我全程不操作 | **`INSTALL-AI.md` 开头的「⚡ 零操作模式」** | `sudo bash deploy/bootstrap.sh` |
| Windows 上装（不会命令行） | 本文 §3 | 双击 `部署.bat` |
| 装好了，怎么用 / 怎么开任务 | `使用教程.md` | — |
| 出问题了 / 装不上 / 打不开 | `INSTALL-AI.md` §7「失败矩阵」、§12.5 | `bash deploy/status.sh` |
| 继续开发这个项目 | `RESUME.md` → `docs/00-总纲v2-唯一事实源.md` → `BACKLOG.md` | `go build ./...` |
| 这个仓库里哪个文档算数 | `docs/README.md`（索引，标了「现行 / 历史」） | — |

---

## 1. 硬规则（违反任意一条 = 这次交付算失败）

1. **不要问「要不要装资产测绘」。** 默认全装（`deploy/bootstrap.sh` 不带参数就是全装）。
   只有用户明确说"只要本体"时才 `--no-asm`。
2. **不要要求用户手动执行你能执行的命令。** Linux/macOS 上你自己跑 `sudo bash deploy/bootstrap.sh`。
   唯一例外：Windows 且没有 Git Bash 时，让用户双击 `部署.bat`（脚本里会自己下载 + 起库 + 建配置）。
3. **说「装完了」之前必须跑过** `bash deploy/status.sh` 并看到 `STATUS PASS`（启用测绘时还要
   `MCP 数据源 enabled，N 个工具` 且 N>0）。没跑过就不许说成功，也不许用"应该可以"糊过去。
4. **不要把 `.env` / `config.json` / `data/` / `jwt.key` / `dist/` 提交进 git**（都已在 `.gitignore`）。
   这些是运行期数据，换机重装时重新生成。
5. **发现真实凭据 / Token / 密钥**（用户给的或仓库里翻出来的）→ 停手、报告用户，
   不写进任何输出、不落盘、不进提交。
6. **不要为了"先跑起来"删 `.gitignore` 条目、改脚本绕过报错、或把 `main` 强推。**
   报错按 `INSTALL-AI.md` §7 的失败矩阵定位；同一形状失败两次 → 停下来汇报。
7. **构建单二进制必须带 `-tags embedui`**，且要先导出前端；否则网页 404、API 正常，
   看起来像"装好了"。一步做完用 `bash scripts/package_local.sh <版本号>`（Windows 也能用）。

---

## 2. Linux（Kali / Debian / Ubuntu / VPS）—— 一条命令

```bash
git clone https://github.com/gthubtom1/ARTEX.git artex && cd artex
sudo bash deploy/bootstrap.sh          # 装依赖→起 docker→生成 .env→构建→起服务→设密码→装测绘→接线→体检
bash deploy/status.sh                  # 必须 STATUS PASS
```

- 只要本体：`sudo bash deploy/bootstrap.sh --no-asm`
- 装完端口：ARTEX `:8787`（用户 `ARTEX`，密码在 `.env` 的 `ARTEX_PASSWORD`）、
  ScopeSentry `:8082`（默认 `ScopeSentry/ScopeSentry`，让用户马上改）。
- 内存 < 8G 或不是 Linux 宿主：不要装测绘（扫描节点要 host 网络），改用
  `--no-asm`，或 `INSTALL-AI.md` §12.2 的轻量三命令路线。

## 3. Windows —— 双击就行，你在旁边兜底

```bat
双击 部署.bat
```

它按顺序做：本地有 `dist\artex-windows-amd64\artex.exe` 就复用 →
否则跟随 GitHub「最新 Release」下载 `artex-<tag>-windows-amd64.zip` 并解压出 `artex.exe` →
没有 `config.json` 就用 Docker 起 PostgreSQL 容器 `artex-pg` 并生成配置 → 调 `start.bat` 启动。

- 要固定版本：`set ARTEX_TAG=v0.7.4-p9` 再双击（不设就取最新 Release）。
- 没有 Docker：脚本会复制 `config.example.json` 并打开记事本，让用户填自己的 PostgreSQL。
- 没有 Node 也要出二进制时：从 Releases 下 zip；**不要**直接 `go build`（漏 `-tags embedui` 会没有界面）。

## 4. 先判断环境，别自由发挥

```bash
bash scripts/detect-env.sh          # Linux/macOS/Git Bash
powershell -File scripts/detect-env.ps1   # Windows 原生
```

它会给出 `PKG_MANAGER` / 是否有 docker / 是否有 node+go / 端口占用。
没有 docker 时只有两条路：源码编译（`INSTALL-AI.md` §4，需要 Go+Node）或 Windows 双击路线。

**只想验证"脚本没错"而不真装**（CI / 无 docker 的开发机）：

```bash
bash scripts/stub-bootstrap.sh      # 期望 STUB_RESULT PASS；它 stub 掉 docker/curl/jq，只验流程
```

## 5. 装完只有两件事必须你亲自做

1. **ARTEX → 系统 → LLM**：填模型和 API Key（planner 建议用强模型）。
2. **ScopeSentry → 配置**：填 FOFA（email + key）或 Quake / Hunter；顺便改默认密码并写回
   `.env` 的 `ASM_ADMIN_PASSWORD`。

这两步要账号/要花钱，不要替用户编造，也不要把 Key 写进任何提交或日志。

---

## 6. 仓库结构（够你导航，不用全读）

| 路径 | 是什么 |
|---|---|
| `INSTALL-AI.md` | **安装执行手册（给 AI）**：零操作模式 / 决策表 / 两条路线 / 验收五条 / 失败矩阵 / 三种环境完整序列 |
| `使用教程.md` | 零基础操作手册（人在浏览器里点） |
| `部署教程.md` | Windows 点鼠标流程（部分历史描述，冲突时以 `INSTALL-AI.md` 为准） |
| `RESUME.md` | 接着开发的第一页（进度、验证命令、当前卡片线） |
| `BACKLOG.md` | 唯一待办（做完写 ✅ + 证据） |
| `deploy/` | 启动器：`bootstrap.sh`（零操作）`start.sh` `status.sh` `wire.sh` `stop.sh` |
| `scripts/stub-bootstrap.sh` | 无 docker 时验证部署脚本（`STUB_RESULT PASS`） |
| `scripts/package_local.sh` | 本机出内嵌前端的单二进制（含 `-tags embedui`） |
| `triage/` | 两套系统之间的批量接口：打分 → 派单（见 `triage/README.md`） |
| `db/migrations/` | 数据库迁移，按文件名升序执行；当前到 `0011` |
| `docs/00-总纲v2-唯一事实源.md` | 机制状态与裁决（"到底是不是这样"看这里） |
| `skills/`、`kb/` | 运行时知识库与技能（**必须随仓库一起存在**，不能只拷 exe） |

## 7. 一句话验收

```bash
curl -fsS http://127.0.0.1:8787/api/health   # 2xx
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8787/   # 200（不是 404 = 前端已内嵌）
bash deploy/status.sh                        # STATUS PASS
```
