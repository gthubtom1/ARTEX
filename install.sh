#!/usr/bin/env bash
# ARTEX 安装脚本：① 全部 Docker  ② 本地编译运行
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }
# read 在非交互环境（管道 / CI）会直接 EOF：给默认值而不是让 set -e 把脚本打死。
ask(){  local p="$1" d="${2:-}" a=""; read -rp "$p${d:+ [$d]}: " a || true; echo "${a:-$d}"; }
rand(){ if command -v openssl >/dev/null 2>&1; then openssl rand -hex 16; else head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24; fi; }

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
  if is_windows; then windows_guide; fi
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    ok "已检测到 docker 与 docker compose"; return
  fi
  warn "未检测到 docker / docker compose"
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
  info "拉取镜像并启动…"
  docker compose pull
  docker compose up -d
  ok "启动完成 → http://localhost:8787"
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
    ( cd web && npm ci && npm run build:static )
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
