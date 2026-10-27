<div align="center">

<img src="assets/icon.svg" alt="Ube Logo" width="130" height="130" />

# Ube Programming Language

**The sweet, grounded systems language.**  
*Easy to program like Go. Fast like C. Zero Garbage Collection.*

[![License: MIT](https://img.shields.io/badge/License-MIT-purple.svg)](LICENSE)

</div>

## What is Ube?

Ube is a statically typed systems language with Go-style ergonomics and deterministic scoped arena allocation. Its compiler frontend is written in Go and emits C for native compilation.

## Memory and concurrency

Lexical `scope { ... }` blocks save and restore an arena watermark. Dynamic slices grow geometrically in contiguous arena-backed buffers; growth copies live elements into a new buffer, so slice contents remain intact. Old buffers remain allocated until the enclosing scope is restored.

Use `spawn function(args)` to start a Vine on a detached OS thread. Typed channels copy flat values between Vines.

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

The language supports short-circuiting `&&` and `||`, plus `break` and `continue` inside loops.

## Implementation status and roadmap

| Feature | Status | Notes |
| --- | --- | --- |
| Go compiler frontend | Shipped | Lexer, parser, typechecker, and C code generator in `pkg/compiler`. |
| Dynamic slices | Shipped | Contiguous arena buffers with geometric growth and copy-forward. |
| Print helpers | Shipped | Polymorphic `println` for supported scalar and string types. |
| Logical operators | Shipped | Short-circuiting boolean `&&` and `||`. |
| Control flow | Shipped | `if`, `while`, `for`, `break`, and `continue`. |
| Spawn and channels | Shipped | Detached Vines with flat typed channel values. |
| M:N fiber scheduler | Roadmap | Cooperative scheduling and platform event integration. |
| WASM and embedded targets | Roadmap | Target profiles for WebAssembly and bare-metal systems. |

The compiler will remain implemented in Go; self-hosting the compiler in Ube is not a roadmap objective.

## Build and use

Requirements: Go 1.20+ and a C compiler such as GCC or Clang.

```sh
go build -o bin/ubec ./cmd/ubec
go test ./pkg/compiler/...
./bin/ubec examples/01_hello/main.ube -o output.c
gcc -std=gnu11 -O2 -pthread -Iruntime output.c -o ube_app
./ube_app
```

## Editor support

Lightweight language recognition is available for both VS Code and Zed. The extensions provide `.ube` file association, syntax highlighting, and editor language configuration; the VS Code extension also includes snippets and a Ube icon. They do not add run buttons or other execution commands, so programs can be built and run from the terminal using the commands above.

- **VS Code:** extension source and packaging files are in [`editors/vscode`](editors/vscode). See its [README](editors/vscode/README.md) for details.
- **Zed:** extension source is in [`editors/zed`](editors/zed), including its language configuration and icon.

## License

Ube is licensed under the MIT License.
