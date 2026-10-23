#!/usr/bin/env bash
# 停止。默认保留全部数据（数据库、凭据、台账）。
#   bash deploy/stop.sh            停容器
#   bash deploy/stop.sh --purge    停并删 pg 卷（ARTEX 数据清空，谨慎）
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
set -a; . ./.env 2>/dev/null || true; set +a

if [ "${1:-}" = "--purge" ]; then
  warn "将删除容器与 pgdata 卷（任务/资产/发现全部丢失）"
  read -r -p "输入 yes 确认：" ans
  [ "$ans" = "yes" ] || { ok "已取消"; exit 0; }
  dc --profile asm down -v
else
  dc --profile asm down
fi
ok "已停止"
