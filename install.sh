#!/usr/bin/env bash
set -euo pipefail

purple=$'\033[38;5;141m'
green=$'\033[38;5;48m'
cyan=$'\033[38;5;81m'
red=$'\033[38;5;197m'
bold=$'\033[1m'
reset=$'\033[0m'

printf '%s%s🍠 Ube installer%s\n' "$purple" "$bold" "$reset"
command -v go >/dev/null 2>&1 || { printf '%sGo 1.21+ is required%s\n' "$red" "$reset" >&2; exit 1; }
if ! command -v cc >/dev/null 2>&1 && ! command -v gcc >/dev/null 2>&1 && ! command -v clang >/dev/null 2>&1; then
  printf '%sA C compiler (GCC or Clang) is required to build Ube programs%s\n' "$red" "$reset" >&2
  exit 1
fi

source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
if [[ ! -f "$source_dir/go.mod" || ! -d "$source_dir/cmd/ube" ]]; then
  printf '%sRun install.sh from a Ube source checkout.%s\n' "$red" "$reset" >&2
  exit 1
fi

install_dir="${UBE_INSTALL_DIR:-$HOME/.ube}"
bin_dir="${UBE_BIN_DIR:-$install_dir/bin}"
mkdir -p "$bin_dir" "$install_dir/runtime"
printf '%s==>%s Building the Ube toolchain...\n' "$cyan" "$reset"
(cd "$source_dir" && go build -trimpath -ldflags='-s -w' -o "$bin_dir/ube" ./cmd/ube)
(cd "$source_dir" && go build -trimpath -ldflags='-s -w' -o "$bin_dir/ubec" ./cmd/ubec)
install -m 0644 "$source_dir/runtime/ube_runtime.h" "$install_dir/runtime/ube_runtime.h"

path_line="export PATH=\"$bin_dir:\$PATH\""
updated=0
for profile in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do
  if [[ -f "$profile" ]]; then
    if ! grep -Fq "$bin_dir" "$profile"; then
      { printf '\n# Ube toolchain\n'; printf '%s\n' "$path_line"; } >> "$profile"
      printf '  Updated %s\n' "$profile"
    else
      printf '  PATH already configured in %s\n' "$profile"
    fi
    updated=1
  fi
done
if [[ $updated -eq 0 ]]; then
  printf '# Ube toolchain\n%s\n' "$path_line" >> "$HOME/.bashrc"
  printf '  Created %s/.bashrc\n' "$HOME"
fi

printf '\n%s%s✓ Installed Ube%s\n' "$green" "$bold" "$reset"
printf '  CLI: %s/ube\n  Core compiler: %s/ubec\n\n' "$bin_dir" "$bin_dir"
printf 'Open a new terminal, or run: source ~/.bashrc\n'
printf 'Get started: ube help\n'
