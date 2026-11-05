
# The Ube Programming Language

<div align="center">

<img src="editors/vscode/icons/ube.svg" width="128" height="128" alt="Ube Logo">

**The sweet, grounded language for games, apps, and high-throughput backends.**
*100% WebAssembly Native • Zero External Toolchains • Zero GC • Clean like Go*

[![Version](https://img.shields.io/badge/version-1.2.0-9333ea.svg)](https://github.com/JohnEsleyer/ube/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Target](https://img.shields.io/badge/target-wasm32--wasi-654ff0.svg)](https://wasi.dev)
[![Architecture](https://img.shields.io/badge/architecture-direct%20wasm%20binary-9333ea.svg)](https://github.com/JohnEsleyer/ube)

</div>

---

## Table of Contents

- [What Ube Is](#what-ube-is)
- [What Ube Is Not](#what-ube-is-not)
- [The Memory Model in One Page](#the-memory-model-in-one-page)
- [Installation & Toolchain](#installation--toolchain)
- [Language Tour & Syntax Reference](#language-tour--syntax-reference)
  - [1. Variables, Mutability & Types](#1-variables-mutability--types)
  - [2. Affine Move Semantics (Ownership)](#2-affine-move-semantics-ownership)
  - [3. Functions, Receivers & Methods](#3-functions-receivers--methods)
  - [4. Anonymous Struct Embedding](#4-anonymous-struct-embedding)
  - [5. Tagged Enums & Pattern Matching](#5-tagged-enums--pattern-matching)
  - [6. Error Handling & Optionals](#6-error-handling--optionals)
  - [7. Strings, Slicing & StringBuilder](#7-strings-slicing--stringbuilder)
  - [8. Arrays, Slices & Coercion](#8-arrays-slices--coercion)
  - [9. Regional Bump Arenas](#9-regional-bump-arenas)
  - [10. Generational Entity Pools](#10-generational-entity-pools)
  - [11. Concurrency, Tasks & Channels](#11-concurrency-tasks--channels)
  - [12. Generics & Trait Bounds](#12-generics--trait-bounds)
  - [13. Traits & Dynamic Dispatch](#13-traits--dynamic-dispatch)
  - [14. Sandboxed Host Imports](#14-sandboxed-host-imports)
- [Packages, Standard Library & Imports](#packages-standard-library--imports)
- [Developer CLI Toolchain](#developer-cli-toolchain)
- [Working With LLMs](#working-with-llms)
- [Editor Integration (VS Code & LSP)](#editor-integration-vs-code--lsp)
- [Running the Test Suite](#running-the-test-suite)
- [License](#license)

---

## What Ube Is

Ube is a statically typed, memory-safe systems language that compiles **directly to WebAssembly bytecode** with no external compiler toolchain — no Clang, LLVM, GCC, Zig, or Emscripten.

It borrows deliberately from three neighbours:

- **The concurrency model and surface syntax of Go** — `let`, `mut`, `spawn`, `Chan[T]`, `select`, receiver methods.
- **The memory determinism of C** — no garbage collector, no pauses, deterministic scope exit.
- **The safety guarantees of Rust** — single ownership and use-after-move rejection, without lifetime annotations, borrow-checker diagnostics, or header/implementation splits.

Every language construct lowers to WebAssembly instructions directly. There is no intermediate C generation step and no text `.wat` representation:

| Language Feature | Native WebAssembly Implementation | Status |
| :--- | :--- | :---: |
| Arithmetic & Bitwise | Direct Wasm numeric opcodes (`i32.add`, `i64.mul`, `f32.sub`, …) | ✅ |
| Structured Control Flow | `block`, `loop`, `br`, `br_if`, `if/else` | ✅ |
| Structs & Mut Receivers | Shadow stack & linear memory pointer offsets | ✅ |
| Dynamic Slices (`[]T`) | Arena-backed fat pointer header `{ptr, len, cap}` | ✅ |
| Regional Arenas (`arena`) | Global `__arena_pointer` recorded on entry, unwound on exit | ✅ |
| Tagged Enums & Matching | Discriminant tag header + payload union + branch jump table | ✅ |
| Monomorphized Generics | Specialization into concrete zero-overhead Wasm functions | ✅ |
| Dynamic Trait Dispatch | Page 0 vtables + fat pointer `{vtable_ptr, data_ptr}` + `call_indirect` | ✅ |
| Optionals & Results | Discriminated memory wrappers `{has_val/is_ok, payload}` | ✅ |

## What Ube Is Not

Worth stating explicitly, because the surface syntax invites the wrong expectations:

- **Not a general-purpose replacement for Rust or C++.** If you need `unsafe`, raw pointers, SIMD intrinsics, or hand-tuned allocation, Ube deliberately forbids all of it. That is a design choice, not a gap.
- **Not a scripting language.** There is no REPL, no eval, no dynamic dispatch by name.
- **Not finished.** Networking and WebGPU support are planned but not shipped; `std/net` and `std/http` do not yet exist.

---

## The Memory Model in One Page

Ube has three storage tiers and no garbage collector. Every allocation belongs to exactly one tier, decided by its lexical position:

```
┌──────────────────────────────────────────────────────────────────────────┐
│  TIER 1 — STACK VALUES                                                   │
│  Primitives (i32, f32, bool), fixed-size arrays ([N]T).                  │
│  Push/pop with function frame activation on the Wasm stack.              │
├──────────────────────────────────────────────────────────────────────────┤
│  TIER 2 — REGIONAL BUMP ARENAS  (arena { ... })                          │
│  Dynamic slices ([]T), string concatenation, scratch buffers.            │
│  • O(1) bump allocation.                                                 │
│  • Scope exit reclaims the entire scratch region in one instruction.     │
│  • Escaping data must be rescued with promote().                         │
├──────────────────────────────────────────────────────────────────────────┤
│  TIER 3 — GENERATIONAL ENTITY POOLS  (Pool[T])                           │
│  Long-lived entities, game state, dynamic graphs.                        │
│  • O(1) slot allocation returning a generational Handle[T].              │
│  • Despawn invalidates stale handles; get() returns none.                │
└──────────────────────────────────────────────────────────────────────────┘
```

Two invariants fall out of this and are enforced at compile time:

1. **The upward reference invariant** — an inner arena cannot assign a slice or string to a variable declared in an outer region, cannot call mutating methods on outer slices, and cannot return unpromoted arena data. Data crosses the boundary only through `promote(x)`, which deep-copies into the parent arena.
2. **The flat invariant** — values crossing a `spawn` boundary or travelling through a `Chan[T]` must be *flat*: primitives, fixed arrays of flat types, or structs/enums whose fields are all flat. `string`, `[]T`, `StringBuilder`, and `Pool[T]` are rejected. This is what makes `spawn` data-race-free by construction — there is no shared mutable heap between tasks.

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

> **Note:** `ube run` embeds a Wasmtime runtime via Cranelift JIT. If the embedded runtime is unavailable on your platform, `run` falls back to a `wasmtime` binary on `$PATH`.

---

## Language Tour & Syntax Reference

### 1. Variables, Mutability & Types

Bindings are **strictly immutable by default**. Use `let mut` for mutable bindings:

```ube
package main

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

- **Signed Integers**: `i8`, `i16`, `i32`, `i64`
- **Unsigned Integers**: `u8`, `u16`, `u32`, `u64`
- **Floats**: `f32`, `f64`
- **Boolean**: `bool` (`true`, `false`)
- **String**: `string` (UTF-8 byte sequence, arena-allocated)
- **Void**: `void` (return type for procedures)

The literal `null` is forbidden — use `none` for optionals. The keyword `unsafe` is rejected by the lexer outright.

---

### 2. Affine Move Semantics (Ownership)

Non-copy types (`string`, `[]T`, `Pool[T]`, and structs holding them) move ownership on assignment:

```ube
package main

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

The complete rule set:

1. **Move on assignment** — assigning a non-copy value transfers ownership.
2. **Reactivation** — assigning a fresh value into a previously moved mutable variable makes it usable again.
3. **Move on argument pass** — passing a non-copy value consumes it, unless the parameter is declared `mut` (a loan).
4. **Loop boundary invariant** — a variable declared outside a loop cannot be moved inside the loop body, preventing use-after-move on subsequent iterations.
5. **No projection consumption** — accessing a sub-field or index (`p.name`, `s[0]`) does not consume the root aggregate.

---

### 3. Functions, Receivers & Methods

```ube
package main

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
package main

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

Promoted field access works through arbitrary embedding depth and is resolved statically.

---

### 5. Tagged Enums & Pattern Matching

Enums are tagged unions carrying arbitrary payloads. Pattern matching is verified for exhaustiveness at compile time:

```ube
package main

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

### 6. Error Handling & Optionals

No exceptions and no hidden runtime panics. Errors and absence are both first-class values:

```ube
package main

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

`?` propagates a `Result` error to the caller. `or_else` unwraps either an optional or a result to a fallback value. `if let` unwraps either into a scoped binding.

---

### 7. Strings, Slicing & StringBuilder

```ube
package main

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

`StringBuilder` is arena-backed. Its growth and conversion are compiled to direct Wasm instructions rather than library calls.

---

### 8. Arrays, Slices & Coercion

- **Fixed Arrays (`[N]T`)**: stack-allocated, fixed length, copyable if the element type is copyable.
- **Dynamic Slices (`[]T`)**: arena-backed, dynamically grown via geometric resizing.
- **Coercion**: fixed arrays coerce automatically to dynamic slice parameters.

```ube
package main

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

### 9. Regional Bump Arenas

Allocations inside an `arena` block are wiped instantly by unwinding the arena pointer on scope exit:

```ube
package main

fn main() {
    let mut persistent = []i32{}

    arena {
        let mut scratch = []i32{}
        scratch.push(10)
        scratch.push(20)

        // Rescues data from the scratch arena into the parent arena:
        persistent = promote(scratch)
    } // Entire child arena reclaimed in 1 instruction

    println(persistent.len) // 2
    println(persistent[0])  // 10
}
```

Assigning `scratch` directly to `persistent` without `promote` is a compile error — that is the upward reference invariant from the memory model above. `promote` performs a deep copy into the parent arena.

---

### 10. Generational Entity Pools

```ube
package main

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

    pool.despawn(h1) // h1 is now stale; get(h1) returns none
}
```

Handles carry a generation counter, so a stale handle can never read a recycled slot.

---

### 11. Concurrency, Tasks & Channels

```ube
package main

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

Note the flat invariant: `Packet` here contains only `i32` fields, so it is legal to send through `Chan[Packet]`. A struct holding a `string` would be rejected at compile time. That restriction is what removes locks and data races from the language rather than merely discouraging them.

---

### 12. Generics & Trait Bounds

Compile-time monomorphization into zero-overhead specialized Wasm code:

```ube
package main

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

Each distinct combination of type arguments produces a concrete specialized function and struct. There is no runtime type dictionary and no boxing.

---

### 13. Traits & Dynamic Dispatch

```ube
package main

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

Trait values are fat pointers `{vtable_ptr, data_ptr}` dispatched via `call_indirect`.

---

### 14. Sandboxed Host Imports

```ube
package main

extern fn time_now_ms() -> i64
extern fn host_print(s: string)

fn main() {
    let start = time_now_ms()
    host_print("Hello from WebAssembly!")
    println(time_now_ms() - start)
}
```

All host access is capability-driven and must be declared as a typed `extern fn` import. There is no ambient access to memory, file descriptors, or the environment.

---

## Packages, Standard Library & Imports

Ube uses Go's package model: **one directory is one package**, and every `.ube` file declares that package on its first line.

```ube
package main

fn main() {
    println("hello from the main package")
}
```

The rules are deliberately explicit:

- Every `.ube` file must declare `package <name>`. Comments and blank lines may come before the clause, but it is mandatory.
- Every `.ube` file in a directory must use the same package name. Mixing package names in one directory is a compile error.
- Files in the same package share one scope. Declarations do not need imports to see each other, and unexported declarations are visible across the whole package.
- `pub` exports a declaration outside its package. Unexported structs, fields, enums, traits, functions, and methods are private to the package.
- `package main` is the executable package. It must define `fn main()`. Importing a package that declares `package main` is rejected.
- Imports reference package directories, not files. `import "../geometry"` is valid; `import "../geometry/geometry.ube"` is rejected.
- The default qualifier is the imported package's declared name. `as` changes only the local alias. Relative imports resolve from the importing package's directory.

A library package and its consumer:

```ube
// geometry/geometry.ube
package geometry

pub struct Rect {
    pub width: i32,
    pub height: i32
}

pub fn area(r: Rect) -> i32 {
    return r.width * r.height
}

fn hidden_helper() -> i32 {
    return 0 // visible to every file in package geometry, invisible outside it
}
```

```ube
// main.ube
package main

import "../geometry" as geo

fn main() {
    let r = geo.Rect{ width: 10, height: 5 }
    println(geo.area(r)) // 50
}
```

The standard library uses the same rules. It is bundled directly into the compiler binary and resolved with the priority order `$UBE_PATH` → `./std/` → `std/` next to the entry file → embedded. Each `std/<name>` directory declares `package <name>`.

```ube
package main

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
| `std/strings` | Index-of, contains, prefix/suffix inspection, trimming, splitting, padding, repeating. |
| `std/bytes` | `ByteBuffer` with `write_u8`, `write_bytes`, `get`, `to_slice`. |
| `std/collections` | Generic `Stack[T]` (with `next()` iterator) and `SimpleMap[K, V]`. |
| `std/io` | `Reader`/`Writer`/`Seeker`/`Flusher` traits; `Stdin`, `Stdout`, `Stderr`, `File`, `BytesReader`, `StringReader`, `BufReader`, `BufWriter`, `BytesWriter`. |
| `std/fs` | `Path`, `OpenOptions`, `File`, `Metadata`, `DirEntry`; read/write/create/remove helpers. |
| `std/wasi` | Raw preview-1 WASI imports and fd/errno constants. |

---

## Developer CLI Toolchain

| Command | Example | Description |
| :--- | :--- | :--- |
| `ube build` | `ube build` / `ube build -o app.wasm` | Compiles the current package (or a directory/file target) into a standalone WebAssembly module. Directory targets default to `<dirname>.wasm`; file targets default to `<stem>.wasm`. |
| `ube run` | `ube run` / `ube run path/to/pkg` | Compiles and executes a `package main` via the embedded Wasmtime runtime. Bare `ube <dir>` is shorthand for `ube run <dir>`. |
| `ube check` | `ube check` / `ube check path/to/pkg` | Static audit of syntax, types, affine moves, package imports, and arena invariants. Defaults to the current directory. |
| `ube test` | `ube test` / `ube test ./...` | Runs `test_*` functions per package directory, including all `_test.ube` files in that package. `./...` walks nested packages. |
| `ube fmt` | `ube fmt -w .` | Canonical formatter with automatic struct field columnar alignment. |
| `ube doc` | `ube doc main.ube [sym]` | Prints signatures, fields, and method documentation. |
| `ube flow` | `ube flow main.ube` | Renders a visual call tree showing memory scopes, tasks, and callees. |
| `ube sig` | `ube sig` | Extracts a zero-body symbol map of the project. |
| `ube build --ai` | `ube build --ai main.ube` | Compiles, but emits dense one-line diagnostics on error. |
| `ube test --ai` | `ube test --ai tests/` | Runs tests, emitting dense one-line diagnostics for failing checks. |
| `ube lsp` | `ube lsp` | Starts the Language Server Protocol for VS Code and Zed. |

---

## Working With LLMs

Ube's compiler emits two things an LLM cannot infer from reading the code, and delegates everything else to [Textify](https://github.com/JohnEsleyer/textify).

### What Ube provides

- **`ube sig`** — a zero-body symbol map: structs, enums, traits, receiver methods, externs. No function bodies, no comments, no constants. Typically a few hundred tokens for an entire project. This is the orientation step for a model that has never seen the codebase.
- **`--ai` on `build` and `test`** — dense one-line-per-error diagnostics in the form:

  ```
  ERR|file.ube:LINE:COL|class|k=v|k=v
  ```

  Example: `ERR|player.ube:45:12|type_mismatch|expected:i32|got:string`. Class and key-value fields are inferred from the compiler message; unrecognized messages fall back to `class=error`. Twenty to forty tokens replace a hundred-line trace dump.

### What Textify provides

Context extraction (`<trace>`, `<select>`, `<xref>`, `<feature-context>`), atomic multi-file patching with a two-pass VFS, clipboard I/O, and system prompts. These are language-agnostic text operations and belong in a separate tool.

### The loop

1. **Orient.** Run `ube sig`. Paste the output into the model's first message.
2. **Request.** Let the model ask for context. Run `textify load -ci -c` to read the model's `<request>` block from the clipboard and copy the extracted context back.
3. **Patch.** Run `textify patch -ci` to apply the model's edits atomically. If any anchor fails, no file is modified.
4. **Verify.** Run `ube build --ai` or `ube test --ai`. The dense diagnostic goes back to the model in about twenty tokens and names the offending variable and the compiler's hint.

The dense format is the one interface Ube promises to keep stable. Treat it as such: if a diagnostic is unrecognized, it degrades to `class=error` with no fields, which is worse than the verbose form for a model but still better than a raw stack trace.

---

## Editor Integration (VS Code & LSP)

Official VS Code language support is in `editors/vscode/`:

- **Syntax Highlighting**: keywords, affine decorators, format strings, tagged enums.
- **LSP Integration**: hover tooltips, go-to-definition, document symbols, signature help, completion, and diagnostics powered by `ube lsp`.
- **On-Save Formatting**: integrated with `ube fmt` and struct field column alignment.
- **Icon Theme**: a purple Ube jam jar icon for `.ube` files.

The extension directory contains step-by-step installation instructions for the symlink, copy, and `.vsix` methods, including the Snap and Flatpak caveats.

---

## Running the Test Suite

```sh
# Compiler unit tests (Go test suite)
go test -v ./...

# Ube language integration tests via the CLI runner
ube test tests/
```

---

## License

Distributed under the MIT License. See [LICENSE](LICENSE) for details.
