@echo off
cd /d "%~dp0"
rem 自动按顺序找可执行文件：同目录 -> dist\ -> dist\artex-windows-amd64\ -> artex-win-amd64.exe
rem 注意：本文件是 GBK 编码 + CRLF，改动请用同样编码保存，否则 cmd 会显示乱码。
set "HASBIN="
if exist "artex.exe" set "HASBIN=1"
if not defined HASBIN if exist "dist\artex.exe" set "HASBIN=1"
if not defined HASBIN if exist "dist\artex-windows-amd64\artex.exe" set "HASBIN=1"
if not defined HASBIN if exist "artex-win-amd64.exe" set "HASBIN=1"
if not defined HASBIN (
  echo [x] 找不到 ARTEX 的可执行文件。已找过：.\artex.exe、.\dist\artex.exe、.\dist\artex-windows-amd64\artex.exe
  echo     三种办法选一种：
  echo       1) 双击同目录下的 部署.bat（自动下载最新 Release 并起数据库）
  echo       2) 下载 Release 压缩包，解压后把 artex.exe 放到本目录
  echo       3) 从源码构建（需要 Node.js + Go）：
  echo          cd web ^&^& npm ci ^&^& set NEXT_EXPORT=1 ^&^& npx next build ^&^& cd ..
  echo          go build -tags embedui -o artex.exe ./cmd/artex
  echo          注意必须带 -tags embedui，否则二进制里没有前端界面。
  pause & exit /b 1
)
if not exist "config.json" (
  echo [x] 缺少 config.json（数据库连接）。请先双击 部署.bat，它会起 PostgreSQL 并生成配置。
  pause & exit /b 1
)
call start.bat
