#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${UBE_INSTALL_DIR:-$HOME/.ube}"
BIN_DIR="${UBE_BIN_DIR:-$HOME/.local/bin}"
REPO_URL="https://github.com/JohnEsleyer/ube.git"

command -v git >/dev/null 2>&1 || { echo "git is required" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "Go 1.20+ is required" >&2; exit 1; }
mkdir -p "$INSTALL_DIR" "$BIN_DIR"
if [ -f go.mod ] && [ -d cmd/ubec ]; then
    SOURCE_DIR="$PWD"
else
    SOURCE_DIR="$INSTALL_DIR/source"
    if [ -d "$SOURCE_DIR/.git" ]; then
        git -C "$SOURCE_DIR" pull --ff-only
    else
        git clone --depth 1 "$REPO_URL" "$SOURCE_DIR"
    fi
fi
(cd "$SOURCE_DIR" && go build -o "$INSTALL_DIR/ubec" ./cmd/ubec)
ln -sf "$INSTALL_DIR/ubec" "$BIN_DIR/ubec"
echo "Installed ubec to $BIN_DIR/ubec"
if ! command -v gcc >/dev/null 2>&1 && ! command -v clang >/dev/null 2>&1; then
    echo "Note: install GCC or Clang to compile generated C programs."
fi
