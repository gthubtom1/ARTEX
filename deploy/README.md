# `deploy/` —— 启动器与可选资产测绘

一个入口，两套系统。默认只跑本体；带上 `--with-asm` 就把资产测绘一起装起来并接好线。

## 命令一览

| 命令 | 做什么 |
|---|---|
| `sudo bash deploy/bootstrap.sh` | **全新机器一条命令**：装 docker/compose/jq/git → 起 docker → 生成 .env → 构建启动 → 装测绘 → 接线 → 体检 |
| `bash deploy/bootstrap.sh --no-asm` | 同上，但不装测绘（内存 < 8G 时建议） |
| `bash deploy/start.sh` | 起本体（`.env` 里 `ASM_ENABLED=1` 时连同测绘一起起） |
| `bash deploy/start.sh --with-asm` | 加装测绘并一起起（会写 `ASM_ENABLED=1`，幂等） |
| `bash deploy/start.sh --no-asm` | **本次**只起本体，不改 `.env` 里的 `ASM_ENABLED` |
| `bash deploy/start.sh --rebuild` | 强制重建本体镜像（改了代码/升级后用） |
| `bash deploy/start.sh --logs` | 起完跟日志 |
| `bash deploy/status.sh` | 体检 → `STATUS PASS` / `STATUS FAIL` |
| `bash deploy/stop.sh` | 停容器，保留数据；`--purge [--yes]` 连 pg 卷一起删 |
| `bash deploy/enable-asm.sh` | 等价于 `start.sh --with-asm` |
| `bash deploy/disable-asm.sh` | 停测绘保留数据；`--purge [--yes]` 删 `data/asm` |
| `bash deploy/wire.sh` | 只重新接线（建 Key → 写 MCP 数据源 → 验证） |

Windows：`.\deploy\start.ps1`（优先转给 Git Bash 的同一套脚本；没有 Git Bash 时退化为
`docker compose` 只起本体，不能加装测绘）。

## 首次运行做了什么

1. `.env` 不存在 → 从 `.env.example` 生成，并把 `POSTGRES_PASSWORD` / `ARTEX_PASSWORD` / `ARTEX_CRED_KEY` 填成随机值
2. 镜像不存在 → `docker compose up -d --build`（容器内编译，宿主不需要 Go/Node，5–15 分钟）
3. 等 `http://127.0.0.1:8787/api/auth/status`（会读库）就绪 → 调 `/api/auth/init` 设登录密码
4. 若本次启用测绘 → 等 8082 就绪 → `wire.sh` 接线
5. 打印地址与密码（密码只在交互终端打印；重定向到日志时写“见 .env”）

## 资产测绘（asm）是什么

ScopeSentry 是一个**外部项目**，不是本仓库的代码：

- Web/API/MCP：<https://github.com/Autumn-27/ScopeSentry>
- 扫描节点：<https://github.com/Autumn-27/ScopeSentry-Scan>
- 镜像：`autumn27/scopesentry:<ASM_TAG>` 与 `autumn27/scopesentry-scan:<ASM_SCAN_TAG>`

本仓库只做三件事：compose 里用 `profiles: ["asm"]` 起这 4 个容器、`wire.sh` 把 MCP
接进 ARTEX、`status.sh` 验证。**不会**拉取/合并上游源码。

**版本锁定**：两个上游仓库发版不同步（Web 通常领先扫描节点），所以 `.env` 里分成两个
tag：`ASM_TAG`（Web）与 `ASM_SCAN_TAG`（扫描节点）。默认 `ASM_TAG=1.9.4`、
`ASM_SCAN_TAG=latest`；扫描节点确认有对应版本 tag 后再改。要彻底锁死可换成
`image@sha256:<digest>`（`docker image inspect` 可查）。

端口：容器内固定 8082 / 27017 / 6379；宿主默认只绑 `127.0.0.1`（`ASM_BIND`）。
`wire.sh` 用的是容器网络地址 `http://scope-sentry:8082/mcp`，不经过宿主端口。

## 接线（`wire.sh`）到底做了什么

```
POST http://127.0.0.1:8082/api/user/login          {username,password} → data.access_token
GET  http://127.0.0.1:8082/api/apikey/list         → 旧的 artex-lab Key id（用于清理）
POST http://127.0.0.1:8082/api/apikey/create       {name:"artex-lab"}  → data.key = ssk_...
POST http://127.0.0.1:8787/api/sync/scopesentry/datasource
     {url:"http://scope-sentry:8082/mcp", api_key:"ssk_..."}           → enabled:true
GET  http://127.0.0.1:8787/api/sync/scopesentry/status                 → tools[] 数量
POST http://127.0.0.1:8082/api/apikey/delete       删除本轮之前的 artex-lab Key
```

这些 ScopeSentry 路由来自上游 `internal/api/routes/{user,apikey}`；ARTEX 侧
`/api/sync/scopesentry/*` 在 `server/sync_scopesentry.go`。数据源写到 PostgreSQL 的
`mcp_servers` 表（名字固定 `ScopeSentry`、传输 `http`、头 `X-API-Key`），重启不丢。

幂等：重复跑 `wire.sh` 会新建一把 Key、更新同一行 `mcp_servers`、再删掉旧
`artex-lab` Key，最终只保留一把。旧 Key 的删除发生在新 Key 写入成功之后，中途失败
不会把现有接线弄断。退出码：0 已接通 / 2 登录失败 / 3 建 Key 失败 / 4 ARTEX 侧失败 /
5 已保存但工具数为 0（初始化未完成，稍后重跑）。

凭据落 `data/asm/credentials.txt`（目录 700、文件 600，含 API Key 与 ScopeSentry 密码，
勿外传）。

## 体检判据

`status.sh` 的 PASS 需要同时满足：`.env` 权限 600、ARTEX `/api/health`、登录、
`schema_migrations` 已到 `0010`、`/api/system/hitrate` 可访问（有任务时 200，无任务时
探到 handler 的 JSON 404 `task not found`）。启用测绘时还要：8082 可达、MCP 数据源
`enabled` 且工具数 > 0、Mongo/Redis 密码不是默认值。ScopeSentry 首次初始化要
3–10 分钟，此时 FAIL 属正常，重跑 `status.sh` 即可。

## 目录

```
deploy/bootstrap.sh       零操作安装器（装依赖 → 交棒 start.sh → 体检）
deploy/lib.sh             公用函数（.env 生成、compose 探测、等待就绪）
deploy/start.sh           启动器主入口
deploy/start.ps1          Windows 入口（转 Git Bash；无 Git Bash 时退化）
deploy/stop.sh            停止（--purge 删 pg 卷）
deploy/status.sh          体检（STATUS PASS / FAIL）
deploy/enable-asm.sh      加装测绘
deploy/disable-asm.sh     卸载测绘
deploy/wire.sh            自动接线
Dockerfile.build          容器内编译（start.sh 默认用它）
docker-compose.yml        本体 + profile "asm" 的测绘组件
```

## 常见问题

**Q：能不能只跑测绘不跑 ARTEX？**
能，但没意义——测绘产出的是资产清单，没有深挖就是一堆表。要单独用：`docker compose --profile asm up -d`。

**Q：测绘能不能放另一台机器？**
能。把 `ASM_BIND` 改成 `0.0.0.0`（或只放行 ARTEX 所在机器），再改 `deploy/wire.sh`
里的 `SS_INTERNAL` 为那台机器的地址。注意扫描节点仍要求 host 网络 + 本机 Mongo/Redis。

**Q：为什么不用一个进程？**
数据库（PG vs Mongo）、前端（Next vs Vue）、迭代节奏（你二开 vs 上游周更）三样都不兼容。

**Q：ScopeSentry 的源码要不要 fork 进这个仓库？**
不要。用官方镜像 + 分开锁定 `ASM_TAG` / `ASM_SCAN_TAG` 即可。要留备份就 fork 成独立
仓库，并在本文件记录 commit sha。同一仓库放两份代码 = 每次上游更新都要人工裁决冲突。
