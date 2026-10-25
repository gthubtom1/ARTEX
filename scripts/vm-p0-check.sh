#!/usr/bin/env bash
# P0 真机判据（P0-01/02/06/08）：在 VM 里跑，验证"等级靠证据、能力不跨洞、短语跟能力、一页纸无人话机器码"。
# 用法：bash /home/kali/vm-p0-check.sh [任务id...]     默认 19 17 18
set -u
IDS="${*:-19 17 18}"
PY=/usr/bin/python3
T=$(echo kali | sudo -S cat /opt/artex/jwt.key 2>/dev/null | $PY -c '
import sys,hmac,hashlib,base64,json,time
key=sys.stdin.read().strip().encode()
b64=lambda b: base64.urlsafe_b64encode(b).rstrip(b"=")
hdr=b64(json.dumps({"alg":"HS256","typ":"JWT"},separators=(",",":")).encode())
now=int(time.time())
pay=b64(json.dumps({"sub":"ARTEX","exp":now+3600,"iat":now},separators=(",",":")).encode())
sig=b64(hmac.new(key,hdr+b"."+pay,hashlib.sha256).digest())
sys.stdout.write((hdr+b"."+pay+b"."+sig).decode())
')
[ -z "$T" ] && { echo "[!] token 取不到"; exit 1; }
H="-H Authorization:Bearer${T}"
for id in $IDS; do
  echo "=== task $id"
  echo -n "  level: "; curl -s -H "Authorization: Bearer $T" "http://127.0.0.1:8787/api/tasks/$id/impact" | $PY -c 'import sys,json;d=json.load(sys.stdin);print(d.get("level"),"|",d.get("basis","")[:60])'
  echo -n "  brief: "; curl -s -H "Authorization: Bearer $T" "http://127.0.0.1:8787/api/tasks/$id/brief" | $PY -c '
import sys,json,collections
d=json.load(sys.stdin)
c=collections.Counter(x.get("standing","") for x in (d.get("can") or []))
print("level",d.get("level"),"has",d.get("has"),"standing",dict(c))'
  echo -n "  phrases: "; curl -s -H "Authorization: Bearer $T" "http://127.0.0.1:8787/api/tasks/$id/phrases" | $PY -c '
import sys,json
d=json.load(sys.stdin)
print("has",d.get("has"),"keys",[p["key"] for p in d["phrases"]])'
  echo -n "  onepager: "; curl -s -H "Authorization: Bearer $T" "http://127.0.0.1:8787/api/tasks/$id/onepager" | $PY -c '
import sys,json,re
d=json.load(sys.stdin)
t=d.get("text","")
bad=re.findall(r"[a-z]+_(?:blocked|error|limited|required)",t)
print("gaps",d.get("gaps"),"| 机器码",bad or "无","| data_objects",len(d.get("data_objects") or []))'
done
