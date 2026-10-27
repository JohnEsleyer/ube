# Ube Language Support for VS Code

Official extension for **Ube** — The sweet, grounded systems language.

## Features

* Syntax highlighting for all primitives, receiver methods, and operators (`?`, `!`, `->`).
* Bracket auto-closing and automatic indentation.
* Ube file icon theme for `.ube` files (select **Ube** using **Preferences: File Icon Theme** after installation).
* Support for `scope { ... }` scoped blocks, `spawn`, and tagged enums.

## Building the `.vsix` Package

From the repository root, run:

```sh
make vsix
```

The command writes `editors/vscode/ube-<version>.vsix`, using the extension version from `editors/vscode/package.json`. Install the VSCE CLI first if needed (`npm install --global @vscode/vsce`); the Make target uses the installed `vsce` command and reports an actionable error if it is unavailable. The `.vscodeignore` file excludes editor metadata and existing package artifacts while keeping the extension icon, grammar, snippets, and license in the package.

## Installing the `.vsix`

- **Via Command Line:**
  ```sh
  code --install-extension editors/vscode/ube-<version>.vsix
  ```
- **Via VS Code Interface:**
  1. Open the Extensions view (`Ctrl+Shift+X` or `Cmd+Shift+X`).
  2. Click the `...` menu in the upper corner of the Extensions pane.
  3. Select **Install from VSIX...** and choose `editors/vscode/ube-<version>.vsix`.
