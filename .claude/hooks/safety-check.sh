#!/usr/bin/env bash
# safety-check.sh - Runtime protection hook for Claude Code
# This hook runs before every command to ensure system safety
# Hook Type: PreToolUse
# Runs before every tool execution to validate safety

# Get the command being executed
COMMAND="$1"
WORKING_DIR="$(pwd)"

# Define project root (must be set by init-project.sh)
PROJECT_ROOT="${PROJECT_ROOT:-$WORKING_DIR}"

# Dangerous command patterns
DANGEROUS_PATTERNS=(
    "rm -rf /"
    "rm -rf ~"
    "rm -rf .."
    "rm -rf /*"
    "sudo rm -rf"
    "chmod 777 /"
    "chmod -R 777"
    "chown -R root"
    "format c:"
    "format d:"
    "del /f /s /q"
    "diskpart"
    "kill -9 1"
    "killall"
    "systemctl stop"
    "service .* stop"
    "shutdown"
    "reboot"
    "init 0"
    "init 6"
    ":(){ :|:& };:"  # Fork bomb
    "dd if=/dev/zero"
    "DROP DATABASE"
    "DROP TABLE"
    "TRUNCATE TABLE"
    "DELETE FROM .* WHERE 1"
    "mkfs"
    "fdisk"
    "> /dev/sda"
)

# Protected paths
PROTECTED_PATHS=(
    "/etc"
    "/usr"
    "/bin"
    "/sbin"
    "/lib"
    "/lib64"
    "/boot"
    "/proc"
    "/sys"
    "/dev"
    "/var/log"
    "/root"
    "C:\\Windows"
    "C:\\Program Files"
    "C:\\Program Files (x86)"
    "C:\\Users\\*\\AppData"
    "~/.ssh"
    "~/.aws"
    "~/.config"
    "~/.gnupg"
)

# Function to check if command is dangerous
is_dangerous() {
    local cmd="$1"
    
    # Check against dangerous patterns
    for pattern in "${DANGEROUS_PATTERNS[@]}"; do
        if echo "$cmd" | grep -qE "$pattern"; then
            echo "❌ BLOCKED: Command matches dangerous pattern: $pattern"
            return 0
        fi
    done
    
    # Check if operating on protected paths
    for path in "${PROTECTED_PATHS[@]}"; do
        if echo "$cmd" | grep -qE "$path"; then
            echo "❌ BLOCKED: Command targets protected path: $path"
            return 0
        fi
    done
    
    # Check if trying to operate outside project root
    if [[ "$cmd" == *"../"* ]] || [[ "$cmd" == *"..\\*" ]]; then
        # Allow reading parent directories but not modifying
        if echo "$cmd" | grep -qE "(rm|del|mv|chmod|chown|write|>).*\.\."; then
            echo "❌ BLOCKED: Cannot modify files outside project root"
            return 0
        fi
    fi
    
    return 1
}

# Function to check for cost-sensitive operations
check_cost_estimate() {
    local cmd="$1"

    # Cloud provisioning commands that may incur costs
    if echo "$cmd" | grep -qE "(terraform apply|aws .* create|gcloud .* create|az .* create|kubectl apply)"; then
        echo "⚠️ COST WARNING: This command may provision cloud resources"
        echo "Command: $cmd"
        echo "💰 Consider running with --dry-run or plan first"
    fi
}

# Function to log MCP tool calls
log_mcp_call() {
    local cmd="$1"
    local mcp_log="$PROJECT_ROOT/.claude/log/mcp-calls.jsonl"

    if echo "$cmd" | grep -qE "mcp|tool-call"; then
        mkdir -p "$(dirname "$mcp_log")"
        echo "{\"ts\":\"$(date -Iseconds)\",\"command\":\"$cmd\"}" >> "$mcp_log"
    fi
}

# Function to check if path is within project root
is_within_project() {
    local target_path="$1"
    local abs_target=$(realpath "$target_path" 2>/dev/null || echo "$target_path")
    local abs_project=$(realpath "$PROJECT_ROOT" 2>/dev/null || echo "$PROJECT_ROOT")
    
    if [[ "$abs_target" == "$abs_project"* ]]; then
        return 0
    fi
    return 1
}

# Main safety check
if is_dangerous "$COMMAND"; then
    echo "🛡️ SAFETY: This command has been blocked to protect your system"
    echo "📁 Project root: $PROJECT_ROOT"
    echo "💡 Tip: If this is intentional, modify .claude/hooks/safety-check.sh"
    exit 1
fi

# Check for cost-sensitive operations
check_cost_estimate "$COMMAND"

# Log MCP tool calls
log_mcp_call "$COMMAND"

# Check specific commands
case "$COMMAND" in
    *"sudo"*)
        echo "⚠️ WARNING: Command requires sudo. Please review carefully."
        echo "Command: $COMMAND"
        read -p "Allow this command? (yes/no): " response
        if [[ "$response" != "yes" ]]; then
            echo "❌ Command cancelled by user"
            exit 1
        fi
        ;;
    *"npm install -g"* | *"pip install --user"* | *"gem install"*)
        echo "⚠️ WARNING: Installing global package"
        echo "Command: $COMMAND"
        echo "Consider using local installation instead"
        ;;
    *"git push --force"*)
        echo "⚠️ WARNING: Force pushing to git"
        echo "This can overwrite remote history"
        read -p "Are you sure? (yes/no): " response
        if [[ "$response" != "yes" ]]; then
            echo "❌ Force push cancelled"
            exit 1
        fi
        ;;
esac

# Log command for audit
echo "$(date -Iseconds) | $COMMAND" >> "$PROJECT_ROOT/.claude/logs/command-audit.log"

# Command is safe, allow execution
exit 0