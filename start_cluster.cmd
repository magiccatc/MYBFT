@echo off
setlocal EnableExtensions EnableDelayedExpansion

if "%~1"=="" goto :usage
if "%~2"=="" goto :usage

set "ALGORITHM=%~1"
set "NODE_COUNT=%~2"

if /I not "%ALGORITHM%"=="sbft" if /I not "%ALGORITHM%"=="hotstuff" if /I not "%ALGORITHM%"=="fast-hotstuff" if /I not "%ALGORITHM%"=="hpbft" (
  echo [ERROR] invalid algorithm: %ALGORITHM%
  goto :usage
)

for /f "delims=0123456789" %%A in ("%NODE_COUNT%") do (
  echo [ERROR] nodeCount must be an integer.
  goto :usage
)
if "%NODE_COUNT%"=="" (
  echo [ERROR] nodeCount must be an integer.
  goto :usage
)
if %NODE_COUNT% LSS 1 (
  echo [ERROR] nodeCount must be ^>= 1.
  goto :usage
)

where go >nul 2>nul
if errorlevel 1 (
  echo [ERROR] command not found: go
  exit /b 1
)

where redis-cli >nul 2>nul
if errorlevel 1 (
  echo [ERROR] command not found: redis-cli
  exit /b 1
)

where redis-server >nul 2>nul
if errorlevel 1 (
  echo [ERROR] command not found: redis-server
  exit /b 1
)

set "REPO_ROOT=%~dp0"
if "%REPO_ROOT:~-1%"=="\" set "REPO_ROOT=%REPO_ROOT:~0,-1%"
set "BIN_DIR=%REPO_ROOT%\bin"

pushd "%REPO_ROOT%" >nul
if errorlevel 1 (
  echo [ERROR] cannot enter repo root: %REPO_ROOT%
  exit /b 1
)

if not exist "%BIN_DIR%" mkdir "%BIN_DIR%"

echo [0/3] checking redis on 127.0.0.1:6379
redis-cli -h 127.0.0.1 -p 6379 ping >nul 2>nul
if errorlevel 1 (
  echo [0/3] redis not running, starting redis-server in new terminal...
  start "MYBFT-redis" cmd /k "redis-server"
  ping -n 3 127.0.0.1 >nul
  redis-cli -h 127.0.0.1 -p 6379 ping >nul 2>nul
  if errorlevel 1 (
    echo [ERROR] redis-server failed to start or port 6379 is unreachable.
    popd >nul
    exit /b 1
  )
)

echo [1/4] building binaries
go build -o "%BIN_DIR%\genkey.exe" ./cmd/genkey
if errorlevel 1 (
  echo [ERROR] genkey build failed.
  popd >nul
  exit /b 1
)
go build -o "%BIN_DIR%\client.exe" ./cmd/client
if errorlevel 1 (
  echo [ERROR] client build failed.
  popd >nul
  exit /b 1
)
go build -o "%BIN_DIR%\node.exe" ./cmd/node
if errorlevel 1 (
  echo [ERROR] node build failed.
  popd >nul
  exit /b 1
)

echo [2/4] generating keys and cluster config: N=%NODE_COUNT%
"%BIN_DIR%\genkey.exe" %NODE_COUNT%
if errorlevel 1 (
  echo [ERROR] genkey failed.
  popd >nul
  exit /b 1
)

echo [3/4] starting client terminal
start "MYBFT-client" cmd /k "cd /d ""%REPO_ROOT%"" && .\bin\client.exe %NODE_COUNT%"

ping -n 2 127.0.0.1 >nul

echo [4/4] starting node terminals: algorithm=%ALGORITHM%
if %NODE_COUNT% GTR 1 (
  for /L %%I in (2,1,%NODE_COUNT%) do (
    start "MYBFT-node-%%I" cmd /k "cd /d ""%REPO_ROOT%"" && .\bin\node.exe %%I %ALGORITHM%"
    ping -n 2 127.0.0.1 >nul
  )
)
start "MYBFT-node-1" cmd /k "cd /d ""%REPO_ROOT%"" && .\bin\node.exe 1 %ALGORITHM%"

popd >nul

echo done: started client + %NODE_COUNT% node windows.
exit /b 0

:usage
echo Usage: .\start_cluster.cmd ^<sbft^|hotstuff^|fast-hotstuff^|hpbft^> ^<nodeCount^>
exit /b 1
