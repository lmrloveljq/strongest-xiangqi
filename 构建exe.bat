@echo off
chcp 65001 >nul
rem ============================================================================
rem  Xiangqi v1.3.1  --  build the final single-file exe
rem
rem  RUN THIS ONLY AFTER THE VERIFICATION PASSED.
rem
rem  Pure ASCII on purpose: a Chinese-named batch file must not contain
rem  Chinese content, because cmd.exe loses the byte offset when it re-opens
rem  the file by name. See tools\run-verify.cmd for the full explanation.
rem ============================================================================

setlocal
chcp 65001 >nul
call "%~dp0tools\build-exe.cmd" %*
set "RC=%ERRORLEVEL%"
endlocal & exit /b %RC%