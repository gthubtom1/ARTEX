# ARTEX P0 本地验收靶场

> **安全声明：本目录所有靶机仅用于本地或明确授权的测试环境；严禁暴露到公网，严禁用于任何未授权目标。**

这套 fixture 用来给 P0 验收脚本 `scripts/accept_p0.sh` 提供稳定的练习目标。它包含三个互相隔离的容器，全部只绑定 `127.0.0.1`：

| 服务 | 镜像 | 访问地址 | 默认账号 | 练什么 |
|---|---|---|---|---|
| DVWA | `vulnerables/web-dvwa:latest` | http://127.0.0.1:8081 | `admin` / `password` | SQL 注入、反射/存储/DOM XSS、文件上传、命令注入、CSRF、暴力破解 |
| OWASP Juice Shop | `bkimminich/juice-shop:latest` | http://127.0.0.1:3000 | `admin@juice-sh.op` / `admin123`（版本不同可能变化） | 业务逻辑、越权/IDOR、认证绕过、JWT、购物车篡改、OWASP Top 10 |
| VulHub Struts2 | `vulhub/struts2:2.3.31` | http://127.0.0.1:8082 | 无登录 | 已知 CVE：S2-045 / S2-046（CVE-2017-5638，multipart/OGNL RCE） |

> 凭据是**故意写死的测试值**，只用于本地靶场，禁止复用到任何真实系统。本文件不引用外部 `.env`。

---

## 1. 前置条件

- Docker Engine 或 Docker Desktop（Windows / macOS / Linux）
- Docker Compose v2（命令是 `docker compose`，不是旧的 `docker-compose`）
- 建议给 Docker 至少 **4 GB 内存**（三个服务一起跑通常占 1.5–3 GB）
- 本机端口 `8081`、`3000`、`8082` 未被占用

---

## 2. 启动

在仓库根目录执行：

```bash
docker compose -f fixtures/docker-compose.yml up -d
```

只校验 YAML、不启动容器：

```bash
docker compose -f fixtures/docker-compose.yml config
```

查看状态和日志：

```bash
docker compose -f fixtures/docker-compose.yml ps
docker compose -f fixtures/docker-compose.yml logs -f dvwa
```

首次启动时 DVWA 要初始化数据库，Juice Shop / Tomcat 也要几十秒，**等 30–90 秒**再访问。

### DVWA 首次使用

1. 打开 http://127.0.0.1:8081
2. 用 `admin` / `password` 登录
3. 如果提示数据库不存在，点击 **Create / Reset Database**
4. 进入 **DVWA Security**，把安全级别设为 **low**，方便练习

### Juice Shop 首次使用

打开 http://127.0.0.1:3000。预置管理员通常是 `admin@juice-sh.op` / `admin123`；如果版本里不可用，直接注册账号即可（部分版本第一个注册用户会获得管理员权限）。

### VulHub Struts2

打开 http://127.0.0.1:8082 确认服务已起来。用 VulHub 官方对应的 S2-045 / S2-046 PoC 验证即可，命令侧建议先用无副作用的 `id` 或 `whoami`。这是**已知 CVE 靶机**，没有登录入口。

---

## 3. VulHub 镜像标签不确定时的替代方案

`vulhub/struts2:2.3.31` 是 VulHub 常用标签，但 Docker Hub 上的标签可能随仓库调整而变化。如果 `docker compose up -d` 报 `manifest unknown` / `not found`，任选下面一种替代：

### 方案 A：换成 VulHub Tomcat（CVE-2017-12615）

把 `fixtures/docker-compose.yml` 里 `vulhub-struts2` 服务的 `image:` 临时改成：

```yaml
image: vulhub/tomcat:8.5.19
```

端口映射保持 `127.0.0.1:8082:8080` 不变，访问 http://127.0.0.1:8082 。练的是 Tomcat PUT 任意文件上传 RCE（CVE-2017-12615）。

### 方案 B：直接用 VulHub 官方仓库里的 compose

```bash
git clone --depth 1 https://github.com/vulhub/vulhub.git
cd vulhub/struts2/s2-045
docker compose up -d
```

该目录自带的 compose 通常会映射到宿主机的 `8080`，访问 http://127.0.0.1:8080 ；如果与本机其他服务冲突，按该文件里的端口映射自行调整。

> 替代只影响“已知 CVE”这一台靶机；DVWA 和 Juice Shop 不受影响。验收脚本固定轮询 `8081/3000/8082`，所以请保持 VulHub 靶机对外仍是 `8082`。

---

## 4. 停止与清理

停止容器但保留数据：

```bash
docker compose -f fixtures/docker-compose.yml stop
```

停止并删除容器/网络（保留命名卷）：

```bash
docker compose -f fixtures/docker-compose.yml down
```

**彻底清理（含数据卷，DVWA 数据会重置）：**

```bash
docker compose -f fixtures/docker-compose.yml down -v
```

需要连镜像一起删除时（可选，比较暴力）：

```bash
docker compose -f fixtures/docker-compose.yml down -v --rmi all
```

---

## 5. 常见问题

### 端口被占用

先看是谁占了端口：

```bash
# Windows（Git Bash / CMD）
netstat -ano | findstr :8081

# Linux
ss -ltnp | grep ':8081'
# 或
lsof -i :8081
```

Windows 下可结束对应 PID：

```bash
taskkill /PID <PID> /F
```

也可以改 compose 里端口映射的**左边**（宿主机端口），例如把 `127.0.0.1:8081:80` 改成 `127.0.0.1:18081:80`。注意：改完后 `scripts/accept_p0.sh` 里的 `PORTS=(8081 3000 8082)` 也要同步修改，否则验收会失败。

### 镜像拉不下来

先单独测试拉取，确认是网络还是标签问题：

```bash
docker pull vulnerables/web-dvwa:latest
docker pull bkimminich/juice-shop:latest
docker pull vulhub/struts2:2.3.31
```

常见原因：

- 访问 Docker Hub 超时：配置代理或国内镜像加速器
- Docker Hub 匿名拉取限流：`docker login` 后重试
- VulHub 标签不存在：按第 3 节换 `vulhub/tomcat:8.5.19`，或直接用 VulHub 仓库自带 compose
- 公司网络拦截：换网络或使用本地缓存的镜像

### 内存不足 / 容器被 OOM kill

- Docker Desktop → Settings → Resources，把内存调到 **4 GB 以上**
- `docker stats` 看三个容器实时占用
- 先 `docker compose -f fixtures/docker-compose.yml down` 停掉其他项目，再启动靶场
- 只练一个靶机时，可以临时注释掉另外两个 service，或单独 `docker compose ... up -d dvwa`
- 不要在生产/办公主力机上长期挂着三个靶机

### 健康检查一直 unhealthy

先等 60–90 秒；DVWA 首次要初始化 MySQL，Juice Shop / Tomcat 也要启动时间。

```bash
docker compose -f fixtures/docker-compose.yml ps
docker compose -f fixtures/docker-compose.yml logs --tail=100 dvwa
docker inspect --format '{{json .State.Health}}' artex-fixture-dvwa
```

健康检查用的工具分别是：

- DVWA：镜像内的 `php`（`fsockopen`）
- Juice Shop：镜像内的 `node`
- VulHub：`curl` 或 `wget`

如果镜像版本里缺少对应工具，健康检查会报 unhealthy，但 HTTP 端口可能仍然可用；验收脚本是直接轮询 HTTP 端口，不依赖 Docker healthcheck。必要时可在 compose 里把该服务的 `healthcheck` 换成镜像内已有的工具。

### Apple Silicon（ARM64）

VulHub / DVWA 的部分镜像只有 `linux/amd64`。Docker Desktop 可以模拟，但会慢一些：

```bash
DOCKER_DEFAULT_PLATFORM=linux/amd64 docker compose -f fixtures/docker-compose.yml up -d
```

### 想重置 DVWA 数据

```bash
docker compose -f fixtures/docker-compose.yml down -v
docker compose -f fixtures/docker-compose.yml up -d
```

然后重新访问 DVWA 并点 **Create / Reset Database**。

---

## 6. 和 P0 验收脚本一起用

```bash
# 1) 先校验 compose 文件（不启动）
docker compose -f fixtures/docker-compose.yml config

# 2) 设置数据库 DSN（脚本不会创建业务数据）
export ARTEX_PG_DSN='postgres://artex:change-me-please@127.0.0.1:5432/artex?sslmode=disable'

# 3) 跑 P0 验收（脚本会自行 up -d 靶场）
bash scripts/accept_p0.sh
```

脚本除了启动靶场容器外只做只读检查；唯一的数据写入是 `audit_log` 里的 `actor='accept_p0'` 测试记录，并且在同一事务里 `ROLLBACK`。缺 `ARTEX_PG_DSN`、缺 `psql` 或 Docker 不可用时，对应步骤会标 `[skip]`，**跳过不算通过**，最终退出码仍为 `1`。

---

> 再次强调：这些靶机故意包含真实漏洞。**仅限本地/授权环境**，不要改绑 `0.0.0.0` 后暴露到公网。
