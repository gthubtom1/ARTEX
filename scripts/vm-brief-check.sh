#!/usr/bin/env bash
# 在 Kali VM 里跑：检查 /brief 是否自洽（等级 / 已拿到 / 能做到 三者不能互相矛盾）。
# 用法：bash /home/kali/vm-brief.sh 14 15 16 17
set -u
BASE=http://127.0.0.1:8787
TASKS="${*:-14 15}"

TOKEN=$(. \"$(dirname \"$0\")/_token.sh\"; artex_token)
[ -z "$TOKEN" ] && { echo "[!] token 取不到"; exit 1; }

for T in $TASKS; do
  J=$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/tasks/$T/brief")
  echo "== task $T"
  echo "$J" | jq -c '{level:.level_label, has:(.has|length),
    can_n:(.can|length), standing:[.can[].standing],
    doable:[.actions[]|select(.doable)|.class],
    not_doable_n:([.actions[]|select(.doable|not)]|length),
    new_since, stall}'
done
