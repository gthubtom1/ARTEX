#!/usr/bin/env bash
# ARTEX 安装脚本：① 全部 Docker  ② 本地编译运行
#
# ⚠️ 新装机请优先用 `deploy/bootstrap.sh`（装依赖 → 起 docker → 生成 .env → 构建/启动
#    → 设登录密码 → 可选资产测绘 → 体检，一条命令）。本脚本保留给老用户与离线/受限场景。
#
# 镜像路线（① 全部 Docker）：
#   · ARTEX_IMAGE 未设或 =artex-fork（默认，本仓库本地构建，没有 registry）→ 只 build，不 pull
#   · 显式设了远程 registry 镜像（如 ghcr.io/you/artex）→ docker compose pull
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }
# read 在非交互环境（管道 / CI）会直接 EOF：给默认值而不是让 set -e 把脚本打死。
ask(){  local p="$1" d="${2:-}" a=""; read -rp "$p${d:+ [$d]}: " a || true; echo "${a:-$d}"; }
rand(){ if command -v openssl >/dev/null 2>&1; then openssl rand -hex 16; else head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24; fi; }
rand_hex(){ local n="${1:-32}"; if command -v openssl >/dev/null 2>&1; then openssl rand -hex "$n"; else od -An -tx1 -N "$n" /dev/urandom | tr -d ' \n'; fi; }

# 读 .env 里的值（不 source，避免用户自定义值里的空格/特殊字符被执行）
env_get(){ grep -E "^$1=" .env 2>/dev/null | tail -1 | cut -d= -f2- | tr -d '\r' || true; }

# set_env_kv KEY VALUE —— 仅当 KEY 为空/不存在/占位值时写入（已有值不覆盖）
set_env_kv(){
  local k="$1" v="$2" cur
  cur="$(env_get "$k")"
  if [ -n "$cur" ] && [ "$cur" != "change-me-please" ]; then return 0; fi
  if grep -qE "^${k}=" .env 2>/dev/null; then
    awk -v k="$k" -v v="$v" 'BEGIN{FS=OFS="="} $1==k{print k"="v; next} {print}' .env > .env.tmp \
      && chmod 600 .env.tmp && mv .env.tmp .env
  else
    printf '%s=%s\n' "$k" "$v" >> .env
  fi
}

# 默认镜像 artex-fork 是本仓库本地构建的（没有 registry）→ 只能 build，pull 必失败。
is_local_image(){ [ -z "${1:-}" ] || [ "$1" = "artex-fork" ]; }

# 等本体就绪 → 调 /api/auth/init 设登录密码（已设过返回 403，忽略）。
# 就绪判据与 deploy/start.sh 一致：/api/auth/status 返回 200/401/403 都算服务+库已起来
# （启用 HTTP Basic Auth 时网关会先拦成 401/403）。
init_login_password(){
  local port="${1:-8787}" pw payload i=0 code="" init_code=""
  pw="$(env_get ARTEX_PASSWORD)"
  [ -n "$pw" ] || { warn "ARTEX_PASSWORD 为空，跳过登录密码初始化"; return 0; }
  if ! command -v curl >/dev/null 2>&1; then
    warn "未找到 curl：跳过就绪等待与登录密码初始化（可打开 http://localhost:${port}/setup 手动设置，密码已写入 .env）"
    return 0
  fi
  info "等待服务就绪…"
  while [ "$i" -lt 120 ]; do
    code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:${port}/api/auth/status" 2>/dev/null || true)"
    case "$code" in 200|401|403) break ;; esac
    i=$((i+1)); sleep 3
  done
  case "$code" in
    200|401|403) ;;
    *) warn "服务未在 6 分钟内就绪（最后 HTTP ${code:-无响应}）：docker compose logs -f artex"
       return 0 ;;
  esac
  # JSON 转义：密码默认是十六进制，但用户可改成任意值（含引号/反斜杠）。
  payload="$(printf '{"password":"%s"}' "$(printf '%s' "$pw" | sed 's/\\/\\\\/g; s/"/\\"/g')")"
  init_code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${port}/api/auth/init" \
    -H 'Content-Type: application/json' -d "$payload" 2>/dev/null || true)"
  case "$init_code" in
    200) ok "登录密码已初始化（用户 ARTEX，密码见 .env 的 ARTEX_PASSWORD）" ;;
    403) info "登录密码此前已设置，保持原密码（见 .env 的 ARTEX_PASSWORD）" ;;
    *)   warn "设置登录密码返回 HTTP ${init_code:-无响应}；可打开 http://localhost:${port}/setup 手动设置" ;;
  esac
}

# ── Windows（Git Bash / MSYS / Cygwin / PowerShell 外壳）────
# 刻意不尝试装 docker：Windows 上正确的做法是 Docker Desktop，而不是脚本里
# curl | sh。硬装必然失败，用户会以为是我们的 bug。所以这里只给"照做就行"的指引，
# 并用 exit 2 和普通错误区分开——2 = 需要人工介入，不是脚本坏了。
windows_guide(){
  cat <<'EOF'

  检测到 Windows 环境（Git Bash / MSYS / Cygwin 或 PowerShell 外壳）。
  这个脚本不会（也做不到）在 Windows 上自动装 docker —— 按下面做，二选一。

  ── 路线 A：用 Docker（推荐，最省心）────────────────────────
  1. 装 Docker Desktop for Windows
       https://www.docker.com/products/docker-desktop/
  2. 装完打开设置 → General → 勾选 "Use the WSL 2 based engine"
     （没有 WSL2 就先在 PowerShell 里跑：wsl --install，然后重启电脑）
  3. 等 Docker Desktop 左下角显示 "Engine running"
  4. 回到 Git Bash，在仓库根目录（能看到 docker-compose.yml 的那一层）重新执行：
       ./install.sh --docker
     或者干脆绕开本脚本，直接：
       docker compose up -d
  5. 浏览器打开 http://localhost:8787 ，用户名 ARTEX

  ── 路线 B：不装 Docker（用 Release 的单文件 + 自备 PostgreSQL）──
  1. 下载 Windows 版 artex.exe（选 windows-amd64 的 zip）：
       https://github.com/gthubtom1/ARTEX/releases
     解压得到 artex.exe + start.bat + skills/，放到同一个固定目录（例如 D:\ARTEX）。
  2. 装 PostgreSQL 16：https://www.postgresql.org/download/windows/
     装完用 pgAdmin 或命令行建库：
       createuser -P artex        # 记住这个密码
       createdb -O artex artex
  3. 在同一目录建 config.json：
       { "database": { "dsn": "postgres://artex:你的密码@127.0.0.1:5432/artex?sslmode=disable" } }
  4. 双击 start.bat → 浏览器打开 http://127.0.0.1:8787 （首次打开时设登录密码，用户名固定 ARTEX）

  打本机靶场时启动前加环境变量 ARTEX_ALLOW_DIRECT=1；
  打公网目标需要系统本身能出网（VPS 直连 / 本机 TUN 或系统级代理）——应用内不再配代理。

EOF
  exit 2
}

# uname -s 在 Git Bash/MSYS/Cygwin 里是 MINGW64_NT-… / MSYS_NT-… / CYGWIN_NT-…；
# 某些外壳里 uname 不可靠，再看 OS=Windows_NT 兜底。
is_windows(){
  case "$(uname -s 2>/dev/null || echo unknown)" in
    MINGW*|MSYS*|CYGWIN*) return 0 ;;
  esac
  case "${OS:-}" in Windows_NT*) return 0 ;; esac
  return 1
}

# ── docker 环境检测 / 自动安装 ───────────────────
ensure_docker(){
  # Windows：先给指引，别去碰 docker（装了一定失败）。
  # 先检测：装了 Docker Desktop 的 Windows 用户必须能继续往下走，
  # 否则"指引第 4 步叫你重跑 ./install.sh --docker"就成了死循环（R4 审查发现）。
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    ok "已检测到 docker 与 docker compose"; return
  fi
  warn "未检测到 docker / docker compose"
  if is_windows; then windows_guide; fi
  case "$(uname -s)" in
    Linux)
      if [ "$(ask '自动安装 Docker? (y/n)' y)" = y ]; then
        curl -fsSL https://get.docker.com | sh
        sudo usermod -aG docker "$USER" || true
        docker compose version >/dev/null 2>&1 \
          || warn "docker 装好了但 compose plugin 缺失：Debian/Ubuntu 跑 'sudo apt install -y docker-compose-plugin' 后重试"
        ok "Docker 安装完成（用户组变更需重新登录后免 sudo）"
      else
        die "请自行安装 docker 后重试：https://docs.docker.com/engine/install/"
      fi ;;
    Darwin)
      cat <<'EOF'

  macOS 上请安装 Docker Desktop（脚本不会替你装 GUI 应用）：
    https://www.docker.com/products/docker-desktop/
  装好后启动它，等菜单栏鲸鱼图标变绿，再回来重跑：./install.sh --docker

EOF
      exit 2 ;;
    *) die "无法识别的系统：$(uname -s)。请手工安装 docker 后重试：https://docs.docker.com/engine/install/" ;;
  esac
}

# ── ① 全部 Docker ───────────────────────────────
install_docker(){
  ensure_docker
  umask 077
  if [ ! -f .env ]; then
    cp .env.example .env 2>/dev/null || true
    local pw key
    pw="$(ask 'Postgres 密码（回车随机生成）' "$(rand)")"
    key="$(ask 'ANTHROPIC_API_KEY（可留空，后续在 UI 配）' '')"
    sed -i.bak "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=${pw}|" .env
    sed -i.bak "s|^ANTHROPIC_API_KEY=.*|ANTHROPIC_API_KEY=${key}|" .env
    rm -f .env.bak
    ok "已生成 .env（POSTGRES_PASSWORD 已设置）"
  else
    info "沿用已存在的 .env"
  fi
  chmod 600 .env 2>/dev/null || true

  # 登录密码 / 凭据密钥：只在缺失时补齐，已有值不覆盖（与 deploy/lib.sh 的 gen() 一致）。
  set_env_kv ARTEX_PASSWORD "$(rand)"
  set_env_kv ARTEX_CRED_KEY "$(rand_hex 32)"

  # 镜像来源分流：本地构建的 artex-fork 没有 registry，pull 必然失败并让 set -e 中断。
  local img tag port
  img="${ARTEX_IMAGE:-$(env_get ARTEX_IMAGE)}"; img="${img:-artex-fork}"
  tag="${ARTEX_TAG:-$(env_get ARTEX_TAG)}"; tag="${tag:-local}"
  if is_local_image "$img"; then
    info "镜像路线：本地构建 ${img}:${tag}（无 registry，不执行 pull）"
    docker compose build
  else
    info "镜像路线：远程 registry ${img}:${tag} → docker compose pull"
    docker compose pull
  fi
  docker compose up -d

  # 就绪后设登录密码（老用户改过密码时 /api/auth/init 返回 403，保留原密码）。
  port="${ARTEX_PORT:-$(env_get ARTEX_PORT)}"; port="${port:-8787}"
  init_login_password "$port"
  ok "启动完成 → http://localhost:${port}"
  if [ -t 1 ]; then
    printf '  用户 ARTEX  密码 %s\n' "$(env_get ARTEX_PASSWORD)"
  else
    printf '  用户 ARTEX  密码见 .env 的 ARTEX_PASSWORD\n'
  fi
  info "查看日志：docker compose logs -f artex"
}

# ── ② 本地编译运行 ──────────────────────────────
install_local(){
  echo "数据库安装方式："
  echo "  1) 连接已有 PostgreSQL"
  echo "  2) 用 Docker 起一个 PostgreSQL（需要 docker）"
  case "$(ask '选择' 1)" in
    2)
      ensure_docker
      local pw; pw="$(ask 'Postgres 密码（回车随机）' "$(rand)")"
      docker run -d --name artex-pg -p 5432:5432 \
        -e POSTGRES_USER=artex -e POSTGRES_PASSWORD="$pw" -e POSTGRES_DB=artex \
        -v artex-pg:/var/lib/postgresql/data postgres:16-alpine
      DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=artex DB_PASS="$pw" DB_NAME=artex DB_SSL=disable ;;
    *)
      DB_HOST="$(ask '数据库地址' 127.0.0.1)"
      DB_PORT="$(ask '端口' 5432)"
      DB_USER="$(ask '账号' artex)"
      DB_PASS="$(ask '密码' '')"
      DB_NAME="$(ask '数据库名' artex)"
      DB_SSL="$(ask 'sslmode (disable/require)' disable)" ;;
  esac

  # 生成 config.json
  cat > config.json <<JSON
{
  "database": {
    "host": "${DB_HOST}",
    "port": ${DB_PORT},
    "user": "${DB_USER}",
    "password": "${DB_PASS}",
    "dbname": "${DB_NAME}",
    "sslmode": "${DB_SSL}"
  }
}
JSON
  ok "已生成 config.json"

  # go 环境检查
  command -v go >/dev/null 2>&1 || die "未检测到 Go，请先安装 Go（>=1.26）：https://go.dev/dl/"
  ok "Go: $(go version)"

  # 内嵌前端需要 node 出静态产物
  if command -v npm >/dev/null 2>&1; then
    info "构建前端静态产物…"
    ( cd web && NODE_ENV=development npm ci --include=dev --ignore-scripts && npm run build:static )
    rm -rf server/webui/dist && cp -r web/out server/webui/dist
    info "编译内嵌单二进制…"
    CGO_ENABLED=0 go build -tags embedui -trimpath -o artex ./cmd/artex
  else
    warn "未检测到 npm：将编译**不内嵌前端**的后端（前端需另跑 npm run dev）"
    CGO_ENABLED=0 go build -o artex ./cmd/artex
  fi
  ok "编译完成 → ./artex"

  info "启动…（Ctrl-C 退出）"
  ./artex
}

echo "=============================="
echo "  ARTEX 安装"
echo "  1) 全部 Docker 安装"
echo "  2) 本地运行（go 编译）"
echo "=============================="

# 非交互入口（教程/自动化用的就是这两个）：./install.sh --docker / --local
case "${1:-}" in
  --docker|-d) install_docker; exit 0 ;;
  --local|-l)  install_local;  exit 0 ;;
  --help|-h)
    cat <<'EOF'
用法：
  ./install.sh              交互选择（1=全部 Docker，2=本地 go 编译）
  ./install.sh --docker     跳过菜单，直接进 Docker 安装
  ./install.sh --local      跳过菜单，直接进本地编译运行

  新装机建议改用 deploy/bootstrap.sh（一条命令装依赖+起服务+体检）。
  Docker 路线的镜像来源：ARTEX_IMAGE 未设/=artex-fork → 本地 build；
  设为远程 registry 镜像 → docker compose pull。

  Windows（Git Bash / PowerShell 外壳）：本脚本不会自动装 docker，
  会打印"照做就行"的指引并以 exit 2 退出（2 = 需要人工介入）。
EOF
    exit 0 ;;
  "") ;;
  *) die "未知参数：$1（可用：--docker / --local / --help）" ;;
esac

case "$(ask '选择' 1)" in
  1) install_docker ;;
  2) install_local ;;
  *) die "无效选择" ;;
esac
