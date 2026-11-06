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
if [ -n "$PRE_RESOLVED" ] \
    && [ "$PRE_RESOLVED" != "/usr/local/bin/ube" ] \
    && [ "$PRE_RESOLVED" != "$HOME/.local/bin/ube" ] \
    && [ "$PRE_RESOLVED" != "${UBE_INSTALL_DIR:-}/ube" ]; then
    echo -e "${RED}WARN: 'ube' currently resolves to $PRE_RESOLVED.${NC}" >&2
    echo -e "${RED}      This is likely a stale install. Remove it and its PATH entry${NC}" >&2
    echo -e "${RED}      before continuing, or the extension/LSP will run stale code.${NC}" >&2
fi

echo -e "${BLUE}==> Building ube from local source...${NC}"
mkdir -p "$REPO_ROOT/bin"
go build -o "$REPO_ROOT/bin/ube" ./cmd/ube
echo -e "${GREEN}✓ Build succeeded: bin/ube${NC}"

# 3. Install binary.
#
# Write to a temporary file in the destination directory, then rename it over
# the target. Copying directly over a running executable fails on Linux with
# "Text file busy" (ETXTBSY) -- commonly triggered when VS Code has the uBe
# extension active and its LSP is running `ube lsp`. A rename replaces the
# directory entry without truncating the inode the running process still
# holds, so installation succeeds even when ube is in use.
INSTALL_DIR="${UBE_INSTALL_DIR:-/usr/local/bin}"
if [ -n "${UBE_INSTALL_DIR:-}" ]; then
    # Honour an explicit destination (useful for testing or non-system installs).
    mkdir -p "$INSTALL_DIR"
fi
USE_SUDO=0
if [ ! -w "$INSTALL_DIR" ]; then
    if command -v sudo >/dev/null 2>&1; then
        USE_SUDO=1
    else
        # Fallback to ~/.local/bin
        INSTALL_DIR="$HOME/.local/bin"
        mkdir -p "$INSTALL_DIR"
        echo -e "${BLUE}Note: Installed to $INSTALL_DIR. Ensure it is in your PATH.${NC}"
    fi
fi

if [ "$USE_SUDO" -eq 1 ]; then
    echo -e "${BLUE}==> Installing to $INSTALL_DIR (requires sudo)...${NC}"
fi

# A running process keeps using the old inode after the rename. Tell the user
# to restart VS Code so the extension/LSP does not keep running stale code.
if command -v fuser >/dev/null 2>&1 && fuser "$INSTALL_DIR/ube" >/dev/null 2>&1; then
    echo -e "${BLUE}Note: $INSTALL_DIR/ube is currently in use.${NC}"
    echo -e "${BLUE}      Close VS Code (or reload its window) so the extension picks up the new build.${NC}"
fi

TMP_TARGET="$INSTALL_DIR/.ube.install.$$.$RANDOM"
cleanup_tmp() {
    [ -e "$TMP_TARGET" ] || return 0
    if [ "$USE_SUDO" -eq 1 ]; then
        sudo rm -f "$TMP_TARGET" 2>/dev/null || true
    else
        rm -f "$TMP_TARGET" 2>/dev/null || true
    fi
}
trap cleanup_tmp EXIT

install_ok=0
if [ "$USE_SUDO" -eq 1 ]; then
    # A single sudo invocation keeps the password prompt to one request.
    if sudo sh -c 'set -e; cp "$1" "$2"; chmod 0755 "$2"; mv -f "$2" "$3"' sh \
        "$REPO_ROOT/bin/ube" "$TMP_TARGET" "$INSTALL_DIR/ube"; then
        install_ok=1
    fi
else
    if cp "$REPO_ROOT/bin/ube" "$TMP_TARGET" \
        && chmod 0755 "$TMP_TARGET" \
        && mv -f "$TMP_TARGET" "$INSTALL_DIR/ube"; then
        install_ok=1
    fi
fi

if [ "$install_ok" -ne 1 ]; then
    echo -e "${RED}ERROR: Could not install ube to $INSTALL_DIR/ube.${NC}" >&2
    if [ -e "$INSTALL_DIR/ube" ]; then
        echo -e "${RED}       If the error mentions \"Text file busy\", the old binary is${NC}" >&2
        echo -e "${RED}       still in use. Close VS Code (the uBe extension runs \`ube lsp\`)${NC}" >&2
        echo -e "${RED}       and stop any running ube processes, then re-run this script:${NC}" >&2
        echo -e "${RED}           pkill -f 'ube lsp'${NC}" >&2
        echo -e "${RED}           ./install_local.sh${NC}" >&2
    fi
    exit 1
fi

trap - EXIT
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
