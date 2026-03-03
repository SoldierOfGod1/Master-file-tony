# safety-check.ps1 - Runtime protection hook for Claude Code (Windows)
# This hook runs before every command to ensure system safety
# Hook Type: PreToolUse
# Runs before every tool execution to validate safety

param(
    [string]$Command
)

# Get current directory
$WorkingDir = Get-Location
$ProjectRoot = if ($env:PROJECT_ROOT) { $env:PROJECT_ROOT } else { $WorkingDir }

# Dangerous command patterns
$DangerousPatterns = @(
    "Remove-Item -Recurse -Force C:\",
    "Remove-Item -Recurse -Force \",
    "rm -rf /",
    "rm -rf ~",
    "rm -rf ..",
    "del /f /s /q C:\",
    "del /f /s /q \",
    "format c:",
    "format d:",
    "diskpart",
    "Stop-Service",
    "Stop-Process -Name System",
    "Stop-Process -Name csrss",
    "Stop-Process -Name explorer",
    "Stop-Computer",
    "Restart-Computer",
    "Clear-RecycleBin -Force",
    "Remove-Item -Path C:\Windows",
    "Remove-Item -Path 'C:\Program Files'",
    "Set-ExecutionPolicy Unrestricted",
    "Disable-WindowsDefender",
    "Disable-WindowsFirewall",
    "DROP DATABASE",
    "DROP TABLE",
    "TRUNCATE TABLE",
    "DELETE FROM .* WHERE 1"
)

# Protected paths
$ProtectedPaths = @(
    "C:\Windows",
    "C:\Program Files",
    "C:\Program Files (x86)",
    "C:\ProgramData",
    "C:\Users\*\AppData\Roaming\Microsoft",
    "C:\Users\*\AppData\Local\Microsoft",
    "$env:USERPROFILE\.ssh",
    "$env:USERPROFILE\.aws",
    "$env:USERPROFILE\.azure",
    "$env:USERPROFILE\.config",
    "HKLM:\",
    "HKCU:\SOFTWARE\Microsoft\Windows"
)

# Function to check if command is dangerous
function Test-DangerousCommand {
    param([string]$Cmd)
    
    foreach ($pattern in $DangerousPatterns) {
        if ($Cmd -match [regex]::Escape($pattern)) {
            Write-Host "❌ BLOCKED: Command matches dangerous pattern: $pattern" -ForegroundColor Red
            return $true
        }
    }
    
    foreach ($path in $ProtectedPaths) {
        $escapedPath = [regex]::Escape($path)
        if ($Cmd -match $escapedPath) {
            Write-Host "❌ BLOCKED: Command targets protected path: $path" -ForegroundColor Red
            return $true
        }
    }
    
    # Check if trying to operate outside project root
    if ($Cmd -match "\.\.[\\/]" -and $Cmd -match "(Remove-Item|del|rm|Move-Item|mv)") {
        Write-Host "❌ BLOCKED: Cannot modify files outside project root" -ForegroundColor Red
        return $true
    }
    
    return $false
}

# Function to check if path is within project root
function Test-WithinProject {
    param([string]$TargetPath)
    
    try {
        $AbsTarget = Resolve-Path $TargetPath -ErrorAction SilentlyContinue
        $AbsProject = Resolve-Path $ProjectRoot -ErrorAction SilentlyContinue
        
        if ($AbsTarget -and $AbsProject) {
            return $AbsTarget.Path.StartsWith($AbsProject.Path)
        }
    }
    catch {
        return $false
    }
    
    return $false
}

# Function to check for cost-sensitive operations
function Test-CostSensitiveCommand {
    param([string]$Cmd)

    if ($Cmd -match "(terraform apply|aws .* create|gcloud .* create|az .* create|kubectl apply)") {
        Write-Host "⚠️ COST WARNING: This command may provision cloud resources" -ForegroundColor Yellow
        Write-Host "Command: $Cmd" -ForegroundColor White
        Write-Host "💰 Consider running with --dry-run or plan first" -ForegroundColor Cyan
    }
}

# Function to log MCP tool calls
function Write-McpCallLog {
    param([string]$Cmd)

    if ($Cmd -match "mcp|tool-call") {
        $McpLog = Join-Path $ProjectRoot ".claude\log\mcp-calls.jsonl"
        $McpLogDir = Split-Path $McpLog -Parent
        if (-not (Test-Path $McpLogDir)) {
            New-Item -ItemType Directory -Path $McpLogDir -Force | Out-Null
        }
        $Entry = "{`"ts`":`"$(Get-Date -Format 'yyyy-MM-ddTHH:mm:ss')`",`"command`":`"$Cmd`"}"
        Add-Content -Path $McpLog -Value $Entry
    }
}

# Main safety check
if (Test-DangerousCommand -Cmd $Command) {
    Write-Host "🛡️ SAFETY: This command has been blocked to protect your system" -ForegroundColor Yellow
    Write-Host "📁 Project root: $ProjectRoot" -ForegroundColor Cyan
    Write-Host "💡 Tip: If this is intentional, modify .claude\hooks\safety-check.ps1" -ForegroundColor Green
    exit 1
}

# Check for cost-sensitive operations
Test-CostSensitiveCommand -Cmd $Command

# Log MCP tool calls
Write-McpCallLog -Cmd $Command

# Check specific commands
switch -Regex ($Command) {
    ".*sudo.*|.*Run as Administrator.*" {
        Write-Host "⚠️ WARNING: Command requires elevation. Please review carefully." -ForegroundColor Yellow
        Write-Host "Command: $Command" -ForegroundColor White
        $response = Read-Host "Allow this command? (yes/no)"
        if ($response -ne "yes") {
            Write-Host "❌ Command cancelled by user" -ForegroundColor Red
            exit 1
        }
    }
    ".*npm install -g.*|.*pip install --user.*|.*Install-Module.*" {
        Write-Host "⚠️ WARNING: Installing global package" -ForegroundColor Yellow
        Write-Host "Command: $Command" -ForegroundColor White
        Write-Host "Consider using local installation instead" -ForegroundColor Cyan
    }
    ".*git push --force.*" {
        Write-Host "⚠️ WARNING: Force pushing to git" -ForegroundColor Yellow
        Write-Host "This can overwrite remote history" -ForegroundColor White
        $response = Read-Host "Are you sure? (yes/no)"
        if ($response -ne "yes") {
            Write-Host "❌ Force push cancelled" -ForegroundColor Red
            exit 1
        }
    }
}

# Log command for audit
$LogFile = Join-Path $ProjectRoot ".claude\logs\command-audit.log"
$LogDir = Split-Path $LogFile -Parent
if (-not (Test-Path $LogDir)) {
    New-Item -ItemType Directory -Path $LogDir -Force | Out-Null
}
$LogEntry = "$(Get-Date -Format 'yyyy-MM-ddTHH:mm:ss') | $Command"
Add-Content -Path $LogFile -Value $LogEntry

# Command is safe, allow execution
Write-Host "✅ Command validated" -ForegroundColor Green -NoNewline
exit 0