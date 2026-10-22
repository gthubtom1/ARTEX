#!/usr/bin/env bash
# P6/P7 验收：前端 9 个页面可构建 + 后端面板/决策/复查/MCP 接口自检。
#   - 有 web/node_modules 时跑 tsc + next build（static export）
#   - 不依赖运行中的服务；需要 PG 的部分由 accept_p0.sh 覆盖
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== 1/3 Go 构建 =="
go build ./...

echo "== 2/3 前端类型检查 + 构建（需要 web/node_modules）=="
if [ -d web/node_modules ]; then
  ( cd web && ./node_modules/.bin/tsc --noEmit -p tsconfig.json )
  ( cd web && NODE_ENV=production NEXT_EXPORT=1 ./node_modules/.bin/next build >/tmp/artex-next-build.log 2>&1 ) \
    && echo "next build OK（日志 /tmp/artex-next-build.log）"
  for page in function/capabilities function/owned function/knowledge function/report function/data \
              system/audit system/environment system/network system/replay system/verifier; do
    grep -q "/$page" /tmp/artex-next-build.log || { echo "缺少页面 /$page"; exit 1; }
  done
  echo "10 个新增页面 + 既有任务页都在构建产物里"
else
  echo "跳过：web/node_modules 不存在（先 npm ci --include=dev --ignore-scripts）"
fi

echo "== 3/3 新包单测 =="
go test ./internal/decision/ ./internal/recheck/ ./internal/inject/ ./internal/kb/ ./internal/adviser/ ./internal/verifier/ ./internal/capability/ ./internal/verify/ ./internal/ledger/          ./internal/dataloc/ ./internal/harvest/ ./internal/reviewer/ ./internal/cases/ ./internal/replay/ ./internal/execx/ ./report/
go test ./server/ -run "TestMCP|TestFindingsForVerifier|TestVerifierGate|TestBasicAuthBlockedSaveDoesNotBlockRequests"

echo
echo "验收通过：P3 决策队列 / P5 复查执行器 / P7 注入层 / KB MCP / 多模型角色 / 回放看板 / 8+1 页面。"
