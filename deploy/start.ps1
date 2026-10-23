# Windows 启动器：优先交给 Git Bash 里的 start.sh（逻辑只维护一份）
#   .\deploy\start.ps1                 起本体（已装测绘就一起起）
#   .\deploy\start.ps1 -WithAsm        加装并一起起测绘
#   .\deploy\start.ps1 -Logs           起完跟日志
#
# 注意：资产测绘的扫描节点用 host 网络，Docker Desktop(Windows) 不支持。
#       要装测绘请在 WSL2 的 Linux 发行版里跑，或直接在 Kali 虚拟机上部署。
param(
  [switch]$WithAsm,
  [switch]$NoAsm,
  [switch]$Rebuild,
  [switch]$Logs
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $root

function Find-Bash {
  $c = @(
    "C:\Program Files\Git\bin\bash.exe",
    "C:\Program Files (x86)\Git\bin\bash.exe",
    "$env:LOCALAPPDATA\Programs\Git\bin\bash.exe"
  )
  foreach ($p in $c) { if (Test-Path $p) { return $p } }
  $cmd = Get-Command bash -ErrorAction SilentlyContinue
  if ($cmd) { return $cmd.Source }
  return $null
}

$bash = Find-Bash
if ($bash) {
  $args = @("deploy/start.sh")
  if ($WithAsm) { $args += "--with-asm" }
  if ($NoAsm)   { $args += "--no-asm" }
  if ($Rebuild) { $args += "--rebuild" }
  if ($Logs)    { $args += "--logs" }
  & $bash @args
  exit $LASTEXITCODE
}

Write-Host "[!] 没找到 bash；退化到直接调 docker compose（不含资产测绘的自动接线）" -ForegroundColor Yellow
if (-not (Test-Path .env)) {
  Copy-Item .env.example .env
  Write-Host "[+] 已从 .env.example 生成 .env —— 请把 POSTGRES_PASSWORD 改掉后重跑" -ForegroundColor Green
  exit 1
}
docker compose up -d --build
Write-Host ""
Write-Host "ARTEX: http://127.0.0.1:8787" -ForegroundColor Green
