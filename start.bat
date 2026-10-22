@echo off
rem ARTEX launcher (Windows). ASCII-only + CRLF on purpose: cmd parses .bat in the
rem OEM codepage, so UTF-8 Chinese here breaks parsing. Chinese docs: see tutorial.
rem
rem Binary is located automatically: .\artex.exe -> .\dist\artex.exe -> .\artex-win-amd64.exe
setlocal enabledelayedexpansion
cd /d "%~dp0"

set "BIN="
if exist "artex.exe" set "BIN=artex.exe"
if not defined BIN if exist "dist\artex.exe" set "BIN=dist\artex.exe"
if not defined BIN if exist "artex-win-amd64.exe" set "BIN=artex-win-amd64.exe"
if not defined BIN (
	echo [artex] ARTEX executable not found. Looked for:
	echo     artex.exe / dist\artex.exe / artex-win-amd64.exe
	echo.
	echo [artex] unzip the release package, or run: go build -o artex.exe ./cmd/artex
	pause
	exit /b 1
)
echo [artex] using %BIN%

rem ---- first-run precheck: a database must be configured ----
if not exist "config.json" if "%ARTEX_PG_DSN%"=="" (
	echo.
	echo [artex] NO DATABASE CONFIG YET.
	echo.
	echo   Create config.json in this folder with this content:
	echo.
	echo     {
	echo       "database": {
	echo         "dsn": "postgres://USER:PASSWORD@127.0.0.1:5432/DBNAME?sslmode=disable"
	echo       }
	echo     }
	echo.
	echo   Need a PostgreSQL first?  Options:
	echo     - Docker:  docker compose up -d   (starts postgres + artex for you)
	echo     - Native:  install PostgreSQL 16, then: createuser -P artex / createdb -O artex artex
	echo.
	echo   Full steps: see the tutorial (human) or INSTALL-AI.md (for AI agents).
	echo.
	pause
	exit /b 2
)

set "PORT=8787"
if "%~1"=="" ( set "ARGS=-addr 127.0.0.1:8787" ) else ( set "ARGS=%*" )

netstat -ano | findstr ":%PORT% " | findstr "LISTENING" >nul 2>&1
if not errorlevel 1 (
	echo [artex] already running on 127.0.0.1:%PORT%
	echo [artex] opening the browser instead of starting a second instance.
	start "" "http://127.0.0.1:%PORT%"
	exit /b 0
)

set "RESTART_CODE=75"
set "MAX_TRIES=3"
set /a tries=0
set /a delay=2

:loop
set /a tries+=1
"%BIN%" %ARGS%
set "code=!ERRORLEVEL!"

if "!code!"=="0" goto :eof
if "!code!"=="!RESTART_CODE!" (
	echo [artex] restart requested, relaunching...
	goto :loop
)

if !tries! geq %MAX_TRIES% (
	echo.
	echo [artex] failed !tries! times, giving up. Last exit code: !code!
	echo.
	echo   Most common causes:
	echo     1) database not reachable   -^> check config.json dsn, is PostgreSQL running?
	echo     2) port %PORT% in use        -^> netstat -ano ^| findstr :%PORT%
	echo     3) wrong working folder      -^> artex.exe, config.json and skills\, kb\ must be together
	echo.
	echo   Read the error lines above; the tutorial has a troubleshooting table.
	echo.
	pause
	exit /b 1
)

echo [artex] exited with code !code!, retry in !delay!s ^(attempt !tries!/%MAX_TRIES%^)
ping -n !delay! 127.0.0.1 >nul
set /a delay=delay*2
goto :loop
