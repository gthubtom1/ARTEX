#!/usr/bin/env bash
# 0018 真机判据：目标能记住（goal_selected 落库）+ token 花销/上限可见。
set -u
PY=/usr/bin/python3
T=$(. "$(dirname "$0")/_token.sh"; artex_token)
ID=${1:-22}
B=http://127.0.0.1:8787/api/tasks/$ID
echo "== 1. 选目标：拖库 + 上限 500k"
curl -s -X POST -H "Authorization: Bearer $T" -H 'Content-Type: application/json' \
  -d '{"goal_selected":"data_dump","token_budget":500000}' "$B/prefs"; echo
echo "== 2. 重新读 brief（模拟刷新页面）"
curl -s -H "Authorization: Bearer $T" "$B/brief" | $PY -c '
import sys,json
d=json.load(sys.stdin)
print("   goal_selected:", d.get("goal_selected"))
print("   已花: in", d.get("spent_in"), "out", d.get("spent_out"), "cached", d.get("spent_cached"),
      "| 上限", d.get("token_budget"), "| 到顶", d.get("budget_hit"))'
echo "== 3. 库里对账"
echo kali | sudo -S -u artex psql -d artex -tAc "select goal_selected||' / '||token_budget from tasks where id=$ID" 2>/dev/null | sed 's/^/   /'
echo "== 4. 非法目标要被拒"
curl -s -X POST -H "Authorization: Bearer $T" -H 'Content-Type: application/json' -d '{"goal_selected":"拖库"}' "$B/prefs"; echo
