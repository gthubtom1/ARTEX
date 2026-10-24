#!/usr/bin/env bash
# 最终回归（真机）：主线判据一次跑完。
# 用法：bash /home/kali/vm-final.sh [task_id]
set -u
TASK="${1:-14}"
BASE=http://127.0.0.1:8787

TOKEN=$(echo kali | sudo -S cat /opt/artex/jwt.key 2>/dev/null | python3 -c '
import sys,hmac,hashlib,base64,json,time
key=sys.stdin.read().strip().encode()
b64=lambda b: base64.urlsafe_b64encode(b).rstrip(b"=")
hdr=b64(json.dumps({"alg":"HS256","typ":"JWT"},separators=(",",":")).encode())
now=int(time.time())
pay=b64(json.dumps({"sub":"ARTEX","exp":now+3600,"iat":now},separators=(",",":")).encode())
sig=b64(hmac.new(key,hdr+b"."+pay,hashlib.sha256).digest())
sys.stdout.write((hdr+b"."+pay+b"."+sig).decode())
')
[ -z "$TOKEN" ] && { echo "[!] token 取不到"; exit 1; }
api() { curl -s -H "Authorization: Bearer $TOKEN" "$@"; }

echo "=== 1. 服务与迁移"
curl -s "$BASE/api/health"; echo
echo kali | sudo -S -u artex psql -d artex -tAc "select version from schema_migrations order by version desc limit 1" | sed 's/^/迁移: /'

echo
echo "=== 2. 战况卡自洽（等级 / 已拿到 / 三段式 / 能做到 / 未读 / 停摆）"
api "$BASE/api/tasks/$TASK/brief" | jq -c '{
  level:.level_label, basis,
  has_n:(.has|length), has:[.has[].text],
  can_n:(.can|length), standing:[.can[].standing]|group_by(.)|map({(.[0]):length})|add,
  ai_n:([.can[]|select(.source=="ai")]|length),
  doable_n:([.actions[]|select(.doable)]|length),
  not_doable_n:([.actions[]|select(.doable|not)]|length),
  actions_state:[.actions[].state]|unique,
  new_since, stall
}'

echo
echo "=== 3. 动态短语（3–5 条，纯规则）"
api "$BASE/api/tasks/$TASK/phrases" | jq -c '{n:(.phrases|length), has, first:.phrases[0].text}'

echo
echo "=== 4. 业务动作工单"
api "$BASE/api/tasks/$TASK/actions" | jq -c '{n:(.runs|length), runs:[.runs[]|{id,action_class,state,evidence:(.evidence|.[0:24])}]}'

echo
echo "=== 5. 未读（列表红点/标题数字来源）"
api "$BASE/api/tasks/unread" | jq -c '{total, mine:(.by_task["'"$TASK"'"] // 0)}'

echo
echo "=== 6. 审查官最近一次（动作 / 判据）"
api "$BASE/api/tasks/$TASK/review/latest" | jq -c '{verdict:.run.verdict, level:.run.level, actions:.run.actions, expectation:.run.expectation}' 2>/dev/null \
  || echo "（无记录）"

echo
echo "=== 7. 三端一致性：brief 的洞数 vs 库里的 finding 数 vs 战果行"
api "$BASE/api/tasks/$TASK/brief" | jq -c '{can_n:(.can|length)}'
echo kali | sudo -S -u artex psql -d artex -tAc "
select 'db_findings', count(*) from exploration_nodes n join tasks t on t.exploration_id=n.exploration_id
where t.id=$TASK and n.kind='finding'"
echo kali | sudo -S -u artex psql -d artex -tAc "
select 'loot_seen', coalesce(seen_finding_id,-1) from loot_seen where task_id=$TASK"
echo kali | sudo -S -u artex psql -d artex -tAc "
select 'action_runs', count(*) filter (where state='requested') as open, count(*) as total from action_runs where task_id=$TASK"
