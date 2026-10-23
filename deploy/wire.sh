#!/usr/bin/env bash
# 把测绘组件接进 ARTEX：ScopeSentry 建 API Key → 写成 ARTEX 的 MCP 数据源 → 验证工具已发现。
# 幂等，可反复跑。
# 退出码：0 已接通 / 2 ScopeSentry 登录失败 / 3 建 key 失败 / 4 ARTEX 侧失败
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

set -a; . ./.env; set +a
ARTEX="http://127.0.0.1:${ARTEX_PORT:-8787}"
ASM_PORT="${ASM_PORT:-8082}"
SS="http://127.0.0.1:${ASM_PORT}"
SS_INTERNAL="http://scope-sentry:8082"   # 容器网络内地址（ARTEX 从容器里访问）
CRED="data/asm/credentials.txt"

jget() { # stdin JSON + 若干 jq 路径，取第一个非空
  local json; json="$(cat)"
  local p v
  for p in "$@"; do
    v=$(printf '%s' "$json" | jq -r "$p // empty" 2>/dev/null || true)
    [ -n "$v" ] && { printf '%s' "$v"; return 0; }
  done
  return 0
}

# ---- ARTEX 侧 ----
ATOKEN=$(curl -sS -X POST "$ARTEX/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"ARTEX\",\"password\":\"${ARTEX_PASSWORD}\"}" | jget '.token' '.data.token')
[ -n "$ATOKEN" ] || { bad "ARTEX 登录失败：检查 .env 的 ARTEX_PASSWORD"; exit 4; }
ok "ARTEX 已登录"

# ---- 测绘侧 ----
LOGIN=$(curl -sS -X POST "$SS/api/user/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"${ASM_ADMIN_USER}\",\"password\":\"${ASM_ADMIN_PASSWORD}\"}" || true)
STOKEN=$(printf '%s' "$LOGIN" | jget '.data.access_token' '.access_token' '.token' '.data.token')
if [ -z "$STOKEN" ]; then
  bad "ScopeSentry 登录失败：$(printf '%s' "$LOGIN" | head -c 200)"
  bad "若已改过密码，把它写进 .env 的 ASM_ADMIN_PASSWORD 再重跑"
  exit 2
fi
ok "ScopeSentry 已登录"

# ---- 建 Key（明文只返回一次） ----
RESP=$(curl -sS -X POST "$SS/api/apikey/create" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${STOKEN}" -d '{"name":"artex-lab"}')
KEY=$(printf '%s' "$RESP" | jget '.data.key' '.key')
[ -n "$KEY" ] || { bad "建 API Key 失败：$(printf '%s' "$RESP" | head -c 200)"; exit 3; }
ok "API Key 已创建 ${KEY:0:10}…"

# ---- 写进 ARTEX 数据源 ----
SAVE=$(curl -sS -X POST "$ARTEX/api/sync/scopesentry/datasource" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${ATOKEN}" \
  -d "$(jq -nc --arg u "$SS_INTERNAL/mcp" --arg k "$KEY" '{url:$u,api_key:$k}')")
[ "$(printf '%s' "$SAVE" | jget '.enabled')" = "true" ] \
  || { bad "数据源写入失败：$(printf '%s' "$SAVE" | head -c 200)"; exit 4; }
ok "数据源已启用（$SS_INTERNAL/mcp）"

# ---- 验证 ----
ST=$(curl -sS "$ARTEX/api/sync/scopesentry/status" -H "Authorization: Bearer $ATOKEN")
N=$(printf '%s' "$ST" | jq -r '[.tools[]?] | length' 2>/dev/null || echo 0)

mkdir -p data/asm; umask 077
cat > "$CRED" <<EOF
# 资产测绘凭据（勿提交、勿外传）
SS_URL=$SS
SS_USER=$ASM_ADMIN_USER
SS_PASSWORD=$ASM_ADMIN_PASSWORD
SS_API_KEY=$KEY
SS_MCP=$SS_INTERNAL/mcp
时间 $(date -Is)
EOF
chmod 600 "$CRED"

if [ "$N" -gt 0 ]; then
  ok "接通：ARTEX 已发现 $N 个测绘工具，凭据在 $CRED"
else
  warn "数据源已保存但工具数为 0（服务端可能仍在初始化）。等 1-2 分钟重跑：bash deploy/wire.sh"
fi
