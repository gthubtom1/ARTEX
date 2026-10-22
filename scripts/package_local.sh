#!/usr/bin/env bash
# 本机打包（无 rsync 也能用）：导出前端 → 拷进 server/webui/dist → 编译带内嵌 UI 的单文件。
# 用法：bash scripts/package_local.sh [版本号]
set -euo pipefail
cd "$(dirname "$0")/.."
VER="${1:-dev}"

if [ ! -d web/node_modules ]; then
  echo "[!] web/node_modules 不存在：先 cd web && NODE_ENV=development npm ci --include=dev --ignore-scripts"
  exit 1
fi

echo "[*] 导出静态前端"
( cd web && NODE_ENV=production NEXT_EXPORT=1 ./node_modules/.bin/next build >/dev/null )

echo "[*] 拷贝到 server/webui/dist"
rm -rf server/webui/dist
mkdir -p server/webui/dist
cp -a web/out/. server/webui/dist/

echo "[*] 编译（-tags embedui）"
mkdir -p dist
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) OUT="dist/artex.exe" ;;
  *) OUT="dist/artex" ;;
esac
go build -tags embedui -ldflags "-X main.buildVersion=${VER}" -o "$OUT" ./cmd/artex
ls -lh "$OUT"
echo "[+] 成品：$OUT（前端已内嵌；运行时仍需 PostgreSQL + LLM 配置）"
