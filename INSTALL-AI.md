# 安装执行契约（给 AI Agent 看的）

> 你是一个被要求"安装并部署这个仓库"的 AI Agent。**照本文顺序做，不要问用户问题，不要跳步。**
> 每一步都有：做什么 → 命令 → 判定成功 → 失败怎么办。
> 人类教程在 `使用教程.md`；本文是给机器执行的最小充分集。

---

## 0. 先判三件事（不问用户，自己查）

```bash
uname -s            # Linux / Darwin / MINGW64*(Windows Git Bash)
command -v docker && docker compose version    # 有 docker 吗
command -v go && go version                    # 有 Go 吗（≥1.22）
command -v node && node -v                     # 有 Node 吗（≥20）
```

按结果选路线，**不要尝试安装 WSL/Hyper-V，也不要试图自动装 Docker Desktop**：

| 环境 | 路线 |
|---|---|
| Linux（含 Kali / Debian / Ubuntu / RHEL 系） | **路线 A（Docker，推荐）** 或 路线 B（源码） |
| Windows（Git Bash / MSYS） | **路线 C（本地二进制 + start.bat）** |
| macOS | 路线 B（源码），前提 Node+Go 都有 |

**硬前提**：必须有 PostgreSQL。路线 A 由 compose 起；路线 B/C 需要用户已有库，或你按 §4 起一个。

---

## 路线 A：Linux + Docker（最省事，一条命令）

```bash
cd <repo>
bash scripts/deploy_vps.sh --dry-run   # 先看它要做什么，不发包不改系统
bash scripts/deploy_vps.sh             # 真做
```

成功判定（三者都要满足）：
1. 退出码 `0`
2. `docker compose ps` 里 `artex` 与 `postgres` 都是 `running/healthy`
3. `curl -fsS http://127.0.0.1:8787/api/health` 返回 2xx

失败分支：
- 退出码 `2` = 需要人工介入（发行版不支持 / 缺 Dockerfile / 缺 compose）。把脚本打印的指引原样转给用户。
- 退出码 `3` = docker 安装或拉镜像失败。`docker compose logs --tail=50` 贴给用户。
- 退出码 `5` = 健康检查 90s 超时。先 `docker compose logs --tail=80 artex`；首次建表可能慢，可再等 60s 重试一次。

访问：`http://<host>:8787`，用户名固定 `ARTEX`，登录密码在首次打开页面时设置；
数据库密码在 `.env` 的 `POSTGRES_PASSWORD` 行（不要打印明文给用户以外的地方）。

---

## 路线 B：Linux/macOS 源码编译

```bash
cd <repo>
# 1) 后端
go build -o artex ./cmd/artex

# 2) 前端（必须；跳过的话 UI 会 404，API 仍可用）
cd web && NODE_ENV=development npm ci --include=dev --ignore-scripts && npm run build:static && cd ..
rm -rf server/webui/dist && mkdir -p server/webui/dist && cp -a web/out/. server/webui/dist/
go build -tags embedui -o artex ./cmd/artex      # 重新编译，把前端嵌进去

# 3) 数据库（若用户没有）
sudo -u postgres createuser -P artex
sudo -u postgres createdb -O artex artex

# 4) 配置并启动
cp config.example.json config.json   # 把 database.dsn 改成 postgres://artex:<密码>@127.0.0.1:5432/artex?sslmode=disable
ARTEX_ALLOW_DIRECT=1 ./start.sh      # 打本地靶场时加这个；只让本机访问
```

成功判定：日志出现 `ARTEX dev backend listening on 127.0.0.1:8787`，且 `curl -fsS 127.0.0.1:8787/api/health` 2xx。
要从别的机器访问：改用 `./start.sh -addr 0.0.0.0:8787`。

---

## 路线 C：Windows（用户双击 start.bat）

```bash
cd <repo>
bash scripts/package_local.sh <ver>     # 产出 dist/artex.exe（前端已内嵌）
```

然后**什么都不用搬**：`start.bat` 会自动按 `.\artex.exe → .\dist\artex.exe → .\artex-win-amd64.exe` 找二进制。

需要用户做的只有一件：把 `config.example.json` 复制成 `config.json` 并填 DSN（见 §4）。
用户之后**双击 `start.bat`** 即用；重复双击不会起第二个实例（会提示已在运行并打开浏览器）。

成功判定：黑窗口出现 `ARTEX dev backend listening on 127.0.0.1:8787`。

---

## 4. 起一个 PostgreSQL（用户没有库时）

Linux：
```bash
sudo apt install -y postgresql        # RHEL 系: sudo dnf install -y postgresql-server
sudo -u postgres createuser -P artex  # 记下密码
sudo -u postgres createdb -O artex artex
```

Windows：让用户装 PostgreSQL 16（https://www.postgresql.org/download/windows/），
然后在 SQL Shell 里执行 `createuser -P artex` + `createdb -O artex artex`。

`config.json` 只有这一行是必须的：
```json
{ "database": { "dsn": "postgres://artex:<密码>@127.0.0.1:5432/artex?sslmode=disable" } }
```

---

## 5. 装完必须做的三件事（交付验收）

1. `go build ./... && go vet ./...`（源码路线）；或 `docker compose ps` 全 healthy（Docker 路线）
2. `curl -fsS http://127.0.0.1:8787/api/health` → 2xx
3. 打开首页 → 应看到登录/设密码页（前端已内嵌）。若 404，回路线 B 的第 2 步补前端。

**给用户的交付说明只写三句**：
- 打开地址 `http://127.0.0.1:8787`，用户名 `ARTEX`，首次自己设密码
- 进「系统 → 首次配置」四步：数据库 → 模型（provider/model/api_key）→ 出口自检 → 完成
- 首页「贴一个网址，直接开跑」

---

## 6. 不要做的事

- 不要为了"跑起来"删 `.gitignore` 里的忽略项，也不要把 `dist/` 提交进 git。
- 不要把 API Key / 数据库密码打印到日志、提交信息或任何输出文件里。
- 不要在没有用户明确允许时改防火墙、开公网端口（`-addr 0.0.0.0` 需要用户点头）。
- 不要试图"修"网络出口：应用内没有代理配置，出网由宿主层负责（VPS 直连或 TUN）。
