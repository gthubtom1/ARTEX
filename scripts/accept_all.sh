#!/usr/bin/env bash
# 全量验收（无 PG / 无原生 sed 的机器上也能跑）：
#   - build 全仓
#   - 除 server（需要 PG）与 intercept（原生 sed 快照测试，Windows 无 sed）外的所有包单测
#   - 前端 tsc + next build（有 node_modules 时）
# 有 PG 时另跑 scripts/accept_p0.sh；Windows 无 sed 属环境限制。
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== 0/3 链路审计（导航↔页面↔路由↔p2api）=="
python scripts/audit_links.py

echo "== 1/3 go build ./... =="
go build ./...

echo "== 2/3 go test（排除 PG/sed 依赖包）=="
PKGS=$(go list ./... | grep -v '/server$' | grep -v '/intercept$')
go test $PKGS -p 1

echo "== 3/3 前端 =="
if [ -d web/node_modules ]; then
  ( cd web && ./node_modules/.bin/tsc --noEmit -p tsconfig.json )
  ( cd web && NODE_ENV=production NEXT_EXPORT=1 ./node_modules/.bin/next build >/tmp/artex-next-build.log 2>&1 )
  for page in function/capabilities function/owned function/knowledge function/report function/data \
              system/audit system/environment system/network system/replay system/verifier; do
    grep -q "/$page" /tmp/artex-next-build.log || { echo "缺少页面 /$page"; exit 1; }
  done
  echo "前端构建 OK（10 个新增页面 + 既有页面）"
else
  echo "跳过前端：web/node_modules 不存在"
fi

echo
echo "全量验收通过（server/intercept 需 PG/原生 sed，见脚本头注释）。"
