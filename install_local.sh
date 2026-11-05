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

# 2. Warn (do not abort) if a stale ube is on PATH before we install.
#    This is the drift that made 'ube lsp' run pre-fix logic while the CLI
#    was already fixed. If resolved != any path we're about to write,
#    say so clearly.
PRE_RESOLVED="$(command -v ube || true)"
if [ -n "$PRE_RESOLVED" ] && [ "$PRE_RESOLVED" != "/usr/local/bin/ube" ] && [ "$PRE_RESOLVED" != "$HOME/.local/bin/ube" ]; then
    echo -e "${RED}WARN: 'ube' currently resolves to $PRE_RESOLVED.${NC}" >&2
    echo -e "${RED}      This is likely a stale install. Remove it and its PATH entry${NC}" >&2
    echo -e "${RED}      before continuing, or the extension/LSP will run stale code.${NC}" >&2
fi

echo -e "${BLUE}==> Building ube from local source...${NC}"
mkdir -p "$REPO_ROOT/bin"
go build -o "$REPO_ROOT/bin/ube" ./cmd/ube
echo -e "${GREEN}✓ Build succeeded: bin/ube${NC}"

# 3. Install binary
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

# 4. Update VS Code Extension (symlink for live development)
VSCODE_EXT_DIR="$HOME/.vscode/extensions/johnesleyer.ube-1.0.0"
if [ -d "$HOME/.vscode/extensions" ]; then
    echo -e "${BLUE}==> Linking VS Code extension to $VSCODE_EXT_DIR...${NC}"
    rm -rf "$VSCODE_EXT_DIR"
    ln -sfn "$REPO_ROOT/editors/vscode" "$VSCODE_EXT_DIR"
    echo -e "${GREEN}✓ VS Code extension linked successfully.${NC}"
fi

# 5. Verify installation. Only $INSTALL_DIR/ube is acceptable; anything
#    else (including ~/.ube/bin/ube from an older install.sh) is drift.
RESOLVED="$(command -v ube || true)"
case "$RESOLVED" in
    "$INSTALL_DIR/ube")
        echo -e "${GREEN}✓ 'ube' on PATH resolves to $INSTALL_DIR/ube${NC}"
        ;;
    "")
        echo -e "${RED}WARN: ube not on PATH. Add $INSTALL_DIR to PATH.${NC}" >&2
        ;;
    *)
        echo -e "${RED}WARN: which ube -> $RESOLVED (not $INSTALL_DIR/ube).${NC}" >&2
        echo -e "${RED}      Remove the stale path from \$PATH or delete the binary.${NC}" >&2
        ;;
esac

echo -e "\n${PURPLE}=== Installation Complete! ===${NC}"
"$INSTALL_DIR/ube" version || true

# Note on legacy paths: this script does NOT write to ~/.ube/bin. That
# path belonged to an older install.sh. Keeping two copies of the binary
# always drifts -- the LSP picks one, the CLI picks the other, and the
# stale one is the one holding ETXTBSY when the installer tries to
# overwrite it. If ~/.ube/bin exists, remove it:
#
#   pkill -f 'ube lsp' ; rm -rf ~/.ube/bin
#   sed -i '/\.ube\/bin/d' ~/.bashrc ~/.profile
#
echo -e "${GREEN}Ready to use. Remember to run 'Developer: Reload Window' in VS Code if running.${NC}"
