#!/usr/bin/env bash
# install-native.sh —— 无 Docker 的一键安装（原生：二进制 + 本机 PostgreSQL + systemd）
#
# 为什么需要它：docker 那条路线只为了两件事（新机器一条命令装好 / 装资产测绘 ScopeSentry）。
# 但 **ARTEX 本体不需要 Docker** —— 用二进制 + 本机 PostgreSQL + systemd 就够了，
# 这也正是很多人在用的形态（VPS 上尤其如此：Docker 白占内存，Kali/Debian 上 PG 一条 apt 就有）。
#
# 用法：
#   sudo bash scripts/install-native.sh                 # 默认 /opt/artex + 8787 + 库 artex
#   sudo bash scripts/install-native.sh --prefix /opt/artex-t2 --port 18788 --db artex_t2
#   sudo bash scripts/install-native.sh --binary ./dist/artex-linux-amd64/artex   # 用已有二进制
#   sudo bash scripts/install-native.sh --dry-run       # 只打印计划，不改系统
#
# 幂等：重复跑会复用已有 .env / 数据库 / 服务单元；不会覆盖已有密码。
set -euo pipefail
cd "$(cd "$(dirname "$0")/.." && pwd)"
ROOT="$(pwd)"

PREFIX="/opt/artex"
PORT="8787"
DBNAME="artex"
BIND="127.0.0.1"
BINARY=""
DRY=0
UNIT="artex"

while [ $# -gt 0 ]; do
  case "$1" in
    --prefix) PREFIX="$2"; shift 2 ;;
    --port)   PORT="$2"; shift 2 ;;
    --db)     DBNAME="$2"; shift 2 ;;
    --bind)   BIND="$2"; shift 2 ;;
    --binary) BINARY="$2"; shift 2 ;;
    --unit)   UNIT="$2"; shift 2 ;;
    --dry-run) DRY=1; shift ;;
    -h|--help) sed -n '2,18p' "$0"; exit 0 ;;
    *) echo "[x] 未知参数：$1" >&2; exit 1 ;;
  esac
done
case "$UNIT" in *[!a-zA-Z0-9_-]*) echo "[x] --unit 只能含字母数字-_" >&2; exit 1 ;; esac

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }
run(){ if [ "$DRY" = "1" ]; then printf '    [dry] %s\n' "$*"; else "$@"; fi; }
rand(){ if command -v openssl >/dev/null 2>&1; then openssl rand -hex 16; else head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24; fi; }

[ "$(uname -s)" = "Linux" ] || die "只支持 Linux（VPS / Kali / Debian / Ubuntu）；Windows 用 WSL2"
[ "$(id -u)" = "0" ] || die "请用 root 跑（sudo bash $0）"

info "安装计划：prefix=$PREFIX port=$PORT db=$DBNAME bind=$BIND unit=$UNIT dry-run=$DRY"

# ── 1. 依赖：只需要 curl（可选 go/node 用于本地编译）、psql（本机库） ──
PKGS=""
command -v curl >/dev/null 2>&1 || PKGS="$PKGS curl"
command -v psql >/dev/null 2>&1 || PKGS="$PKGS postgresql"
if [ -n "$PKGS" ]; then
  info "安装系统依赖：$PKGS"
  if command -v apt-get >/dev/null 2>&1; then
    run apt-get update -qq
    run apt-get install -y -qq $PKGS ca-certificates
  elif command -v dnf >/dev/null 2>&1; then
    run dnf install -y $PKGS
  else
    die "没有 apt/dnf，请先手工装：$PKGS"
  fi
fi

# ── 2. PostgreSQL：起服务 + 建库/账号（幂等） ──
if ! command -v psql >/dev/null 2>&1; then
  warn "没有 psql，跳过数据库创建（请自行准备 PostgreSQL 并稍后用 --db-dsn 指定）"
else
  info "确保 PostgreSQL 在跑"
  if command -v systemctl >/dev/null 2>&1; then
    run systemctl enable --now postgresql 2>/dev/null || true
  fi
  DBPASS_FILE="$PREFIX/.dbpass"
  if [ -f "$DBPASS_FILE" ]; then
    DBPASS="$(cat "$DBPASS_FILE")"
  else
    DBPASS="$(rand)"
    run mkdir -p "$PREFIX"
    if [ "$DRY" = "0" ]; then printf '%s' "$DBPASS" > "$DBPASS_FILE"; chmod 600 "$DBPASS_FILE"; fi
  fi
  info "创建数据库角色/库（已存在则跳过）：$DBNAME"
  if [ "$DRY" = "0" ]; then
    su - postgres -c "psql -tAc \"SELECT 1 FROM pg_roles WHERE rolname='${DBNAME}'\"" 2>/dev/null | grep -q 1 \
      || su - postgres -c "psql -c \"CREATE ROLE ${DBNAME} LOGIN PASSWORD '${DBPASS}'\"" >/dev/null
    su - postgres -c "psql -tAc \"SELECT 1 FROM pg_database WHERE datname='${DBNAME}'\"" 2>/dev/null | grep -q 1 \
      || su - postgres -c "createdb -O ${DBNAME} ${DBNAME}" >/dev/null
    ok "数据库就绪：${DBNAME}"
  else
    printf '    [dry] create role/db %s\n' "$DBNAME"
  fi
fi

# ── 3. 二进制：用 --binary 指定的，或本地编译（需 go+node+前端已导出） ──
info "准备二进制"
run mkdir -p "$PREFIX"
if [ -n "$BINARY" ]; then
  [ -f "$BINARY" ] || die "找不到 --binary 指定的文件：$BINARY"
  run install -m 755 "$BINARY" "$PREFIX/artex"
else
  if [ -d server/webui/dist ] && command -v go >/dev/null 2>&1; then
    info "本地编译（内嵌前端）：go build -tags embedui"
    if [ "$DRY" = "0" ]; then
      CGO_ENABLED=0 go build -tags embedui -trimpath -ldflags "-s -w -X main.version=native" -o "$PREFIX/artex" ./cmd/artex
    fi
  else
    die "没有 --binary，也没法本地编译（缺 server/webui/dist 或 go）。请从 Release 下载 artex-v*-linux-amd64.zip 解压后：
    sudo bash scripts/install-native.sh --binary /path/to/artex"
  fi
fi
[ "$DRY" = "1" ] || ok "二进制就位：$PREFIX/artex"

# ── 4. 运行期文件：config.json（数据库连接）+ 目录 ──
info "生成 config.json 与数据目录"
if [ "$DRY" = "0" ]; then
  mkdir -p "$PREFIX/data"
  if [ ! -f "$PREFIX/config.json" ]; then
    cat > "$PREFIX/config.json" <<EOF
{
  "database": {
    "host": "127.0.0.1",
    "port": 5432,
    "user": "${DBNAME}",
    "password": "${DBPASS}",
    "dbname": "${DBNAME}",
    "sslmode": "disable"
  },
  "skill_dir": "${PREFIX}/skills"
}
EOF
    chmod 600 "$PREFIX/config.json"
  fi
  # 运行期资源：技能与知识库必须和二进制在一起
  for d in skills kb; do
    if [ -d "$ROOT/$d" ] && [ ! -d "$PREFIX/$d" ]; then cp -a "$ROOT/$d" "$PREFIX/"; fi
  done
  ok "配置就绪：$PREFIX/config.json"
else
  printf '    [dry] write %s/config.json\n' "$PREFIX"
fi

# ── 5. systemd 单元 ──
info "安装 systemd 单元：$UNIT.service"
UNIT_PATH="/etc/systemd/system/${UNIT}.service"
if [ "$DRY" = "0" ]; then
  cat > "$UNIT_PATH" <<EOF
[Unit]
Description=ARTEX (AI pentest agent, native)
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${PREFIX}
ExecStart=${PREFIX}/artex -addr ${BIND}:${PORT}
Restart=on-failure
RestartSec=5
TimeoutStopSec=20
LimitNOFILE=65535
Environment=ARTEX_CONFIG=${PREFIX}/config.json

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now "$UNIT" >/dev/null 2>&1 || systemctl restart "$UNIT"
else
  printf '    [dry] write %s\n' "$UNIT_PATH"
fi

# ── 6. 自检（判据：health 200 + 前端已内嵌） ──
if [ "$DRY" = "0" ]; then
  info "等待就绪：http://${BIND}:${PORT}/api/health"
  for i in $(seq 1 40); do
    if curl -fsS -m 3 "http://${BIND}:${PORT}/api/health" >/dev/null 2>&1; then break; fi
    sleep 2
  done
  echo
  H=$(curl -sS -m 5 "http://${BIND}:${PORT}/api/health" || true)
  case "$H" in *'"ok":true'*) ok "health OK：$H" ;; *) die "health 不通：$H" ;; esac
  CODE=$(curl -s -o /dev/null -w '%{http_code}' "http://${BIND}:${PORT}/")
  [ "$CODE" = "200" ] && ok "前端已内嵌（/ → 200）" || warn "/ 返回 $CODE（若用 Release 二进制应 200）"
  echo
  printf '\033[1;32m================ 装好了 ================\033[0m\n'
  printf '  地址   http://%s:%s\n' "$BIND" "$PORT"
  printf '  用户   ARTEX（首次启动设密码：curl -X POST /api/auth/init）\n'
  printf '  目录   %s\n' "$PREFIX"
  printf '  服务   systemctl status %s\n' "$UNIT"
  printf '  体检   bash scripts/verify-0.9.6.sh（设 ARTEX_DIR=%s ARTEX_PORT=%s）\n' "$PREFIX" "$PORT"
fi
