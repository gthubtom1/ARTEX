#!/usr/bin/env bash
# 判据脚本共用的取 token 逻辑（P1-08：换机器也能跑，不再依赖固定 sudo 口令）。
#
#   ARTEX_TOKEN   直接给一个现成的 JWT（最高优先）
#   ARTEX_JWT_KEY 密钥文件路径（默认 /opt/artex/jwt.key；读不到才退回 `echo kali | sudo -S`）
#
# 用法：T=$(. "$(dirname "$0")/_token.sh"; artex_token)
artex_token() {
  if [ -n "${ARTEX_TOKEN:-}" ]; then
    printf '%s' "$ARTEX_TOKEN"
    return
  fi
  local key_file="${ARTEX_JWT_KEY:-/opt/artex/jwt.key}"
  local key
  key=$(cat "$key_file" 2>/dev/null)
  if [ -z "$key" ]; then
    key=$(echo kali | sudo -S cat "$key_file" 2>/dev/null)
  fi
  printf '%s' "$key" | /usr/bin/python3 -c '
import sys,hmac,hashlib,base64,json,time
key=sys.stdin.read().strip().encode()
if not key:
    sys.exit(1)
b64=lambda b: base64.urlsafe_b64encode(b).rstrip(b"=")
hdr=b64(json.dumps({"alg":"HS256","typ":"JWT"},separators=(",",":")).encode())
now=int(time.time())
pay=b64(json.dumps({"sub":"ARTEX","exp":now+3600,"iat":now},separators=(",",":")).encode())
sig=b64(hmac.new(key,hdr+b"."+pay,hashlib.sha256).digest())
sys.stdout.write((hdr+b"."+pay+b"."+sig).decode())
'
}
