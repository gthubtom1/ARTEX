#!/usr/bin/env bash
# deploy/ 下所有脚本共用的工具函数。不单独执行。
# shellcheck shell=bash

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
ROOT="$(pwd)"

log()  { printf '\n\033[1;36m[*] %s\033[0m\n' "$*"; }
ok()   { printf '\033[1;32m[+] %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m[!] %s\033[0m\n' "$*"; }
bad()  { printf '\033[1;31m[x] %s\033[0m\n' "$*"; }
die()  { bad "$*"; exit 1; }

dc() {
  if docker compose version >/dev/null 2>&1; then docker compose "$@";
  else docker-compose "$@"; fi
}

gen() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c "${1:-24}" || true; }

# 生成 .env（幂等：已存在则只补齐缺失的键）
ensure_env() {
  if [ ! -f .env ]; then
    ( umask 077; cp .env.example .env )
    ok "已从 .env.example 生成 .env"
  fi
  # Windows 侧（PowerShell/记事本）可能留下 CRLF；Git Bash 的 `source .env`
  # 会把 \r 留在值尾部，导致密码/端口全部带脏字符。这里统一改成 LF。
  local tmp
  tmp="$(mktemp)"
  tr -d '\r' < .env > "$tmp"
  chmod 600 "$tmp"
  mv "$tmp" .env
  chmod 600 .env 2>/dev/null || warn "无法把 .env 改成 600（可能属主不是当前用户）"
  set_kv POSTGRES_PASSWORD "$(gen 24)"
  set_kv ARTEX_PASSWORD   "$(gen 20)"
  set_kv ARTEX_CRED_KEY   "$(openssl rand -hex 32 2>/dev/null || gen 64)"
  set_kv POSTGRES_USER    "artex"
  set_kv POSTGRES_DB      "artex"
}

# set_kv KEY VALUE —— 仅当 KEY 为空/不存在时写入
# 注意：awk 重写会替换 inode，必须显式把临时文件设为 600，否则 .env 会从
# 600 掉回默认 umask（通常是 644），密码/密钥就变成同机其他用户可读。
set_kv() {
  local k="$1" v="$2" cur
  cur=$(grep -E "^${k}=" .env 2>/dev/null | tail -1 | cut -d= -f2- || true)
  if [ -z "${cur}" ] || [ "${cur}" = "change-me-please" ]; then
    if grep -qE "^${k}=" .env 2>/dev/null; then
      # 用 awk 替换，避免 sed 转义问题
      awk -v k="$k" -v v="$v" 'BEGIN{FS=OFS="="} $1==k{print k"="v; next} {print}' .env > .env.tmp \
        && chmod 600 .env.tmp && mv .env.tmp .env
    else
      printf '%s=%s\n' "$k" "$v" >> .env
    fi
  fi
}


env_get() {
  grep -E "^$1=" .env 2>/dev/null | tail -1 | cut -d= -f2- | tr -d '\r' || true
}

# force_kv KEY VALUE —— 无条件写入（用于开关类配置，如 ASM_ENABLED）
force_kv() {
  local k="$1" v="$2"
  if grep -qE "^${k}=" .env 2>/dev/null; then
    awk -v k="$k" -v v="$v" 'BEGIN{FS=OFS="="} $1==k{print k"="v; next} {print}' .env > .env.tmp \
      && chmod 600 .env.tmp && mv .env.tmp .env
  else
    printf '%s=%s\n' "$k" "$v" >> .env
  fi
}

asm_enabled() { [ "$(env_get ASM_ENABLED)" = "1" ]; }

# 生成测绘组件所需的密码/默认配置（幂等，只补缺）
ensure_asm_env() {
  set_kv ASM_MONGO_USER      "root"
  set_kv ASM_MONGO_PASSWORD  "$(gen 24)"
  set_kv ASM_REDIS_PASSWORD  "$(gen 24)"
  set_kv ASM_ADMIN_USER      "ScopeSentry"
  set_kv ASM_ADMIN_PASSWORD  "ScopeSentry"
  # 上游 ScopeSentry 与 ScopeSentry-Scan 的发版节奏不同（扫描端通常落后），
  # 因此两个镜像各用各的 tag；锁版本见 deploy/README.md。
  set_kv ASM_TAG             "1.9.4"
  set_kv ASM_SCAN_TAG        "latest"
  set_kv ASM_BIND            "127.0.0.1"
  set_kv ASM_NODE_NAME       "lab-node"
  set_kv ASM_PORT            "8082"
}

compose_profile() { if asm_enabled; then printf '%s' "--profile asm"; fi; }

wait_http() { # url name tries
  local url="$1" name="$2" tries="${3:-120}" i=0
  log "等待 $name 就绪：$url"
  while [ "$i" -lt "$tries" ]; do
    if curl -fsS -o /dev/null --max-time 5 "$url" 2>/dev/null; then ok "$name 就绪"; return 0; fi
    i=$((i+1)); sleep 3
  done
  bad "$name 超时（$url）"
  dc ps || true
  return 1
}

# 与 wait_http 相同，但接受指定 HTTP 码（用于启用了 Basic Auth 时 auth/status 会回 401）
wait_http_codes() { # url name tries code...
  local url="$1" name="$2" tries="${3:-120}" i=0 code accept
  shift 3
  accept=" $* "
  log "等待 $name 就绪：$url（接受 $*）"
  while [ "$i" -lt "$tries" ]; do
    code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "$url" 2>/dev/null || true)
    case "$accept" in *" $code "*) ok "$name 就绪（HTTP $code）"; return 0 ;; esac
    i=$((i+1)); sleep 3
  done
  bad "$name 超时（$url，最后 HTTP ${code:-?}）"
  dc ps || true
  return 1
}

artex_base() { printf 'http://127.0.0.1:%s' "$(env_get ARTEX_PORT || true)"; }
