param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet('sbft','hotstuff','fast-hotstuff','hpbft')]
    [string]$Algorithm,

    [Parameter(Mandatory = $true, Position = 1)]
    [ValidateRange(1, 1000)]
    [int]$NodeCount
)

$ErrorActionPreference = 'Stop'

function Assert-CommandExists {
    param([string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "未找到命令: $Name，请先安装并加入 PATH。"
    }
}

Assert-CommandExists -Name 'go'
Assert-CommandExists -Name 'redis-cli'

# 进入仓库根目录（脚本位于 scripts/ 下）
$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot '..')
Set-Location $RepoRoot
$BinDir = Join-Path $RepoRoot 'bin'
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

Write-Host "[1/4] 构建可执行文件" -ForegroundColor Cyan
go build -o (Join-Path $BinDir 'genkey.exe') ./cmd/genkey
if ($LASTEXITCODE -ne 0) {
    throw "genkey 构建失败。"
}
go build -o (Join-Path $BinDir 'client.exe') ./cmd/client
if ($LASTEXITCODE -ne 0) {
    throw "client 构建失败。"
}
go build -o (Join-Path $BinDir 'node.exe') ./cmd/node
if ($LASTEXITCODE -ne 0) {
    throw "node 构建失败。"
}

Write-Host "[2/4] 生成签名材料与集群配置: N=$NodeCount" -ForegroundColor Cyan
& (Join-Path $BinDir 'genkey.exe') $NodeCount
if ($LASTEXITCODE -ne 0) {
    throw "genkey 执行失败。"
}

Write-Host "[3/4] 启动 Client 终端" -ForegroundColor Cyan
$clientCmd = "cd /d \"$RepoRoot\" && .\\bin\\client.exe $NodeCount"
Start-Process -FilePath 'cmd.exe' -ArgumentList '/k', $clientCmd | Out-Null

Start-Sleep -Milliseconds 500

Write-Host "[4/4] 启动 Node 终端（每个节点一个窗口），算法: $Algorithm" -ForegroundColor Cyan
for ($id = 1; $id -le $NodeCount; $id++) {
    $nodeCmd = "cd /d \"$RepoRoot\" && .\\bin\\node.exe $id $Algorithm"
    Start-Process -FilePath 'cmd.exe' -ArgumentList '/k', $nodeCmd | Out-Null
    Start-Sleep -Milliseconds 120
}

Write-Host "完成：已启动 Client + $NodeCount 个 Node 窗口。" -ForegroundColor Green
Write-Host "示例调用：powershell -ExecutionPolicy Bypass -File .\\scripts\\start_cluster.ps1 sbft 4"
