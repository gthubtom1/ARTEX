@echo off
chcp 65001 >nul 2>&1
setlocal enabledelayedexpansion
cd /d "%~dp0"

set "REPO=gthubtom1/ARTEX"
set "TAG=v0.5.4"
set "EXE=artex.exe"

echo ============================================
echo  ARTEX 一键部署（Windows）
echo ============================================

if not exist "skills" (
  echo [x] 当前目录缺少 skills\，请先完整 clone 仓库：https://github.com/%REPO%
  pause & exit /b 1
)
if not exist "kb" (
  echo [x] 当前目录缺少 kb\，请先完整 clone 仓库。
  pause & exit /b 1
)

if not exist "%EXE%" (
  echo [*] 下载 artex.exe ...
  curl.exe -L --fail -o "%EXE%" "https://github.com/%REPO%/releases/download/%TAG%/artex.exe"
  if not exist "%EXE%" (
    powershell -NoProfile -Command "try{Invoke-WebRequest -UseBasicParsing 'https://github.com/%REPO%/releases/download/%TAG%/artex.exe' -OutFile '%EXE%'}catch{exit 1}"
  )
  if not exist "%EXE%" (
    echo [x] 下载失败。请检查网络/代理，或手动从 Releases 页下载 artex.exe 放到本目录。
    pause & exit /b 1
  )
  echo [+] 已下载 %EXE%
)

if exist "config.json" goto :start

where docker >nul 2>&1
if errorlevel 1 goto :manual_db

echo [*] 未发现 config.json，尝试用 Docker 启动 PostgreSQL ...
docker info >nul 2>&1
if errorlevel 1 (
  echo [x] Docker 已安装但未运行，请先启动 Docker Desktop。
  pause & exit /b 1
)
docker start artex-pg >nul 2>&1
if errorlevel 1 (
  docker run -d --name artex-pg -e POSTGRES_PASSWORD=artexpass -e POSTGRES_DB=artex -p 5432:5432 postgres:16
  if errorlevel 1 (
    echo [x] 启动 PostgreSQL 容器失败，请查看 Docker 输出。
    pause & exit /b 1
  )
)
echo [*] 等待数据库就绪 ...
timeout /t 10 /nobreak >nul
powershell -NoProfile -Command "$j=@{database=@{host='127.0.0.1';port=5432;user='postgres';password='artexpass';dbname='artex';sslmode='disable'}}; $j|ConvertTo-Json -Depth 5 | Set-Content -Encoding UTF8 'config.json'"
echo [+] 已生成 config.json（Docker PostgreSQL：artex/artexpass@127.0.0.1:5432）
goto :start

:manual_db
if not exist "config.json" copy "config.example.json" "config.json" >nul
echo [!] 没检测到 Docker。已生成 config.json，请填写你的 PostgreSQL 连接信息后保存。
echo     填写格式参考 config.example.json；保存后重新双击本脚本。
notepad "config.json"
pause & exit /b 1

:start
echo [*] 启动 ARTEX（关闭本窗口即停止） ...
echo [*] 浏览器打开：http://127.0.0.1:8787
echo [*] 首次登录：用户名 ARTEX，密码在首次启动时设置
echo.
call start.bat
