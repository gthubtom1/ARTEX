#!/usr/bin/env bash
# 在 Kali VM 里跑：检查"下一步提案"是否按前置能力分派（E0 不许提 webshell）。
# 用法：bash /home/kali/vm-impact.sh [task_id ...]
set -u
BASE=http://127.0.0.1:8787
TASKS="${*:-14 15}"

TOKEN=$(. \"$(dirname \"$0\")/_token.sh\"; artex_token)
[ -z "$TOKEN" ] && { echo "[!] token 取不到"; exit 1; }

for T in $TASKS; do
  echo "== task $T"
  curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/tasks/$T/brief" \
    | jq -c '{level:.level_label, has:(.has|length), standing:[.can[].standing]}'
  curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/tasks/$T/impact" \
    | jq -c '{level:.level_label, steps:[.steps[]|{title,ready,precondition}]}'
  echo
done
