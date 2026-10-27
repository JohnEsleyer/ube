#!/usr/bin/env bash
#
# 🍠 Ube Programming Language Installer
# Automatic installer for Linux & macOS
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/JohnEsleyer/ube/main/install.sh | bash
#   - or -
#   ./install.sh (from within a cloned repository)

set -euo pipefail

REPO_URL="https://github.com/JohnEsleyer/ube.git"
INSTALL_DIR="${UBE_INSTALL_DIR:-$HOME/.ube}"
BIN_DIR="${UBE_BIN_DIR:-$HOME/.local/bin}"

# ANSI Colors
BOLD="\033[1m"
PURPLE="\033[1;35m"
GREEN="\033[1;32m"
YELLOW="\033[1;33m"
RED="\033[1;31m"
RESET="\033[0m"

info()    { echo -e "${PURPLE}==>${RESET} ${BOLD}$1${RESET}"; }
success() { echo -e "${GREEN}✔${RESET} $1"; }
warn()    { echo -e "${YELLOW}Warning:${RESET} $1"; }
error()   { echo -e "${RED}Error:${RESET} $1" >&2; exit 1; }

echo -e "${PURPLE}"
cat << 'EOF'
   __  ____        
  / / / / /_  ___  
 / / / / __ \/ _ \ 
/ /_/ / /_/ /  __/ 
\____/_.___/\___/  
EOF
echo -e "${RESET}${BOLD}The sweet, grounded systems language.${RESET}\n"

# 1. Check prerequisites
info "Checking required dependencies..."

command -v git >/dev/null 2>&1 || error "'git' is required but not installed."

PYTHON_BIN=""
for cmd in python3 python; do
    if command -v "$cmd" >/dev/null 2>&1; then
        if "$cmd" -c 'import sys; exit(0 if sys.version_info >= (3, 8) else 1)' >/dev/null 2>&1; then
            PYTHON_BIN="$cmd"
            break
        fi
    fi
done

if [ -z "$PYTHON_BIN" ]; then
    error "Python 3.8+ is required but was not found."
fi
success "Found Python: $($PYTHON_BIN --version)"

# Check for a C compiler (gcc, clang, or tcc)
CC_BIN=""
for cc in gcc clang tcc; do
    if command -v "$cc" >/dev/null 2>&1; then
        CC_BIN="$cc"
        break
    fi
done

if [ -z "$CC_BIN" ]; then
    warn "No C compiler (gcc, clang, tcc) found in PATH. You will need one to compile native Ube binaries."
else
    success "Found C compiler: $CC_BIN"
fi

# 2. Setup source directory
mkdir -p "$INSTALL_DIR"
mkdir -p "$BIN_DIR"
mkdir -p "$INSTALL_DIR/pkg"  # Global package cache

# Detect if we are already inside an existing ube checkout
if [ -f "src/compiler/ubec.py" ] && [ -f "src/runtime/ube_runtime.h" ]; then
    info "Installing from local repository..."
    cp -r src "$INSTALL_DIR/"
    if [ -f "ube" ]; then
        cp ube "$INSTALL_DIR/"
    fi
    if [ -d "examples" ]; then
        cp -r examples "$INSTALL_DIR/"
    fi
else
    info "Fetching latest Ube release from ${REPO_URL}..."
    if [ -d "$INSTALL_DIR/repo/.git" ]; then
        git -C "$INSTALL_DIR/repo" pull --ff-only
    else
        rm -rf "$INSTALL_DIR/repo"
        git clone --depth 1 "$REPO_URL" "$INSTALL_DIR/repo"
    fi
    cp -r "$INSTALL_DIR/repo/src" "$INSTALL_DIR/"
    if [ -f "$INSTALL_DIR/repo/ube" ]; then
        cp "$INSTALL_DIR/repo/ube" "$INSTALL_DIR/"
    fi
    if [ -d "$INSTALL_DIR/repo/examples" ]; then
        cp -r "$INSTALL_DIR/repo/examples" "$INSTALL_DIR/"
    fi
fi

# 3. Create the `ubec` driver wrapper
cat << EOF > "$INSTALL_DIR/ubec_driver"
#!/usr/bin/env bash
set -e
UBE_HOME="${INSTALL_DIR}"
export C_INCLUDE_PATH="\${UBE_HOME}/src/runtime:\${C_INCLUDE_PATH:-}"
exec "${PYTHON_BIN}" "\${UBE_HOME}/src/compiler/ubec.py" "\$@"
EOF
chmod +x "$INSTALL_DIR/ubec_driver"
ln -sf "$INSTALL_DIR/ubec_driver" "$BIN_DIR/ubec"
success "Linked 'ubec' into ${BIN_DIR}/ubec"

# 4. Create or link the `ube` unified CLI
if [ -f "$INSTALL_DIR/ube" ]; then
    chmod +x "$INSTALL_DIR/ube"
    ln -sf "$INSTALL_DIR/ube" "$BIN_DIR/ube"
    success "Linked 'ube' into ${BIN_DIR}/ube"
else
    # Minimal fallback runner if top-level 'ube' script is absent
    cat << EOF > "$INSTALL_DIR/ube_cli"
#!/usr/bin/env bash
set -e
UBE_HOME="${INSTALL_DIR}"
CC="${CC_BIN:-gcc}"

case "\${1:-}" in
    run)
        shift
        TARGET="\${1:-main.ube}"
        "${BIN_DIR}/ubec" "\$TARGET"
        "\$CC" -O2 -pthread -I"\$UBE_HOME/src/runtime" output.c -o .ube_tmp_bin
        ./.ube_tmp_bin
        rm -f .ube_tmp_bin output.c
        ;;
    build)
        shift
        TARGET="main.ube"
        OUT="app"
        STATIC=""
        while [ \$# -gt 0 ]; do
            case "\$1" in
                -o) OUT="\$2"; shift 2 ;;
                --static) STATIC="-static"; shift ;;
                *) TARGET="\$1"; shift ;;
            esac
        done
        "${BIN_DIR}/ubec" "\$TARGET"
        "\$CC" -O2 \$STATIC -pthread -I"\$UBE_HOME/src/runtime" output.c -o "\$OUT"
        rm -f output.c
        echo "Built \$OUT successfully"
        ;;
    *)
        echo "Usage: ube <run|build> [file.ube]"
        exit 1
        ;;
esac
EOF
    chmod +x "$INSTALL_DIR/ube_cli"
    ln -sf "$INSTALL_DIR/ube_cli" "$BIN_DIR/ube"
    success "Created fallback 'ube' CLI in ${BIN_DIR}/ube"
fi

# 5. Verify the installation
info "Testing installation..."
TEST_TMP=$(mktemp -d)
cat << 'EOF' > "$TEST_TMP/test.ube"
fn main() {
    println_str("🍠 Ube installed successfully!")
}
EOF

(
    cd "$TEST_TMP"
    "$BIN_DIR/ubec" test.ube >/dev/null 2>&1
    if [ -n "$CC_BIN" ]; then
        "$CC_BIN" -O2 -pthread -I"$INSTALL_DIR/src/runtime" output.c -o test_bin
        ./test_bin
    fi
)
rm -rf "$TEST_TMP"

# 6. Shell PATH instructions
info "Checking PATH configuration..."
SHELL_NAME=$(basename "${SHELL:-bash}")
PROFILE_FILE=""

case "$SHELL_NAME" in
    zsh)  PROFILE_FILE="$HOME/.zshrc" ;;
    bash) PROFILE_FILE="$HOME/.bashrc" ;;
    fish) PROFILE_FILE="$HOME/.config/fish/config.fish" ;;
    *)    PROFILE_FILE="$HOME/.profile" ;;
esac

PATH_EXPORT="export PATH=\"\$HOME/.local/bin:\$PATH\""
if [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
    echo
    warn "${BIN_DIR} is not currently in your \$PATH."
    echo -e "Add it to your environment by running:\n"
    if [ "$SHELL_NAME" = "fish" ]; then
        echo -e "    ${BOLD}fish_add_path ${BIN_DIR}${RESET}\n"
    else
        echo -e "    ${BOLD}echo '${PATH_EXPORT}' >> ${PROFILE_FILE}${RESET}"
        echo -e "    ${BOLD}source ${PROFILE_FILE}${RESET}\n"
    fi
fi

echo -e "\n${GREEN}${BOLD}✔ Installation complete!${RESET}"
echo -e "To get started, try:"
echo -e "    ${BOLD}ube run ${INSTALL_DIR}/examples/01_hello/main.ube${RESET}\n"