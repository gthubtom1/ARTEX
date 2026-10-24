#!/usr/bin/env bash
set -u
TOKEN=$(echo kali | sudo -S cat /opt/artex/jwt.key 2>/dev/null | python3 -c '
import sys,hmac,hashlib,base64,json,time
key=sys.stdin.read().strip().encode(); b64=lambda b: base64.urlsafe_b64encode(b).rstrip(b"=")
now=int(time.time()); h=b64(json.dumps({"alg":"HS256","typ":"JWT"},separators=(",",":")).encode()); p=b64(json.dumps({"sub":"ARTEX","exp":now+7200,"iat":now},separators=(",",":")).encode())
sys.stdout.write((h+b"."+p+b"."+b64(hmac.new(key,h+b"."+p,hashlib.sha256).digest())).decode())
')
B=http://127.0.0.1:8787
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{
  "name":"链式验证 · testfire 打穿（0.11.43）",
  "description":"产品验收：验证“给网址→找洞→拿到后台或数据→一页纸”这条链在当前版本是否跑通",
  "goal":"对 https://demo.testfire.net 做一次最小打穿：优先用 /doLogin 的 uid 参数（已知存在登录注入）绕过认证拿到后台会话，再用该会话读取 1 个账户的交易记录（最多 3 条）作为数据证据。只读，不改任何数据；不做上传/落文件/批量导出；每类样本不超过 3 条。拿到后台会话与 3 条数据样本后立刻停止并把结论写成 finding。",
  "timeout_seconds":900
}' "$B/api/tasks" | jq -c '{id,name}' | sed 's/^/new: /'
