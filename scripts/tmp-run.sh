#!/usr/bin/env bash
set -u
TOKEN=$(echo kali | sudo -S cat /opt/artex/jwt.key 2>/dev/null | python3 -c '
import sys,hmac,hashlib,base64,json,time
key=sys.stdin.read().strip().encode()
b64=lambda b: base64.urlsafe_b64encode(b).rstrip(b"=")
now=int(time.time())
h=b64(json.dumps({"alg":"HS256","typ":"JWT"},separators=(",",":")).encode())
p=b64(json.dumps({"sub":"ARTEX","exp":now+5400,"iat":now},separators=(",",":")).encode())
sys.stdout.write((h+b"."+p+b"."+b64(hmac.new(key,h+b"."+p,hashlib.sha256).digest())).decode())
')
B=http://127.0.0.1:8787
echo "== 建任务（继承 #14）"
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{
  "name":"真机验收 · 继承 #14 续打",
  "description":"验收用：继承 #14 已有的后台会话与数据样本，验证“有战果→下一步→留证”的完整体验",
  "goal":"在 #14 已拿到后台管理员会话与业务数据样本的基础上，确认还能读到哪些表与关键字段：每类最多取 3 条样本作为证据，写清表名与字段名。只读，不改任何数据；不做上传/落文件；不做批量导出。做完把结论写成一条 finding 并登记。",
  "source_task_ids":["14"],
  "timeout_seconds":1200
}' "$B/api/tasks" | tee /tmp/newtask.json | head -c 500; echo
