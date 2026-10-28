
<div align="center">

<img src="assets/icon.svg" alt="Ube Logo" width="130" height="130" />

# The Ube Programming Language

**The sweet, grounded systems language.**
*Easy to program like Go. Fast like C. Zero Garbage Collection.*

**Latest version: `0.5.2`**

[![License: MIT](https://img.shields.io/badge/License-MIT-purple.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.20+-00ADD8?logo=go)](https://golang.org)
[![C Standard](https://img.shields.io/badge/C-GNU11-blue.svg)](https://en.wikipedia.org/wiki/C11)

</div>

---

## Table of Contents

- [What is Ube?](#what-is-ube)
- [Key Features](#key-features)
- [Quick Installation](#quick-installation)
- [The `ube` CLI Tool](#the-ube-cli-tool)
- [Memory Safety & Purity Analysis](#memory-safety--purity-analysis)
- [Language Tour](#language-tour)
  - [1. Scoped Memory Arenas (`scope`)](#1-scoped-memory-arenas-scope)
  - [2. Dynamic Slices with Managed Growth](#2-dynamic-slices-with-managed-growth)
  - [3. Concurrency via Vines & Flat Channels](#3-concurrency-via-vines--flat-channels)
  - [4. Generational Pools (`Pool[T]`)](#4-generational-pools-poolt)
  - [5. Structs & Receiver Methods](#5-structs--receiver-methods)
  - [6. Error Handling (`?`, Result, `or_else`)](#6-error-handling--result-or_else)
  - [7. Null-Safety with Optionals (`if let`)](#7-null-safety-with-optionals-if-let)
  - [8. Tagged Enums & Pattern Matching](#8-tagged-enums--pattern-matching)
- [Editor & Tooling Support](#editor--tooling-support)
  - [Language Server Protocol (LSP)](#language-server-protocol-lsp)
  - [VS Code Extension](#vs-code-extension)
- [Project Roadmap](#project-roadmap)
- [License](#license)

---

## What is Ube?

**Ube** is a statically typed systems language combining the clean syntax and ergonomics of Go with the zero-cost execution and control of C.

Instead of relying on a runtime garbage collector or manual lifetime annotations, Ube uses **lexical scope arenas**, **generational entity pools**, and **typed channels**. The compiler emits GNU11 C for native compilation with GCC or Clang. Ube's safety checks catch several common hazards, but do not yet constitute a formal memory-safety proof.

---

## Key Features

- **Zero Garbage Collection:** Deterministic scoped arenas with bulk watermark reclamation in a single CPU instruction.
- **Go-Style Ergonomics:** No mandatory semicolons, Go-style receiver methods, and first-class channels.
- **Safety checks:** Compile-time scope escape checks, runtime bounds-checked indexing, and generational pool handles.
- **Vines and channels:** Spawned threads exchange flat values through typed channels.
- **Native C interoperability:** Foreign calls and raw pointer operations use explicit `unsafe { ... }` blocks.
- **Instant Toolchain:** Minimalist, Cargo-like CLI (`ube`) with automated project generation, checking, building, and running.

---

## Quick Installation

Ube installs via a single automated setup script. The installer builds the compiler, configures runtime headers, and automatically configures your `PATH` in `~/.bashrc`, `~/.zshrc`, or `~/.profile`.

### 1. Prerequisites
- **Go** (1.21 or newer)
- **GCC** or **Clang** (with `-std=gnu11` and `-pthread` support)

### 2. Run the Installer

```bash
git clone https://github.com/JohnEsleyer/ube.git
cd ube
chmod +x install.sh
./install.sh
```

### 3. Reload Shell

```bash
source ~/.bashrc  # or: source ~/.zshrc
```

Verify your installation by running:

```bash
ube
```

---

## The `ube` CLI Tool

Like `cargo` or `go`, Ube provides a unified, clean command-line interface with formatted terminal outputs and build status reports:

```text
🍠 Ube — The sweet, grounded systems language

USAGE:
    ube <COMMAND> [OPTIONS]
    ube <file.ube>

COMMANDS:
    run <file.ube>          Compile and execute a program immediately
    build <file.ube>        Compile an Ube program into an optimized binary
    check <file.ube>        Validate syntax, types, and safety without building
    test [dir|file]         Discover and run test files and test_ functions
    new <project-name>      Scaffold a new Ube project
    lsp                     Launch the Language Server Protocol engine
    version                 Print toolchain version and runtime details
    help                    Show command-line usage information

FLAGS:
    -o <file>             Specify target binary output name
    --emit-c              Preserve generated C source files
    --forbid-unsafe       Reject C FFI declarations and unsafe blocks

CHECK OPTIONS:
    --strict              Reject C FFI declarations and unsafe blocks
    --format=json         Emit a machine-readable safety audit
```

### Quick Commands

```bash
# Create a new project
ube new game_server
cd game_server

# Run immediately
ube run main.ube

# Build an optimized release binary
ube build main.ube -o game_server
./game_server

# Discover and run tests (files ending in _test.ube or functions named test_*)
ube test .
```

---

## Memory Safety & Purity Analysis

Ube distinguishes programs that declare C foreign functions from those that do not. This reports the FFI boundary; it does not certify the whole program as memory safe. Ube compiles through generated C, and unsafe code, compiler/runtime defects, data races outside typed channel use, and external C libraries remain relevant risks.

Foreign calls, taking raw addresses with `&`, accessing a raw pointer with `.ptr`, and dereferencing a pointer with `*` require an explicit unsafe block. A call can be wrapped as an unsafe expression so the surrounding wrapper remains safe to call:

```ube
extern fn puts(s: *u8) -> i32

fn main() {
    let result = unsafe { puts("hello".ptr) }
}
```

An `unsafe` block marks a trust boundary; it does not prove the C function is synchronous or prevent it from retaining a pointer. Only pass arena-backed strings and slices to C APIs whose contract guarantees the pointer is no longer used after the call returns, or copy the data into C-owned memory first. Ube cannot verify third-party C contracts or automatically determine whether a wrapper has been audited. `defer` schedules a cleanup call at the end of its lexical scope, including returns and loop exits.

Slice, array, and string indexing checks bounds at runtime. Out-of-range access prints the source location and exits with status 101. The compiler rejects assignments and returns that would let arena-backed references escape an explicit `scope` block. Helper functions share the caller's active arena, so their returned strings and slices remain valid until that arena scope ends. Pool elements and channel elements must be flat; a stale pool handle returns an empty optional, while a valid handle returns a value copy.

`ube check main.ube --format=json` emits an audit object for CI. Add `--strict` to fail when the program declares C FFI or contains unsafe blocks. `ube build main.ube --forbid-unsafe` applies the same policy before compiling a binary. These checks reduce common risks but are not a mathematical guarantee of memory safety.

---

## Language Tour

### 1. Scoped Memory Arenas (`scope`)

Lexical `scope { ... }` blocks capture an arena watermark. Scratch memory allocated inside the block is reclaimed instantly when the block exits:

```ube
fn main() {
    let mut numbers = []i32{}
    numbers.push(1)

    scope {
        let mut temp = []i32{}
        temp.push(100)
        temp.push(200)
        // 'temp' exists only within this block
    } // Memory is restored to the saved watermark.

    numbers.push(2)
    println(numbers[0]) // 1
    println(numbers[1]) // 2
}
```

### 2. Dynamic Slices with Managed Growth

Slices are contiguous buffers backed by the active arena. When capacity is exceeded, storage expands geometrically while preserving live elements. A function that calls `.push()` on a slice parameter receives a mutable reference to the caller's slice descriptor, so its new length and capacity are visible to the caller:

```ube
fn main() {
    let mut values = []i32{}
    let mut i = 0
    while i < 50 {
        values.push(i * 2)
        i = i + 1
    }

    for val in values {
        println(val)
    }
}
```

### 3. Concurrency via Vines & Flat Channels

Vines are lightweight tasks queued onto a bounded pool of OS worker threads. Each worker keeps a private arena that is reset between tasks. Channels accept flat values, which avoids sending Ube-managed pointers across threads. A `select` with no `default` case blocks on channel notifications instead of polling. This does not prevent races through external shared state or unsafe C code.

```ube
struct WorkOrder {
    id: i32,
    amount: i32
}

fn worker(orders: Chan[WorkOrder], results: Chan[i32]) {
    while let order = orders.recv() {
        results.send(order.id * order.amount)
    }
}

fn main() {
    let orders = Chan[WorkOrder].new(8)
    let results = Chan[i32].new(8)

    spawn worker(orders, results)

    orders.send(WorkOrder{ id: 1, amount: 15 })
    orders.send(WorkOrder{ id: 2, amount: 30 })
    orders.close()

    println(results.recv()) // 15
    println(results.recv()) // 60
}
```

### 4. Generational Pools (`Pool[T]`)

For non-lexical lifetimes (such as graphs or entity collections), Ube provides generational pools. Accessing a despawned or stale handle returns an empty optional. A valid handle returns a copy of its value, not a pointer into the pool:

```ube
struct Entity {
    id: i32,
    active: bool
}

fn main() {
    let mut pool = Pool[Entity].new(16)

    let handle = pool.spawn(Entity{ id: 42, active: true })

    // Access returns an optional value copy
    if let ent = pool.get(handle) {
        println(ent.id)
    }

    pool.despawn(handle)

    // A stale handle resolves to an empty optional
    if let dead = pool.get(handle) {
        println("Still active")
    } else {
        println("Handle safely invalidated")
    }
}
```

### 5. Structs & Receiver Methods

Ube supports Go-style receiver methods defined directly on struct types:

```ube
struct Rect {
    width: i32,
    height: i32
}

fn (r Rect) area() -> i32 {
    return r.width * r.height
}

fn main() {
    let box = Rect{ width: 12, height: 4 }
    println(box.area()) // 48
}
```

### 6. Error Handling (`?`, Result, `or_else`)

Functions return explicit `Result` types (`Type ! ErrorType`). Errors can be propagated using `?` or resolved inline using `or_else`:

```ube
fn divide(a: i32, b: i32) -> i32 ! string {
    if b == 0 {
        return error("division by zero")
    }
    return a / b
}

fn compute() -> i32 ! string {
    // Propagate error if divide fails
    let value = divide(100, 5)?
    return value * 2
}

fn main() {
    // Provide a safe fallback on error
    let result = divide(10, 0) or_else 0
    println(result) // 0
}
```

### 7. Null-Safety with Optionals (`if let`)

Types are non-nullable by default. Optional values are explicitly declared using the `?` suffix:

```ube
fn find_user(id: i32) -> string? {
    if id == 1 {
        return "Alice"
    }
    return null
}

fn main() {
    let user = find_user(1)
    if let name = user {
        println(name)
    } else {
        println("User not found")
    }
}
```

### 8. Tagged Enums & Pattern Matching

Enums represent tagged sum types with optional per-variant data:

```ube
enum Action {
    Move(i32, i32),
    Pause,
    Stop
}

fn execute(act: Action) {
    match act {
        Move(x, y) => {
            println("Moving coordinate:")
            println(x)
            println(y)
        },
        Pause => println("Paused"),
        Stop  => println("Stopped")
    }
}
```

---

## Editor & Tooling Support

### Language Server Protocol (LSP)

The Ube compiler contains a built-in Language Server providing real-time diagnostics, autocompletion, and hover information:

```bash
ube lsp
```

Configure your editor's LSP client (such as Neovim, Helix, or Sublime Text) to execute `ube lsp` for all `.ube` files.

### VS Code Extension

The VS Code extension is located under [`editors/vscode`](editors/vscode), providing syntax highlighting, snippets, file icons, and language client integration.

---

## Project Roadmap

| Target | Status | Notes |
| :--- | :--- | :--- |
| **Go Compiler Frontend** | Shipped | Full lexer, parser, static typechecker, and GNU11 C generator |
| **Scoped Arena Runtime** | Shipped | Watermark resets, contiguous geometric slice expansion |
| **Vines & Channels** | Shipped | Queued spawned tasks on a bounded worker pool with flat-type validation |
| **Generational Pools** | Shipped | Generation-checked handles for non-lexical lifetimes |
| **LSP Server** | Shipped | Native JSON-RPC server with hover, completion, definitions, and document symbols |
| **M:N Fiber Scheduler** | Shipped | Queued Vine tasks multiplexed over native OS worker threads with `yield()` |
| **Embedded & WebAssembly** | Shipped | Freestanding profiles (`--freestanding`) and Emscripten compilation (`--wasm`) |

---

## License

Ube is open-source software distributed under the terms of the [MIT License](LICENSE).
