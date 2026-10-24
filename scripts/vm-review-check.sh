#!/usr/bin/env bash
# 在 Kali VM 里跑：真机验证审查官 P3（读战况 + 结构化动作 + 必须有判据）。
# 用法：bash /home/kali/vm-review.sh [task_id]
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

echo "== 触发一次审查（会花一次模型调用）"
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{}' \
  "$BASE/api/tasks/$TASK/review" | head -c 400; echo

sleep 3
echo "== 最近一次 run（verdict / level / expectation / actions）"
echo kali | sudo -S -u artex psql -d artex -tAc "
select json_build_object(
  'id', id, 'verdict', verdict, 'level', level,
  'actions', actions,
  'expectation', expectation,
  'body_head', left(body, 120)
) from review_runs where task_id=$TASK order by id desc limit 1;" | sed 's/^/run: /'

echo "== actions 是否非空 + 是否带 expectation"
echo kali | sudo -S -u artex psql -d artex -tAc "
select count(*) filter (where jsonb_array_length(actions) > 0) as runs_with_actions,
       count(*) filter (where expectation <> '') as runs_with_expectation,
       count(*) as total
from review_runs where task_id=$TASK;" | sed 's/^/agg: /'
