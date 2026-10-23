#!/usr/bin/env bash
# 零操作安装器：全新 Kali / Debian / Ubuntu 上从零到能登录，一条命令。
#
#   sudo bash deploy/bootstrap.sh            # 装本体 + 资产测绘（默认全装）
#   sudo bash deploy/bootstrap.sh --no-asm   # 只装本体
#
# 它做完这些事，然后交棒给 deploy/start.sh：
#   1. 装 docker / compose / curl / jq / git / openssl
#   2. 启动 docker 并设为开机自启
#   3. 把当前用户加进 docker 组（当前会话里用 sudo 兜底）
#   4. 生成 .env（随机密码）
#   5. 构建并启动，等就绪，设登录密码
#   6. 装测绘（默认）并自动接线
#   7. 体检，输出 PASS/FAIL 与下一步要填的两把 Key
#
# 退出码：0 成功（体检可能因测绘首次初始化而暂时 FAIL，按提示重跑 wire.sh）/ 1 环境不支持
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
ROOT="$(pwd)"
# 复用 force_kv 等 .env 工具（lib.sh 也会再 cd 一次，无副作用）
# shellcheck source=deploy/lib.sh
source deploy/lib.sh

WITH_ASM=1
for a in "$@"; do
  case "$a" in
    --no-asm) WITH_ASM=0 ;;
    --with-asm) WITH_ASM=1 ;;
    -h|--help) sed -n '2,18p' "$0"; exit 0 ;;
    *) echo "[x] 未知参数：$a"; exit 1 ;;
  esac
done

say() { printf '\n\033[1;36m[*] %s\033[0m\n' "$*"; }
ok()  { printf '\033[1;32m[+] %s\033[0m\n' "$*"; }
warn(){ printf '\033[1;33m[!] %s\033[0m\n' "$*"; }
die() { printf '\033[1;31m[x] %s\033[0m\n' "$*"; exit 1; }

# ---- 0. 身份与系统 ----
[ "$(uname -s)" = "Linux" ] || die "只支持 Linux（Kali / Debian / Ubuntu）。Windows 用 WSL2 里的 Linux，或 .\\deploy\\start.ps1"
TARGET_USER="${SUDO_USER:-$USER}"
[ "$TARGET_USER" = "root" ] && warn "以 root 运行：数据目录会归 root，之后用普通用户跑要 chown"

if [ "$(id -u)" -ne 0 ]; then
  say "需要 root 装软件，正在申请 sudo"
  exec sudo -E bash "$0" "$@"
fi

if [ "$WITH_ASM" = "1" ] && [ -r /proc/meminfo ]; then
  MEM_MB=$(awk '/MemTotal/{print int($2/1024)}' /proc/meminfo 2>/dev/null || echo 0)
  if [ "${MEM_MB:-0}" -lt 7500 ]; then
    warn "内存约 ${MEM_MB}MB：Mongo + 扫描节点可能吃紧；建议改跑 --no-asm 或加 swap"
  fi
fi

if command -v apt-get >/dev/null; then PM=apt;
elif command -v dnf >/dev/null; then PM=dnf;
elif command -v pacman >/dev/null; then PM=pacman;
else PM=none; fi

# ---- 1. 装依赖 ----
need=()
for b in docker curl jq git openssl; do command -v "$b" >/dev/null || need+=("$b"); done
have_compose=0
docker compose version >/dev/null 2>&1 && have_compose=1
command -v docker-compose >/dev/null && have_compose=1

if [ "${#need[@]}" -gt 0 ] || [ "$have_compose" = "0" ]; then
  say "安装依赖：${need[*]:-compose}"
  case "$PM" in
    apt)
      export DEBIAN_FRONTEND=noninteractive
      apt-get update -qq
      apt-get install -y -qq docker.io curl jq git openssl ca-certificates
      # compose：优先插件，其次 v1 包；都没有就装官方插件
      if ! docker compose version >/dev/null 2>&1 && ! command -v docker-compose >/dev/null; then
        apt-get install -y -qq docker-compose 2>/dev/null || {
          warn "apt 里没有 compose，装官方插件"
          mkdir -p /usr/local/lib/docker/cli-plugins
          apt-get install -y -qq docker-compose-plugin 2>/dev/null || {
            # Docker 官方 release 资产名是小写 OS + uname 架构（docker-compose-linux-x86_64）。
            COMPOSE_OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
            COMPOSE_ARCH="$(uname -m)"
            COMPOSE_TMP="$(mktemp)"
            curl -fsSL "https://github.com/docker/compose/releases/latest/download/docker-compose-${COMPOSE_OS}-${COMPOSE_ARCH}" \
              -o "$COMPOSE_TMP" || die "compose 下载失败：检查网络"
            chmod +x "$COMPOSE_TMP" || die "chmod 失败"
            mv "$COMPOSE_TMP" /usr/local/lib/docker/cli-plugins/docker-compose || die "安装 compose 失败"
          }
        }
      fi
      ;;
    dnf)    dnf install -y docker docker-compose curl jq git openssl || die "dnf 安装失败" ;;
    pacman) pacman -Sy --noconfirm docker docker-compose curl jq git openssl || die "pacman 安装失败" ;;
    *)      die "没有可用的包管理器（apt/dnf/pacman），请手工装 docker + compose + curl + jq" ;;
  esac
fi
ok "依赖就绪：$(docker --version 2>/dev/null | head -1)"

# ---- 2. 起 docker ----
if ! docker info >/dev/null 2>&1; then
  say "启动 docker"
  systemctl enable --now docker 2>/dev/null || service docker start 2>/dev/null || true
  sleep 2
fi
docker info >/dev/null 2>&1 || die "docker 起不来：journalctl -u docker -n 50 看原因"
ok "docker 守护进程在跑"

# ---- 3. 组权限 ----
if [ -n "${TARGET_USER:-}" ] && [ "$TARGET_USER" != "root" ]; then
  if ! id -nG "$TARGET_USER" | tr ' ' '\n' | grep -qx docker; then
    usermod -aG docker "$TARGET_USER" && ok "已把 $TARGET_USER 加入 docker 组（重新登录后免 sudo）"
  fi
fi

# 决定这次用什么身份跑 docker
RUN_AS_ROOT=0
if [ "$TARGET_USER" != "root" ]; then
  if sudo -u "$TARGET_USER" docker ps >/dev/null 2>&1; then
    RUN_AS_ROOT=0
  else
    RUN_AS_ROOT=1
    warn "当前会话还没拿到 docker 组权限，这一段先用 root 跑（数据稍后改回 $TARGET_USER 所有）"
  fi
fi

run_start() {
  local args=()
  if [ "$WITH_ASM" = "1" ]; then args+=(--with-asm); else args+=(--no-asm); fi
  if [ "$TARGET_USER" = "root" ] || [ "$RUN_AS_ROOT" = "1" ]; then
    bash deploy/start.sh "${args[@]}"
  else
    sudo -u "$TARGET_USER" -H bash deploy/start.sh "${args[@]}"
  fi
}

# ---- 4. 交棒 ----
if [ "$RUN_AS_ROOT" = "1" ]; then
  run_start
  # bootstrap --no-asm 是安装选择：把持久开关也置 0，避免下次普通 start 又把测绘拉起来。
  if [ "$WITH_ASM" = "0" ] && [ -f .env ]; then force_kv ASM_ENABLED 0; fi
  chown -R "$TARGET_USER":"$TARGET_USER" data .env 2>/dev/null || true
  ok "数据目录归属已改回 $TARGET_USER"
else
  run_start
  if [ "$WITH_ASM" = "0" ] && [ -f .env ]; then
    force_kv ASM_ENABLED 0
    chown "$TARGET_USER":"$TARGET_USER" .env 2>/dev/null || true
  fi
fi

# ---- 5. 收尾 ----
echo
printf '\033[1;32m================ 安装完成 ================\033[0m\n\n'
if [ "$WITH_ASM" = "1" ]; then
  printf '  ARTEX         http://127.0.0.1:8787\n'
  printf '  ScopeSentry   http://127.0.0.1:8082\n'
else
  printf '  ARTEX         http://127.0.0.1:8787\n'
fi
cat <<'EOF'

  你只需要做两件事（都只做一次）：
    1) ARTEX 登录后 → 系统 → LLM：填模型与 API Key（planner 用强模型）
    2) ScopeSentry 登录后 → 配置：填 FOFA（email + key）或 Quake/Hunter 的 Key
       顺便把默认密码 ScopeSentry/ScopeSentry 改掉，并写进仓库 .env 的 ASM_ADMIN_PASSWORD

  以后开机只要：bash deploy/start.sh
  体检：bash deploy/status.sh     停止：bash deploy/stop.sh
EOF

echo
log "体检（测绘首次初始化时可能暂时 FAIL，属正常）"
bash deploy/status.sh || warn "体检未全通过，按上面提示处理后可重跑：bash deploy/status.sh"

if [ "$RUN_AS_ROOT" = "1" ] && [ "$TARGET_USER" != "root" ]; then
  echo
  warn "建议重启一次（或注销重登），之后 docker 就不用 sudo 了"
fi
exit 0
