<div align="center">

<img src="assets/icon.svg" alt="Ube Logo" width="120" />

# Ube Programming Language

**A grounded systems language with scoped memory and a small, focused CLI.**

</div>

## Install

Requirements: Go 1.21+ and a C compiler such as GCC or Clang.

```sh
git clone https://github.com/JohnEsleyer/ube.git
cd ube
./install.sh
```

The installer builds `ube` and `ubec` into `~/.ube/bin`, installs the runtime header into `~/.ube/runtime`, and adds the binary directory to existing `~/.bashrc`, `~/.zshrc`, and `~/.profile` files. Open a new terminal (or source the relevant profile) to use the command.

## The `ube` CLI

```text
ube <command> [options]
ube <file.ube>

new <name>                 Create a starter project
check <file.ube>           Check syntax and types
build <file.ube> [-o bin]  Build a native executable
run <file.ube> [args...]   Compile and run a program
lsp                        Start the language server
version                    Show the version
help                       Show help
```

For example:

```sh
ube new hello
cd hello
ube run main.ube
ube build main.ube -o hello
./hello
```

Build and run report whether the program declares C foreign functions. A program without `extern fn` declarations is reported as **Pure Ube**; programs that declare foreign functions list those symbols and note that safety also depends on their C implementations. This is an FFI inventory, not a formal memory-safety proof: Ube currently compiles through generated C and its runtime.

## Language

Ube is a statically typed systems language. Its Go frontend lexes, parses, typechecks, and emits C. Lexical `scope { ... }` blocks save and restore an arena watermark. Dynamic slices grow in contiguous arena-backed buffers, copying live elements as they grow; old buffers remain allocated until the enclosing scope is restored.

```ube
fn main() {
    let mut values = []i32{}
    values.push(10)
    values.push(20)
    if true && true {
        println("two values")
    }
}
```

The compiler supports control flow, short-circuiting logical operators, slices, spawn, and typed channels. The compiler remains implemented in Go.

## Editor support

- **VS Code:** Extension source is in [editors/vscode](editors/vscode). It provides syntax highlighting, snippets, icons, and LSP diagnostics, completion, and hover support when `ube` is installed.
## Development

```sh
go test ./...
go build ./cmd/ube
go build ./cmd/ubec
```

## License

Ube is licensed under the MIT License.
