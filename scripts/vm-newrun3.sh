#!/usr/bin/env bash
# 0.11.53 回归：确认 kb_playbook 真被 worker 调用（P0-02 验收 #2）。
set -u
TOKEN=$(. \"$(dirname \"$0\")/_token.sh\"; artex_token)
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{
  "name":"回归 · kb_playbook 接线（0.11.53）",
  "description":"验证 worker 拿到入口后会先查 kb_playbook 打法（P0-02）",
  "goal":"对 https://demo.testfire.net 做最小验证：用 /doLogin 的 uid 参数试试能不能绕过认证；如果能，只用只读方式确认后台可见（不做任何写操作），拿到后台会话即停。最多 3 个请求尝试。",
  "timeout_seconds":600
}' http://127.0.0.1:8787/api/tasks | python3 -c 'import sys,json;d=json.load(sys.stdin);print("new-task:",d.get("id"))'
