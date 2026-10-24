#!/usr/bin/env bash
set -u
T=${1:-19}
TOKEN=$(echo kali | sudo -S cat /opt/artex/jwt.key 2>/dev/null | python3 -c '
import sys,hmac,hashlib,base64,json,time
key=sys.stdin.read().strip().encode(); b64=lambda b: base64.urlsafe_b64encode(b).rstrip(b"=")
now=int(time.time()); h=b64(json.dumps({"alg":"HS256","typ":"JWT"},separators=(",",":")).encode()); p=b64(json.dumps({"sub":"ARTEX","exp":now+7200,"iat":now},separators=(",",":")).encode())
sys.stdout.write((h+b"."+p+b"."+b64(hmac.new(key,h+b"."+p,hashlib.sha256).digest())).decode())
')
B=http://127.0.0.1:8787
api() { curl -s -H "Authorization: Bearer $TOKEN" "$@"; }
echo "== $(date +%H:%M:%S) task $T"
api "$B/api/tasks/$T" | jq -c '{status, paused, in_flight, findings:(.findings//{}), tokens:{i:.tokens.input_tokens,o:.tokens.output_tokens,c:.tokens.cache_read_tokens}}'
api "$B/api/tasks/$T/brief" | jq -c '{level:.level_label, has:[.has[].kind], can_n:(.can|length), standing:[.can[].standing], doable_n:([.actions[]|select(.doable)]|length), open_runs:([.actions[]|select(.run_state=="requested")]|length), new_since, blockers:[.blockers[].class], stall:.stall.level}'
