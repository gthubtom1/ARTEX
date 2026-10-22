@echo off
chcp 65001 >nul 2>&1
cd /d "%~dp0"
if not exist "artex.exe" (
  echo [x] 缺少 artex.exe，请先双击 部署.bat
  pause & exit /b 1
)
if not exist "config.json" (
  echo [x] 缺少 config.json，请先双击 部署.bat
  pause & exit /b 1
)
call start.bat
