#!/usr/bin/env bash
# 产出 Dockerfile 需要的 Linux 单二进制：dist/<arch>/artex（前端已内嵌）。
# 需要 Go（编译）与 Node（前端静态导出）。
# 用法：bash scripts/package_linux.sh [amd64|arm64]
set -euo pipefail
cd "$(dirname "$0")/.."
ARCH="${1:-amd64}"

command -v go   >/dev/null || { echo "[!] 缺 Go：apt install -y golang-go / dnf install -y golang"; exit 1; }
command -v node >/dev/null || { echo "[!] 缺 Node(>=20)：apt install -y nodejs npm / dnf install -y nodejs npm"; exit 1; }

echo "[*] 前端静态导出"
if [ ! -d web/node_modules ]; then
  ( cd web && NODE_ENV=development npm ci --include=dev --ignore-scripts )
fi
( cd web && NODE_ENV=production NEXT_EXPORT=1 ./node_modules/.bin/next build >/dev/null )

echo "[*] 拷进 server/webui/dist"
rm -rf server/webui/dist && mkdir -p server/webui/dist
cp -a web/out/. server/webui/dist/

echo "[*] 交叉编译 $ARCH（CGO_ENABLED=0，纯 Go）"
mkdir -p "dist/$ARCH"
CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -tags embedui -o "dist/$ARCH/artex" ./cmd/artex
ls -lh "dist/$ARCH/artex"
echo "[+] dist/$ARCH/artex 就绪 —— 现在可以 docker compose up -d --build"
