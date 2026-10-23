#!/usr/bin/env bash
# 停掉「资产测绘」组件，保留数据（下次 enable 直接续用）。
# 彻底删数据：bash deploy/disable-asm.sh --purge
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

PURGE=0
[ "${1:-}" = "--purge" ] && PURGE=1

force_kv ASM_ENABLED 0
set -a; . ./.env; set +a

log "停止测绘组件"
dc --profile asm rm -sf scope-sentry asm-scan asm-mongo asm-redis 2>/dev/null || true

if [ "$PURGE" = "1" ]; then
  warn "删除测绘数据（data/asm 与 mongo/redis 卷）"
  rm -rf data/asm
fi

ok "已停止（ARTEX 本体不受影响，仍在运行）"
