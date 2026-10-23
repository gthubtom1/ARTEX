# `deploy/` —— 启动器与可选资产测绘

一个入口，两套系统。默认只跑本体；带上 `--with-asm` 就把资产测绘一起装起来并接好线。

## 命令一览

| 命令 | 做什么 |
|---|---|
| `sudo bash deploy/bootstrap.sh` | **全新机器一条命令**：装 docker/compose/jq/git → 起 docker → 生成 .env → 构建启动 → 装测绘 → 接线 → 体检 |
| `bash deploy/start.sh` | 起本体（`.env` 里 `ASM_ENABLED=1` 时连同测绘一起起） |
| `bash deploy/start.sh --with-asm` | 加装测绘并一起起（幂等） |
| `bash deploy/start.sh --no-asm` | 只起本体，不改配置 |
| `bash deploy/start.sh --rebuild` | 强制重建镜像（改了代码/升级后用） |
| `bash deploy/start.sh --logs` | 起完跟日志 |
| `bash deploy/status.sh` | 体检 → `STATUS PASS` / `STATUS FAIL` |
| `bash deploy/stop.sh` | 停容器，保留数据；`--purge` 连 pg 卷一起删 |
| `bash deploy/enable-asm.sh` | 等价于 `start.sh --with-asm` |
| `bash deploy/disable-asm.sh` | 停测绘保留数据；`--purge` 删数据 |
| `bash deploy/wire.sh` | 只重新接线（建 Key → 写 MCP 数据源 → 验证） |

Windows：`.\deploy\start.ps1`（转给 Git Bash 的同一套脚本）。

## 首次运行做了什么

1. `.env` 不存在 → 从 `.env.example` 生成，并把 `POSTGRES_PASSWORD` / `ARTEX_PASSWORD` / `ARTEX_CRED_KEY` 填成随机值
2. 镜像不存在 → `docker compose up -d --build`（容器内编译，宿主不需要 Go/Node，5–15 分钟）
3. 等 `http://127.0.0.1:8787/` 就绪 → 调 `/api/auth/init` 设登录密码
4. 若 `ASM_ENABLED=1` → 等 8082 就绪 → `wire.sh` 接线
5. 打印地址与密码

## 接线（`wire.sh`）到底做了什么

```
POST http://127.0.0.1:8082/api/user/login          {username,password} → access_token
POST http://127.0.0.1:8082/api/apikey/create       {name:"artex-lab"}  → ssk_...
POST http://127.0.0.1:8787/api/sync/scopesentry/datasource
     {url:"http://scope-sentry:8082/mcp", api_key:"ssk_..."}           → enabled:true
GET  http://127.0.0.1:8787/api/sync/scopesentry/status                 → tools[] 数量
```

`http://scope-sentry:8082/mcp` 是 **容器网络内的地址**，不经过宿主机端口。
所以即使 8082 只绑在 127.0.0.1，ARTEX 照样能同步。

凭据落 `data/asm/credentials.txt`（chmod 600）。

## 目录

```
deploy/bootstrap.sh       零操作安装器（装依赖 → 交棒 start.sh）
deploy/lib.sh            公用函数（.env 生成、compose 探测、等待就绪）
deploy/start.sh          启动器主入口
deploy/start.ps1         Windows 入口（转 bash）
deploy/stop.sh           停止
deploy/status.sh         体检（STATUS PASS / FAIL）
deploy/enable-asm.sh     加装测绘
deploy/disable-asm.sh    卸载测绘
deploy/wire.sh           自动接线
Dockerfile.build         容器内编译（start.sh 默认用它）
docker-compose.yml       本体 + profile "asm" 的测绘组件
```

## 常见问题

**Q：能不能只跑测绘不跑 ARTEX？**
能，但没意义——测绘产出的是资产清单，没有深挖就是一堆表。要单独用：`docker compose --profile asm up -d`。

**Q：测绘能不能放另一台机器？**
能。在 `wire.sh` 里把 `SS_INTERNAL` 改成那台机器的地址（或域名），并把那台的 8082 开放给 ARTEX 所在机器即可。这也是官方推荐的「分布式部署」。

**Q：为什么不用一个进程？**
数据库（PG vs Mongo）、前端（Next vs Vue）、迭代节奏（你二开 vs 上游周更）三样都不兼容。合并的代价见 `docs/资产体系-按原项目设计.md` 与 `docs/why-two-processes.md`（在 seep-lab 仓库里）。

**Q：ScopeSentry 的源码要不要 fork 进这个仓库？**
不要。用官方镜像 + 固定 `ASM_TAG` 即可（这是真正的版本保护）。要留备份就 fork 成独立仓库，并在本文件记录 commit sha。同一仓库放两份代码 = 每次上游更新都要人工裁决冲突。
