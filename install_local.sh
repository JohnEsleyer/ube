#!/usr/bin/env bash
set -euo pipefail

# Colors
PURPLE='\033[0;35m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${PURPLE}=== Ube Local Development Installer ===${NC}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$REPO_ROOT"

# 1. Verify Go is installed
if ! command -v go >/dev/null 2>&1; then
    echo -e "${RED}Error: Go compiler is not installed or not in PATH.${NC}" >&2
    exit 1
fi

echo -e "${BLUE}==> Building ube from local source...${NC}"
mkdir -p "$REPO_ROOT/bin"
go build -o "$REPO_ROOT/bin/ube" ./cmd/ube
echo -e "${GREEN}✓ Build succeeded: bin/ube${NC}"

# 2. Install binary
INSTALL_DIR="/usr/local/bin"
if [ -w "$INSTALL_DIR" ]; then
    cp "$REPO_ROOT/bin/ube" "$INSTALL_DIR/ube"
    chmod +x "$INSTALL_DIR/ube"
elif command -v sudo >/dev/null 2>&1; then
    echo -e "${BLUE}==> Installing to $INSTALL_DIR (requires sudo)...${NC}"
    sudo cp "$REPO_ROOT/bin/ube" "$INSTALL_DIR/ube"
    sudo chmod +x "$INSTALL_DIR/ube"
else
    # Fallback to ~/.local/bin
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
    cp "$REPO_ROOT/bin/ube" "$INSTALL_DIR/ube"
    chmod +x "$INSTALL_DIR/ube"
    echo -e "${BLUE}Note: Installed to $INSTALL_DIR. Ensure it is in your PATH.${NC}"
fi

echo -e "${GREEN}✓ Binary installed to: $INSTALL_DIR/ube${NC}"

# 3. Update VS Code Extension (symlink for live development)
VSCODE_EXT_DIR="$HOME/.vscode/extensions/johnesleyer.ube-1.0.0"
if [ -d "$HOME/.vscode/extensions" ]; then
    echo -e "${BLUE}==> Linking VS Code extension to $VSCODE_EXT_DIR...${NC}"
    rm -rf "$VSCODE_EXT_DIR"
    ln -sfn "$REPO_ROOT/editors/vscode" "$VSCODE_EXT_DIR"
    echo -e "${GREEN}✓ VS Code extension linked successfully.${NC}"
fi

# 4. Verify installation
# Warn if "which ube" does not resolve to what we just installed.
RESOLVED="$(command -v ube || true)"
case "$RESOLVED" in
    "$INSTALL_DIR/ube"|"$HOME/.ube/bin/ube") ;;
    "")  echo -e "${RED}WARN: ube not on PATH. Add $INSTALL_DIR to PATH.${NC}" ;;
    *)   echo -e "${RED}WARN: which ube -> $RESOLVED (not the freshly installed binary).${NC}" ;;
esac

echo -e "\n${PURPLE}=== Installation Complete! ===${NC}"
"$INSTALL_DIR/ube" version || true

# Also update the legacy ~/.ube/bin location from install.sh, if present.
if [ -d "$HOME/.ube/bin" ] && [ "$INSTALL_DIR" != "$HOME/.ube/bin" ]; then
    cp "$REPO_ROOT/bin/ube" "$HOME/.ube/bin/ube"
    chmod +x "$HOME/.ube/bin/ube"
    echo -e "${GREEN}✓ Also updated $HOME/.ube/bin/ube${NC}"
fi

# Also update the older ~/.ube/bin location if it exists and is on PATH
if [ -d "$HOME/.ube/bin" ]; then
    cp "$REPO_ROOT/bin/ube" "$HOME/.ube/bin/ube"
    chmod +x "$HOME/.ube/bin/ube"
    echo -e "${GREEN}✓ Also updated $HOME/.ube/bin/ube${NC}"
fi
echo -e "${GREEN}Ready to use. Remember to run 'Developer: Reload Window' in VS Code if running.${NC}"