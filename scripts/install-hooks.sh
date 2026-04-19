#!/usr/bin/env bash
# =============================================================================
# Install Git Hooks — SOLDIER OF GOD Command Centre
# Copies pre-commit and commit-msg hooks to .git/hooks/
# =============================================================================

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

# Find project root (where .git directory lives)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

GIT_HOOKS_DIR="$PROJECT_ROOT/.git/hooks"
SCRIPTS_DIR="$PROJECT_ROOT/scripts"

echo "Installing git hooks..."
echo "  Project root: $PROJECT_ROOT"
echo ""

# Verify .git directory exists
if [ ! -d "$PROJECT_ROOT/.git" ]; then
    echo -e "${RED}Error: No .git directory found at $PROJECT_ROOT${NC}"
    echo "  Are you in the correct project directory?"
    exit 1
fi

# Create hooks directory if it doesn't exist
mkdir -p "$GIT_HOOKS_DIR"

# Install pre-commit hook
if [ -f "$SCRIPTS_DIR/pre-commit" ]; then
    cp "$SCRIPTS_DIR/pre-commit" "$GIT_HOOKS_DIR/pre-commit"
    chmod +x "$GIT_HOOKS_DIR/pre-commit"
    echo -e "  ${GREEN}pre-commit${NC}  installed"
else
    echo -e "  ${YELLOW}pre-commit${NC}  not found in scripts/ — skipping"
fi

# Install commit-msg hook
if [ -f "$SCRIPTS_DIR/commit-msg" ]; then
    cp "$SCRIPTS_DIR/commit-msg" "$GIT_HOOKS_DIR/commit-msg"
    chmod +x "$GIT_HOOKS_DIR/commit-msg"
    echo -e "  ${GREEN}commit-msg${NC}  installed"
else
    echo -e "  ${YELLOW}commit-msg${NC}  not found in scripts/ — skipping"
fi

echo ""
echo -e "${GREEN}Git hooks installed successfully.${NC}"
echo ""
echo "Hooks will enforce:"
echo "  - Go formatting, vetting, and tests on backend changes"
echo "  - ESLint and TypeScript checks on frontend changes"
echo "  - Conventional commit message format"
echo "  - Secret detection in staged changes"
