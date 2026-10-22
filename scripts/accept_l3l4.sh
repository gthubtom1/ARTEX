#!/usr/bin/env bash
# 本轮（L3/L4 执行面 + 自动裁决 + 真回放 + 数据/凭据）验收：
#   1) 新模块单测  2) 编译  3) 前端新增页面存在  4) 打包产物存在（可选）
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== 1/3 新模块单测 =="
go test ./internal/dataloc/ ./internal/harvest/ ./internal/reviewer/ ./internal/cases/ \
        ./internal/replay/ ./internal/execx/ ./internal/decision/ ./internal/verifier/ \
        ./internal/remote/ ./internal/creds/ ./internal/access/
go test ./server/ -run "TestDataLocateContract|TestFindingsForVerifier|TestVerifierGate|TestMCP"

echo "== 2/3 编译 =="
go build ./...

echo "== 3/3 页面与成品 =="
if [ -d server/webui/dist ]; then
  for p in function/data function/capabilities system/verifier system/replay system/audit system/environment system/network; do
    [ -f "server/webui/dist/$p/index.html" ] || { echo "缺少页面 /$p"; exit 1; }
  done
  echo "内嵌前端页面齐全（server/webui/dist）"
else
  echo "跳过：server/webui/dist 不存在（先跑 scripts/package_local.sh）"
fi

echo
echo "验收通过：数据定位 / 凭据收割 / 自动裁决 / 案例沉淀 / 真回放 / 执行面（提权·横移·清理）已接线。"
