# 安装执行手册（给 AI Agent）

> 你被要求"把这个仓库装好，让用户以后一点就能用"。**照本文执行，不要问用户问题。**
> 每一步都有：做什么 → 命令 → 判定成功 → 失败怎么办。
> 人类的教程在 `使用教程.md`；本文是给机器执行的最小充分集，写得比人类版更啰嗦是故意的。

**入口索引（按顺序看，不要通读全文）**

| 你要做什么 | 看哪 | 命令 |
|---|---|---|
| 用户说「都装 / 零操作」 | 下面的 ⚡ 零操作模式 | `sudo bash deploy/bootstrap.sh` |
| 用户没说装不装测绘 | §0 末的「只允许问的一个问题」 | 先起本体，再问 |
| 安装/接线细节 | `deploy/README.md` | — |
| 出错了 | §7 失败矩阵 / §12.5 测绘失败分支 | — |
| 测绘是什么、产出什么 | `deploy/README.md` §接线 / `docs/资产体系-按原项目设计.md` | — |

---

## ⚡ 零操作模式（用户说「全装」「我零操作」「你自己搞定」时走这条）

**不要问任何问题**（包括「要不要装测绘」）。直接执行：

```bash
git clone <用户给的仓库链接> artex-lab && cd artex-lab
sudo bash deploy/bootstrap.sh
```

`bootstrap.sh` 自己做完：装 docker/compose/jq/git/openssl → 起 docker → 加组 → 生成 .env
→ 构建并启动 → 设登录密码 → 装资产测绘 → 自动接线 → 体检。

判定成功（三条全过）：

```bash
bash deploy/status.sh                                  # STATUS PASS
curl -fsS http://127.0.0.1:8787/ >/dev/null && echo A_OK
curl -fsS http://127.0.0.1:8082/ >/dev/null && echo S_OK    # 只装本体时可省
```

然后**只做一件事**：把下面两把 Key 交给用户自己填（这是唯一无法自动化的步骤，因为要花钱/要账号）：

1. ARTEX → 系统 → LLM（模型 + API Key）
2. ScopeSentry → 配置 → FOFA（email + key）或 Quake/Hunter

用户如果直接把 Key 给了你：

```bash
# LLM：写进 .env 后重启即生效（ARTEX 启动时读 env）
#   ARTEX_LLM_PROVIDER / ARTEX_LLM_MODEL / ARTEX_LLM_BASE_URL / OPENAI_API_KEY
bash deploy/start.sh
# 测绘的 FOFA Key：在 8082 界面里填（存在 Mongo 里，没有稳定的写入接口）
```

**失败就按 §12.5 与 §7 的矩阵排查，不要自由发挥。** 剩余内容（§0 及以下）是通用安装契约，零操作模式下可跳过。

---

---

## 0. 你要产出的终态（只有这三种，别的都算没装完）

| 用户环境 | 终态（用户以后的操作） |
|---|---|
| Windows | 双击 `start.bat`（或 `启动.bat`）→ 浏览器开 `http://127.0.0.1:8787` |
| Linux / Kali / VPS | 终端执行 `./start.sh`（或 `docker compose up -d`）→ 浏览器开 `http://<host>:8787` |
| 任意 | 浏览器里能打开登录页 = 装完 |

**装完必须留下三样**：① 数据库在跑 ② 服务能起来 ③ 用户知道"点哪里/敲什么"。缺一个都不算完成。

**装完之后允许问且只允许问一个问题**（不是澄清，是选装配件）：

```
本体已经能用了。要不要顺手把「资产测绘」（ScopeSentry）一起装上？
装了以后：你给它一个域名，它先扫出这个域名名下所有子域/端口/站点/指纹/已知漏洞，
再把值得打的目标交给你手上的 ARTEX 深挖。不装也能用，只是目标要你自己一个一个贴。
占资源约：4 核 8G、10G 磁盘；首次初始化 3-10 分钟。
```

用户说要 → 执行 §12。用户说不要 → 结束，并告知以后加装只需一条命令。

---

## 1. 第一步：环境自检（不要凭猜测选路线）

```bash
# Linux / macOS / Windows(Git Bash)
bash scripts/detect-env.sh

# Windows 原生 PowerShell（Git Bash 不可用时）
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\detect-env.ps1
```

输出是 `KEY=VALUE`。**把你读到的这些值写进你的计划里**，然后按下表选路线：

```
OS=            linux | darwin | windows-gitbash
PKG_MANAGER=   apt | dnf | yum | pacman | brew | winget | choco | none
DOCKER=        yes/no          DOCKER_COMPOSE= yes/no
GO=            yes/no          NODE= / NPM=
PSQL=          yes/no
PORT_5432= open/closed         PORT_8787= open/closed
ARTEX_BINARY=  artex | dist/artex.exe | artex-win-amd64.exe | (缺失)
HAS_CONFIG=    yes/no          HAS_SKILLS= / HAS_KB=
```

---

## 2. 决策表（照着走，不要自由发挥）

| OS | DOCKER | 二进制 | 结论 |
|---|---|---|---|
| linux | yes | — | **路线 A**（Docker，最快） |
| linux | no | — | 有 `apt/dnf` → **路线 A'**（先装 docker 再 A）；没有包管理器或用户禁止装 → **路线 B** |
| darwin | yes | — | 路线 A |
| darwin | no | — | 路线 B（需 `brew install go node`） |
| windows-gitbash | yes | — | 路线 A（`docker compose up -d`） |
| **windows-gitbash** | **no** | 有 `dist/artex.exe` | **路线 C-1**：`bash scripts/package_local.sh v1` 然后双击 `start.bat` |
| **windows-gitbash** | **no** | 有 `artex.exe` / `artex-win-amd64.exe` | **路线 C-2**：直接双击 `start.bat` |
| **windows-gitbash** | **no** | 二进制缺失 + 有 Node | 路线 C-1（先 `npm ci` 再打包） |
| **windows-gitbash** | **no** | 二进制缺失 + 无 Node | **路线 C-3**：让用户从 Releases 下 `windows-amd64` 的 zip（你无法在无 Node 机器上产出内嵌前端） |

**硬前提：必须有 PostgreSQL。** 路线 A 由 compose 起；路线 B/C 需要 §5 的三选一。

---

## 3. 路线 A：Docker（Linux / macOS / Windows 有 Docker 时）

```bash
cd <repo>
bash scripts/deploy_vps.sh --dry-run   # 先看计划，不发包不改系统
bash scripts/deploy_vps.sh             # 真执行
```

> **Docker 路线有个前置**：镜像要用 `Dockerfile` **本地构建**（默认不再拉上游镜像
> `ruoji6/artex` —— 那是上游作者的，不含本 fork 能力且数据库迁移对不上）。
> `deploy_vps.sh` 会检查 `dist/amd64/artex`：缺了就**自动**用
> `bash scripts/package_linux.sh` 构建（需要 Go + Node）；两者都没有时报错并给两条出路。
> 手工等价命令：
> ```bash
> bash scripts/package_linux.sh        # → dist/amd64/artex（前端已内嵌）
> docker compose up -d --build
> ```

`deploy_vps.sh` 做的事：检测发行版 → 装 docker（缺失时）→ 生成随机 PG 密码写 `.env` → `docker compose up -d --build` → 轮询 `/api/health` 90s。

**按退出码处理（这是它设计好的契约）**：

| 退出码 | 含义 | 你该做什么 |
|---|---|---|
| 0 | 成功 | 跳到 §6 验收 |
| 2 | 需要人工介入（发行版不支持 / 缺 Dockerfile / 缺 compose） | 把脚本打印的指引原样转给用户，**不要自己绕过** |
| 3 | docker 安装或镜像拉取失败 | `docker compose logs --tail=50`；国内网络可在脚本里用 `--mirror Aliyun` |
| 4 | 容器启动失败 | `docker compose ps` + `logs`；常见是 5432/8787 端口被占 |
| 5 | 健康检查 90s 超时 | 首次建表可能慢：再等 60s 重试一次 `curl /api/health`；仍失败看 `logs artex` |

Windows 有 Docker 时不需要这个脚本，但要先生成 Linux 二进制（Git Bash 里）：

```bash
bash scripts/package_linux.sh    # 需要 Go + Node
docker compose up -d --build
```

---

## 4. 路线 B：源码编译（Linux/macOS，或 Windows 有 Go+Node 时）

### 4.1 装依赖（按 `PKG_MANAGER` 选一条）

```bash
# Debian / Ubuntu / Kali
sudo apt update && sudo apt install -y golang-go nodejs npm postgresql-client git

# RHEL / CentOS / Rocky / Alma
sudo dnf install -y golang nodejs npm postgresql git

# Arch
sudo pacman -S --noconfirm go nodejs npm postgresql git

# macOS
brew install go node postgresql git
```

Go 需要 ≥1.22、Node ≥20。装完重跑 `scripts/detect-env.sh` 确认 `GO=yes NODE=yes`。

### 4.2 编后端

```bash
cd <repo>
go build -o artex ./cmd/artex
```

### 4.3 编前端并内嵌（**不能跳过**，否则网页 404、API 正常）

```bash
cd web
NODE_ENV=development npm ci --include=dev --ignore-scripts   # 注意必须带 --include=dev，否则 next build 缺 typescript
npm run build:static
cd ..
rm -rf server/webui/dist && mkdir -p server/webui/dist
cp -a web/out/. server/webui/dist/
go build -tags embedui -o artex ./cmd/artex
```

### 4.4 数据库（用户没有时，见 §5）

### 4.5 配置并启动

```bash
cp config.example.json config.json      # 改 database.dsn
./start.sh                              # 前台；崩了自动拉起
# 打本地靶场： ARTEX_ALLOW_DIRECT=1 ./start.sh
# 允许外部访问： ./start.sh -addr 0.0.0.0:8787   ← 需要用户同意
```

---

## 5. 数据库三选一

**5.1 用 Docker 起一个 PG（最省事，Linux/macOS/Windows 有 Docker）**

```bash
docker run -d --name artex-pg -p 127.0.0.1:5432:5432 \
  -e POSTGRES_USER=artex -e POSTGRES_PASSWORD=<随机密码> -e POSTGRES_DB=artex \
  -v artex-pgdata:/var/lib/postgresql/data postgres:16
```

**5.2 本机装 PG**

```bash
# Debian 系
sudo apt install -y postgresql && sudo systemctl enable --now postgresql
sudo -u postgres createuser -P artex      # 记下密码
sudo -u postgres createdb -O artex artex

# RHEL 系
sudo dnf install -y postgresql-server && sudo postgresql-setup --initdb
sudo systemctl enable --now postgresql
sudo -u postgres createuser -P artex && sudo -u postgres createdb -O artex artex

# Windows：装 PostgreSQL 16 安装包，在 SQL Shell 里执行上面两条 createuser/createdb
```

**5.3 用户已有 PG** → 只要问出「主机/端口/用户/密码/库名」，拼成 DSN 即可。

**然后写 `config.json`（这个文件是 gitignore 的，不会污染仓库）**：

```json
{
  "database": {
    "dsn": "postgres://artex:<密码>@127.0.0.1:5432/artex?sslmode=disable"
  }
}
```

---

## 6. 验收（5 条，缺一不可）

```bash
# ① 数据库连通
psql "<dsn>" -c 'select 1'           # 没有 psql 就跳过，靠 ③ 判断

# ② 服务在监听
curl -fsS http://127.0.0.1:8787/api/health   # 期望 2xx

# ③ 迁移真的跑了（Docker 路线看容器日志）
docker compose logs artex | grep "migration applied"   # 期望 0001..0004 四行

# ④ 前端已内嵌（不是 404）
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8787/     # 期望 200

# ⑤ 关键接口活着
curl -fsS -H "Authorization: Bearer <登录后的 token>" http://127.0.0.1:8787/api/settings | head -c 200
```

首次打开网页会让你设管理员密码（用户名固定 `ARTEX`）。**不要把密码/key 写进你的输出或提交里。**

---

## 7. 失败矩阵（症状 → 原因 → 动作）

| 症状 | 原因 | 动作 |
|---|---|---|
| `open stores: 未找到数据库配置` | 没有 `config.json`，也没设 `ARTEX_PG_DSN` | 按 §5.3 写 `config.json` |
| `dial tcp 127.0.0.1:5432: connect: connection refused` | PG 没起 / 端口不对 | 起 PG（§5.1/5.2），或改 DSN 端口 |
| `password authentication failed for user "artex"` | 密码错 | 重置：`ALTER USER artex WITH PASSWORD '...'` |
| `serve: listen tcp 127.0.0.1:8787: bind: ...` | 端口被占（常见于开了两个窗口） | `netstat -ano \| findstr :8787` 找 PID；或换 `-addr 127.0.0.1:8799` |
| 网页 `404`、但 `/api/health` 200 | 前端没编进去 | 回 §4.3 完整跑一遍（含 `-tags embedui` 重编） |
| `web/node_modules 不存在` | 没 `npm ci` | `cd web && NODE_ENV=development npm ci --include=dev --ignore-scripts` |
| `next build` 报缺 `typescript`/`@types/*` | `npm ci` 在 `NODE_ENV=production` 下跳过了 devDeps | 用上面带 `--include=dev` 的命令 |
| 任务一建就 `paused` | **P9 之后不再由此原因暂停**；若出现请查 `task_error_events` | 出口 IP 变了只会出现在「系统 → 网络」 |
| DLL/权限错误（Windows） | 杀软拦截或路径含空格 | 换到 `D:\ARTEX` 这类纯英文路径 |
| `go: command not found` | 没装 Go | §4.1 |

---

## 8. 交付给用户的话术（装完照抄，只改地址）

```
装好了。以后这样用：

1) 启动：<双击 start.bat ／ 终端执行 ./start.sh ／ docker compose up -d>
2) 打开：http://<127.0.0.1 或 VPS_IP>:8787
3) 用户名 ARTEX，密码你第一次打开时自己设
4) 进去后：系统 → 首次配置（四步：数据库 / 模型 / 出口自检 / 完成）
5) 然后首页「贴一个网址，直接开跑」

数据库：<Docker 容器 artex-pg ／ 本机 PostgreSQL>，密码在 <位置>。
如果打不开：先看终端最后 5 行，常见是数据库没起或端口被占。
```

---

## 9. 三种环境的完整序列（照抄即可）

### 9.1 Kali（有 GUI，但这是**网页应用**，最终在浏览器里用）

```bash
# ① 拿代码
git clone <repo> ARTEX && cd ARTEX

# ② 自检
bash scripts/detect-env.sh

# ③ 一条命令装（Debian 系，脚本自己装 docker + 起 PG + 起服务）
bash scripts/deploy_vps.sh

# ④ 验证
curl -fsS http://127.0.0.1:8787/api/health && echo OK
# 浏览器：http://127.0.0.1:8787   用户名 ARTEX  首次设密码
```

Kali 上不想装 Docker 就走路线 B（`sudo apt install -y golang-go nodejs npm postgresql` 后按 §4）。

### 9.2 VPS（Ubuntu/Debian，无桌面）

```bash
ssh -o ConnectTimeout=15 -o ServerAliveInterval=15 -o ServerAliveCountMax=3 root@<VPS_IP>
apt update && apt install -y git
git clone <repo> ARTEX && cd ARTEX
bash scripts/deploy_vps.sh
curl -fsS http://127.0.0.1:8787/api/health
# 浏览器：http://<VPS_IP>:8787   （记得在云控制台安全组放行 8787）
```

### 9.3 Windows（无 Docker）

```bash
# ① 自检（Git Bash 里）
bash scripts/detect-env.sh

# ② 有 Node：产出内嵌前端的二进制
cd web && NODE_ENV=development npm ci --include=dev --ignore-scripts && cd ..
bash scripts/package_local.sh v1        # 产出 dist/artex.exe

# ③ 没有 Node：跳过 ②，让用户从 Releases 下 windows-amd64 的 zip 解压

# ④ 装 PostgreSQL 16（https://www.postgresql.org/download/windows/）
#    然后 SQL Shell 里： createuser -P artex / createdb -O artex artex

# ⑤ 在仓库根写 config.json（见 §5.3）

# ⑥ 双击 start.bat   或者命令行： cmd /c start.bat
```

`start.bat` 会自动找二进制（`.\artex.exe` → `.\dist\artex.exe` → `.\artex-win-amd64.exe`），**不需要改名或搬文件**。
重复双击不会起第二个实例：会提示 `already running on 127.0.0.1:8787` 并自动打开浏览器。

---

## 10. 禁止项

- **不要**把 `dist/`、`config.json`、`.env`、`data/` 提交进 git（都在 `.gitignore` 里）。
- **不要**把 API Key / 数据库密码打印到日志、提交信息、或任何可分享的输出。
- **不要**在用户没明确同意时 `-addr 0.0.0.0` 或改防火墙 / 安全组。
- **不要**试图"修网络出口"：应用内**没有**代理/Clash 配置，出网由宿主层负责（VPS 直连 / 虚拟机 TUN）。
- **不要**为了跑起来去删 `.gitignore` 里的忽略项。
- **不要**改仓库里的二进制或脚本去"绕过"错误 —— 先按 §7 定位原因。

---

## 11. 一句话判断"装完了没有"

```
curl -fsS http://127.0.0.1:8787/api/health   # 2xx
curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8787/   # 200（不是 404）
```

两条都过 → 装完了，把 §8 的话术发给用户。
任何一条不过 → 回 §7 的失败矩阵，从症状找原因，不要猜。

---

## 12. 可选组件：资产测绘（ScopeSentry）

### 12.1 它解决什么

ARTEX 接一个目标；测绘接一个域名，产出"这个域名名下所有资产 + 指纹 + 已知漏洞命中"。
两者由原作者预置的接口相连，不需要改业务代码。

### 12.2 硬前提（不满足就别装，换成轻量三命令路线）

| 条件 | 判据 | 不满足怎么办 |
|---|---|---|
| Linux 宿主 | `uname -s` = Linux | Docker Desktop 不支持 `network_mode: host`，测绘的扫描节点起不来 |
| 内存 ≥ 8G | `free -g` | 低于 8G 走轻量路线：`uncover` + `httpx` + `nuclei`，不装 Mongo |
| docker compose | `docker compose version` | 先装 |

### 12.3 一条命令

```bash
cd <repo>
bash deploy/start.sh --with-asm
```

它做的事（幂等）：
1. 往 `.env` 补 `ASM_*` 随机密码、`ASM_ENABLED=1`
2. `docker compose --profile asm up -d`（起 mongo/redis/scope-sentry/asm-scan）
3. 等 `http://127.0.0.1:8082/` 就绪（**首次 3-10 分钟**，Mongo 要灌内置指纹/PoC 库）
4. `deploy/wire.sh`：ScopeSentry 登录 → 建 API Key → 写进 ARTEX 的 MCP 数据源 → 验证工具数
5. `deploy/status.sh` 体检

### 12.4 判据（全过才算装完）

```bash
docker compose --profile asm ps            # 6 个容器 running
curl -fsS http://127.0.0.1:8082/ >/dev/null && echo SS_OK
bash deploy/status.sh                      # STATUS PASS，且「MCP 数据源 enabled，N 个工具」N>0
```

### 12.5 失败分支

| 症状 | 原因 | 动作 |
|---|---|---|
| `wire.sh` 退出码 2 | ScopeSentry 登录失败 | 默认 `ScopeSentry/ScopeSentry`；改过就写进 `.env` 的 `ASM_ADMIN_PASSWORD` |
| `wire.sh` 退出码 3 | 建 Key 失败 | 服务端还在初始化 → 等 2 分钟重跑 |
| `wire.sh` 退出码 4 | ARTEX 侧失败 | 检查 `.env` 的 `ARTEX_PASSWORD`；`docker compose logs artex` |
| 工具数 0 | MCP 未完成 discovery | 重跑 `bash deploy/wire.sh` |
| `asm-scan` 反复重启 | host 网络不支持 | 换 Linux 宿主；Windows 用 WSL2 内的 Linux |

### 12.6 装完之后必须告诉用户的三件事（不要代做）

1. ScopeSentry → 配置：填测绘引擎 Key（FOFA 的 `email+key`，或 Quake / Hunter）。不填只能靠爆破字典，覆盖差一个量级。
2. ScopeSentry 登录后立刻改默认密码 `ScopeSentry/ScopeSentry`；改完同步写进 `.env` 的 `ASM_ADMIN_PASSWORD`。
3. 日常流程：测绘建项目（根域名）→ 建任务（勾子域/端口/指纹/敏感信息/漏洞）→ ARTEX「功能 → 资产同步」选项目同步 → 挑高分的开任务。

### 12.7 卸载

```bash
bash deploy/disable-asm.sh            # 停测绘、保留数据
bash deploy/disable-asm.sh --purge    # 连数据一起删
```

ARTEX 本体不受影响；数据源行会留着但显示未启用，不报错。
