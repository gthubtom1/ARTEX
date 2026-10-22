#!/usr/bin/env bash
# P2~P5 验收：编译 + 全部无库单测 + 关键包清单。
# 有 PostgreSQL 时再跑 scripts/accept_p0.sh（五项验收）。
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== 1/3 go build ./... =="
go build ./...

echo "== 2/3 go vet（P2~P5 新包）=="
go vet ./internal/gatechain ./internal/stall ./internal/adviser \
        ./internal/capability ./internal/verify ./internal/creds \
        ./internal/access ./internal/privesc ./internal/lateral ./internal/ledger \
        ./agent ./server

echo "== 3/3 go test（无库可跑的全量）=="
go test ./internal/... ./db/... ./guard/... ./agent/... ./report/...

echo
echo "验收通过：P2 GateChain/顾问、P3 能力卡/三门校验、P4 凭据/立足点/提权/横移、P5 台账 已接线。"
echo "下一步（需要 PG + LLM）：起服务 → 建任务 → 跑 target，检查："
echo "  GET /api/progress?task_id=<id>     看到 stall/diagnosis"
echo "  GET /api/capabilities?task_id=<id> 看到能力卡与 13 组覆盖度"
echo "  GET /api/owned                     看到持有台账"
