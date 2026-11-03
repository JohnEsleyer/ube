<div align="center">

<img src="editors/vscode/icons/ube.svg" width="128" height="128" alt="Ube Logo">

# The Ube Programming Language

**The sweet, grounded language for games, apps, and high-throughput backends.**  
*100% WebAssembly Native &bull; Zero External Toolchains &bull; Zero GC &bull; Clean like Go*

[![Version](https://img.shields.io/badge/version-1.0.0-9333ea.svg)](https://github.com/JohnEsleyer/ube/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Target](https://img.shields.io/badge/target-wasm32--wasi-654ff0.svg)](https://wasi.dev)
[![Architecture](https://img.shields.io/badge/architecture-direct%20wasm%20binary-9333ea.svg)](https://github.com/JohnEsleyer/ube)

</div>

---

## Table of Contents

- [The Ube Philosophy](#the-ube-philosophy)
- [Direct WebAssembly Architecture](#direct-webassembly-architecture)
- [Memory Architecture: Zero GC, Zero Pauses](#memory-architecture-zero-gc-zero-pauses)
- [Installation & Toolchain](#installation--toolchain)
- [Language Tour & Syntax Reference](#language-tour--syntax-reference)
  - [1. Variables, Mutability & Types](#1-variables-mutability--types)
  - [2. Affine Move Semantics (Ownership)](#2-affine-move-semantics-ownership)
  - [3. Functions, Receivers & Methods](#3-functions-receivers--methods)
  - [4. Anonymous Struct Embedding](#4-anonymous-struct-embedding)
  - [5. Tagged Enums & Pattern Matching](#5-tagged-enums--pattern-matching)
  - [6. Error Handling & Optionals (`!`, `?`, `or_else`)](#6-error-handling--optionals---or_else)
  - [7. Strings, Slicing & `StringBuilder`](#7-strings-slicing--stringbuilder)
  - [8. Arrays, Slices & Coercion](#8-arrays-slices--coercion)
  - [9. Regional Bump Arenas (`arena` & `promote`)](#9-regional-bump-arenas-arena--promote)
  - [10. Generational Entity Pools (`Pool[T]`)](#10-generational-entity-pools-poolt)
  - [11. Concurrency, Tasks & Channels (`spawn`, `Chan[T]`, `select`)](#11-concurrency-tasks--channels-spawn-chant-select)
  - [12. Generics & Trait Bounds](#12-generics--trait-bounds)
  - [13. Traits & Dynamic Dispatch (Fat Pointers)](#13-traits--dynamic-dispatch-fat-pointers)
  - [14. Sandboxed Host Imports (`extern fn`)](#14-sandboxed-host-imports-extern-fn)
- [Standard Library & Module System](#standard-library--module-system)
- [Developer CLI Toolchain](#developer-cli-toolchain)
- [Native AI Pipeline (`ube sig / load / patch`)](#native-ai-pipeline-ube-sig--load--patch)
- [Editor Integration (VS Code & LSP)](#editor-integration-vs-code--lsp)
- [Running the Test Suite](#running-the-test-suite)
- [License](#license)

---

## The Ube Philosophy

Ube is a statically typed, memory-safe language engineered for **interactive games, high-throughput backend services, desktop tools, and sandboxed plugins**.

Ube compiles **directly to WebAssembly bytecode in single-digit milliseconds** with **zero external compiler toolchains** (no Clang, no LLVM, no Zig, no GCC, no Emscripten needed).

* **The readability and concurrency model of Go** (`let`, `mut`, `spawn`, `Chan[T]`, `select`)
* **The predictable latency and speed of C** (no garbage collection pauses, instant regional frees)
* **The safety guarantees of Rust** without lifetime annotations (`'a`), header files, or compiler borrow-checker fights.

| Feature | Go | Rust | C / C++ | Ube |
| :--- | :--- | :--- | :--- | :--- |
| **Garbage Collector** | Yes (runtime pauses) | None | None | **None (Zero GC)** |
| **Lifetimes / Borrow Checker** | No | Yes (`'a`, complex) | No (manual free) | **No (Affine + Arenas)** |
| **Compilation Toolchain** | Heavy Go runtime | Heavy LLVM | Clang / GCC | **Direct Wasm (1ms build)** |
| **Raw Pointer Arithmetic** | Yes (`unsafe`) | Yes (`unsafe`) | Yes (standard) | **Permanently Forbidden** |
| **Execution Target** | OS Machine Code | OS Machine Code | OS Machine Code | **WebAssembly / WASI** |
| **AI Patch Efficiency** | Verbose `if err != nil` | Lifetime friction | Header divergence | **Native `sig/load/patch` AST** |

---

## Direct WebAssembly Architecture

```
                      ┌──────────────────────┐
                      │   .ube Source Code   │
                      └──────────┬───────────┘
                                 │
                   Lex, Parse, Monomorphize, Typecheck
                                 │
                                 ▼
    ┌─────────────────────────────────────────────────────────┐
    │  Pure Wasm Bytecode Emitter (pkg/wasm)                  │
    │                                                         │
    │  • 100% self-contained binary module builder            │
    │  • Direct LEB128 opcode & section streaming             │
    │  • 1ms to 5ms compile times                             │
    │  • Zero external toolchains or headers                  │
    └────────────────────────────┬────────────────────────────┘
                                 │
                                 ▼
                            output.wasm
                                 │
                                 ▼
                     WASI / Wasmtime Runtime
```

Every language construct compiles directly to Wasm bytecode instructions:

| Language Feature | Native WebAssembly Implementation | Status |
| :--- | :--- | :---: |
| Arithmetic & Bitwise | Direct Wasm numeric opcodes (`i32.add`, `i64.mul`, `f32.sub`, etc.) | ✅ |
| Structured Control Flow | `block`, `loop`, `br`, `br_if`, `if/else` | ✅ |
| Structs & Mut Receivers | Shadow stack & linear memory pointer offsets | ✅ |
| Dynamic Slices (`[]T`) | Arena-backed fat pointer header `{ptr, len, cap}` | ✅ |
| Regional Arenas (`arena`) | Global `__arena_pointer` recorded on entry & unwound on exit | ✅ |
| Tagged Enums & Matching | Discriminant tag header + payload union + branch jump table | ✅ |
| Monomorphized Generics | Specialization into concrete zero-overhead Wasm functions | ✅ |
| Dynamic Trait Dispatch | Page 0 Vtables + fat pointer `{vtable_ptr, data_ptr}` + `call_indirect` | ✅ |
| Optionals & Results | Discriminated memory wrappers `{has_val/is_ok, payload}` | ✅ |

---

## Memory Architecture: Zero GC, Zero Pauses

Ube achieves zero-cost deterministic memory management through three layered tiers:

```
┌────────────────────────────────────────────────────────────────────────┐
│                        TIER 1: STACK VALUES                            │
│           Primitives (i32, f32, bool), Fixed-size Arrays ([N]T)        │
│           Push/pop with function frame activation on Wasm stack.       │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
┌───────────────────────────────────▼────────────────────────────────────┐
│                  TIER 2: REGIONAL BUMP ARENAS (arena { })              │
│       Dynamic Slices ([]T), String Concatenation, Scratch Buffers      │
│       - Linear bump allocation: O(1) allocation cost.                  │
│       - Scope exit: Reclaims entire scratch buffer in 1 CPU cycle.     │
│       - Upward Invariant: Escaping data is promoted via promote().     │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
┌───────────────────────────────────▼────────────────────────────────────┐
│              TIER 3: GENERATIONAL ENTITY POOLS (Pool[T])               │
│       Long-lived entities, game state objects, dynamic graphs          │
│       - O(1) slot allocation returning generational Handle[T].         │
│       - Linear consumption on despawn: stale handles return none.      │
└────────────────────────────────────────────────────────────────────────┘
```

---

## Installation & Toolchain

### Option 1: Automated Shell Installer (macOS & Linux)

```sh
curl -sSL https://raw.githubusercontent.com/JohnEsleyer/ube/main/install.sh | bash
```

### Option 2: Build From Source (Requires Go 1.18+)

```sh
git clone https://github.com/JohnEsleyer/ube.git
cd ube
go build -o bin/ube ./cmd/ube
sudo cp bin/ube /usr/local/bin/ube
```

Verify your installation:

```sh
ube version
```

---

## Language Tour & Syntax Reference

### 1. Variables, Mutability & Types

Bindings are **strictly immutable by default**. Use `let mut` for mutable bindings:

```ube
fn main() {
    // Immutable by default
    let year: i32 = 2026
    let pi: f32 = 3.14159
    let active: bool = true

    // Type inference
    let name = "Ube"

    // Mutable variable
    let mut score = 100
    score += 25
    score *= 2
    println(score) // 250
}
```

#### Sized Primitive Types

* **Signed Integers**: `i8`, `i16`, `i32`, `i64`
* **Unsigned Integers**: `u8`, `u16`, `u32`, `u64`
* **Floats**: `f32`, `f64`
* **Boolean**: `bool` (`true`, `false`)
* **String**: `string` (UTF-8 byte sequence, arena-allocated)
* **Void**: `void` (return type for procedures)

---

### 2. Affine Move Semantics (Ownership)

Non-copy types (`string`, `[]T`, `Pool[T]`, and structs holding them) move ownership on assignment:

```ube
fn main() {
    let a = "Sweet Roots"
    let b = a         // Ownership of string is moved to 'b'
    // println(a)     // COMPILE ERROR: use of moved value 'a'
    println(b)        // OK

    let mut message = "hello"
    let old = message // 'message' is moved
    message = "world" // Reassignment reactivates 'message'
    println(message)  // OK
}
```

---

### 3. Functions, Receivers & Methods

```ube
struct Rectangle {
    width: i32,
    height: i32
}

// Top-level function
fn add(a: i32, b: i32) -> i32 {
    return a + b
}

// Value receiver (read-only)
fn (r Rectangle) area() -> i32 {
    return r.width * r.height
}

// Mutable receiver (in-place modification)
fn (mut r Rectangle) scale(factor: i32) {
    r.width *= factor
    r.height *= factor
}

fn main() {
    let mut rect = Rectangle{ width: 10, height: 5 }
    println(rect.area()) // 50

    rect.scale(2)
    println(rect.area()) // 200
}
```

---

### 4. Anonymous Struct Embedding

```ube
struct Position {
    x: f32,
    y: f32
}

struct Player {
    id: i32,
    Position // Embedded anonymously
}

fn main() {
    let p = Player{
        id: 1,
        Position: Position{ x: 10.0, y: 20.0 }
    }

    // Promoted field access:
    println(p.x)          // 10.0
    println(p.Position.y) // 20.0
}
```

---

### 5. Tagged Enums & Pattern Matching

Enums are tagged unions carrying arbitrary payloads. Pattern matching is verified at compile time:

```ube
enum Action {
    Move(f32, f32),
    Attack(i32),
    UseItem(string),
    Idle
}

fn handle_action(act: Action) {
    match act {
        Move(x, y) => println(f"Moving to {x}, {y}"),
        Attack(dmg) => println(f"Attacking with damage {dmg}"),
        UseItem(name) => println(f"Used item: {name}"),
        Idle => println("Player is idle")
    }
}
```

---

### 6. Error Handling & Optionals (`!`, `?`, `or_else`)

No exceptions or hidden runtime panics:

```ube
// Result type: T ! E
fn parse_port(code: i32) -> i32 ! string {
    if code < 1024 || code > 65535 {
        return error("port must be between 1024 and 65535")
    }
    return code
}

// Optional type: T?
fn find_user(id: i32) -> string? {
    if id == 42 {
        return "Aria"
    }
    return none
}

fn boot() -> i32 ! string {
    let port = parse_port(8080)?
    return port
}

fn main() {
    let port = parse_port(-1) or_else 3000
    println(port) // 3000

    if let user = find_user(42) {
        println(f"Found user: {user}")
    } else {
        println("User not found")
    }
}
```

---

### 7. Strings, Slicing & `StringBuilder`

```ube
fn main() {
    let title = "WebAssembly"
    let slice = title[0:4] // "WebA"
    println(slice)

    // Formatted interpolation
    let frame = 60
    println(f"Rendered: {title} at {frame} FPS")

    // Dynamic builder
    let mut sb = StringBuilder.new(32)
    sb.append_str("Score: ")
    sb.append_int(100)
    sb.append_char(33) // '!'
    println(sb.to_string()) // "Score: 100!"
}
```

---

### 8. Arrays, Slices & Coercion

* **Fixed Arrays (`[N]T`)**: Stack-allocated, fixed length, copyable if element is copyable.
* **Dynamic Slices (`[]T`)**: Regional bump arena-backed, dynamic growth via geometric resizing.
* **Coercion**: Fixed arrays automatically coerce to dynamic slice parameters:

```ube
fn sum_slice(vals: []i32) -> i32 {
    let mut total = 0
    for v in vals {
        total += v
    }
    return total
}

fn main() {
    let arr = [3]i32{10, 20, 30}
    println(sum_slice(arr)) // Coerces [3]i32 -> []i32: prints 60

    let mut s = []i32{}
    s.push(1)
    s.push(2)
    s.push(3)
    println(s.len) // 3

    if let popped = s.pop() {
        println(popped) // 3
    }
}
```

---

### 9. Regional Bump Arenas (`arena` & `promote`)

Allocations inside an `arena` block are wiped instantly by unwinding `__arena_pointer` on scope exit:

```ube
fn main() {
    let mut persistent = []i32{}

    arena {
        let mut scratch = []i32{}
        scratch.push(10)
        scratch.push(20)

        // Rescues data from scratch arena into parent arena:
        persistent = promote(scratch)
    } // Entire child arena reclaimed in 1 instruction

    println(persistent.len) // 2
    println(persistent[0])  // 10
}
```

---

### 10. Generational Entity Pools (`Pool[T]`)

```ube
struct Enemy {
    id: i32,
    hp: i32
}

fn main() {
    let mut pool = Pool[Enemy].new(64)
    let h1 = pool.spawn(Enemy{ id: 1, hp: 100 })

    if let e = pool.get(h1) {
        println(e.hp) // 100
    }

    pool.despawn(h1)
}
```

---

### 11. Concurrency, Tasks & Channels (`spawn`, `Chan[T]`, `select`)

```ube
struct Packet {
    id: i32,
    code: i32
}

fn worker(inbox: Chan[Packet], done: Chan[bool]) {
    while let pkt = inbox.recv() {
        println(f"Processed packet {pkt.id} with status {pkt.code}")
    }
    done.send(true)
}

fn main() {
    let inbox = Chan[Packet].new(8)
    let done = Chan[bool].new(1)

    spawn worker(inbox, done)

    inbox.send(Packet{ id: 101, code: 200 })
    inbox.send(Packet{ id: 102, code: 204 })
    inbox.close()

    select {
        ok = done => println("Worker completed all jobs"),
        default => println("Worker busy...")
    }
}
```

---

### 12. Generics & Trait Bounds

Compile-time monomorphization into zero-overhead specialized Wasm code:

```ube
struct Pair[A, B] {
    first: A,
    second: B
}

fn make_pair[A, B](a: A, b: B) -> Pair[A, B] {
    return Pair[A, B]{ first: a, second: b }
}

fn main() {
    let p = make_pair[i32, string](42, "Ube")
    println(p.first)  // 42
    println(p.second) // "Ube"
}
```

---

### 13. Traits & Dynamic Dispatch (Fat Pointers)

```ube
trait Speaker {
    fn speak() -> string
}

struct Dog { name: string }
struct Robot { id: i32 }

fn (d Dog) speak() -> string { return f"{d.name} says Woof!" }
fn (r Robot) speak() -> string { return f"Robot #{r.id} beep!" }

fn announce(s: Speaker) {
    println(s.speak())
}

fn main() {
    announce(Dog{ name: "Buddy" })
    announce(Robot{ id: 9 })
}
```

---

### 14. Sandboxed Host Imports (`extern fn`)

```ube
extern fn time_now_ms() -> i64
extern fn host_print(s: string)

fn main() {
    let start = time_now_ms()
    host_print("Hello from WebAssembly!")
    println(time_now_ms() - start)
}
```

---

## Standard Library & Module System

Bundled directly into the compiler binary:

```ube
import "std/math" as math
import "std/strings" as str
import "std/collections"
import "std/io"
import "std/wasi"

fn main() {
    let val = math.clamp(15, 0, 10) // 10
    println(val)

    let trimmed = str.str_trim("   systems   ")
    println(trimmed)

    let mut stack = collections.stack_new[i32]()
    stack.push(100)
    println(stack.pop() or_else 0) // 100
}
```

| Module | Description |
| :--- | :--- |
| `std/math` | Trigonometry, exponents, min/max, clamping, constants (`PI()`, `TAU()`, `E()`). |
| `std/strings` | Padding, repeating, splitting, prefix/suffix inspection, and trimming. |
| `std/bytes` | Dynamic `ByteBuffer` with raw byte inspection and slice conversions. |
| `std/collections` | Generic `Stack[T]` and key-value `SimpleMap[K, V]` with iterator support. |
| `std/io` | `Reader`, `Writer`, `BufReader`, `BufWriter`, `Stdin`, `Stdout`, and stream copying. |
| `std/wasi` | Raw preview-1 WASI imports and file descriptor I/O. |

---

## Developer CLI Toolchain

| Command | Example | Description |
| :--- | :--- | :--- |
| `ube build` | `ube build main.ube` | Compiles directly into a standalone WebAssembly module (`main.wasm`) in ~1ms. |
| `ube run` | `ube run main.ube` | Compiles to Wasm bytecode in-memory and executes via embedded Wasm runtime. |
| `ube check` | `ube check main.ube` | Static audit of syntax, types, and affine moves. |
| `ube test` | `ube test tests/` | Runs all test files (`test_*.ube`) and standalone `test_*` functions. |
| `ube fmt` | `ube fmt -w .` | Canonical auto-formatter with automatic struct field columnar alignment. |
| `ube doc` | `ube doc main.ube [sym]` | Prints signatures, fields, and method documentation. |
| `ube flow` | `ube flow main.ube` | Renders a visual call-tree showing memory scopes, tasks, and callees. |
| `ube lsp` | `ube lsp` | Starts the Language Server Protocol for VS Code and Zed. |

---

## Native AI Pipeline (`ube sig / load / patch`)

Ube includes a built-in **1-Click Clipboard Pipeline** designed for Large Language Models (Claude, DeepSeek, ChatGPT) allowing complete multi-file implementations in **1 to 2 turns max**:

```
[Local Codebase]                        [AI Agent / LLM Chat]
       │                                              │
       │  1. ube sig -c                               │
       ├─────────────────────────────────────────────►│ Turn 1 (Map):
       │  (Symbol skeletons copied to clipboard)      │ LLM outputs <request>
       │                                              │
       │  2. ube load -ci -c                          │
       ├─────────────────────────────────────────────►│ Turn 2 (Solve):
       │  (Extracts sub-graph AST & copies back)      │ LLM outputs <replace>
       │                                              │
       │  3. ube patch -ci                            │
       ▼                                              │
 [Atomic Disk Update] ◄───────────────────────────────┘
```

* `ube prompt -c`: Copies the token-efficient Ube system prompt to your clipboard.
* `ube sig [dir] -c`: Dumps zero-body symbol signatures (~300 tokens for an entire codebase).
* `ube load -ci -c`: Reads an LLM request from clipboard, extracts only the referenced AST nodes, and copies the context back.
* `ube patch -ci`: Reads an LLM patch from clipboard and applies all changes atomically via an in-memory Virtual File System (VFS).

---

## Editor Integration (VS Code & LSP)

Official VS Code language support is included in `editors/vscode/`:

* **Syntax Highlighting**: Keywords, affine decorators, format strings, and tagged enums.
* **LSP Integration**: Hover tooltips, go-to-definition, symbol outline, and diagnostics powered by `ube lsp`.
* **On-Save Formatting**: Integrated with `ube fmt` and struct field column alignment.
* **Icon Theme**: Native purple Ube jam jar icon for `.ube` files.

---

## Running the Test Suite

```sh
# Run compiler unit tests (Go test suite)
go test -v ./...

# Run Ube language integration tests via CLI runner
ube test tests/
```

---

## License

Distributed under the MIT License. See [LICENSE](LICENSE) for details.
