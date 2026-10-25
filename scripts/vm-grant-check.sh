#!/usr/bin/env bash
# P0-05 真机判据：授权能收回（granted=true → false），且收回到位后 impact.grants 立刻反映。
set -u
PY=/usr/bin/python3
T=$(. "$(dirname "$0")/_token.sh"; artex_token)
ID=${1:-19}
B=http://127.0.0.1:8787/api/tasks/$ID
show() { curl -s -H "Authorization: Bearer $T" "$B/impact" | $PY -c '
import sys,json
d=json.load(sys.stdin)
gs={g["class"]:g["granted"] for g in (d.get("grants") or [])}
print("   grants:",gs or "{}")'; }

echo "== 1. 先授权 data_write"
curl -s -X POST -H "Authorization: Bearer $T" -H 'Content-Type: application/json' \
  -d '{"class":"data_write","granted":true,"note":"真机判据"}' "$B/grant"; echo
show
echo "== 2. 收回（granted=false）"
curl -s -X POST -H "Authorization: Bearer $T" -H 'Content-Type: application/json' \
  -d '{"class":"data_write","granted":false,"note":"真机判据·收回"}' "$B/grant"; echo
show
echo "== 3. 库里对账"
echo kali | sudo -S -u artex psql -d artex -tAc "select action_class||'='||granted from task_grants where task_id=$ID order by action_class" 2>/dev/null | sed 's/^/   /'
