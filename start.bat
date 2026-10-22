@echo off
rem ARTEX launcher (Windows). ASCII-only on purpose:
rem cmd.exe parses .bat in the OEM codepage; UTF-8 Chinese comments break parsing.
rem
rem usage:
rem   start.bat                run in foreground (Ctrl-C to stop)
rem   start.bat -addr :9000    extra args are passed through
rem
rem exit codes of artex:
rem   0   user stopped      -> quit
rem   75  restart requested -> relaunch immediately (update/rollback)
rem   else crash             -> backoff 1,2,4..60s then relaunch
rem
rem NOTE 1: sleep uses `ping -n` instead of `timeout /t`.
rem   Under Git Bash, PATH may resolve `timeout` to GNU coreutils, which
rem   rejects /t and returns instantly -> tight crash loop.
rem NOTE 2: before launching we check whether something already listens on the
rem   port. Double-clicking twice is a normal user habit; without this check the
rem   second instance fails to bind and respawns forever.

setlocal enabledelayedexpansion
cd /d "%~dp0"

set "BIN=artex.exe"
if not exist "%BIN%" (
	echo [artex] %BIN% not found in %CD%
	echo [artex] put artex.exe next to start.bat
	exit /b 1
)

set "PORT=8787"
if not "%~1"=="" set "ARGS=%*"
if "%~1"=="" set "ARGS=-addr 127.0.0.1:%PORT%"
echo %ARGS% | findstr /c:"-addr" >nul && (
	for /f "tokens=2 delims=:" %%p in ("%ARGS%") do set "PORT=%%p"
)

rem ---- already running? ----
netstat -ano | findstr ":%PORT% " | findstr "LISTENING" >nul 2>&1
if not errorlevel 1 (
	echo [artex] already running on 127.0.0.1:%PORT% ^(another instance is serving^)
	echo [artex] open http://127.0.0.1:%PORT% in your browser, or stop the other window first.
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

rem bind failure is NOT worth retrying: usually another instance holds the port
netstat -ano | findstr ":%PORT% " | findstr "LISTENING" >nul 2>&1
if not errorlevel 1 (
	echo [artex] port %PORT% is now taken by another process ^(code !code!^).
	echo [artex] if that is a second ARTEX window, use it; otherwise free the port and rerun.
	exit /b 1
)

echo [artex] exited with code !code!, retry in !delay!s
ping -n !delay! 127.0.0.1 >nul
set /a delay=delay*2
if !delay! gtr %MAX_DELAY% set /a delay=%MAX_DELAY%
goto :loop
