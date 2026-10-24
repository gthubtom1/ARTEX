#!/usr/bin/env bash
# 在 Kali VM 里跑：验证未读通知（⑦）—— 战果新证据 + 审查官未读的聚合口径。
set -u
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
echo "== GET /api/tasks/unread"
curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/tasks/unread" | jq -c .
echo "== 对照：库里 findings 与 review 未读"
echo kali | sudo -S -u artex psql -d artex -tAc "
select 'findings_new(task_id,finding_id)', t.id, count(*)
from tasks t join exploration_nodes n on n.exploration_id=t.exploration_id and n.kind='finding'
left join loot_seen ls on ls.task_id=t.id
where n.id > coalesce(ls.seen_finding_id,0) group by t.id order by t.id" | sed 's/^/db: /'
echo kali | sudo -S -u artex psql -d artex -tAc "
select 'review_unseen', task_id, count(*) from review_runs where status='done' and seen_at is null group by task_id order by task_id" | sed 's/^/db: /'
