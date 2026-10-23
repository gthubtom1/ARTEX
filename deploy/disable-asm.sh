#!/usr/bin/env bash
# 停掉「资产测绘」组件，保留数据（下次 enable 直接续用）。
# 彻底删数据：bash deploy/disable-asm.sh --purge [--yes]
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

PURGE=0
YES=0
for a in "$@"; do
  case "$a" in
    --purge) PURGE=1 ;;
    --yes|-y) YES=1 ;;
    -h|--help) sed -n '2,5p' "$0"; exit 0 ;;
    *) die "未知参数：$a" ;;
  esac
done

ensure_env
force_kv ASM_ENABLED 0
set -a; . ./.env; set +a

log "停止测绘组件（ARTEX 本体不受影响）"
dc --profile asm rm -sf scope-sentry asm-scan asm-mongo asm-redis 2>/dev/null || true

if [ "$PURGE" = "1" ]; then
  if [ "$YES" != "1" ]; then
    if [ -t 0 ]; then
      warn "将删除 data/asm（Mongo/Redis/上传/凭据全部丢失）"
      read -r -p "输入 yes 确认：" ans || ans=""
    else
      bad "非交互环境拒绝 --purge；确认要删请加 --yes"
      exit 1
    fi
    [ "$ans" = "yes" ] || { ok "已取消删数据"; exit 0; }
  fi
  warn "删除测绘数据：$ROOT/data/asm"
  rm -rf "$ROOT/data/asm"
fi

ok "已停止（ASM_ENABLED=0；数据保留）"
