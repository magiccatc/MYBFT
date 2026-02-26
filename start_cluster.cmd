@echo off
setlocal

if "%~1"=="" goto :usage
if "%~2"=="" goto :usage

set ALGORITHM=%~1
set NODE_COUNT=%~2

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\start_cluster.ps1" %ALGORITHM% %NODE_COUNT%
set EXITCODE=%ERRORLEVEL%
exit /b %EXITCODE%

:usage
echo Usage: start_cluster.cmd ^<pbft^|hotstuff^|fast-hotstuff^|hpbft^> ^<nodeCount^>
exit /b 1
