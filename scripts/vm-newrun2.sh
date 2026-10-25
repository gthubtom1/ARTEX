#!/usr/bin/env bash
# 0.11.50 回归跑：同一条"已知能打通"的链，验证收紧证据判据后没把真结果砍掉。
set -u
TOKEN=$(echo kali | sudo -S cat /opt/artex/jwt.key 2>/dev/null | python3 -c '
import sys,hmac,hashlib,base64,json,time
key=sys.stdin.read().strip().encode(); b64=lambda b: base64.urlsafe_b64encode(b).rstrip(b"=")
now=int(time.time()); h=b64(json.dumps({"alg":"HS256","typ":"JWT"},separators=(",",":")).encode()); p=b64(json.dumps({"sub":"ARTEX","exp":now+7200,"iat":now},separators=(",",":")).encode())
sys.stdout.write((h+b"."+p+b"."+b64(hmac.new(key,h+b"."+p,hashlib.sha256).digest())).decode())
')
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{
  "name":"回归验证 · testfire 打穿（0.11.50）",
  "description":"回归：验证等级判据改成可解析证据后，真结果仍能被认出来（E4 + 数据样本 + 一页纸）",
  "goal":"对 https://demo.testfire.net 做一次最小打穿：用 /doLogin 的 uid 参数（已知登录注入）绕过认证拿到后台会话，再用该会话读取 1 个账户的交易记录（最多 3 条）作为数据证据。只读，不改任何数据；每类样本不超过 3 条。拿到后台会话与 3 条数据样本后立刻停并把结论写成 finding。",
  "timeout_seconds":900
}' http://127.0.0.1:8787/api/tasks | python3 -c 'import sys,json;d=json.load(sys.stdin);print("new-task:",d.get("id"),d.get("status",""))'
