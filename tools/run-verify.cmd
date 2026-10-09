@echo off
rem ============================================================================
rem  Xiangqi v1.3.1 - one-click source verification (real body)
rem
rem  THIS FILE IS PURE ASCII ON PURPOSE.
rem  cmd.exe parses batch files unreliably when they contain multi-byte
rem  characters: lines get truncated and half-lines are executed as commands
rem  (reproduced on this machine with both UTF-8 and GBK content, and with both
rem  ASCII and Chinese file names). So every .bat/.cmd here stays ASCII, and the
rem  Chinese user-facing text lives in tools\msg-*.txt as UTF-8 and is printed
rem  with `type`, which is a raw byte copy and therefore safe. The console code
rem  page is switched to 65001 so that the UTF-8 text renders correctly.
rem ============================================================================

setlocal
chcp 65001 >nul
cd /d "%~dp0.."
title Xiangqi v1.3.1 - source verification

type "%~dp0msg-run.txt"

echo.
echo [1/3] Checking the Go toolchain ...
where go >nul 2>nul
if errorlevel 1 (
	echo.
	echo   [ERROR] "go" was not found in PATH.  See the notes printed above.
	echo.
	pause
	exit /b 1
)
for /f "delims=" %%v in ('go version') do echo       %%v

echo.
echo [2/3] Building and launching (20-60 s on the first build) ...
echo       project dir: %CD%
echo.

go run .
set "RC=%ERRORLEVEL%"

echo.
echo [3/3] go run exit code = %RC%
if not "%RC%"=="0" (
	echo.
	echo   [FAILED] See the troubleshooting notes printed above.
	echo.
	pause
	exit /b %RC%
)

echo   [OK] The application exited normally.
echo        To build the final single-file exe, run the build script.
echo.
pause
endlocal