#!/usr/bin/env bash
# 停止。默认保留全部数据（数据库、凭据、台账）。
#   bash deploy/stop.sh            停容器
#   bash deploy/stop.sh --purge    停并删 pg 卷（ARTEX 数据清空，谨慎）
#   bash deploy/stop.sh --purge --yes   非交互确认
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
set -a; . ./.env 2>/dev/null || true; set +a

PURGE=0
YES=0
for a in "$@"; do
  case "$a" in
    --purge) PURGE=1 ;;
    --yes|-y) YES=1 ;;
    -h|--help) sed -n '2,8p' "$0"; exit 0 ;;
    *) die "未知参数：$a" ;;
  esac
done

if [ "$PURGE" = "1" ]; then
  if [ "$YES" != "1" ]; then
    if [ -t 0 ]; then
      warn "将删除容器与 pgdata 卷（任务/资产/发现全部丢失；data/asm 是绑定目录，不会被删）"
      read -r -p "输入 yes 确认：" ans || ans=""
    else
      bad "非交互环境拒绝 --purge；确认要删请加 --yes"
      exit 1
    fi
    [ "$ans" = "yes" ] || { ok "已取消"; exit 0; }
  fi
  warn "删除 pgdata 卷（如需同时删测绘数据：bash deploy/disable-asm.sh --purge --yes）"
  dc --profile asm down -v
else
  dc --profile asm down
fi
ok "已停止"
