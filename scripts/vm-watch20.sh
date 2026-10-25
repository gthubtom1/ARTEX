#!/usr/bin/env bash
set -u
TOKEN=$(. \"$(dirname \"$0\")/_token.sh\"; artex_token)
ID=${1:-20}
for i in $(seq 1 15); do
  curl -s -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8787/api/tasks/$ID/brief" | python3 -c '
import sys,json,time
d=json.load(sys.stdin)
print(time.strftime("%H:%M:%S"),"lv",d.get("level"),"| has",[h["text"] for h in (d.get("has") or [])],
      "| can",len(d.get("can") or []),{s:sum(1 for x in (d.get("can") or []) if x.get("standing")==s) for s in ["got","one_step","no_gain"]},
      "| todo",len(d.get("actions") or []),"| inherit",len(d.get("inherited") or []))' 2>/dev/null
  st=$(curl -s -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8787/api/tasks/$ID" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("status"),d.get("elapsed",""))' 2>/dev/null)
  echo "    task: $st"
  case "$st" in done*|failed*|timeout*|paused*) echo "  [*] 结束：$st"; break;; esac
  sleep 30
done
