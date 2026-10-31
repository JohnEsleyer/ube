# Ube Language Support for Zed

Official language extension for **Ube** — *The sweet, grounded systems language.*

Equips the [Zed](https://zed.dev) editor with Tree-sitter grammar support, Vim text objects, runnable code triggers, and integration with the native `ube lsp` language server.

---

## ✨ Features

- **Tree-sitter Syntax Highlighting**: Accurate, semantic colorization for keywords (`arena`, `promote`, `spawn`, `or_else`), tagged enums, struct definitions, generics, receiver methods, and format strings.
- **Bracket Matching & Auto-Closing**: Auto-closes brackets, braces, and strings with full support for Zed's rainbow brackets.
- **Auto-Indentation**: Indentation rules for code blocks, match arms, and clause alignment (`if` / `else`).
- **Code Outline & Symbol Search**: Fast navigation in Zed's Outline Panel (`Cmd+Shift+O` / `Ctrl+Shift+O`) across functions, methods, structs, enums, and traits.
- **Vim Mode Text Objects**: Method-level and struct-level navigation (`af`, `if`, `ac`, `ic`).
- **Runnable Code Buttons**: Gutter **Run** buttons for `fn main()` and `test_*` functions directly in the editor.
- **Language Server Protocol (`ube lsp`)**:
  - Hover type inspection
  - Go to definition
  - Document and workspace symbols
  - Auto-completions
  - Live compiler diagnostics on save

---

## 🛠️ Prerequisites

1. **Ube Compiler Toolchain**: Ensure `ube` is installed and available in your `$PATH`:
   ```sh
   # From the repository root
   go build -o bin/ube ./cmd/ube
   sudo cp bin/ube /usr/local/bin/ube
   ```
   Verify installation:
   ```sh
   ube version
   ```

2. **Rust & WebAssembly Target** (needed by Zed to compile extension plugins):
   ```sh
   rustup target add wasm32-wasip1
   ```

3. **Tree-sitter Grammar**:
   The grammar repository lives under `editors/zed/grammars/ube` (or on GitHub at `https://github.com/johnesleyer/tree-sitter-ube`). Ensure `src/parser.c` is generated.

---

## 🚀 Installation

### Installing as a Dev Extension in Zed

1. Open **Zed**.
2. Open the Command Palette (`Ctrl+Shift+P` on Linux/Windows, `Cmd+Shift+P` on macOS).
3. Type and select: **`zed: install dev extension`**.
4. In the folder picker, select the **`editors/zed`** directory inside this repository.
5. Zed will compile the extension WebAssembly bundle, link the Tree-sitter grammar, and activate support for `.ube` files immediately.

---

## ⚙️ Configuration (`settings.json`)

To enable automatic formatting on save with `ube fmt` and configure LSP behavior, add the following to your Zed `settings.json` (`Ctrl+,` / `Cmd+,`):

```json
{
  "languages": {
    "Ube": {
      "tab_size": 4,
      "hard_tabs": false,
      "format_on_save": "on",
      "formatter": {
        "external": {
          "command": "ube",
          "arguments": ["fmt", "{buffer_path}"]
        }
      }
    }
  },
  "lsp": {
    "ube-lsp": {
      "initialization_options": {}
    }
  }
}
```

---

## 📁 Extension Structure

```
editors/zed/
├── Cargo.toml                  # Rust extension manifest
├── extension.toml              # Zed extension & grammar registry
├── README.md                   # Setup documentation
├── src/
│   └── lib.rs                  # LSP launcher (invokes `ube lsp`)
├── languages/
│   └── ube/
│       ├── config.toml         # Comment styles, brackets, tabs, regex indents
│       ├── highlights.scm      # Tree-sitter theme styling captures
│       ├── brackets.scm        # Rainbow bracket matching pairs
│       ├── indents.scm         # Syntax-based indentation rules
│       ├── outline.scm         # Symbol outline queries
│       ├── textobjects.scm     # Vim motions (af, if, ac, ic)
│       ├── overrides.scm       # Scope-specific setting overrides
│       └── runnables.scm       # Gutter run actions for main() and test_*
└── grammars/
    └── ube/                    # Tree-sitter grammar repo (with src/parser.c)
```

---

## 🔍 Troubleshooting

- **`Failed to install dev extension: compiling grammar 'ube'`**:
  - Zed requires the grammar directory `editors/zed/grammars/ube` to be a Git repository matching the repository URL and 40-character commit SHA in `extension.toml`.
  - Run `git rev-parse HEAD` inside `editors/zed/grammars/ube` and ensure the full hash is pasted into `rev = "..."` in `extension.toml`.
- **LSP features (Hover, Definition) not working**:
  - Ensure `ube` is in your system `$PATH` (`which ube`).
  - You can test the language server directly in your terminal: `ube lsp`.
- **Check Zed Logs**:
  - Linux: `tail -n 50 ~/.local/share/zed/logs/Zed.log`
  - macOS: `tail -n 50 ~/Library/Logs/Zed/Zed.log`
