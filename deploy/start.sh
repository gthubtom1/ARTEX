#!/usr/bin/env bash
# 启动器 —— 同时管本体和（可选的）资产测绘。
#
#   bash deploy/start.sh                 起本体（已装测绘就一起起）
#   bash deploy/start.sh --with-asm      加装并一起起测绘（会写 ASM_ENABLED=1）
#   bash deploy/start.sh --no-asm        本次只起本体（不改 .env 里的 ASM_ENABLED）
#   bash deploy/start.sh --rebuild       强制重建镜像
#   bash deploy/start.sh --logs          起完跟日志（Ctrl-C 退出，服务不停）
#
# 首次运行会生成 .env（随机密码）、构建镜像、设置登录密码，并把两套系统接好。
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

WITH_ASM=""      # "" = 看 .env 里的 ASM_ENABLED；1 = 强制加装；0 = 强制只起本体
REBUILD=0
FOLLOW=0
for a in "$@"; do
  case "$a" in
    --with-asm) WITH_ASM=1 ;;
    --no-asm)   WITH_ASM=0 ;;
    --rebuild)  REBUILD=1 ;;
    --logs)     FOLLOW=1 ;;
    -h|--help)  sed -n '2,13p' "$0"; exit 0 ;;
    *) die "未知参数：$a" ;;
  esac
done

command -v docker >/dev/null || die "缺 docker。先跑：sudo apt install -y docker.io docker-compose jq
或者：curl -fsSL https://get.docker.com | sh && sudo apt install -y docker-compose-plugin jq"
docker info >/dev/null 2>&1 || die "docker 守护进程没跑：sudo systemctl start docker && sudo systemctl enable docker"
command -v curl >/dev/null || die "缺 curl：sudo apt install -y curl"
command -v jq   >/dev/null || die "缺 jq（接线脚本要用它解析 JSON）：sudo apt install -y jq"
if ! docker compose version >/dev/null 2>&1 && ! command -v docker-compose >/dev/null; then
  die "缺 compose 插件：sudo apt install -y docker-compose 或 docker-compose-plugin"
fi

ensure_env
set -a; . ./.env; set +a
ARTEX_PORT="${ARTEX_PORT:-8787}"

# ASM_RUN 是「本次是否启动测绘」，与持久配置 ASM_ENABLED 分开：
# --no-asm 只影响这一次，不改 .env；enable/disable-asm.sh 才改持久配置。
ASM_RUN=0
if [ "$WITH_ASM" = "1" ]; then
  force_kv ASM_ENABLED 1
  ASM_RUN=1
elif [ "$WITH_ASM" = "0" ]; then
  ASM_RUN=0
elif asm_enabled; then
  ASM_RUN=1
fi

# 不管是 --with-asm 还是 .env 里已经是 1，都把 ASM_* 缺失项补齐（幂等）。
if [ "$ASM_RUN" = "1" ]; then
  ensure_asm_env
  set -a; . ./.env; set +a
fi

# 先把绑定目录建好，避免 docker 以 root 身份创建 data/ 导致宿主机普通用户无法备份
mkdir -p data
[ "$ASM_RUN" = "1" ] && mkdir -p data/asm

# ---- 1. 启动 ----
PROFILE=""
[ "$ASM_RUN" = "1" ] && PROFILE="--profile asm"
BUILD_FLAG=""; [ "$REBUILD" = "1" ] && BUILD_FLAG="--build"
if [ -z "$BUILD_FLAG" ] && ! docker image inspect "${ARTEX_IMAGE:-artex-fork}:${ARTEX_TAG:-local}" >/dev/null 2>&1; then
  BUILD_FLAG="--build"
  log "首次启动：构建镜像（5-15 分钟，容器内编译前后端）"
fi

log "启动服务 ${PROFILE:+(含资产测绘)}"
# shellcheck disable=SC2086
dc $PROFILE up -d $BUILD_FLAG

# ---- 2. 等本体（/api/auth/status 会读数据库，比 / 更能确认真的可用）----
wait_http "http://127.0.0.1:${ARTEX_PORT}/api/health" "ARTEX" 120 \
  || die "ARTEX 起不来，看日志：bash deploy/start.sh --logs"
# 启用 HTTP Basic Auth 后 auth/status 会先被网关拦成 401；那也说明服务+库已就绪。
wait_http_codes "http://127.0.0.1:${ARTEX_PORT}/api/auth/status" "ARTEX 数据库" 120 200 401 403 \
  || die "ARTEX 数据库未就绪，看日志：bash deploy/start.sh --logs"

# ---- 3. 首次设密码（已设过返回 403，忽略） ----
curl -sS -o /dev/null -X POST "http://127.0.0.1:${ARTEX_PORT}/api/auth/init" \
  -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg p "${ARTEX_PASSWORD}" '{password:$p}')" || true

# ---- 4. 测绘（可选） ----
if [ "$ASM_RUN" = "1" ]; then
  wait_http "http://127.0.0.1:${ASM_PORT:-8082}/" "ScopeSentry" 240 \
    || warn "ScopeSentry 未就绪：首次初始化 Mongo 内置库需 3-10 分钟，稍后重跑 bash deploy/wire.sh"
  bash deploy/wire.sh || warn "接线未完成（见上面提示），稍后重跑 bash deploy/wire.sh"
fi

# ---- 5. 汇报 ----
echo
printf '\033[1;32m=========== 已启动 ===========\033[0m\n\n'
printf '  ARTEX        http://127.0.0.1:%s\n' "$ARTEX_PORT"
# 只在交互终端打印密码；被重定向/进 CI 时不留明文日志。
if [ -t 1 ]; then
  printf '               用户 ARTEX  密码 %s\n' "${ARTEX_PASSWORD}"
else
  printf '               用户 ARTEX  密码见 .env 的 ARTEX_PASSWORD\n'
fi
if [ "$ASM_RUN" = "1" ]; then
  printf '  ScopeSentry  http://127.0.0.1:%s\n' "${ASM_PORT:-8082}"
  if [ -t 1 ]; then
    printf '               默认 ScopeSentry/ScopeSentry（立刻改，改完写进 .env 的 ASM_ADMIN_PASSWORD）\n'
  else
    printf '               默认密码见 .env 的 ASM_ADMIN_PASSWORD（首次登录后立刻改）\n'
  fi
else
  printf '  资产测绘      本次未启动（加装/启动：bash deploy/start.sh --with-asm）\n'
fi
printf '\n  状态：bash deploy/status.sh     停止：bash deploy/stop.sh\n\n'

[ "$FOLLOW" = "1" ] && dc logs -f --tail=80
exit 0
