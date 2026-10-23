#!/usr/bin/env bash
# 把测绘组件接进 ARTEX：ScopeSentry 建 API Key → 写成 ARTEX 的 MCP 数据源 → 验证工具已发现。
# 幂等：重复跑会复用/替换名为 artex-lab 的 Key，最终只保留一个，不会越跑越多。
# 退出码：0 已接通 / 2 ScopeSentry 登录失败 / 3 建 key 失败 / 4 ARTEX 侧失败 / 5 已保存但工具数为 0
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

[ -f .env ] || die "缺 .env，先跑 bash deploy/start.sh（或 deploy/enable-asm.sh）"
set -a; . ./.env; set +a
ARTEX="http://127.0.0.1:${ARTEX_PORT:-8787}"
ASM_PORT="${ASM_PORT:-8082}"
SS="http://127.0.0.1:${ASM_PORT}"
SS_INTERNAL="http://scope-sentry:8082"   # 容器网络内地址（ARTEX 从容器里访问）
CRED="data/asm/credentials.txt"
KEY_NAME="artex-lab"

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
  -d "$(jq -nc --arg p "${ARTEX_PASSWORD}" '{username:"ARTEX",password:$p}')" | jget '.token' '.data.token')
[ -n "$ATOKEN" ] || { bad "ARTEX 登录失败：检查 .env 的 ARTEX_PASSWORD"; exit 4; }
ok "ARTEX 已登录"

# ---- 测绘侧 ----
LOGIN=$(curl -sS -X POST "$SS/api/user/login" -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg u "${ASM_ADMIN_USER}" --arg p "${ASM_ADMIN_PASSWORD}" '{username:$u,password:$p}')" || true)
STOKEN=$(printf '%s' "$LOGIN" | jget '.data.access_token' '.access_token' '.token' '.data.token')
if [ -z "$STOKEN" ]; then
  bad "ScopeSentry 登录失败：$(printf '%s' "$LOGIN" | head -c 200)"
  bad "若已改过密码，把它写进 .env 的 ASM_ADMIN_PASSWORD 再重跑"
  exit 2
fi
ok "ScopeSentry 已登录"

# 记录本次之前名为 artex-lab 的旧 Key；等新 Key 生效后再删，避免中途失败把现有接线弄断。
OLD_IDS=$(curl -sS "$SS/api/apikey/list" -H "Authorization: Bearer ${STOKEN}" 2>/dev/null \
  | jq -r --arg n "$KEY_NAME" '.data.list[]? | select(.name == $n) | .id' 2>/dev/null || true)

# ---- 建 Key（明文只返回一次） ----
RESP=$(curl -sS -X POST "$SS/api/apikey/create" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${STOKEN}" -d "{\"name\":\"${KEY_NAME}\"}")
KEY=$(printf '%s' "$RESP" | jget '.data.key' '.key')
NEW_ID=$(printf '%s' "$RESP" | jget '.data.id' '.id')
[ -n "$KEY" ] || { bad "建 API Key 失败：$(printf '%s' "$RESP" | head -c 200)"; exit 3; }
ok "API Key 已创建 ${KEY:0:10}…"

# ---- 写进 ARTEX 数据源 ----
SAVE=$(curl -sS -X POST "$ARTEX/api/sync/scopesentry/datasource" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${ATOKEN}" \
  -d "$(jq -nc --arg u "$SS_INTERNAL/mcp" --arg k "$KEY" '{url:$u,api_key:$k}')")
[ "$(printf '%s' "$SAVE" | jget '.enabled')" = "true" ] \
  || { bad "数据源写入失败：$(printf '%s' "$SAVE" | head -c 200)"; exit 4; }
ok "数据源已启用（$SS_INTERNAL/mcp）"

# ---- 清理旧 Key（新 Key 已写入成功，此时删旧的是安全的） ----
REMOVED=0
for id in $OLD_IDS; do
  [ -z "$id" ] && continue
  [ -n "$NEW_ID" ] && [ "$id" = "$NEW_ID" ] && continue
  if curl -sS -o /dev/null -X POST "$SS/api/apikey/delete" -H 'Content-Type: application/json' \
       -H "Authorization: Bearer ${STOKEN}" -d "$(jq -nc --arg id "$id" '{id:$id}')"; then
    REMOVED=$((REMOVED+1))
  fi
done
[ "$REMOVED" -gt 0 ] && ok "已清理 $REMOVED 个旧 Key（避免重复接线越积越多）"

# ---- 验证 ----
ST=$(curl -sS "$ARTEX/api/sync/scopesentry/status" -H "Authorization: Bearer $ATOKEN")
N=$(printf '%s' "$ST" | jq -r '[.tools[]?] | length' 2>/dev/null || true)
case "$N" in ''|*[!0-9]*) N=0 ;; esac

umask 077
mkdir -p data/asm
chmod 700 data/asm 2>/dev/null || true
cat > "$CRED" <<CREDEOF
# 资产测绘凭据（勿提交、勿外传）
SS_URL=$SS
SS_USER=$ASM_ADMIN_USER
SS_PASSWORD=$ASM_ADMIN_PASSWORD
SS_API_KEY=$KEY
SS_KEY_ID=$NEW_ID
SS_MCP=$SS_INTERNAL/mcp
时间 $(date -Is)
CREDEOF
chmod 600 "$CRED"

if [ "$N" -gt 0 ]; then
  ok "接通：ARTEX 已发现 $N 个测绘工具，凭据在 $CRED"
  exit 0
else
  warn "数据源已保存但工具数为 0（服务端可能仍在初始化）。等 1-2 分钟重跑：bash deploy/wire.sh"
  exit 5
fi
