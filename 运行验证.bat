@echo off
chcp 65001 >nul
rem ============================================================================
rem  Xiangqi v1.3.1  --  one-click source verification launcher
rem
rem  This launcher is intentionally PURE ASCII.
rem  cmd.exe re-opens a batch file by NAME for every buffered read; when the
rem  file name contains non-ASCII characters that re-open loses the byte
rem  offset, and Chinese lines get truncated / half-lines would be run as
rem  commands (reproduced on this machine). So the Chinese UI text lives in
rem  tools\run-verify.cmd, whose file name is ASCII.
rem ============================================================================

setlocal
chcp 65001 >nul
call "%~dp0tools\run-verify.cmd" %*
set "RC=%ERRORLEVEL%"
endlocal & exit /b %RC%