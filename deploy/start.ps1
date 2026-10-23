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
  $candidates = @(
    "$env:ProgramFiles\Git\bin\bash.exe",
    "${env:ProgramFiles(x86)}\Git\bin\bash.exe",
    "$env:LOCALAPPDATA\Programs\Git\bin\bash.exe"
  )
  foreach ($p in $candidates) { if (Test-Path -LiteralPath $p) { return $p } }
  $cmd = Get-Command bash -ErrorAction SilentlyContinue
  # System32\bash.exe 是 WSL 的入口，不是 Git Bash；路径映射不同，不能直接套用。
  if ($cmd -and $cmd.Source -and ($cmd.Source -notmatch '\System32\bash\.exe$')) {
    return $cmd.Source
  }
  return $null
}

$bash = Find-Bash
if ($bash) {
  $bashArgs = @("deploy/start.sh")
  if ($WithAsm) { $bashArgs += "--with-asm" }
  if ($NoAsm)   { $bashArgs += "--no-asm" }
  if ($Rebuild) { $bashArgs += "--rebuild" }
  if ($Logs)    { $bashArgs += "--logs" }
  & $bash @bashArgs
  exit $LASTEXITCODE
}

# ---- 没有 Git Bash 时的退化路径：只起本体，不做测绘接线 ----
Write-Host "[!] 没找到 Git Bash；退化到直接调 docker compose（不含资产测绘的自动接线）" -ForegroundColor Yellow
if ($WithAsm) {
  Write-Host "[x] 没有 Git Bash 时不能自动加装资产测绘（而且 Docker Desktop 也不支持 host 网络）。" -ForegroundColor Red
  Write-Host "    请在 WSL2 的 Linux 发行版里跑：sudo bash deploy/bootstrap.sh" -ForegroundColor Yellow
  exit 1
}

function New-RandomString([int]$Length) {
  $chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'
  $bytes = New-Object byte[] $Length
  $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
  try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
  $sb = New-Object System.Text.StringBuilder
  foreach ($b in $bytes) { [void]$sb.Append($chars[$b % $chars.Length]) }
  return $sb.ToString()
}

function New-RandomHex([int]$ByteCount) {
  $bytes = New-Object byte[] $ByteCount
  $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
  try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
  return (($bytes | ForEach-Object { $_.ToString('x2') }) -join '')
}

function Get-EnvValue([string]$Key) {
  if (-not (Test-Path -LiteralPath .env)) { return '' }
  $line = Get-Content -LiteralPath .env -Encoding UTF8 |
    Where-Object { $_ -match ('^' + [regex]::Escape($Key) + '=') } |
    Select-Object -Last 1
  if (-not $line) { return '' }
  return ($line -split '=', 2)[1]
}

function Set-EnvValue([string]$Key, [string]$Value) {
  $lines = @(Get-Content -LiteralPath .env -Encoding UTF8)
  $found = $false
  $out = foreach ($line in $lines) {
    if ($line -match ('^' + [regex]::Escape($Key) + '=')) { $found = $true; "$Key=$Value" }
    else { $line }
  }
  if (-not $found) { $out = @($out) + "$Key=$Value" }
  $enc = New-Object System.Text.UTF8Encoding($false)
  # 必须显式用 LF：Git Bash 的 `source .env` 会把 CRLF 的 \r 留在变量值里，
  # 导致 POSTGRES_PASSWORD/ARTEX_PASSWORD 尾部多一个 \r，登录和数据库认证全失败。
  $text = (($out -join "`n") + "`n")
  [System.IO.File]::WriteAllText((Resolve-Path -LiteralPath .env).Path, $text, $enc)
}

if (-not (Test-Path -LiteralPath .env)) {
  Copy-Item -LiteralPath .env.example -Destination .env
  Write-Host "[+] 已从 .env.example 生成 .env" -ForegroundColor Green
}
if (-not (Get-EnvValue 'POSTGRES_PASSWORD') -or (Get-EnvValue 'POSTGRES_PASSWORD') -eq 'change-me-please') {
  Set-EnvValue 'POSTGRES_PASSWORD' (New-RandomString 24)
}
if (-not (Get-EnvValue 'ARTEX_PASSWORD')) { Set-EnvValue 'ARTEX_PASSWORD' (New-RandomString 20) }
if (-not (Get-EnvValue 'ARTEX_CRED_KEY')) { Set-EnvValue 'ARTEX_CRED_KEY' (New-RandomHex 32) }

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
  Write-Host "[x] 没找到 docker。请先安装 Docker Desktop 并启动。" -ForegroundColor Red
  exit 1
}

Write-Host "[*] docker compose up -d --build（首次 5-15 分钟）" -ForegroundColor Cyan
docker compose up -d --build
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$port = Get-EnvValue 'ARTEX_PORT'
if (-not $port) { $port = '8787' }
$base = "http://127.0.0.1:$port"
Write-Host "[*] 等待 $base/api/auth/status" -ForegroundColor Cyan
$ready = $false
for ($i = 0; $i -lt 120; $i++) {
  try {
    $r = Invoke-WebRequest -UseBasicParsing -Uri "$base/api/auth/status" -TimeoutSec 5
    if ($r.StatusCode -eq 200) { $ready = $true; break }
  } catch {
    # 启用 HTTP Basic Auth 后该接口会被网关拦成 401；此时服务/库已就绪。
    $resp = $_.Exception.Response
    if ($resp -and ([int]$resp.StatusCode -eq 401 -or [int]$resp.StatusCode -eq 403)) { $ready = $true; break }
  }
  Start-Sleep -Seconds 3
}
if (-not $ready) {
  Write-Host "[!] 等不到服务就绪，看日志：docker compose logs -f artex" -ForegroundColor Yellow
  exit 1
}

$pw = Get-EnvValue 'ARTEX_PASSWORD'
if ($pw) {
  try {
    $body = @{ password = $pw } | ConvertTo-Json
    Invoke-RestMethod -Uri "$base/api/auth/init" -Method Post -ContentType 'application/json' -Body $body | Out-Null
  } catch {
    # 403 = 密码已经设置过；忽略
  }
}

Write-Host ""
Write-Host "=========== 已启动 ===========" -ForegroundColor Green
Write-Host "  ARTEX: $base"
Write-Host "  用户 ARTEX  密码 $pw"
Write-Host "  加装资产测绘请在 WSL2/Linux 里跑：bash deploy/start.sh --with-asm"
