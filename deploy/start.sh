#!/usr/bin/env bash
# 启动器 —— 同时管本体和（可选的）资产测绘。
#
#   bash deploy/start.sh                 起本体（已装测绘就一起起）
#   bash deploy/start.sh --with-asm      加装并一起起测绘
#   bash deploy/start.sh --no-asm        只起本体（不改配置）
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
    -h|--help)  sed -n '2,12p' "$0"; exit 0 ;;
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

if [ "$WITH_ASM" = "1" ]; then
  force_kv ASM_ENABLED 1
  ensure_asm_env
  set -a; . ./.env; set +a
elif [ "$WITH_ASM" = "0" ]; then
  force_kv ASM_ENABLED 0
  set -a; . ./.env; set +a
fi

# ---- 1. 启动 ----
PROFILE="$(compose_profile)"
BUILD_FLAG=""; [ "$REBUILD" = "1" ] && BUILD_FLAG="--build"
if [ -z "$BUILD_FLAG" ] && ! docker image inspect "${ARTEX_IMAGE:-artex-fork}:${ARTEX_TAG:-local}" >/dev/null 2>&1; then
  BUILD_FLAG="--build"
  log "首次启动：构建镜像（5-15 分钟，容器内编译前后端）"
fi

log "启动服务 ${PROFILE:+(含资产测绘)}"
# shellcheck disable=SC2086
dc $PROFILE up -d $BUILD_FLAG

# ---- 2. 等本体 ----
wait_http "http://127.0.0.1:${ARTEX_PORT}/" "ARTEX" 120 || die "ARTEX 起不来，看日志：bash deploy/start.sh --logs"

# ---- 3. 首次设密码（已设过返回 403，忽略） ----
curl -sS -o /dev/null -X POST "http://127.0.0.1:${ARTEX_PORT}/api/auth/init" \
  -H 'Content-Type: application/json' \
  -d "{\"password\":\"${ARTEX_PASSWORD}\"}" || true

# ---- 4. 测绘（可选） ----
if asm_enabled; then
  wait_http "http://127.0.0.1:${ASM_PORT:-8082}/" "ScopeSentry" 240 \
    || warn "ScopeSentry 未就绪：首次初始化 Mongo 内置库需 3-10 分钟，稍后重跑 bash deploy/wire.sh"
  bash deploy/wire.sh || warn "接线未完成（见上面提示），稍后重跑 bash deploy/wire.sh"
fi

# ---- 5. 汇报 ----
echo
printf '\033[1;32m=========== 已启动 ===========\033[0m\n\n'
printf '  ARTEX        http://127.0.0.1:%s\n' "$ARTEX_PORT"
printf '               用户 ARTEX  密码 %s\n' "${ARTEX_PASSWORD}"
if asm_enabled; then
  printf '  ScopeSentry  http://127.0.0.1:%s\n' "${ASM_PORT:-8082}"
  printf '               默认 ScopeSentry/ScopeSentry（立刻改，改完写进 .env 的 ASM_ADMIN_PASSWORD）\n'
else
  printf '  资产测绘      未启用（加装：bash deploy/start.sh --with-asm）\n'
fi
printf '\n  状态：bash deploy/status.sh     停止：bash deploy/stop.sh\n\n'

[ "$FOLLOW" = "1" ] && dc logs -f --tail=80
exit 0
