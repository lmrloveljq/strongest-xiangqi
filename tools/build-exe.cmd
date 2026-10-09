@echo off
rem ============================================================================
rem  Xiangqi v1.3.1 - build the final single-file exe (real body)
rem
rem  DO NOT RUN THIS UNTIL THE VERIFICATION PASSED.
rem
rem  PURE ASCII ON PURPOSE - see tools\run-verify.cmd for the full explanation.
rem  Chinese user-facing text lives in tools\msg-build.txt (UTF-8) and is
rem  printed with `type`.
rem ============================================================================

setlocal
chcp 65001 >nul
cd /d "%~dp0.."
title Xiangqi v1.3.1 - build exe

type "%~dp0msg-build.txt"

where go >nul 2>nul
if errorlevel 1 (
	echo.
	echo   [ERROR] "go" was not found in PATH.
	echo   Install Go 1.22+:  winget install GoLang.Go
	echo.
	pause
	exit /b 1
)

if exist "xiangqi.exe" (
	echo [NOTE] xiangqi.exe already exists and will be overwritten.
	echo        Close the running application first if it is open.
	echo.
)

echo [1/2] Building with:  go build -ldflags "-H windowsgui -s -w" -o xiangqi.exe .
echo.

go build -ldflags "-H windowsgui -s -w" -o xiangqi.exe .
set "RC=%ERRORLEVEL%"

echo.
if not "%RC%"=="0" (
	echo   [FAILED] build exit code = %RC%
	echo   If the file is locked, close the running application and retry.
	echo.
	pause
	exit /b %RC%
)

echo [2/2] Build finished.
if exist "xiangqi.exe" (
	for %%f in ("xiangqi.exe") do echo       output: %%~ff   size: %%~zf bytes
)
echo.
echo   Double-click xiangqi.exe to run it. No Go install and no runtime needed.
echo.
pause
endlocal