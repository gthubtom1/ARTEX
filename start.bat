@echo off
rem ARTEX launcher (Windows). ASCII-only + CRLF on purpose: cmd parses .bat in the
rem OEM codepage, so UTF-8 Chinese here breaks parsing. Chinese docs: see tutorial.
rem
rem usage:  start.bat              foreground (Ctrl-C stops)
rem         start.bat -addr :9000 extra args pass through
rem
rem The binary is located automatically, no manual rename/move:
rem   .\artex.exe  ->  .\dist\artex.exe  ->  .\artex-win-amd64.exe
setlocal enabledelayedexpansion
cd /d "%~dp0"

set "BIN="
if exist "artex.exe" set "BIN=artex.exe"
if not defined BIN if exist "dist\artex.exe" set "BIN=dist\artex.exe"
if not defined BIN if exist "artex-win-amd64.exe" set "BIN=artex-win-amd64.exe"
if not defined BIN (
	echo [artex] ARTEX executable not found. Looked for:
	echo     artex.exe
	echo     dist\artex.exe
	echo     artex-win-amd64.exe
	echo.
	echo [artex] unzip the release package, or run: go build -o artex.exe ./cmd/artex
	exit /b 1
)
echo [artex] using %BIN%

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
set "MAX_DELAY=60"
set /a delay=1

:loop
"%BIN%" %ARGS%
set "code=!ERRORLEVEL!"
if "!code!"=="0" goto :eof
if "!code!"=="!RESTART_CODE!" (
	echo [artex] restart requested, relaunching...
	set /a delay=1
	goto :loop
)
netstat -ano | findstr ":%PORT% " | findstr "LISTENING" >nul 2>&1
if not errorlevel 1 (
	echo [artex] port %PORT% is taken by another process. Use that window, or free the port.
	exit /b 1
)
echo [artex] exited with code !code!, retry in !delay!s
ping -n !delay! 127.0.0.1 >nul
set /a delay=delay*2
if !delay! gtr %MAX_DELAY% set /a delay=%MAX_DELAY%
goto :loop
