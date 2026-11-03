
<div align="center">

<img src="editors/vscode/icons/ube.svg" width="128" height="128" alt="Ube Logo">

# The Ube Programming Language

**The sweet, grounded language for games, apps, and high-throughput backends.**  
*WebAssembly-first &bull; Fully Sandboxed &bull; Zero GC &bull; Clean like Go*

[![Version](https://img.shields.io/badge/version-0.9.0-9333ea.svg)](https://github.com/JohnEsleyer/ube/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Target](https://img.shields.io/badge/target-wasm32--wasi-654ff0.svg)](https://wasi.dev)
[![Runtime](https://img.shields.io/badge/runtime-embedded%20wasmtime-orange.svg)](https://wasmtime.dev)

</div>

---

## 🍠 Table of Contents

- [The Ube Philosophy](#-the-ube-philosophy)
- [Compilation Model: What Actually Happens](#-compilation-model-what-actually-happens)
  - [The Two Backends](#the-two-backends)
  - [What Requires a C Toolchain Today](#what-requires-a-c-toolchain-today)
  - [The Roadmap to a Pure Wasm Backend](#the-roadmap-to-a-pure-wasm-backend)
- [Memory Architecture: Zero GC, Zero Pauses](#-memory-architecture-zero-gc-zero-pauses)
- [Installation & Toolchain](#-installation--toolchain)
- [Language Tour & Syntax Reference](#-language-tour--syntax-reference)
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
- [Standard Library & Module System](#-standard-library--module-system)
- [Developer CLI Toolchain](#-developer-cli-toolchain)
- [Native AI Pipeline (`ube sig / load / patch`)](#-native-ai-pipeline-ube-sig--load--patch)
- [Editor Integration (VS Code & LSP)](#-editor-integration-vs-code--lsp)
- [Running the Test Suite](#-running-the-test-suite)
- [License](#-license)

---

## 🍠 The Ube Philosophy

Ube is a statically typed, memory-safe language engineered for a specific pragmatic domain: **interactive games, high-throughput backend services, desktop tools, and sandboxed plugins**.

Ube is **not** a kernel or embedded hardware language. It does not manipulate CPU control registers, emit raw inline assembly, or expose raw pointers (`*T`). Those domains belong to C, C++, and Zig.

Instead, Ube exists for developers who want:

* **The readability and concurrency model of Go** (`let`, `mut`, `spawn`, `Chan[T]`, `select`)
* **The predictable latency and speed of C** (no garbage collection pauses, instant regional frees)
* **The safety guarantees of Rust** without lifetime annotations (`'a`), header files, or compiler borrow-checker fights.

| Feature | Go | Rust | C / C++ | Ube |
| :--- | :--- | :--- | :--- | :--- |
| **Garbage Collector** | Yes (runtime pauses) | None | None | **None (Zero GC)** |
| **Lifetimes / Borrow Checker** | No | Yes (`'a`, complex) | No (manual free) | **No (Affine + Arenas)** |
| **Raw Pointer Arithmetic** | Yes (`unsafe`) | Yes (`unsafe`) | Yes (standard) | **Permanently Forbidden** |
| **Concurrency Safety** | Data races possible | Compile-time check | Unsafe manual | **Shared-Nothing Flat Data** |
| **Execution Target** | OS Machine Code | OS Machine Code | OS Machine Code | **WebAssembly / WASI** |
| **AI Patch Efficiency** | Verbose `if err != nil` | Lifetime friction | Header divergence | **Native `sig/load/patch` AST** |

---

## 🛡️ Compilation Model: What Actually Happens

Ube targets WebAssembly. Every Ube program that ships ends up as a `.wasm` module (or, for `--html`, a browser-loadable WebAssembly bundle). There are, however, **two compilation backends**, and it's worth being precise about which one you get.

### The Two Backends

```
                      ┌──────────────────────┐
                      │   .ube Source Code   │
                      └──────────┬───────────┘
                                 │
                     Parse, Monomorphize, Typecheck
                                 │
             ┌───────────────────┴───────────────────┐
             ▼                                       ▼
   ┌─────────────────────┐               ┌─────────────────────────────┐
   │  BACKEND A          │               │  BACKEND B                  │
   │  Direct Wasm Emitter│               │  C Transpiler               │
   │  (wasm_emit.go)     │               │  (codegen*.go → ube_runtime.h)│
   │                     │               │                             │
   │  No toolchain needed│               │  Requires one of:           │
   │  Sub-millisecond    │               │    clang --target=wasm32-wasi│
   │                     │               │    zig cc -target wasm32-wasi│
   │                     │               │    emcc                     │
   └──────────┬──────────┘               └──────────────┬──────────────┘
              │                                          │
              ▼                                          ▼
        output.wasm                              output.wasm / output.html
              │                                          │
              └──────────────────┬───────────────────────┘
                                 ▼
                 Executed via Embedded Wasmtime JIT
                     (Zero external binaries needed to *run*)
```

**Backend A — the Direct Wasm Emitter** (`pkg/compiler/wasm_emit.go`):
Handles a restricted but growing subset of the language (arithmetic, control flow, functions, structs, fixed arrays, dynamic slices, string literals, methods, bitwise ops). Emits raw `.wasm` bytes directly from the AST in memory. Requires **no external toolchain** — no clang, zig, or emcc.

**Backend B — the C Transpiler** (`pkg/compiler/codegen*.go` → `runtime/ube_runtime.h` → clang/zig/emcc):
Handles the **full language**, including every advanced runtime feature. Emits C that links against `ube_runtime.h` (which implements arenas, channels, cooperative fibers, generational pools, string builders), then invokes whichever of `clang`, `zig cc`, or `emcc` is available on the host. If none are available, it can also build a **native C binary** so you're never blocked from running your code.

### What Requires a C Toolchain Today

The direct emitter refuses to handle the following constructs and automatically falls back to Backend B:

| Language feature | Direct Wasm (Backend A) | C Transpile (Backend B) |
| :--- | :---: | :---: |
| Arithmetic, loops, `if`/`else`, `match` | ✅ | — |
| Functions, methods, structs, enums, fixed arrays, dynamic slices | ✅ | — |
| Generics (monomorphized) | ✅ | — |
| **Traits (dynamic dispatch / vtables)** | ❌ | ✅ |
| **`spawn` (cooperative fibers)** | ❌ | ✅ |
| **`Chan[T]` (channel concurrency)** | ❌ | ✅ |
| **`select` (channel multiplexing)** | ❌ | ✅ |
| **`defer` statements** | ❌ | ✅ |
| **`Pool[T]` (generational entity pools)** | ❌ | ✅ |
| **`--html` (browser canvas bundling)** | ❌ | ✅ (via `emcc`) |
| **`--emit-c` (inspect generated C)** | ❌ | ✅ |

**If your program uses traits, `spawn`, channels, `select`, `defer`, or pools — you need `clang`, `zig`, or `emcc` on your PATH.** This is expected to change (see roadmap below). We would rather be honest about this today than ship a language that quietly fails.

### The Roadmap to a Pure Wasm Backend

Closing the gap between Backend A and Backend B is a top priority. The plan is ordered by user impact ÷ implementation cost:

| Stage | Feature | Status | Notes |
| :--- | :--- | :---: | :--- |
| **P0** | `defer` | Planned | Desugars to cleanup blocks at every return path; the typechecker already tracks defer scopes. |
| **P0** | `Pool[T]` | Planned | Linear-memory bookkeeping; high game-dev impact. |
| **P1** | Traits (static dispatch) | Planned | Monomorphization already exists; static calls lower to direct calls. |
| **P1** | `spawn` + `Chan[T]` | Planned | A **Wasm-native cooperative scheduler** using linear memory for fiber stacks and `call_indirect` for entry points — not a port of the C `setjmp`-based scheduler. |
| **P2** | `select` | Planned | Falls out naturally once channels are Wasm-native. |
| **P2** | Traits (dynamic dispatch) | Planned | Vtables as `{fn_table_ptr, data_ptr}` fat pointers in linear memory. |
| **P3** | `--html` | Planned | Direct Wasm + a thin JS host import layer; smaller bundles than emcc, auditable imports. |

**The end state is a single backend that emits all of Ube directly to Wasm bytecode.** The C transpiler will be retained as a **conformance oracle** in the test suite (both backends must produce identical observable program output) but will not be required to build or run any Ube program.

Until that lands, we recommend:

- Running `ube check --backend` to see **which backend your program will use** before you build.
- Using `ube build --require-direct` in CI if you want to assert your program stays on the direct Wasm path.
- Filing an issue if a feature you need is unexpectedly on Backend B.

---

## 🧠 Memory Architecture: Zero GC, Zero Pauses

Ube achieves zero-cost deterministic memory management through three layered tiers. This design is identical regardless of which backend compiles your program.

```
┌────────────────────────────────────────────────────────────────────────┐
│                        TIER 1: STACK VALUES                            │
│           Primitives (i32, f32, bool), Fixed-size Arrays ([N]T)        │
│           Instant push/pop with function frame activation.             │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
┌───────────────────────────────────▼────────────────────────────────────┐
│                  TIER 2: REGIONAL BUMP ARENAS (arena { })              │
│       Dynamic Slices ([]T), String Concatenation, Scratch Buffers      │
│       - Linear bump allocation: O(1) allocation cost.                  │
│       - Scope exit: Reclaims entire scratch buffer in 1 CPU cycle.     │
│       - Upward Invariant: Inner data escaping to outer scope must be    │
│         promoted with explicit promote(value) deep-copy.               │
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

## 📦 Installation & Toolchain

### Option 1: Automated Shell Installer (macOS & Linux)

```sh
curl -sSL https://raw.githubusercontent.com/JohnEsleyer/ube/main/install.sh | bash
```

### Option 2: Build From Source (Requires Go 1.18+ and CGO)

```sh
git clone https://github.com/JohnEsleyer/ube.git
cd ube
CGO_ENABLED=1 go build -o bin/ube ./cmd/ube
sudo cp bin/ube /usr/local/bin/ube
```

> **Why is `CGO_ENABLED=1` required?**
> Ube embeds the native Wasmtime Cranelift JIT engine so you can *run* `.wasm` binaries immediately without external dependencies. Precompiled native libraries for Linux (x86_64, aarch64), macOS (Intel & Apple Silicon), and Windows (x86_64) are resolved automatically by `go build`.

### Optional: C toolchain for advanced features

If your program uses traits, `spawn`, channels, `select`, `defer`, or `Pool[T]`, or if you want `--html` output, you'll need **one** of:

```sh
# Easiest on macOS / Linux:
brew install zig          # or: apt install clang

# Or Emscripten for --html:
# https://emscripten.org/docs/getting_started/downloads.html
```

The `ube` binary auto-detects whichever is available. If you don't have any of them, `ube run` will transparently fall back to a native C build using the system `cc`.

Verify your installation:

```sh
ube version
```

---

## 📖 Language Tour & Syntax Reference

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

Non-copy types (`string`, `[]T`, `Pool[T]`, and structs holding them) move ownership on assignment. Reassignment reactivates a variable:

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

Functions are top-level and statically typed. Methods attach via Go-style receiver parameters:

```ube
struct Rectangle {
    width: i32,
    height: i32
}

// Regular top-level function
fn add(a: i32, b: i32) -> i32 {
    return a + b
}

// Value receiver (read-only copy)
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

Compose data types cleanly using struct embedding. Embedded fields and methods promote to the parent:

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

Enums are tagged unions that can carry arbitrary payloads. Matching is exhaustive at compile time:

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

No exceptions, no runtime panic checks. Errors and missing values are explicit in signatures:

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
    // '?' bubbles up errors immediately
    let port = parse_port(8080)?
    return port
}

fn main() {
    // 'or_else' provides a fallback value
    let port = parse_port(-1) or_else 3000
    println(port) // 3000

    // 'if let' safely unwraps optionals or results
    if let user = find_user(42) {
        println(f"Found user: {user}")
    } else {
        println("User not found")
    }
}
```

---

### 7. Strings, Slicing & `StringBuilder`

Strings are UTF-8 byte slices in linear memory. Format interpolation is enabled with `f"..."`:

```ube
fn main() {
    let title = "WebAssembly"
    let slice = title[0:4] // "WebA" (end exclusive)
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
    // Stack-allocated array
    let arr = [3]i32{10, 20, 30}
    println(sum_slice(arr)) // Coerces [3]i32 -> []i32: prints 60

    // Dynamic slice
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

Scratch allocations inside an `arena` block are wiped in **a single CPU instruction** upon exiting:

```ube
fn main() {
    let mut persistent = []i32{}

    arena {
        let mut scratch = []i32{}
        scratch.push(10)
        scratch.push(20)

        // Rescues data from scratch arena into parent arena:
        persistent = promote(scratch)
    } // Entire child arena wiped in 1 instruction

    println(persistent.len) // 2
    println(persistent[0])  // 10

    // Arena expression form:
    let msg = arena {
        let raw = "sandbox_token"
        promote(raw[0:7])
    }
    println(msg) // "sandbox"
}
```

> **The Upward Reference Invariant:**
> Inner arenas cannot mutate outer slices or return inner references directly. The compiler enforces this statically: data escaping an arena **must** pass through `promote()`.

---

### 10. Generational Entity Pools (`Pool[T]`)

For game entities, dynamic UI nodes, and graph lifecycles:

```ube
struct Enemy {
    id: i32,
    hp: i32
}

fn main() {
    let mut pool = Pool[Enemy].new(64)

    // Spawning yields a generational Handle[Enemy]
    let h1 = pool.spawn(Enemy{ id: 1, hp: 100 })

    if let e = pool.get(h1) {
        println(e.hp) // 100
    }

    // Linearly frees handle; invalidates it at compile time
    pool.despawn(h1)
    // pool.get(h1) -> Compile error: use of moved variable 'h1'
}
```

> **Backend note:** `Pool[T]` currently requires Backend B (C transpiler). A Wasm-native implementation is planned (see [Roadmap](#the-roadmap-to-a-pure-wasm-backend)).

---

### 11. Concurrency, Tasks & Channels (`spawn`, `Chan[T]`, `select`)

Shared-nothing concurrency guarantees data race freedom by construction:

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

> **The Flat Type Invariant:**
> Values sent across channels or passed to `spawn` **must be flat** (primitives, fixed arrays, or structs of flat types). Heap-backed dynamic slices, strings, and pools cannot cross task boundaries, preventing data races without lock overhead.

> **Backend note:** `spawn`, `Chan[T]`, and `select` currently require Backend B (C transpiler). A Wasm-native cooperative scheduler is planned (see [Roadmap](#the-roadmap-to-a-pure-wasm-backend)).

---

### 12. Generics & Trait Bounds

Generics in Ube are **monomorphized at compile time** into zero-overhead specialized code:

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

Define shared interfaces for heterogeneous objects:

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

> **Backend note:** Dynamic trait dispatch (fat pointers / vtables) currently requires Backend B. Static dispatch via monomorphized generic bounds is on the P1 roadmap for Backend A.

---

### 14. Sandboxed Host Imports (`extern fn`)

WASI and host environment capabilities enter explicitly through typed WebAssembly host imports without unsafe blocks:

```ube
// Wasm host imports (WASI / JavaScript / WebGPU bindings)
extern fn time_now_ms() -> i64
extern fn host_print(s: string)

fn main() {
    let start = time_now_ms()
    host_print("Hello from fully sandboxed WebAssembly!")
    println(time_now_ms() - start)
}
```

---

## 📚 Standard Library & Module System

The standard library is bundled directly into the compiler binary and can be imported anywhere:

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
| `std/wasi` | Raw preview-1 WASI imports, file descriptor I/O, and file metadata access. |

---

## 🛠️ Developer CLI Toolchain

The single `ube` binary provides a complete toolchain:

| Command | Example | Description |
| :--- | :--- | :--- |
| `ube run` | `ube run main.ube` | Compiles to Wasm and executes directly via the **embedded Wasmtime** Cranelift JIT engine. |
| `ube build` | `ube build main.ube` | Compiles into a standalone sandboxed WebAssembly module (`main.wasm`). |
| `ube build --html` | `ube build game.ube --html` | Packages into a Web Canvas HTML5 application using Emscripten. |
| `ube check` | `ube check main.ube` | Static audit of syntax, types, affine moves, and sandbox purity. |
| `ube check --backend` | `ube check --backend main.ube` | Reports which compilation backend your program will use (direct Wasm or C transpile). |
| `ube test` | `ube test tests/` | Runs all test files (`test_*.ube`) and standalone `test_*` functions. |
| `ube fmt` | `ube fmt -w .` | Canonical auto-formatter with automatic struct field columnar alignment. |
| `ube doc` | `ube doc main.ube [sym]` | Prints signatures, fields, and method documentation. |
| `ube flow` | `ube flow main.ube` | Renders a visual call-tree showing memory scopes, tasks, and callees. |
| `ube lsp` | `ube lsp` | Starts the Language Server Protocol for VS Code and Zed. |

#### Backend-selection flags

* `--backend=direct`: Force the direct Wasm emitter. Fails if the program uses any feature only supported by Backend B.
* `--backend=c`: Force the C transpiler even if the program would compile directly.
* `--require-direct`: Alias for `--backend=direct`; useful in CI to guarantee your project stays on the pure-Wasm path.
* `--emit-c`: Write the generated C to a file (implies `--backend=c`).

---

## 🤖 Native AI Pipeline (`ube sig / load / patch`)

Ube includes a built-in **1-Click Clipboard Pipeline** designed for Large Language Models (Claude, DeepSeek, ChatGPT). It eliminates token waste by allowing complete multi-file implementations in **1 to 2 turns max**:

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

### AI Commands

* `ube prompt -c`: Copies the token-efficient Ube system prompt to your clipboard.
* `ube sig [dir] -c`: Dumps zero-body symbol signatures (~300 tokens for an entire codebase).
* `ube load -ci -c`: Reads an LLM request from clipboard, extracts only the referenced AST nodes, and copies the context back.
* `ube patch -ci`: Reads an LLM patch from clipboard and applies all changes atomically via an in-memory Virtual File System (VFS).

#### Supported `ube load` Request Directives

```xml
<request>
  <!-- Recursively loads the target function, its callees, and referenced types -->
  <trace symbol="Server.handle_request" depth="2" />

  <!-- Extracts single symbol declaration -->
  <def symbol="Player" />

  <!-- Isolates a specific match arm or switch case -->
  <select path="game.ube" fn="update" case="Action::Attack" />

  <!-- Isolates specific methods on a struct -->
  <select struct="Server" methods="start,shutdown" />

  <!-- Cross-references all read, write, or call sites across the project -->
  <xref symbol="Config" action="write" />
</request>
```

#### Supported `ube patch` Directives (Atomic VFS Engine)

```xml
<!-- Surgical search & replace scoped to a function -->
<replace path="src/player.ube" within="take_damage">
<search>
    self.hp -= amount
</search>
<with>
    self.hp = max(0, self.hp - amount)
</with>
</replace>

<!-- Overwrite or create file -->
<file path="src/utils.ube">
pub fn clamp(v: i32, lo: i32, hi: i32) -> i32 {
    if v < lo { return lo }
    if v > hi { return hi }
    return v
}
</file>

<!-- Delete file -->
<delete path="src/obsolete.ube" />
```

---

## 🔌 Editor Integration (VS Code & LSP)

Official VS Code language support is included in `editors/vscode/`:

* **Syntax Highlighting**: Keywords, affine decorators, format strings, and tagged enums.
* **LSP Integration**: Hover tooltips, go-to-definition, symbol outline, and diagnostics powered by `ube lsp`.
* **On-Save Formatting**: Integrated with `ube fmt` and struct field column alignment.
* **Icon Theme**: Native purple Ube jam jar icon for `.ube` files.

Install the extension:

```sh
cd editors/vscode
npm install
npm run package
code --install-extension ube-0.9.0.vsix
```

---

## 🧪 Running the Test Suite

Run Ube's language and end-to-end integration test runner:

```sh
# Run compiler unit tests (Go test suite)
go test -v ./...

# Run Ube language integration tests via CLI runner
ube test tests/

# Assert every program in examples/ and tests/ compiles on the pure-Wasm backend
ube check --backend=direct examples/ tests/
```

---

## 📄 License

Distributed under the MIT License. See [LICENSE](LICENSE) for details.
