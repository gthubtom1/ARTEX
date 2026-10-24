#!/usr/bin/env bash
set -u
TOKEN=$(echo kali | sudo -S cat /opt/artex/jwt.key 2>/dev/null | python3 -c '
import sys,hmac,hashlib,base64,json,time
key=sys.stdin.read().strip().encode()
b64=lambda b: base64.urlsafe_b64encode(b).rstrip(b"=")
now=int(time.time())
h=b64(json.dumps({"alg":"HS256","typ":"JWT"},separators=(",",":")).encode())
p=b64(json.dumps({"sub":"ARTEX","exp":now+900,"iat":now},separators=(",",":")).encode())
sys.stdout.write((h+b"."+p+b"."+b64(hmac.new(key,h+b"."+p,hashlib.sha256).digest())).decode())
')
T=17
echo "-- brief.can ids"; curl -s -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8787/api/tasks/$T/brief" | jq -c '[.can[].finding_id]'
echo "-- findings ids"; curl -s -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8787/api/exploration/findings?task=$T" | jq -c '[.[]|{id, finding_id}]'
