@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

set "REPO=gthubtom1/ARTEX"
set "EXE=artex.exe"
rem 固定版本：set ARTEX_TAG=v0.7.4-p9 ；不设就取「最新 Release」的稳定资产名
if defined ARTEX_TAG set "TAG=%ARTEX_TAG%"

echo ============================================
echo  ARTEX 一键部署（Windows）
echo ============================================

if not exist "skills\" (
  echo [x] 当前目录缺少 skills\，请先 clone 仓库：https://github.com/%REPO%
  pause & exit /b 1
)
if not exist "kb\" (
  echo [x] 当前目录缺少 kb\，请先 clone 仓库。
  pause & exit /b 1
)

if exist "%EXE%" goto :have_bin

rem ① 本地已经构建/解压过：直接复用，不联网
if exist "dist\artex-windows-amd64\artex.exe" (
  copy /y "dist\artex-windows-amd64\artex.exe" "%EXE%" >nul
  if exist "%EXE%" (echo [+] 已从 dist\artex-windows-amd64\ 复制 %EXE% & goto :have_bin)
)

rem ② 有 ARTEX_TAG：按版本名下载
if defined TAG (
  set "ZIP=artex-%TAG%-windows-amd64.zip"
  call :fetch "https://github.com/%REPO%/releases/download/%TAG%/%ZIP%"
  if exist "%ZIP%" goto :extract
  goto :no_bin
)

rem ③ 没指定版本：先试稳定资产名（每个 Release 都发这个名），失败再解析最新 tag
echo [*] 下载最新 Release 的 Windows 包 ...
set "ZIP=artex-windows-amd64.zip"
call :fetch "https://github.com/%REPO%/releases/latest/download/artex-windows-amd64.zip"
if exist "%ZIP%" goto :extract

echo [*] 稳定名字取不到，改按最新 tag 取 ...
set "LOC="
for /f "delims=" %%a in ('curl.exe -sI "https://github.com/%REPO%/releases/latest" ^| findstr /i "location:"') do set "LOC=%%a"
if not defined LOC goto :no_bin
set "TAG=!LOC:*releases/tag/=!"
set "ZIP=artex-!TAG!-windows-amd64.zip"
call :fetch "https://github.com/%REPO%/releases/download/!TAG!/!ZIP!"
if not exist "!ZIP!" goto :no_bin

:extract
echo [*] 解压 %ZIP% ...
if exist "__rel" rmdir /s /q "__rel"
powershell -NoProfile -Command "Expand-Archive -Force '%ZIP%' '__rel'; $e=Get-ChildItem -Recurse '__rel' -Filter artex.exe | Select-Object -First 1; if($e){Copy-Item $e.FullName '%EXE%' -Force}"
if not exist "%EXE%" goto :no_bin
rmdir /s /q "__rel" 2>nul
del /q "%ZIP%" 2>nul
echo [+] 已获得 %EXE%

:have_bin
if exist "config.json" goto :start

where docker >nul 2>&1
if errorlevel 1 goto :manual_db

echo [*] 未发现 config.json，用 Docker 起 PostgreSQL ...
docker info >nul 2>&1
if errorlevel 1 (
  echo [x] Docker 已安装但未运行，请打开 Docker Desktop。
  pause & exit /b 1
)
docker start artex-pg >nul 2>&1
if errorlevel 1 (
  docker run -d --name artex-pg -e POSTGRES_PASSWORD=artexpass -e POSTGRES_DB=artex -p 5432:5432 postgres:16
  if errorlevel 1 (
    echo [x] 启动 PostgreSQL 失败，请检查 Docker。
    pause & exit /b 1
  )
)
echo [*] 等待数据库起来 ...
timeout /t 10 /nobreak >nul
powershell -NoProfile -Command "$j=@{database=@{host='127.0.0.1';port=5432;user='postgres';password='artexpass';dbname='artex';sslmode='disable'}}; $j|ConvertTo-Json -Depth 5 | Set-Content -Encoding UTF8 'config.json'"
echo [+] 已生成 config.json（Docker PostgreSQL：artex/artexpass@127.0.0.1:5432）
goto :start

:manual_db
if not exist "config.json" copy "config.example.json" "config.json" >nul
echo [!] 没检测到 Docker，已生成 config.json 模板，请填写你的 PostgreSQL 连接信息后保存。
echo     写法参考 config.example.json，保存后再双击本脚本。
notepad "config.json"
pause & exit /b 1

:no_bin
rem 下载不通时（国内网络常见：github 可达但 Release CDN 被墙），有 Go+Node 就本地构建
where go >nul 2>&1
if errorlevel 1 goto :no_bin_help
where node >nul 2>&1
if errorlevel 1 goto :no_bin_help
set "BASH="
where bash >nul 2>&1 && set "BASH=bash"
if not defined BASH if exist "%ProgramFiles%\Git\bin\bash.exe" set "BASH=%ProgramFiles%\Git\bin\bash.exe"
if not defined BASH if exist "%ProgramFiles(x86)%\Git\bin\bash.exe" set "BASH=%ProgramFiles(x86)%\Git\bin\bash.exe"
if not defined BASH if exist "%LOCALAPPDATA%\Programs\Git\bin\bash.exe" set "BASH=%LOCALAPPDATA%\Programs\Git\bin\bash.exe"
if not defined BASH goto :no_bin_help
if not exist "scripts\package_local.sh" goto :no_bin_help
echo.
echo [*] 下载不通，但本机有 Go+Node：改为本地构建（首次约 5-10 分钟）...
"%BASH%" scripts\package_local.sh v0.7.4-p9
if exist "dist\artex.exe" copy /y "dist\artex.exe" "%EXE%" >nul
if exist "%EXE%" (echo [+] 本地构建完成 & goto :have_bin)

:no_bin_help
echo [x] 没拿到 artex.exe。
echo     1) 检查网络能否访问 github.com 与 release-assets.githubusercontent.com（或设代理）
echo     2) 手动到 Releases 页下载 artex-windows-amd64.zip（或 artex-*-windows-amd64.zip），解压后把 artex.exe 放到本目录
echo     3) 从源码构建（需要 Go + Node + Git for Windows），或在 Git Bash 里执行：
echo          bash scripts\package_local.sh v0.7.4-p9
pause & exit /b 1

:start
echo [*] 启动 ARTEX（关闭本窗口即停止）...
echo [*] 浏览器打开：http://127.0.0.1:8787
echo [*] 首次登录：用户 ARTEX，密码首次启动时生成（看窗口输出）
echo.
call start.bat
goto :eof

:fetch
curl.exe -L --fail --connect-timeout 15 --max-time 300 -o "%~2" "%~1" 2>nul
if exist "%~2" goto :eof
powershell -NoProfile -Command "try{Invoke-WebRequest -UseBasicParsing '%~1' -OutFile '%~2'}catch{exit 1}" 2>nul
goto :eof
