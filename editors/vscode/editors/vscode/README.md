# Ube Language Support for VS Code

Official extension for **Ube** — The sweet, grounded systems language.

<div align="center">
  <img src="icons/ube.svg" width="96" height="96" alt="Ube Logo">
</div>

## Features

* **Rich Syntax Highlighting**: Primitives, receiver methods, format string interpolation (`f"{x}"`), raw string tags, and operators (`?`, `!`, `->`).
* **LSP Integration**: Real-time syntax checking, type diagnostics, hover info, signature help, and symbol outlines powered by `ube lsp`.
* **Built-in Commands**:
  * `Ube: Run Current File` (`ube run`)
  * `Ube: Check Safety & Types` (`ube check`)
  * `Ube: Format Document` (`ube fmt`)
  * `Ube: Restart Language Server`
* **Snippets**: Pre-built templates for `fn`, `fnrecv`, `struct`, `enum`, `match`, `arena`, `promote`, `spawn`, `select`, `chan`, and `pool`.
* **Neo-Brutalist File Icon**: Distinct purple jam jar file icon for `.ube` files.

---

## Installation Methods

### Option 1: Direct Local Install (Recommended for Development)

You can link or copy the extension directly into VS Code without publishing to the Marketplace.

> **Important Folder Naming**: VS Code's internal extension scanner requires the directory name to match the publisher format: **`johnesleyer.ube-1.0.0`**.

#### Step 1: Open the extension folder
```sh
cd editors/vscode
```

#### Step 2: Install dependencies & bundle the extension
```sh
npm install
npm run compile
```
*(This bundles `extension.js` for the Language Server client and commands).*

#### Step 3: Link or copy into your extensions directory

**Choice A — Symlink (Recommended for active development):**

* **Linux & macOS (Standard native `.deb` / `.tar.gz` / brew):**
  ```sh
  mkdir -p ~/.vscode/extensions
  ln -sfn "$(pwd)" ~/.vscode/extensions/johnesleyer.ube-1.0.0
  ```
* **Linux (Flatpak):**
  ```sh
  mkdir -p ~/.var/app/com.visualstudio.code/data/vscode/extensions
  ln -sfn "$(pwd)" ~/.var/app/com.visualstudio.code/data/vscode/extensions/johnesleyer.ube-1.0.0
  ```
* **Windows (PowerShell as Administrator):**
  ```powershell
  New-Item -ItemType SymbolicLink -Path "$HOME\.vscode\extensions\johnesleyer.ube-1.0.0" -Target (Get-Location)
  ```

**Choice B — Direct Copy (Standalone):**

* **Linux & macOS:**
  ```sh
  mkdir -p ~/.vscode/extensions/johnesleyer.ube-1.0.0
  cp -r . ~/.vscode/extensions/johnesleyer.ube-1.0.0/
  ```
* **Windows (PowerShell):**
  ```powershell
  New-Item -ItemType Directory -Force -Path "$HOME\.vscode\extensions\johnesleyer.ube-1.0.0"
  Copy-Item -Recurse -Force .\* "$HOME\.vscode\extensions\johnesleyer.ube-1.0.0\"
  ```

#### Step 4: Verify & Reload
Verify the CLI sees the extension:
```sh
code --list-extensions | grep -i ube
```
*(Outputs `johnesleyer.ube`)*

In VS Code:
1. Press `Ctrl+Shift+P` (or `Cmd+Shift+P` on macOS).
2. Run: **`Developer: Reload Window`**.
3. Open any `.ube` file to verify syntax highlighting and language server status.

#### Step 5: Enable File Icon Theme
1. Press `Ctrl+Shift+P` / `Cmd+Shift+P`.
2. Type: **`Preferences: File Icon Theme`**.
3. Select **`Ube File Icons`**.

---

### Option 2: Package & Install `.vsix`

If you prefer installing a single self-contained package file:

#### 1. Package the extension
Inside `editors/vscode`:
```sh
npx @vscode/vsce package
```
This generates `ube-1.0.0.vsix`.

#### 2. Install the package
* **Via Command Line:**
  ```sh
  code --install-extension ube-1.0.0.vsix --force
  ```
* **Via VS Code Interface:**
  1. Open Extensions (`Ctrl+Shift+X` or `Cmd+Shift+X`).
  2. Click the `...` (Views and More Actions) menu at the top of the Extensions pane.
  3. Select **Install from VSIX...** and select `ube-1.0.0.vsix`.

---

## Configuration Settings

Under `Settings` &rarr; `Extensions` &rarr; `Ube`:

| Setting | Default | Description |
| :--- | :--- | :--- |
| `ube.compilerPath` | `"ube"` | Path to the `ube` compiler binary on your `$PATH`. |
| `ube.lsp.enable` | `true` | Enables Language Server Protocol features (`ube lsp`). |
| `ube.lsp.trace.server` | `"off"` | Traces JSON-RPC communication between VS Code and `ube lsp`. |

---

## Linux Note: Snap Confinement Warning

On Ubuntu / Pop!_OS, installing VS Code via Snap (`/snap/bin/code`) can block the extension from accessing host compilers (`/usr/local/bin/ube`) or reading symlinks located on secondary partitions (`/media/...`). 

For development, use the official native `.deb` package from [code.visualstudio.com](https://code.visualstudio.com) or the Microsoft APT repository for full host system integration.

---

## License

Distributed under the MIT License. See [LICENSE](../../LICENSE) for details.
