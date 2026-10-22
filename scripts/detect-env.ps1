# 环境自检（Windows 原生）：输出 KEY=VALUE，给 AI Agent 用。
# 用法：powershell -NoProfile -ExecutionPolicy Bypass -File scripts\detect-env.ps1
$ErrorActionPreference = 'SilentlyContinue'
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $root

function KV($k, $v) { Write-Output "$k=$v" }
function Has($c) { if (Get-Command $c) { 'yes' } else { 'no' } }
function PortOpen($p) {
  try { $c = New-Object Net.Sockets.TcpClient; $c.Connect('127.0.0.1', $p); $c.Close(); 'open' } catch { 'closed' }
}

KV OS windows
KV ARCH $env:PROCESSOR_ARCHITECTURE
KV PKG_MANAGER $(if (Get-Command winget) { 'winget' } elseif (Get-Command choco) { 'choco' } else { 'none' })
KV DOCKER (Has docker)
KV DOCKER_COMPOSE $(if ((Get-Command docker) -and (docker compose version 2>$null)) { 'yes' } else { 'no' })
KV GO (Has go)
KV NODE (Has node)
KV NPM (Has npm)
KV GIT (Has git)
KV PSQL (Has psql)
KV PORT_5432 (PortOpen 5432)
KV PORT_8787 (PortOpen 8787)
KV PORT_8788 (PortOpen 8788)
if (Test-Path 'artex.exe') { KV ARTEX_BINARY 'artex.exe' }
elseif (Test-Path 'dist\artex.exe') { KV ARTEX_BINARY 'dist\artex.exe' }
elseif (Test-Path 'artex-win-amd64.exe') { KV ARTEX_BINARY 'artex-win-amd64.exe' }
KV HAS_CONFIG $(if (Test-Path 'config.json') { 'yes' } else { 'no' })
KV HAS_SKILLS $(if (Test-Path 'skills') { 'yes' } else { 'no' })
KV HAS_KB $(if (Test-Path 'kb') { 'yes' } else { 'no' })
KV REPO_ROOT $root
Write-Output '---'
Write-Output 'NEXT: read INSTALL-AI.md and pick a route from the decision table'
