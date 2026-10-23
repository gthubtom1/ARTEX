#!/usr/bin/env bash
# 体检。全绿输出 STATUS PASS（退出码 0），否则 STATUS FAIL（退出码 1）。
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
set -a; . ./.env 2>/dev/null || true; set +a

FAIL=0
row_ok()  { printf '\033[1;32m  [+] %-22s %s\033[0m\n' "$1" "$2"; }
row_bad() { printf '\033[1;31m  [x] %-22s %s\033[0m\n' "$1" "$2"; FAIL=1; }
row_inf() { printf '      %-22s %s\n' "$1" "$2"; }

echo "== 容器 =="
dc ps --format '      {{.Name}}  {{.State}}' 2>/dev/null || row_bad "compose" "列不出容器"
RUNNING=$(dc ps --status running --format '{{.Name}}' 2>/dev/null | wc -l | tr -d ' ')
row_inf "running" "$RUNNING 个"

echo "== 本体 =="
if curl -fsS -o /dev/null --max-time 5 "http://127.0.0.1:${ARTEX_PORT:-8787}/"; then
  row_ok "artex http" "200"
else row_bad "artex http" "不可达"; fi

TOKEN=$(curl -sS -X POST "http://127.0.0.1:${ARTEX_PORT:-8787}/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"ARTEX\",\"password\":\"${ARTEX_PASSWORD:-}\"}" \
  | jq -r '.token // empty' 2>/dev/null)
if [ -n "$TOKEN" ]; then row_ok "artex login" "ok"; else row_bad "artex login" "失败（看 .env 的 ARTEX_PASSWORD）"; fi

echo "== 资产测绘 =="
if asm_enabled; then
  if curl -fsS -o /dev/null --max-time 5 "http://127.0.0.1:${ASM_PORT:-8082}/"; then
    row_ok "scopesentry http" "200"
  else row_bad "scopesentry http" "不可达（首次初始化需 3-10 分钟）"; fi

  if [ -n "${TOKEN:-}" ]; then
    ST=$(curl -sS "http://127.0.0.1:${ARTEX_PORT:-8787}/api/sync/scopesentry/status" -H "Authorization: Bearer $TOKEN")
    E=$(printf '%s' "$ST" | jq -r '.enabled // false' 2>/dev/null)
    N=$(printf '%s' "$ST" | jq -r '[.tools[]?] | length' 2>/dev/null || echo 0)
    if [ "$E" = "true" ]; then row_ok "MCP 数据源" "enabled，$N 个工具"; else row_bad "MCP 数据源" "disabled（跑 bash deploy/wire.sh）"; fi
  fi
  [ "${ASM_MONGO_PASSWORD:-}" = "asm-not-configured" ] && row_bad "默认密码" "ASM_* 密码仍是默认值，跑 bash deploy/enable-asm.sh 重新生成"
else
  row_inf "状态" "未启用（bash deploy/start.sh --with-asm 加装）"
fi

echo "== 出口 =="
row_inf "exit ip" "$(curl -sS --max-time 8 https://api.ipify.org 2>/dev/null || echo '?')"

echo
if [ "$FAIL" -eq 0 ]; then echo "STATUS PASS"; else echo "STATUS FAIL"; fi
exit "$FAIL"
