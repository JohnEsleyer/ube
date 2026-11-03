# Ube Language Support for VS Code

Official extension for **Ube** — The sweet, grounded systems language.

<div align="center">
  <img src="icons/ube.svg" width="96" height="96" alt="Ube Logo">
</div>

## Features

* **Rich Syntax Highlighting**: Primitives, receiver methods, format string interpolation (`f"{x}"`), raw string tags, and operators (`?`, `!`, `->`).
* **LSP Integration**: Real-time syntax checking, type diagnostics, hover info, signature help, and symbol outlines powered by `ube lsp`.
* **Built-in Commands**: Run (`ube run`), safety check (`ube check`), format (`ube fmt`), and server restart.
* **Snippets**: Pre-built templates for `fn`, `fnrecv`, `struct`, `enum`, `match`, `arena`, `promote`, `spawn`, `select`, `chan`, and `pool`.
* **Neo-Brutalist File Icon**: Distinct purple jam jar file icon for `.ube` files.

---

## Installation Methods

### Option 1: Direct Local Install (Recommended for Developers)

You can install the extension locally without publishing to the Marketplace or packaging a `.vsix`.

#### Step 1: Navigate to the extension folder
Open your terminal and enter the extension directory:
```sh
cd editors/vscode
```

#### Step 2: Install dependencies & compile the extension
If you plan to use the Language Server (`ube lsp`) and extension commands, bundle the extension runner:
```sh
npm install
npm run compile
```
> **Note:** If you only need syntax highlighting, icons, and snippets without LSP, you can skip this compilation step.

#### Step 3: Copy or symlink into your VS Code extensions folder

**Choice A — Symlink (Best for active development):**
Changes made to the extension take effect immediately after reloading VS Code.

* **macOS & Linux (Standard):**
  ```sh
  ln -s "$(pwd)" ~/.vscode/extensions/ube-lang
  ```
* **Linux (Snap on Ubuntu / Pop!_OS):**
  ```sh
  mkdir -p ~/snap/code/current/.vscode/extensions
  ln -s "$(pwd)" ~/snap/code/current/.vscode/extensions/ube-lang
  ```
* **Linux (Flatpak):**
  ```sh
  mkdir -p ~/.var/app/com.visualstudio.code/data/vscode/extensions
  ln -s "$(pwd)" ~/.var/app/com.visualstudio.code/data/vscode/extensions/ube-lang
  ```
* **Windows (PowerShell as Administrator):**
  ```powershell
  New-Item -ItemType SymbolicLink -Path "$HOME\.vscode\extensions\ube-lang" -Target (Get-Location)
  ```

**Choice B — Direct Copy:**
Copies the folder so it remains installed even if you move the repository.

* **macOS & Linux (Standard):**
  ```sh
  mkdir -p ~/.vscode/extensions/ube-lang
  cp -r . ~/.vscode/extensions/ube-lang/
  ```
* **Linux (Snap on Ubuntu / Pop!_OS):**
  ```sh
  mkdir -p ~/snap/code/current/.vscode/extensions/ube-lang
  cp -r . ~/snap/code/current/.vscode/extensions/ube-lang/
  ```
* **Windows (PowerShell):**
  ```powershell
  New-Item -ItemType Directory -Force -Path "$HOME\.vscode\extensions\ube-lang"
  Copy-Item -Recurse -Force .\* "$HOME\.vscode\extensions\ube-lang\"
  ```

#### Step 4: Reload VS Code
1. In VS Code, open the Command Palette (`Ctrl+Shift+P` or `Cmd+Shift+P`).
2. Type and run: **`Developer: Reload Window`**.
3. Open any `.ube` file to verify syntax highlighting.

#### Step 5: Enable File Icon Theme
1. Press `Ctrl+Shift+P` / `Cmd+Shift+P`.
2. Type: **`Preferences: File Icon Theme`**.
3. Select **`Ube File Icons`**.

---

### Option 2: Build & Install `.vsix` Package

If you prefer installing a single self-contained package file:

#### 1. Build the package
From the repository root:
```sh
make vsix
# or inside editors/vscode:
npx @vscode/vsce package
```
This generates `editors/vscode/ube-1.0.0.vsix`.

#### 2. Install the package
* **Via Command Line:**
  ```sh
  code --install-extension editors/vscode/ube-1.0.0.vsix
  ```
* **Via VS Code UI:**
  1. Open the Extensions view (`Ctrl+Shift+X` or `Cmd+Shift+X`).
  2. Click the `...` (Views and More Actions) menu in the top-right corner of the Extensions pane.
  3. Select **Install from VSIX...**.
  4. Select `ube-1.0.0.vsix`.

---

## Configuration Settings

Under `Settings` &rarr; `Extensions` &rarr; `Ube`:

| Setting | Default | Description |
| :--- | :--- | :--- |
| `ube.compilerPath` | `"ube"` | Path to the `ube` compiler binary on your `$PATH`. |
| `ube.lsp.enable` | `true` | Enables real-time typechecking and hover diagnostics. |
| `ube.lsp.trace.server` | `"off"` | Traces JSON-RPC communication between VS Code and `ube lsp`. |

---

## License

Distributed under the MIT License. See [LICENSE](../../LICENSE) for details.
