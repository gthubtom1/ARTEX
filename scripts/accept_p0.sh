#!/usr/bin/env bash
# ============================================================================
# accept_p0.sh — ARTEX P0 验收脚本
#
# 只做只读检查，不创建任何业务数据。
# 唯一会启动的东西是 fixtures/docker-compose.yml 里的本地靶场；
# 唯一的数据写入是 audit_log 中 actor='accept_p0' 的测试记录，并在事务内 ROLLBACK。
#
# 检查项：
#   1. 靶场能起：compose up -d 后轮询 8081 / 3000 / 8082，最多 90 秒
#   2. 数据库可连：psql "$ARTEX_PG_DSN" -c 'select 1'
#   3. 10 张 P0 新表存在（to_regclass）
#   4. 幂等键唯一索引存在（pg_indexes 中模糊匹配 idem）
#   5. audit_log 可写（BEGIN; INSERT; DELETE; ROLLBACK;）
#
# 用法：
#   export ARTEX_PG_DSN='postgres://artex:...@127.0.0.1:5432/artex?sslmode=disable'
#   bash scripts/accept_p0.sh
#
# 缺 docker / psql / ARTEX_PG_DSN 时对应步骤标 [skip]；跳过不算通过，最终 exit 1。
# ============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/fixtures/docker-compose.yml"

PORTS=(8081 3000 8082)
TIMEOUT_SECONDS=90

P0_TABLES=(
  assets_owned
  access_refs
  routes
  asset_checks
  change_events
  capabilities
  creds_vault
  assumptions
  evidence_timeline
  audit_log
)

PASS=0
FAIL=0
SKIP=0
DB_CHECKED=0
DB_OK=0

say()  { printf '%s\n' "$*"; }
ok()   { PASS=$((PASS + 1)); printf '[ok]   %s\n' "$*"; }
fail() { FAIL=$((FAIL + 1)); printf '[fail] %s\n' "$*" >&2; }
skip() { SKIP=$((SKIP + 1)); printf '[skip] %s\n' "$*"; }

# 返回 0 表示 TCP/HTTP 可达（不要求 HTTP 2xx；连接被拒绝才算不可达）。
port_reachable() {
  local port="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -sS -L --max-time 3 -o /dev/null "http://127.0.0.1:${port}/" >/dev/null 2>&1
    return
  fi
  (exec 3<>"/dev/tcp/127.0.0.1/${port}") >/dev/null 2>&1
}

# 统一的 psql 调用：-w 不提示密码，-X 忽略 .psqlrc，-tA 便于脚本解析。
db_query() {
  local sql="$1"
  PGCONNECT_TIMEOUT=5 psql "$ARTEX_PG_DSN" \
    -w -X -q -t -A -v ON_ERROR_STOP=1 -c "$sql"
}

require_db() {
  local what="$1"
  if [ -z "${ARTEX_PG_DSN:-}" ]; then
    skip "${what}：未设置 ARTEX_PG_DSN"
    return 1
  fi
  if ! command -v psql >/dev/null 2>&1; then
    skip "${what}：未找到 psql"
    return 1
  fi
  if [ "${DB_CHECKED}" -eq 1 ] && [ "${DB_OK}" -ne 1 ]; then
    skip "${what}：数据库不可用（见第 2 步）"
    return 1
  fi
  return 0
}

# ---------------------------------------------------------------------------
# 1. 本地靶场
# ---------------------------------------------------------------------------
check_fixtures() {
  say ""
  say "== 1/5 靶场可启动（8081 / 3000 / 8082） =="

  if [ ! -f "${COMPOSE_FILE}" ]; then
    fail "缺少 ${COMPOSE_FILE}"
    return
  fi
  if ! command -v docker >/dev/null 2>&1; then
    skip "未找到 docker，无法启动本地靶场"
    return
  fi
  if ! docker compose version >/dev/null 2>&1; then
    skip "docker compose v2 不可用，无法启动本地靶场"
    return
  fi
  if ! docker info >/dev/null 2>&1; then
    skip "Docker daemon 不可用（Docker Desktop 未启动？）"
    return
  fi

  if ! docker compose -f "${COMPOSE_FILE}" up -d; then
    fail "docker compose up -d 失败（见上方错误；端口占用/镜像拉取失败最常见）"
    return
  fi

  say "容器已触发启动，轮询端口：${PORTS[*]}（最多 ${TIMEOUT_SECONDS}s）"
  local start now elapsed port all down
  start=$(date +%s)

  while :; do
    all=1
    for port in "${PORTS[@]}"; do
      if ! port_reachable "${port}"; then
        all=0
      fi
    done
    if [ "${all}" -eq 1 ]; then
      ok "靶场 3 个端口均可访问：${PORTS[*]}"
      return
    fi

    now=$(date +%s)
    elapsed=$((now - start))
    if [ "${elapsed}" -ge "${TIMEOUT_SECONDS}" ]; then
      down=""
      for port in "${PORTS[@]}"; do
        if ! port_reachable "${port}"; then
          down="${down}${down:+ }${port}"
        fi
      done
      fail "等待 ${TIMEOUT_SECONDS}s 后仍不可访问：${down:-未知}（用 docker compose -f fixtures/docker-compose.yml logs 查看）"
      return
    fi
    sleep 2
  done
}

# ---------------------------------------------------------------------------
# 2. 数据库连接
# ---------------------------------------------------------------------------
check_db() {
  say ""
  say "== 2/5 数据库可连接 =="

  if [ -z "${ARTEX_PG_DSN:-}" ]; then
    skip "未设置 ARTEX_PG_DSN"
    return
  fi
  if ! command -v psql >/dev/null 2>&1; then
    skip "未找到 psql（请安装 PostgreSQL 客户端，或把 psql 加入 PATH）"
    return
  fi

  DB_CHECKED=1
  local out
  if out=$(db_query "select 1" 2>&1); then
    out=$(printf '%s' "${out}" | tr -d '[:space:]')
    if [ "${out}" = "1" ]; then
      DB_OK=1
      ok "数据库连接成功（select 1）"
      return
    fi
  fi
  fail "数据库连接失败：$(printf '%s' "${out}" | tr '\n' ' ' | cut -c1-300)"
}

# ---------------------------------------------------------------------------
# 3. P0 新表
# ---------------------------------------------------------------------------
check_tables() {
  say ""
  say "== 3/5 P0 新表存在（10 张） =="

  if ! require_db "新表检查"; then
    return
  fi

  local t list="" sql missing
  for t in "${P0_TABLES[@]}"; do
    list="${list}'${t}',"
  done
  list="${list%,}"
  sql="SELECT t FROM unnest(ARRAY[${list}]::text[]) AS t WHERE to_regclass(t) IS NULL ORDER BY t"

  if missing=$(db_query "${sql}" 2>&1); then
    missing=$(printf '%s' "${missing}" | tr -d '\r')
    if [ -z "${missing}" ]; then
      ok "10 张 P0 表全部存在"
    else
      fail "缺少表：$(printf '%s' "${missing}" | tr '\n' ' ')"
    fi
  else
    fail "查询表存在性失败：$(printf '%s' "${missing}" | tr '\n' ' ' | cut -c1-300)"
  fi
}

# ---------------------------------------------------------------------------
# 4. 幂等键唯一索引
# ---------------------------------------------------------------------------
check_idem_index() {
  say ""
  say "== 4/5 幂等键唯一索引存在（pg_indexes 模糊匹配 idem） =="

  if ! require_db "幂等索引检查"; then
    return
  fi

  local sql found
  sql="SELECT indexname
FROM pg_indexes
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
  AND (indexname ILIKE '%idem%' OR indexdef ILIKE '%idem%')
  AND indexdef ILIKE 'CREATE UNIQUE INDEX%'
ORDER BY indexname"

  if found=$(db_query "${sql}" 2>&1); then
    found=$(printf '%s' "${found}" | tr -d '\r')
    if [ -n "${found}" ]; then
      ok "找到幂等唯一索引：$(printf '%s' "${found}" | tr '\n' ' ')"
    else
      fail "未找到包含 idem 的唯一索引（期望 uq_*_idem 或 idem_keys 主键）"
    fi
  else
    fail "查询 pg_indexes 失败：$(printf '%s' "${found}" | tr '\n' ' ' | cut -c1-300)"
  fi
}

# ---------------------------------------------------------------------------
# 5. audit_log 可写（事务内回滚，不留数据）
# ---------------------------------------------------------------------------
check_audit_log() {
  say ""
  say "== 5/5 audit_log 可写（事务内回滚） =="

  if ! require_db "audit_log 检查"; then
    return
  fi

  local out residual
  if out=$(db_query "BEGIN; INSERT INTO audit_log (actor) VALUES ('accept_p0'); DELETE FROM audit_log WHERE actor = 'accept_p0'; ROLLBACK;" 2>&1); then
    if residual=$(db_query "SELECT count(*) FROM audit_log WHERE actor = 'accept_p0'" 2>&1); then
      residual=$(printf '%s' "${residual}" | tr -d '[:space:]')
      if [ "${residual}" = "0" ]; then
        ok "audit_log 可写，事务已回滚，未留下测试记录"
      else
        fail "audit_log 回滚后仍有 ${residual} 条 actor='accept_p0' 记录"
      fi
    else
      fail "回滚后校验失败：$(printf '%s' "${residual}" | tr '\n' ' ' | cut -c1-300)"
    fi
  else
    fail "audit_log 写入失败：$(printf '%s' "${out}" | tr '\n' ' ' | cut -c1-300)"
  fi
}

# ---------------------------------------------------------------------------
# 汇总
# ---------------------------------------------------------------------------
summary() {
  say ""
  say "== 汇总 =="
  say "通过 ${PASS} / 失败 ${FAIL} / 跳过 ${SKIP}（共 5 项）"

  if [ "${PASS}" -eq 5 ] && [ "${FAIL}" -eq 0 ] && [ "${SKIP}" -eq 0 ]; then
    say "P0 验收通过"
    exit 0
  fi
  say "P0 验收未通过（跳过不算通过）"
  exit 1
}

check_fixtures
check_db
check_tables
check_idem_index
check_audit_log
summary
