#!/usr/bin/env bash
# 体检。全绿输出 STATUS PASS（退出码 0），否则 STATUS FAIL（退出码 1）。
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
set -a; . ./.env 2>/dev/null || true; set +a

FAIL=0
BASE="http://127.0.0.1:${ARTEX_PORT:-8787}"
row_ok()  { printf '\033[1;32m  [+] %-22s %s\033[0m\n' "$1" "$2"; }
row_bad() { printf '\033[1;31m  [x] %-22s %s\033[0m\n' "$1" "$2"; FAIL=1; }
row_inf() { printf '      %-22s %s\n' "$1" "$2"; }

echo "== 配置 =="
ENV_PERM=$(stat -c %a .env 2>/dev/null || echo '?')
if [ "$ENV_PERM" = "600" ]; then
  row_ok ".env 权限" "600"
else
  row_bad ".env 权限" "$ENV_PERM（含密码，应为 600：chmod 600 .env）"
fi
row_inf "ASM_ENABLED" "${ASM_ENABLED:-0}"
if asm_enabled; then
  row_inf "ASM 镜像 tag" "web=${ASM_TAG:-1.9.4} scan=${ASM_SCAN_TAG:-1.9.1}"
fi

echo "== 容器 =="
dc ps --format '      {{.Name}}  {{.State}}' 2>/dev/null || row_bad "compose" "列不出容器"
RUNNING=$(dc ps --format '{{.State}}' 2>/dev/null | grep -c '^running$' || true)
row_inf "running" "${RUNNING:-0} 个"

echo "== 本体 =="
HEALTH=$(curl -fsS --max-time 5 "$BASE/api/health" 2>/dev/null || true)
if printf '%s' "$HEALTH" | jq -e '.ok == true' >/dev/null 2>&1; then
  row_ok "artex health" "ok（$(printf '%s' "$HEALTH" | jq -r '.version // "?"' 2>/dev/null)）"
else
  row_bad "artex health" "不可达（$BASE/api/health）"
fi

SKIP_AUTH=0
AUTH_PROBE=$(curl -sS --max-time 5 -w $'\n%{http_code}' "$BASE/api/auth/status" 2>/dev/null || true)
AUTH_CODE=$(printf '%s' "$AUTH_PROBE" | tail -n1)
AUTH_BODY=$(printf '%s' "$AUTH_PROBE" | sed '$d')
if [ "$AUTH_CODE" = "200" ]; then
  INIT=$(printf '%s' "$AUTH_BODY" | jq -r '.initialized // empty' 2>/dev/null || true)
  if [ "$INIT" = "true" ]; then
    row_ok "artex init" "已设置登录密码"
  else
    row_bad "artex init" "未初始化（跑 bash deploy/start.sh 设置密码）"
  fi
elif [ "$AUTH_CODE" = "401" ] || [ "$AUTH_CODE" = "403" ]; then
  SKIP_AUTH=1
  row_inf "artex auth" "HTTP Basic Auth 已启用；脚本不带 Basic 凭据，跳过登录/命中率检查"
else
  row_bad "artex init" "数据库不可用（HTTP ${AUTH_CODE:-?}）"
fi

TOKEN=""
if [ "$SKIP_AUTH" = "1" ]; then
  row_inf "artex login" "跳过（Basic Auth）"
else
  TOKEN=$(curl -sS -X POST "$BASE/api/auth/login" \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg p "${ARTEX_PASSWORD:-}" '{username:"ARTEX",password:$p}')" \
    | jq -r '.token // empty' 2>/dev/null || true)
  if [ -n "$TOKEN" ]; then row_ok "artex login" "ok"; else row_bad "artex login" "失败（看 .env 的 ARTEX_PASSWORD）"; fi
fi

echo "== 数据库 =="
PGU="${POSTGRES_USER:-artex}"; PGD="${POSTGRES_DB:-artex}"
psql_q() { dc exec -T postgres psql -U "$PGU" -d "$PGD" -tAc "$1" 2>/dev/null | tr -d '\r' || true; }
MIG_MAX=$(psql_q "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1" | tr -d '\n')
# 只要最新迁移的序号 >= 0011 就算就位（0011 = 战果清单人工状态表）。
# 用前缀数字比较而不是 LIKE '0011%'，以后加 0012 不用再改这里。
MIG_NUM="${MIG_MAX:0:4}"
case "$MIG_NUM" in ''|*[!0-9]*) MIG_NUM=0 ;; esac
# 10# 前缀强制按十进制解释，否则 0011 会被当成八进制 9。
MIG_NUM=$((10#$MIG_NUM))
if [ "$MIG_NUM" -ge 11 ]; then
  row_ok "schema_migrations" "${MIG_MAX:-?}（已到 0011）"
else
  row_bad "schema_migrations" "未到 0011（当前 ${MIG_MAX:-读取失败}）"
fi

echo "== 机制命中率 API =="
if [ "$SKIP_AUTH" = "1" ]; then
  row_inf "hitrate api" "跳过（Basic Auth）"
elif [ -z "${TOKEN:-}" ]; then
  row_bad "hitrate api" "跳过（尚未登录）"
else
  TASK=$(curl -sS --max-time 8 "$BASE/api/tasks" -H "Authorization: Bearer $TOKEN" \
    | jq -r '.tasks[0].id // empty' 2>/dev/null || true)
  if [ -n "$TASK" ]; then
    HR=$(curl -sS --max-time 10 "$BASE/api/system/hitrate?task=$TASK" -H "Authorization: Bearer $TOKEN" 2>/dev/null || true)
    if printf '%s' "$HR" | jq -e '.tables | type == "object"' >/dev/null 2>&1; then
      row_ok "hitrate api" "200（task $TASK）"
    else
      row_bad "hitrate api" "返回异常（task $TASK）"
    fi
  else
    # 无任务时用不存在的 id 探路由：命中 handler 会回 JSON 404 "task not found"，
    # 与 ServeMux 的纯文本 404 可区分。
    PROBE=$(curl -sS --max-time 10 -w $'\n%{http_code}' "$BASE/api/system/hitrate?task=__status_probe__" \
      -H "Authorization: Bearer $TOKEN" 2>/dev/null || true)
    CODE=$(printf '%s' "$PROBE" | tail -n1)
    BODY=$(printf '%s' "$PROBE" | sed '$d')
    if [ "$CODE" = "404" ] && printf '%s' "$BODY" | grep -q 'task not found'; then
      row_ok "hitrate api" "路由存在（暂无任务）"
    else
      row_bad "hitrate api" "不可用（HTTP ${CODE:-?}）"
    fi
  fi
fi

echo "== 资产测绘 =="
if asm_enabled; then
  if curl -fsS -o /dev/null --max-time 5 "http://127.0.0.1:${ASM_PORT:-8082}/"; then
    row_ok "scopesentry http" "200"
  else row_bad "scopesentry http" "不可达（首次初始化需 3-10 分钟）"; fi

  if [ -n "${TOKEN:-}" ]; then
    ST=$(curl -sS --max-time 15 "$BASE/api/sync/scopesentry/status" -H "Authorization: Bearer $TOKEN" 2>/dev/null || true)
    E=$(printf '%s' "$ST" | jq -r '.enabled // false' 2>/dev/null || echo false)
    N=$(printf '%s' "$ST" | jq -r '[.tools[]?] | length' 2>/dev/null || echo 0)
    case "$N" in ''|*[!0-9]*) N=0 ;; esac
    if [ "$E" = "true" ] && [ "$N" -gt 0 ]; then
      row_ok "MCP 数据源" "enabled，$N 个工具"
    elif [ "$E" = "true" ]; then
      row_bad "MCP 数据源" "enabled 但 0 个工具（重跑 bash deploy/wire.sh）"
    else
      row_bad "MCP 数据源" "disabled（跑 bash deploy/wire.sh）"
    fi
  elif [ "$SKIP_AUTH" = "1" ]; then
    row_inf "MCP 数据源" "跳过（Basic Auth）"
  fi

  if [ -z "${ASM_MONGO_PASSWORD:-}" ] || [ "${ASM_MONGO_PASSWORD}" = "asm-not-configured" ] \
     || [ -z "${ASM_REDIS_PASSWORD:-}" ] || [ "${ASM_REDIS_PASSWORD}" = "asm-not-configured" ]; then
    row_bad "ASM 密码" "Mongo/Redis 仍是默认值，跑 bash deploy/enable-asm.sh 重新生成"
  else
    row_ok "ASM 密码" "Mongo/Redis 已随机生成"
  fi
  if [ "${ASM_ADMIN_PASSWORD:-}" = "ScopeSentry" ]; then
    row_inf "ASM 管理员密码" "仍是 ScopeSentry/ScopeSentry（登录后立刻改，并写回 .env）"
  fi
else
  row_inf "状态" "未启用（bash deploy/start.sh --with-asm 加装）"
fi

echo "== 出口 =="
row_inf "exit ip" "$(curl -sS --max-time 8 https://api.ipify.org 2>/dev/null || echo '?')"

echo
if [ "$FAIL" -eq 0 ]; then echo "STATUS PASS"; else echo "STATUS FAIL"; fi
exit "$FAIL"
