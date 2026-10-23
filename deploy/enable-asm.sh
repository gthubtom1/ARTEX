#!/usr/bin/env bash
# 加装「资产测绘」组件（ScopeSentry）：生成本地密码 → 开 profile → 等就绪 → 自动接线。
# 幂等：重复跑只会补齐缺失配置，不动已有数据。
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

ensure_env
ensure_asm_env
force_kv ASM_ENABLED 1

set -a; . ./.env; set +a
ok ".env 已配置（ASM_ENABLED=1）"

echo
warn "两条必须知道的事："
warn "  1) asm-scan 用 host 网络 —— Docker Desktop（Windows/macOS）不支持，需 Linux 宿主（Kali/Ubuntu）"
warn "  2) 首次启动 Mongo 要灌内置指纹/PoC 库，3-10 分钟，别以为卡死"
echo

exec bash deploy/start.sh
