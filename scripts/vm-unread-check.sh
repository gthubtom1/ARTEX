#!/usr/bin/env bash
# 在 Kali VM 里跑：验证未读通知（⑦）—— 战果新证据 + 审查官未读的聚合口径。
set -u
BASE=http://127.0.0.1:8787
TOKEN=$(. \"$(dirname \"$0\")/_token.sh\"; artex_token)
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
