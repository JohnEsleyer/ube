
#!/usr/bin/env bash
set -euo pipefail

purple=$'\033[38;5;141m'
green=$'\033[38;5;48m'
cyan=$'\033[38;5;81m'
red=$'\033[38;5;197m'
bold=$'\033[1m'
reset=$'\033[0m'

printf '%s%s🍠 Ube installer%s\n' "$purple" "$bold" "$reset"

command -v go >/dev/null 2>&1 || { 
    printf '%sError: Go is required to build Ube from source.%s\n' "$red" "$reset" >&2
    exit 1
}

source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
if [[ ! -d "$source_dir/cmd/ube" ]]; then
  printf '%sError: Run install.sh from the root of the Ube repository.%s\n' "$red" "$reset" >&2
  exit 1
fi

install_dir="${UBE_INSTALL_DIR:-$HOME/.ube}"
bin_dir="${UBE_BIN_DIR:-$install_dir/bin}"
mkdir -p "$bin_dir"

printf '%s==>%s Building Ube WebAssembly toolchain...\n' "$cyan" "$reset"
(cd "$source_dir" && go build -trimpath -ldflags='-s -w' -o "$bin_dir/ube" ./cmd/ube)

if [[ -w "/usr/local/bin" ]]; then
  cp "$bin_dir/ube" "/usr/local/bin/ube"
  chmod +x "/usr/local/bin/ube"
  printf '  Installed system-wide -> /usr/local/bin/ube\n'
elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
  sudo cp "$bin_dir/ube" "/usr/local/bin/ube"
  sudo chmod +x "/usr/local/bin/ube"
  printf '  Installed system-wide -> /usr/local/bin/ube\n'
fi

vscode_ext_dir="$HOME/.vscode/extensions/johnesleyer.ube-1.0.0"
if [[ -d "$HOME/.vscode/extensions" && -d "$source_dir/editors/vscode" ]]; then
  rm -rf "$vscode_ext_dir"
  ln -sfn "$source_dir/editors/vscode" "$vscode_ext_dir"
  printf '  Linked VS Code extension -> %s\n' "$vscode_ext_dir"
fi

path_line="export PATH=\"$bin_dir:\$PATH\""
updated=0
for profile in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do
  if [[ -f "$profile" ]]; then
    if ! grep -Fq "$bin_dir" "$profile"; then
      { printf '\n# Ube toolchain\n'; printf '%s\n' "$path_line"; } >> "$profile"
      printf '  Updated PATH in %s\n' "$profile"
    fi
    updated=1
  fi
done
if [[ $updated -eq 0 ]]; then
  printf '# Ube toolchain\n%s\n' "$path_line" >> "$HOME/.bashrc"
  printf '  Created %s/.bashrc\n' "$HOME"
fi

printf '\n%s%s✓ Installed Ube successfully%s\n' "$green" "$bold" "$reset"
printf '  Binary: %s/ube (Direct WebAssembly)\n\n' "$bin_dir"
printf 'Get started:\n'
printf '  ube help\n'
