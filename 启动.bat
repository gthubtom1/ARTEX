@echo off
cd /d "%~dp0"
rem 自动查找二进制：同目录 -> dist\ -> artex-win-amd64.exe（不需要手动改名）
set "HASBIN="
if exist "artex.exe" set "HASBIN=1"
if not defined HASBIN if exist "distrtex.exe" set "HASBIN=1"
if not defined HASBIN if exist "artex-win-amd64.exe" set "HASBIN=1"
if not defined HASBIN (
  echo [x] 找不到 ARTEX 可执行文件（已查找：.rtex.exe、.\distrtex.exe、.rtex-win-amd64.exe）
  echo     解决办法二选一：
  echo       1) 下载 Release 压缩包，解压后直接双击 启动.bat
  echo       2) 已装 Go 的话，在仓库根目录执行： go build -o artex.exe ./cmd/artex
  pause & exit /b 1
)
if not exist "config.json" (
  echo [x] 缺少 config.json，请先双击 部署.bat
  pause & exit /b 1
)
call start.bat
