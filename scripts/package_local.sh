#!/usr/bin/env bash
# 本机打包（无 rsync 也能用）：导出前端 → 拷进 server/webui/dist → 编译带内嵌 UI 的单文件。
# 用法：bash scripts/package_local.sh [版本号]
set -euo pipefail
cd "$(dirname "$0")/.."
VER="${1:-dev}"

if [ ! -d web/node_modules ]; then
  # 换机后第一次打包会走到这里：自己装依赖，不用人记命令（需要能访问 npm registry）
  command -v npm >/dev/null 2>&1 || { echo "[!] 缺 npm（Node.js 18+）：https://nodejs.org/ 装完重跑"; exit 1; }
  echo "[*] web/node_modules 不存在，先装依赖（npm ci，几分钟）..."
  ( cd web && NODE_ENV=development npm ci --include=dev --ignore-scripts ) || {
    echo "[!] npm ci 失败：检查网络/代理，或手动执行 cd web && npm ci --include=dev"; exit 1; }
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
# 守门：-X 的符号名写错时 Go **不报错**、静默忽略（历史 bug：这里曾写 main.buildVersion，
# 而 cmd/artex 里是 main.version → 二进制永远报 dev）。先断言符号存在再编译。
grep -q '^var version = ' cmd/artex/main.go || {
  echo "[!] cmd/artex/main.go 里没有 'var version = ...'，-ldflags -X main.version 会失效"
  exit 1
}
go build -tags embedui -trimpath -ldflags "-s -w -X main.version=${VER}" -o "$OUT" ./cmd/artex
ls -lh "$OUT"
echo "[+] 成品：$OUT（前端已内嵌；运行时仍需 PostgreSQL + LLM 配置）"
