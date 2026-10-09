@echo off
rem ============================================================
rem  Xiangqi Strong Engine v1.3.0  -  launcher
rem  ASCII only on purpose: cmd.exe pre-scans .bat files and can
rem  corrupt non-ASCII text before execution.
rem ============================================================
setlocal
cd /d "%~dp0"

if exist "xiangqi.exe" goto RUN

echo [1/3] xiangqi.exe not found, building from source...
where go >nul 2>nul
if errorlevel 1 (
    echo.
    echo ERROR: Go toolchain not found on PATH.
    echo Install Go 1.22+ from https://go.dev/dl/ and reopen the terminal.
    pause
    exit /b 1
)

set CGO_ENABLED=1
where gcc >nul 2>nul
if errorlevel 1 (
    echo.
    echo ERROR: GCC ^(mingw-w64^) not found on PATH.
    echo Fyne needs cgo on Windows. Install it with:
    echo     winget install BrechtSanders.WinLibs.POSIX.UCRT
    echo then reopen the terminal.
    pause
    exit /b 1
)

echo [2/3] go build -ldflags "-H windowsgui -s -w" -o xiangqi.exe .
go build -ldflags "-H windowsgui -s -w" -o xiangqi.exe .
if errorlevel 1 (
    echo.
    echo ERROR: build failed, see the messages above.
    pause
    exit /b 1
)

:RUN
echo [3/3] starting xiangqi.exe ...
start "" "xiangqi.exe"
exit /b 0
