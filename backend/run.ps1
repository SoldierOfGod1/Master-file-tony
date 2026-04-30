# run.ps1 — Soldier of God backend launcher
#
# Loads .env (or falls back to .env.example), rebuilds, and runs the
# server from %TEMP% to dodge a Windows ACL quirk where freshly-built
# binaries in this directory get blocked from execution.
#
# Usage (from PowerShell):
#   ./run.ps1
#
# The script kills any existing listener on port 8080 first.

$ErrorActionPreference = "Stop"
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $here

# ---- 1. Load env from .env (preferred) or .env.example (fallback) ----
$envFile = if (Test-Path .env) { ".env" } elseif (Test-Path .env.example) { ".env.example" } else { $null }
if ($envFile) {
    Write-Host "loading $envFile" -ForegroundColor Gray
    Get-Content $envFile | ForEach-Object {
        $line = $_.Trim()
        if ($line -and -not $line.StartsWith("#")) {
            $eq = $line.IndexOf("=")
            if ($eq -gt 0) {
                $k = $line.Substring(0, $eq).Trim()
                $v = $line.Substring($eq + 1).Trim()
                Set-Item -Path "Env:$k" -Value $v
            }
        }
    }
}

# ---- 2. Stop any existing listener on 8080 ----
$existing = Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue
if ($existing) {
    Write-Host "stopping pid=$($existing.OwningProcess) on :8080" -ForegroundColor Yellow
    Stop-Process -Id $existing.OwningProcess -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 500
}

# ---- 3. Build to %TEMP% — avoids a Windows ACL quirk where freshly-
#         compiled Go binaries in this directory refuse to execute. ----
$exe = Join-Path $env:TEMP "sog-server.exe"
Write-Host "building -> $exe"
& go build -o $exe ./cmd/server
if ($LASTEXITCODE -ne 0) { Write-Host "build failed" -ForegroundColor Red; exit 1 }

# ---- 4. Start detached ----
$p = Start-Process -FilePath $exe `
    -RedirectStandardOutput "server.log" `
    -RedirectStandardError "server.err" `
    -WindowStyle Hidden -PassThru -WorkingDirectory $here

Write-Host "started pid=$($p.Id)" -ForegroundColor Green
Write-Host "log:    backend/server.log"
Write-Host "health: http://127.0.0.1:8080/health"
