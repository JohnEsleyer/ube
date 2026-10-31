
<div align="center">

<img src="editors/vscode/icons/ube.svg" width="120" height="120" alt="Ube Logo">

# The Ube Programming Language

**The sweet, grounded systems language.**  
*Easy to program like Go. Fast like C. Zero Garbage Collection.*

**Latest version: `0.5.6`**

</div>

---

## 🍠 Overview

Ube is a modern, statically typed systems programming language designed to bridge the gap between developer ergonomics and bare-metal performance. It combines the readable, approachable syntax and concurrency model of Go with the pause-free speed, deterministic memory layout, and zero runtime overhead of ANSI C99.

Ube delivers **Zero Garbage Collection (GC)** without requiring manual `malloc`/`free` or complex Rust-style lifetime annotations. It is built for real-time game engines, CLI tools, embedded devices, high-throughput network services, and performance-critical systems.

---

## 🚀 Installation

### Automated Install Script
```sh
curl -sSL https://raw.githubusercontent.com/JohnEsleyer/ube/main/install.sh | bash
```

### Build from Source
Requires Go 1.18+ and a standard C compiler (`gcc` or `clang`):
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

## 🧁 Compilation Pipeline

1. **Frontend**: Lexing, parsing, monomorphization of generics, visibility checks, affine ownership tracking, disjoint mutable-loan checking, and regional escape analysis.
2. **C Code Generation**: Emits clean, deterministic ANSI C99 code (`.c`).
3. **Native Compilation**: Invokes system `clang` or `gcc` with `-O2` optimization to produce standalone, dependency-free executables.

---

## 🧠 Memory Architecture: Zero GC

Ube operates without a tracing garbage collector or reference counting. Memory safety and reclamation are achieved through three complementary mechanisms:

```
┌───────────────────────────────────────────────────────────┐
│                    Stack Allocations                      │
│        Primitives (i32, f32), Fixed Arrays ([N]T)         │
└───────────────────────────────────────────────────────────┘
                            │
┌───────────────────────────▼───────────────────────────────┐
│              Regional Bump Arenas (arena { })             │
│   Strings, Slices ([]T), StringBuilders, Scratch Memory   │
│   - Instant reclamation on exit (1 CPU instruction)       │
│   - Explicit promotion across boundaries (promote(x))     │
└───────────────────────────────────────────────────────────┘
                            │
┌───────────────────────────▼───────────────────────────────┐
│             Generational Entity Pools (Pool[T])           │
│   Long-lived entities, game objects, graph structures     │
│   - Generational index handles (Handle[T])                │
│   - Linear static consumption prevents use-after-free     │
└───────────────────────────────────────────────────────────┘
```

---

## 🛡️ Core Compiler Rules

Ube guarantees static memory safety without lifetime annotations by enforcing a small, predictable set of compile-time rules:

### 1. Copy Types vs. Affine Types
* **Copy Types** (`i8`–`i64`, `u8`–`u64`, `f32`, `f64`, `bool`, raw pointers `*T`):  
  Bitwise copied on assignment or argument pass. May be read and copied repeatedly.
* **Affine Types** (`string`, slices `[]T`, structs with non-copy fields, `Pool[T]`, `Chan[T]`):  
  Have a single unique owner. Moving or passing them transfers ownership.

### 2. Move on Assignment
Assigning or initializing from an owned affine variable marks the source **statically dead**:
```ube
let a = "Ube"
let b = a          // 'a' is moved to 'b'
// println(a)      // COMPILE ERROR: use of moved variable 'a'
println(b)         // OK
```
*Reassignment* reactivates a mutable variable:
```ube
let mut s = "first"
let other = s      // 's' is moved
s = "second"       // Reassignment reactivates 's'
println(s)         // OK: prints "second"
```

### 3. Disjoint Mutable Loans (`mut`)
Mutable function parameters and mutable receivers (`mut`) create an exclusive loan. The caller must explicitly mark the argument with `mut`, and aliasing the same variable across arguments in the same call is statically rejected:
```ube
fn transfer(mut dst: Counter, mut src: Counter, amt: i32) { ... }

// COMPILE ERROR: aliasing violation (cannot loan 'c' mutably twice):
// transfer(mut c, mut c, 10)

// OK: disjoint variables
transfer(mut c1, mut c2, 10)
```

### 4. Optional Unwrapping for Safety
Functions and methods that might fail return an optional `T?` (`null` or `T`):
* `slice.pop()` returns `T?`
* `pool.get(handle)` returns `T?`
Raw optionals cannot be printed or accessed directly. You must unwrap them via `if let` or `or_else`:
```ube
if let item = items.pop() {
    println(item)
}
let fallback = items.pop() or_else 0
```

### 5. Linear Generational Pool Handles
Calling `pool.free(h)` consumes the handle `h`. Any subsequent call to `pool.get(h)` or `pool.free(h)` is rejected at compile time as a use-after-move error.

### 6. Sandboxed Unsafe & Foreign Code
Foreign C declarations (`extern fn`) and raw pointer dereferences (`*ptr`) **must** reside inside an `unsafe { ... }` block. Calling an `extern fn` from safe code produces a compiler error.

---

## 📖 Complete Syntax & Language Tour

### 1. Variables & Types
```ube
// Immutable bindings
let year: i32 = 2025
let ratio: f32 = 3.14        // Floating-point literals default to f32
let flag: bool = true
let name: string = "Ube"

// Mutable bindings
let mut total = 0
total += 10
total *= 2
println(total) // 20
```

### 2. Control Flow
```ube
// If-else
if total > 50 {
    println("High")
} else if total == 20 {
    println("Exact")
} else {
    println("Low")
}

// While loops
let mut i = 0
while i < 10 {
    i += 1
    if i == 2 { continue }
    if i == 5 { break }
}

// For-in loops over ranges and collections
for idx in 0..5 {
    println(idx) // 0, 1, 2, 3, 4
}
```

### 3. Functions & Methods
```ube
struct Player {
    score: i32
}

// Regular function
fn add(a: i32, b: i32) -> i32 {
    return a + b
}

// Read-only value receiver
fn (p Player) get_score() -> i32 {
    return p.score
}

// Mutable receiver (modifies caller)
fn (mut p Player) add_points(pts: i32) {
    p.score += pts
}
```

### 4. Structs & Encapsulation
The `pub` keyword exports structs, fields, and functions to importing modules:
```ube
pub struct Config {
    pub port: i32,
    secret_key: string // Private to this module
}
```

### 5. Tagged Enums & Exhaustive Matching
Enums support data payloads and must be exhaustively matched using `=>`:
```ube
enum Shape {
    Circle(f32),
    Rect(f32, f32),
    Point
}

fn describe(s: Shape) {
    match s {
        Circle(r) => {
            print("Circle radius: ")
            println(r)
        }
        Rect(w, h) => {
            print("Area: ")
            println(w * h)
        }
        Point => {
            println("Point at origin")
        }
    }
}
```

### 6. Arrays & Slices
* Fixed stack arrays: `[N]T` (e.g. `[3]i32{1, 2, 3}`). Fixed arrays automatically coerce to `[]T` when passed to slice parameters.
* Dynamic arena slices: `[]T` with `.push()`, `.pop()`, `.len`, and `.cap`.
* Slicing sub-ranges uses colon notation: `slice[start:end]`.
```ube
let mut items = []i32{}
items.push(10)
items.push(20)
items.push(30)

println(items[0])       // 10
let sub = items[0:2]    // sub-slice of first 2 elements
let last = items.pop() or_else 0
println(last)           // 30
```

### 7. Optionals (`T?`) & Results (`T ! E`)
```ube
// Optional return
fn lookup(id: i32) -> string? {
    if id == 1 { return "Admin" }
    return null
}

// Result return with error handling
fn parse_port(code: i32) -> i32 ! string {
    if code <= 0 { return error("invalid port") }
    return code
}

fn boot() -> bool ! string {
    // '?' propagates error to caller
    let port = parse_port(8080)?
    println(port)
    return true
}

fn main() {
    // Unwrapping with `or_else`
    let role = lookup(99) or_else "Guest"
    println(role)

    // Unwrapping with `if let`
    if let user = lookup(1) {
        println(user)
    }
}
```

### 8. Regional Memory Arenas (`arena` & `promote`)
Arena allocations are rewound in a single instruction upon exiting the block:
```ube
fn main() {
    let mut persistent = []i32{}

    arena {
        let mut scratch = []i32{}
        scratch.push(100)
        scratch.push(200)

        // Copies data into the parent arena before child wipes clean
        persistent = promote(scratch)
    } // scratch arena is instantly freed here

    println(persistent.len) // 2
}
```

### 9. Generational Handle Pools (`Pool[T]`)
For entity graphs and games where lifecycles are non-hierarchical:
```ube
struct Bullet {
    damage: i32
}

fn main() {
    let mut pool = Pool[Bullet](128)

    // alloc returns a Handle[Bullet]
    let h1 = pool.alloc(Bullet{damage: 50})

    // get() returns Bullet? (optional) for generational safety
    if let b = pool.get(h1) {
        println(b.damage) // 50
    }

    // free() linearly consumes h1
    pool.free(h1)
    // pool.get(h1) -> rejected at compile time: use of moved variable
}
```

### 10. Concurrency & Channels (`spawn` & `select`)
Ube uses shared-nothing concurrency. Message passing transfers ownership across threads:
```ube
fn worker(ch: Chan[i32], done: Chan[bool]) {
    while let msg = ch.recv() {
        println(msg)
    }
    done.send(true)
}

fn main() {
    let ch = Chan[i32].new(10)
    let done = Chan[bool].new(1)

    spawn worker(ch, done)

    ch.send(42)
    ch.close()

    // Multiplexing channels with select
    select {
        is_done = done => {
            println("Worker finished successfully.")
        }
        default => {
            println("Worker is still busy...")
        }
    }
}
```

### 11. Resource Cleanup (`defer`)
Defers execution until the enclosing function or loop iteration exits:
```ube
fn demo() {
    defer println("Exiting demo function")

    let mut i = 0
    while i < 3 {
        defer println("Loop step complete")
        i += 1
    }
}
```

### 12. Low-Level Control (`unsafe` & `extern`)
```ube
extern fn abs(n: i32) -> i32
extern fn sinf(rad: f32) -> f32

fn main() {
    let mut val: i32 = 42

    unsafe {
        // Calling foreign C symbols
        println(abs(-100))    // 100
        println(sinf(0.0))    // 0.0

        // Taking addresses and dereferencing pointers
        let ptr: *i32 = &val
        let read_val = *ptr
        println(read_val)     // 42
    }
}
```

---

## ⚡ Language Cheatsheet

| Construct | Syntax | Notes |
| :--- | :--- | :--- |
| **Variable (Immutable)** | `let x = 10` | Type inferred (`i32`) |
| **Variable (Mutable)** | `let mut x = 10` | Allows reassignment & compound ops |
| **Float Literal** | `let f: f32 = 3.14` | Decimals default to `f32` |
| **Fixed Array** | `let a: [3]i32 = [3]i32{1, 2, 3}` | Stack-allocated, auto-coerces to `[]T` |
| **Dynamic Slice** | `let mut s = []i32{}` | Arena-backed, growable (`push`, `pop`) |
| **Sub-Slice** | `let sub = s[0:2]` | Uses colon `[start:end]` |
| **Slice Pop** | `let val = s.pop() or_else 0` | `.pop()` returns optional `T?` |
| **Optional Type** | `T?` (`let x: i32? = null`) | Either `T` or `null` |
| **Result Type** | `T ! E` (`return error("msg")`) | Typed success or error |
| **Error Propagate** | `let v = parse()?` | Bubbles error out of function |
| **Fallback Default** | `let v = opt or_else fallback` | Unwraps optional or result |
| **If-Let Unwrap** | `if let v = opt { ... }` | Scope-binds unwrapped value |
| **While-Let Loop** | `while let item = ch.recv() { ... }` | Loops while optional is not `null` |
| **Loop Range** | `for i in 0..5 { ... }` | Iterates `0` up to `4` |
| **Enum Match** | `match val { Arm => { ... } }` | Exhaustive, separated by `=>` |
| **Arena Block** | `arena { let x = promote(y) }` | Bump allocated, freed in 1 cycle |
| **Channel Make** | `let ch = Chan[i32].new(cap)` | Thread-safe bounded channel |
| **Channel Select** | `select { v = ch => { } default => { } }` | Event multiplexing with `=>` |
| **Pool Alloc** | `let mut p = Pool[T](64)` | Generational handle pool |
| **Pool Get** | `if let e = p.get(h) { ... }` | Returns optional `T?` |
| **Pool Free** | `p.free(h)` | Linearly invalidates handle `h` |
| **Unsafe Block** | `unsafe { let ptr: *T = &val }` | Required for `*T` and `extern fn` |
| **Print (inline)** | `print("Hello ", name)` | No trailing newline |
| **Print (line)** | `println(val)` | Appends trailing newline |

---

## 🛠️ Developer CLI Workflow

| Command | Usage | Description |
| :--- | :--- | :--- |
| `ube run` | `ube run main.ube` | Compiles and executes in-memory. |
| `ube build` | `ube build main.ube -o app` | Builds an optimized native executable (`-O2`). |
| `ube test` | `ube test tests/` | Runs all test files or `test_*` functions. |
| `ube check` | `ube check main.ube` | Audits syntax, ownership, and types without compiling. |
| `ube fmt` | `ube fmt .` | Canonical auto-formatter for all `.ube` files. |
| `ube doc` | `ube doc main.ube` | Generates documentation for public exports. |
| `ube lsp` | `ube lsp` | Runs Language Server Protocol for editors. |
| `ube ai guide` | `ube ai guide` | Outputs the complete formal language guide for LLMs. |
| `ube ai context`| `ube ai context . -o ctx.txt`| Generates codebase architecture context for LLMs. |

---

## 📄 License

Distributed under the MIT License. See [LICENSE](LICENSE) for details.
