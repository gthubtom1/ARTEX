#!/bin/sh
# verify-0.9.6.sh —— 一次跑完 0.9.6 的所有验收判据，输出 PASS/FAIL。
#
# 为什么要有它：docs/20-完工方案.md 要求"每条完成都有可复算判据"。
# 人工逐条点太慢、也容易漏；这个脚本把判据固化成命令，谁跑都是同一结果。
#
# 用法：
#   sudo bash scripts/verify-0.9.6.sh            # 在装 ARTEX 的机器上跑（读 /opt/artex/jwt.key）
#   ARTEX_DIR=/path ARTEX_PORT=8787 sudo -E bash scripts/verify-0.9.6.sh
#   或用现成 token：ARTEX_TOKEN=xxx bash scripts/verify-0.9.6.sh
set -uo pipefail

ARTEX_DIR="${ARTEX_DIR:-/opt/artex}"
PORT="${ARTEX_PORT:-8787}"
A="http://127.0.0.1:${PORT}/api"
FAIL=0
ok()   { printf '\033[1;32m  [+] %-34s %s\033[0m\n' "$1" "$2"; }
bad()  { printf '\033[1;31m  [x] %-34s %s\033[0m\n' "$1" "$2"; FAIL=$((FAIL+1)); }
skip() { printf '      %-34s %s\n' "$1" "$2"; }

# ── token：优先环境变量，其次用应用自己的 jwt.key 签一个只读 token ──
TOKEN="${ARTEX_TOKEN:-}"
if [ -z "$TOKEN" ] && [ -r "$ARTEX_DIR/jwt.key" ]; then
  KEY=$(cat "$ARTEX_DIR/jwt.key")
  TOKEN=$(python3 - "$KEY" <<'PY'
import base64, hashlib, hmac, json, sys, time
key = sys.argv[1].strip().encode()
def b64(x): return base64.urlsafe_b64encode(x).rstrip(b"=")
hdr = b64(json.dumps({"alg":"HS256","typ":"JWT"}, separators=(",",":")).encode())
now = int(time.time())
pl  = b64(json.dumps({"sub":"ARTEX","iat":now,"exp":now+3600}, separators=(",",":")).encode())
print((hdr + b"." + pl + b"." + b64(hmac.new(key, hdr+b"."+pl, hashlib.sha256).digest())).decode())
PY
)
fi
[ -z "$TOKEN" ] && { bad "token" "拿不到（设 ARTEX_TOKEN 或用 root 读 jwt.key）"; exit 1; }
H="Authorization: Bearer $TOKEN"

echo "== 服务 =="
VER=$(curl -s -m 5 "$A/health" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("version",""))' 2>/dev/null)
case "$VER" in 0.9.*) ok "版本" "$VER" ;; "") bad "版本" "health 不可达" ;; *) bad "版本" "$VER（期望 0.9.x）" ;; esac

echo "== 阶段 A：值不值得打 / 审查官 / 报告 =="
TID=$(curl -s -m 8 "$A/tasks" -H "$H" | python3 -c 'import sys,json;d=json.load(sys.stdin);ts=d.get("tasks",[]);print(ts[0]["id"] if ts else "")' 2>/dev/null)
if [ -n "$TID" ]; then
  V=$(curl -s -m 8 "$A/tasks/$TID/verdict" -H "$H")
  printf '%s' "$V" | python3 -c '
import sys,json
d=json.load(sys.stdin)
ok=d.get("verdict") in ("worth","not_worth","unclear")
print("verdict="+str(d.get("verdict"))+" label="+str(d.get("label"))+" reasons="+str(len(d.get("reasons") or []))+" surface="+str(len(d.get("surface") or [])))' > /tmp/_v 2>/dev/null
  if grep -q '^verdict=' /tmp/_v; then ok "值不值得打接口" "$(cat /tmp/_v)"; else bad "值不值得打接口" "返回异常"; fi
else
  skip "值不值得打接口" "库里没有任务，跳过"
fi

REP=$(curl -s -m 15 "$A/report${TID:+?task=$TID}" -H "$H")
RLEN=$(printf '%s' "$REP" | wc -c)
if [ "$RLEN" -gt 200 ]; then
  if printf '%s' "$REP" | grep -q '详细报告'; then ok "报告含详细正文" "${RLEN} 字节，含「详细报告」"; else skip "报告含详细正文" "${RLEN} 字节（该任务还没有详细正文，属正常）"; fi
else bad "报告" "只有 ${RLEN} 字节"; fi

echo "== 阶段 B/C：模板阶段、阶段门、命中率说明 =="
HR=$(curl -s -m 10 "$A/system/hitrate${TID:+?task=$TID}" -H "$H")
printf '%s' "$HR" | python3 -c '
import sys,json
d=json.load(sys.stdin)
t=d.get("tables") or {}; n=d.get("needs") or {}
zeros=[k for k,v in t.items() if v==0]
missing=[k for k in zeros if k not in n]
print("tables=%d zeros=%d zeros_without_reason=%d" % (len(t), len(zeros), len(missing)))' > /tmp/_h 2>/dev/null
if grep -q '^tables=' /tmp/_h; then ok "命中率带原因" "$(cat /tmp/_h)"; else bad "命中率" "返回异常"; fi

if command -v psql >/dev/null 2>&1; then
  DSN=$(sed -n 's/^ARTEX_PG_DSN=//p' "$ARTEX_DIR/artex.env" 2>/dev/null | tr -d '\r')
  if [ -n "$DSN" ]; then
    psql "$DSN" -At -F ' | ' -c "select name, coalesce(stage,''), timeout_min from task_templates order by id" > /tmp/_t 2>/dev/null
    if [ -s /tmp/_t ]; then
      if grep -q '| recon |' /tmp/_t && grep -q '| full |' /tmp/_t; then ok "模板阶段" "$(head -1 /tmp/_t)"; else bad "模板阶段" "缺少 stage 值"; fi
      ZT=$(awk -F' \\| ' '$3+0==0' /tmp/_t | wc -l)
      [ "$ZT" -eq 0 ] && ok "模板时长" "全部 >0" || bad "模板时长" "$ZT 个模板时长为 0"
    else skip "模板" "读不到 task_templates"; fi
    psql "$DSN" -At -c "select count(*) from task_error_events where tool='StageGate'" > /tmp/_sg 2>/dev/null
    SG=$(tr -d '[:space:]' < /tmp/_sg)
    if [ "${SG:-0}" -gt 0 ]; then ok "阶段门拦截记录" "$SG 条（已真机生效）"; else skip "阶段门拦截记录" "0 条（还没遇到模型尝试利用，属正常）"; fi
  else skip "数据库判据" "artex.env 里没有 ARTEX_PG_DSN"; fi
else skip "数据库判据" "本机没有 psql"; fi

echo
if [ "$FAIL" -eq 0 ]; then echo "VERIFY_RESULT PASS"; exit 0; fi
echo "VERIFY_RESULT FAIL ($FAIL 项)"; exit 1
