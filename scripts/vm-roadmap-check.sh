#!/usr/bin/env bash
# 进展清单真机判据：GET /api/roadmap 能出统计与分组，且 todo 都写了原因。
set -u
PY=/usr/bin/python3
T=$(. "$(dirname "$0")/_token.sh"; artex_token)
curl -s -H "Authorization: Bearer $T" http://127.0.0.1:8787/api/roadmap | $PY -c '
import sys,json
d=json.load(sys.stdin)
if "error" in d:
    print("ERR", d); sys.exit(1)
print("headline:", d["headline"])
print("stats:", d["done"], "done /", d["todo"], "todo /", d["blocked"], "blocked / total", d["total"])
bad=0
for g in d["groups"]:
    print(" -", g["title"], "|", ", ".join(i["status"] for i in g["items"]))
    for i in g["items"]:
        if i["status"]!="done" and not i.get("why_not"):
            bad+=1; print("   [!] 没写原因:", i["id"])
print("缺原因的条目:", bad)
'
