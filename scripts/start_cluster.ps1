param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet('pbft','hotstuff','fast-hotstuff','hpbft')]
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

Write-Host "[1/3] 生成签名材料与集群配置: N=$NodeCount" -ForegroundColor Cyan
go run ./cmd/genkey $NodeCount
if ($LASTEXITCODE -ne 0) {
    throw "genkey 执行失败。"
}

Write-Host "[2/3] 启动 Client 终端" -ForegroundColor Cyan
$clientCmd = "cd /d \"$RepoRoot\" && go run ./cmd/client $NodeCount"
Start-Process -FilePath 'cmd.exe' -ArgumentList '/k', $clientCmd | Out-Null

Start-Sleep -Milliseconds 500

Write-Host "[3/3] 启动 Node 终端（每个节点一个窗口），算法: $Algorithm" -ForegroundColor Cyan
for ($id = 1; $id -le $NodeCount; $id++) {
    $nodeCmd = "cd /d \"$RepoRoot\" && go run ./cmd/node $id $Algorithm"
    Start-Process -FilePath 'cmd.exe' -ArgumentList '/k', $nodeCmd | Out-Null
    Start-Sleep -Milliseconds 120
}

Write-Host "完成：已启动 Client + $NodeCount 个 Node 窗口。" -ForegroundColor Green
Write-Host "示例调用：powershell -ExecutionPolicy Bypass -File .\\scripts\\start_cluster.ps1 pbft 4"
