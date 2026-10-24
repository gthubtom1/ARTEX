#!/usr/bin/env bash
# 在 Kali VM 里跑：验战况卡 P0-b/P0-c 的真库判据。
# 用法：bash /tmp/vm-check.sh [task_id]
# 只读 + 两次写操作（/loot/seen、/brief/ai），不改任何目标。
set -u
TASK="${1:-14}"
BASE=http://127.0.0.1:8787
KEY=/opt/artex/jwt.key

TOKEN=$(echo kali | sudo -S cat "$KEY" 2>/dev/null | python3 -c '
import sys,hmac,hashlib,base64,json,time
key=sys.stdin.read().strip().encode()
b64=lambda b: base64.urlsafe_b64encode(b).rstrip(b"=")
hdr=b64(json.dumps({"alg":"HS256","typ":"JWT"},separators=(",",":")).encode())
now=int(time.time())
pay=b64(json.dumps({"sub":"ARTEX","exp":now+3600,"iat":now},separators=(",",":")).encode())
sig=b64(hmac.new(key,hdr+b"."+pay,hashlib.sha256).digest())
sys.stdout.write((hdr+b"."+pay+b"."+sig).decode())
')

if [ -z "$TOKEN" ]; then echo "[!] 取不到 token（jwt.key 读不到？）"; exit 1; fi
api() { curl -s -H "Authorization: Bearer $TOKEN" "$@"; }

echo "== health: $(curl -s -o /dev/null -w '%{http_code}' $BASE/api/health)"
echo "== 版本: $(curl -s $BASE/api/health)"

echo
echo "== [P0-a/b] GET /api/tasks/$TASK/brief"
api "$BASE/api/tasks/$TASK/brief" | jq -c '{
  level_label, basis,
  has:(.has|length), has_text:[.has[].text],
  can:(.can|length), can_src:[.can[].source]|group_by(.)|map({(.[0]):length})|add,
  can_standing:[.can[].standing]|group_by(.)|map({(.[0]):length})|add,
  can_detail:[.can[]|{id:.finding_id,standing,gap}],
  actions:(.actions|length), action_states:[.actions[].state]|unique,
  new_since:(if has("new_since") then .new_since else "字段不存在(旧二进制)" end),
  verdict_label, say_this
}'

echo
echo "== [P0-b] 未读游标：POST /loot/seen → 再看 new_since"
api -X POST -H 'Content-Type: application/json' -d '{}' "$BASE/api/tasks/$TASK/loot/seen" | jq -c .
api "$BASE/api/tasks/$TASK/brief" | jq -c '{new_since:.new_since}'

echo
echo "== [P0-c] POST /brief/ai（真模型；没配模型会 filled=0 + 回落规则）"
api -X POST -H 'Content-Type: application/json' -d '{}' "$BASE/api/tasks/$TASK/brief/ai" | jq -c '{asked,filled,model, ai_rows:[.can[]|select(.source=="ai")|{id:.finding_id,cached,grants}]}'

echo
echo "== [P0-c] 再取一次 /brief（应命中缓存：source=ai + cached=true）"
api "$BASE/api/tasks/$TASK/brief" | jq -c '[.can[]|{id:.finding_id,source,cached}]'

echo
echo "== [P1] GET /api/tasks/$TASK/phrases（期望 3–5 条，纯规则）"
api "$BASE/api/tasks/$TASK/phrases" | jq -c '{n:(.phrases|length), source, has, keys:[.phrases[].key], first:.phrases[0].text}'

echo
echo "== [P2] 动作工单：对话即授权 → 留证 → 状态回读"
echo "-- 登记（模拟人在对话里说那句）"
RUN=$(api -X POST -H 'Content-Type: application/json'   -d '{"class":"data_export","say_it":"帮我把它数据库里的订单表数据拉出来，先各取几条看看"}'   "$BASE/api/tasks/$TASK/actions" | tee /tmp/run.json | jq -r '.run.id')
cat /tmp/run.json | jq -c '.run|{id,action_class,state,base_ref,say_it}'
echo "-- /brief 里该动作应变成 requested，且 state 仍为 idle（永不自动执行）"
api "$BASE/api/tasks/$TASK/brief" | jq -c '.actions[]|select(.class=="data_export")|{state,run_state,run_id,evidence_need}'
echo "-- 没证据就标 done 必须被拒（留证纪律）"
api -X POST -H 'Content-Type: application/json' -d '{"state":"done"}'   "$BASE/api/tasks/$TASK/actions/$RUN/result" | jq -c . 2>/dev/null || true
echo "-- 带证据标 done"
api -X POST -H 'Content-Type: application/json'   -d '{"state":"done","evidence":"订单表取到 3 条脱敏样本，共 1284 行","evidence_ref":298}'   "$BASE/api/tasks/$TASK/actions/$RUN/result" | jq -c .
echo "-- 再读 /brief：应 done + 带证据"
api "$BASE/api/tasks/$TASK/brief" | jq -c '.actions[]|select(.class=="data_export")|{run_state,run_evidence}'
echo "-- GET /actions"
api "$BASE/api/tasks/$TASK/actions" | jq -c '{n:(.runs|length), first:(.runs[0]|{id,action_class,state,evidence})}'

echo
echo "== 数据库侧"
echo kali | sudo -S -u artex psql -d artex -tAc "select version from schema_migrations order by version desc limit 2" | sed 's/^/迁移: /'
for t in loot_seen finding_abilities; do
  echo kali | sudo -S -u artex psql -d artex -tAc "select to_regclass('public.$t') is not null" | sed "s/^/表 $t 存在: /"
done
echo kali | sudo -S -u artex psql -d artex -tAc "select task_id,seen_finding_id,updated_at from loot_seen where task_id=$TASK" | sed 's/^/loot_seen: /'
echo kali | sudo -S -u artex psql -d artex -tAc "select finding_id,left(evidence_hash,12),left(text,40) from finding_abilities where task_id=$TASK limit 5" | sed 's/^/finding_abilities: /'
echo kali | sudo -S -u artex psql -d artex -tAc "select id,action_class,state,base_ref,evidence_ref,left(evidence,30) from action_runs where task_id=$TASK order by id desc limit 3" | sed 's/^/action_runs: /'

echo
echo "== 前端已内嵌：拉首页引用的 chunk，grep P0-b/P0-c 的新界面关键字"
code=$(curl -s -o /tmp/index.html -w '%{http_code}' "$BASE/")
echo "/ -> $code"
curl -s "$BASE/" | grep -o '/_next/static/[^"\]*\.js' | sort -u > /tmp/chunks.txt
echo "chunk 数: $(wc -l < /tmp/chunks.txt)"
hit=0
while read -r u; do
  body=$(curl -s "$BASE$u")
  for kw in 战况 每个漏洞能换到什么 详情看战况 用 AI 说清楚; do
    if printf '%s' "$body" | grep -q "$kw"; then echo "  命中「$kw」 <- $u"; hit=$((hit+1)); fi
  done
done < /tmp/chunks.txt
[ "$hit" -gt 0 ] && echo "[+] 部署的前端包含新战况卡（命中 $hit 处）" || echo "[!] 前端还是旧的（0 命中）"
