<div align="center">

# 🍠 Ube Programming Language

**The sweet, grounded systems language.**  
*Easy to program like Go. Fast like C. Zero Garbage Collection.*

[![License: MIT](https://img.shields.io/badge/License-MIT-purple.svg)](LICENSE)
[![Status: Prototype](https://img.shields.io/badge/Status-Active_Prototype-brightgreen.svg)]()
[![Target: C99_WASM](https://img.shields.io/badge/Target-C99%20%7C%20WASM%20%7C%20Embedded-blue.svg)]()

</div>

---

## 1. What is Ube?

**Ube** is a statically typed, compiled systems language designed for high developer happiness, blazing speed, and universal portability.

It solves a fundamental dilemma in modern software engineering:

* **Go** is wonderfully productive, but its **Garbage Collector (GC)** causes unpredictable latency spikes, wastes memory, and makes it unsuitable for microcontrollers and hard real-time systems.
* **Rust** gives high performance without GC, but its **borrow checker and explicit lifetime annotations (`'a`)** impose a brutal cognitive overhead that slows down feature delivery.
* **C/C++** is everywhere, but lacks memory safety and modern ergonomics.

**Ube gives you Go's simplicity and developer ergonomics, but without a Garbage Collector.**

---

## 2. The Core Pillars

| Pillar | How Ube Achieves It |
| :--- | :--- |
| 🚀 **Fast as C/Rust** | No GC pauses, contiguous memory layouts, monotone bump growth, and single-store deallocation. |
| 🧁 **Easy as Go** | Minimalist syntax, Go-style receiver functions, fast compilation, and **zero lifetime annotations**. |
| 🛡️ **Memory Safe** | Eliminates use-after-free, double-free, and dangling pointers via static root checks and scoped Tubers. |
| 🌍 **Runs Everywhere** | Transpiles to clean ANSI C99/C23, giving day-one support for **Desktops, WebAssembly (WASM), and Microcontrollers (ESP32, STM32, RISC-V)**. |
| ⚡ **Zero-Cost C Interop** | Since Ube data is flat and contiguous, passing slices and structs to existing C libraries (`sqlite`, `raylib`, `curl`) requires **zero overhead and no glue code**. |

---

## 3. The Memory Model: "Tubers"

Ube rejects both runtime garbage collection and manual `malloc`/`free`. Instead, it uses **Tubers** (scoped regional bump allocators), adapted for portable 32-bit and 64-bit devices:

```
+-------------------------------------------------------------------+
|  Tuber Page (Default 64 KB, matches WASM Page Size)               |
+-------------------------------------------------------------------+
| [Fixed Stack Data] | [Dynamic Slice Items ...] -> (bumps top)     |
+-------------------------------------------------------------------+
                     ^                                              ^
                 Watermark                                      Tuber Top
```

### 1. Single-Store Deallocation
Every function and lexical block saves a 64-bit `watermark` representing the current top of the Tuber. When the scope exits, deallocating an array with a million elements takes **one CPU instruction**:
```c
tuber.top = watermark; // All memory reclaimed instantly!
```

### 2. References Stay Valid Across Growth
In Rust, calling `vec.push()` invalidates all existing references into that vector.  
In Ube, arrays grow monotonically inside the Tuber without moving existing items. **References to elements inside an array remain valid even as you push more items to it.**

### 3. Caller-Allocated Returns
Instead of using escape analysis to dump returned values onto a GC heap (like Go does), Ube’s compiler automatically redirects function return allocations into the **caller's Tuber**. Returning dynamic collections is 100% zero-copy.

---

## 4. Syntax at a Glance

### Clean, Predictable, and Safe

```ube
package main

import "std/fmt"

struct User {
    id:    u32,
    name:  string,
    email: string?, // Nullable/Optional (no nil pointer crashes!)
}

// Go-style receiver method
fn (u User) greeting() -> string {
    return "Hello, " + u.name
}

fn main() {
    // Dynamic slice allocated on the local Tuber scratchpad
    let mut team = []User{}

    team.push(User{ id: 1, name: "Maria", email: null })
    team.push(User{ id: 2, name: "Jose",  email: "jose@ube.dev" })

    // Safe reference: stays valid even after future pushes!
    let leader = &team[0]

    for member in team {
        fmt.println(member.greeting())
    }

    // Isolated memory scratchpad
    tuber {
        let mut temp_data = []i32{}
        temp_data.push(100)
        temp_data.push(200)
        // do heavy processing...
    } // <-- temp_data memory reclaimed instantly in ONE instruction!
}
```

### Better Error Handling than Go (`?` Operator)

No more repetitive `if err != nil` blocks:

```ube
fn load_config(path: string) -> Config ! fs.Error {
    let file = fs.open(path)?   // Bubbles up errors automatically
    let data = file.read_all()?
    return parse_json(data)
}

fn main() {
    let cfg = load_config("config.json") or_else {
        fmt.println("Using default configuration")
        return Config.default()
    }
}
```

---

## 5. Architectural Pipeline

Ube uses a high-velocity transpiler architecture targeting ANSI C99 as universal intermediate representation:

```
┌─────────────────┐
│ Ube Source File │ (*.ube)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   ubec Parser   │ (Lexer, AST Builder, Typechecker)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Root Analysis  │ (Proves safe lifetimes & Tuber assignments)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ C99 Transpiler  │ (Emits clean, readable output.c)
└────────┬────────┘
         │
         ├───────────────────────────────┬───────────────────────────────┐
         ▼                               ▼                               ▼
┌─────────────────┐             ┌─────────────────┐             ┌─────────────────┐
│ Clang / GCC     │             │ Clang WASM Target│            │ Embedded GCC    │
│ (Desktop/Server)│             │ (Browser/WASI)  │             │ (ESP32 / ARM)   │
└─────────────────┘             └─────────────────┘             └─────────────────┘
```

---

## 6. Implementation Roadmap

### Phase 1: The Core Foundation (Current Milestone)
- [x] Minimal Tuber runtime (`ube_runtime.h`) with watermark save/restore.
- [x] End-to-end prototype compiler emitting C (`ubec.py`).
- [x] Basic types: `i32`, `bool`, and dynamic slices `[]i32`.
- [x] Monotone `push()` semantics and `tuber { ... }` scoped memory resets.

### Phase 2: Ergonomics & Object Model
- [x] Go-style receiver functions: `fn (s Slice) sum() -> i32`.
- [x] Struct declarations and field access.
- [x] Tuber-allocated `string` values: literals, `+` concatenation, printing.
- [ ] Implicit caller-allocated return values (`fn make_list() -> []i32`).

### Phase 3: Error Handling & Modern Types
- [x] Result types (`i32 ! string`) with error bubbling via the `?` operator.
- [ ] Null-safe Optionals (`T?`) eliminating null pointer panics.
- [ ] Tagged Unions / Enums (`enum Shape { Circle(f32), Rectangle(f32, f32) }`).
- [ ] Pattern matching (`match` expressions).

### Phase 4: Standard Library & Concurrency
- [ ] Standard I/O, File System, and Math modules.
- [ ] Zero-copy C foreign function interface (`extern fn`).
- [ ] Vines (lightweight coroutines/fibers) with thread-safe flat channel queues.

### Phase 5: Self-Hosting & Native Tooling
- [ ] Rewrite `ubec` in Ube itself (Self-hosting compiler).
- [ ] Instant-compilation dev workflow via in-process TinyCC (TCC).
- [ ] Direct WebAssembly (`.wasm`) generation mode.

---

## 7. Quickstart (Testing the Prototype)

### Prerequisites
* Python 3.8+ (for running the compiler prototype)
* Any C compiler (`gcc`, `clang`, or `tcc`)

### 1. Clone the Repository
```bash
git clone https://github.com/your-username/ube.git
cd ube
```

### 2. Compile an Ube Program
```bash
python3 ubec.py test_v2.ube    # structs, receiver methods, `?`
```
Also try `demo.ube` (slices and Tuber scopes) and `test_strings.ube`
(Tuber-allocated strings and concatenation).

### 3. Build & Run the Native Binary
```bash
gcc -O2 output.c -o ube_app
./ube_app
```

---

## 8. Guiding Principles

1. **Pragmatism Over Purity:** If a feature makes the compiler slightly more complex but saves the developer hours of frustration, we implement it.
2. **Explicit Intent, Implicit Safety:** Mutability is explicit (`let mut`), but memory cleanup is implicit and guaranteed.
3. **No Hidden Costs:** No invisible allocations, no background GC threads, no stop-the-world pauses.
4. **Small Conceptual Surface:** You should be able to learn the entire language syntax and semantics over a weekend.

---

## 9. License

Ube is open-source software licensed under the [MIT License](LICENSE).
