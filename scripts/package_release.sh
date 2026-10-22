#!/usr/bin/env bash
# 打「解压即用」的 Release 包：布局就是用户期望的样子，不需要改名/搬文件。
#   artex-<ver>-windows-amd64.zip
#     artex.exe  start.bat  启动.bat  config.example.json  使用教程.md  skills/  kb/
# 用法：bash scripts/package_release.sh v0.7.1-p9
set -euo pipefail
cd "$(dirname "$0")/.."
VER="${1:-dev}"

[ -f dist/artex.exe ] || { echo "[!] 先 bash scripts/package_local.sh $VER 生成 dist/artex.exe"; exit 1; }

STAGE="dist/artex-$VER-windows-amd64"
rm -rf "$STAGE"; mkdir -p "$STAGE"
cp dist/artex.exe "$STAGE/artex.exe"          # ← 关键：改名在这里做掉，用户拿到就是 artex.exe
cp start.bat 启动.bat config.example.json 使用教程.md "$STAGE/" 2>/dev/null || true
cp -r skills kb "$STAGE/"

( cd dist && zip -qr "$(basename "$STAGE").zip" "$(basename "$STAGE")" )
SIZE=$(du -h "dist/$(basename "$STAGE").zip" | cut -f1)
echo "[+] dist/$(basename "$STAGE").zip  ($SIZE)"
echo "    解压后：双击 start.bat 或 启动.bat 即可；只需自己准备 config.json 里的数据库"
