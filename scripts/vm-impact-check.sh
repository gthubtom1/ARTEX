#!/usr/bin/env bash
# 在 Kali VM 里跑：检查"下一步提案"是否按前置能力分派（E0 不许提 webshell）。
# 用法：bash /home/kali/vm-impact.sh [task_id ...]
set -u
BASE=http://127.0.0.1:8787
TASKS="${*:-14 15}"

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

for T in $TASKS; do
  echo "== task $T"
  curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/tasks/$T/brief" \
    | jq -c '{level:.level_label, has:(.has|length), standing:[.can[].standing]}'
  curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/tasks/$T/impact" \
    | jq -c '{level:.level_label, steps:[.steps[]|{title,ready,precondition}]}'
  echo
done
