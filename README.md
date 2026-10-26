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

> **Prototype note:** the current compiler only implements `[]i32` slices and does not yet expose reference types (`&T`). This subsection describes the intended invariant, not a feature you can use today.

### 3. Caller-Allocated Returns
Instead of using escape analysis to dump returned values onto a GC heap (like Go does), Ube’s compiler automatically redirects function return allocations into the **caller's Tuber**. Returning dynamic collections is 100% zero-copy.

A callee that returns `[]i32` simply does not restore its watermark: everything it built stays above the caller’s watermark, so the caller receives a live slice and frees it with its own scope exit.

> **Prototype limit:** only one slice may be growing on a Tuber at a time. While a slice returned from a callee is live, do not `push` to another slice in the same scope; the production design enforces this invariant at compile time.

### 4. Vines: Shared-Nothing Concurrency

A **Vine** is Ube’s unit of concurrency. Every Vine owns a **private Tuber**, reached through thread-local storage, so no Vine can ever hold a pointer into another Vine’s scratch memory. Data crosses Vine boundaries only through typed channels:

```ube
struct Job { id: i32, weight: i32 }

fn worker(jobs: Chan[Job], results: Chan[i32]) {
    let job = jobs.recv()
    results.send(job.id * job.weight)
}

fn main() {
    let jobs = Chan[Job].new(8)
    let results = Chan[i32].new(8)

    spawn worker(jobs, results)

    jobs.send(Job{ id: 1, weight: 10 })
    jobs.close()
    println(results.recv())
}
```

A channel copies each value with `memcpy` into a runtime-owned ring buffer, and the receiving Vine reads its own bytes into its own Tuber. `Chan[T]` is therefore restricted at compile time to **flat** types: primitives, channel handles, and structs or enums built only from flat fields. `string`, slices, and borrowed references are rejected before any C is generated:

```text
[ubec]: error: cannot pass non-flat type 'string' across Vines (Chan[string])
```

That single rule replaces Rust’s `Send`/`Sync` trait machinery: with no pointers crossing the boundary, data races are impossible by construction. The prototype backend maps each spawned Vine to a detached pthread with its own TLS Tuber. `select`, channel timeouts, and alternative schedulers (WASM event loop, cooperative fibers or FreeRTOS tasks on microcontrollers) are roadmap items.

> **Prototype limits:** `recv` is blocking and yields a zero value after `close`; the optional/`while let` machinery for clean closed-channel loops is not implemented yet. Channel ring buffers are never freed, and a detached Vine must be kept alive by the program’s own send/receive handshake.

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
    // Inline fallback: use a default when the result is an error.
    let cfg = load_config("config.json") or_else Config.default()
}
```

### Null Safety Without Null Pointers

```ube
let present: i32? = 777
let missing: i32? = null

if let val = present {
    println(val)                 // runs only when a value exists
}

if let val = missing {
    println(val)                 // never runs
}

let safe = missing or_else 0     // unwrap with a default
```

`T?` is a tagged value (`has_val` plus the value), not a pointer, so there is no `nil` to dereference. The prototype supports scalar optionals (`i32`, `i64`, `u32`, `u64`, `u8`, `f32`, `f64`, `bool`) and `string?`. Optional function return types and `if let ... else` chains remain roadmap items.

---

## 5. Concurrency: Direct Style, Not `async`/`await`

Ube adopts Go’s **direct (sequential) style** by design. There is no `async`, no `await`, no `Future`, and no red/blue function coloring. A function that waits is an ordinary function:

```ube
struct Job { id: i32, weight: i32 }

fn worker(jobs: Chan[Job], results: Chan[i32]) {
    let job = jobs.recv()          // suspends this Vine, not the world
    results.send(job.id * job.weight)
}

fn main() {
    let jobs = Chan[Job].new(8)
    let results = Chan[i32].new(8)

    spawn worker(jobs, results)    // run it on another Vine

    jobs.send(Job{ id: 1, weight: 10 })
    jobs.close()

    let result = results.recv()
    println(result)
}
```

There is only one kind of function. A blocking channel read is legal in `main`, in a helper, in a receiver method, and inside a spawned Vine — with no viral annotations, no `.await`, and no duplicated standard library split between sync and async variants.

> **Enforced by the compiler.** `ubec` rejects `async` and `await` outright, so colored concurrency cannot creep into a codebase:
>
> ```text
> [ubec]: error: 'async' is not part of Ube; concurrency is direct-style.
>         Write plain blocking code, use `spawn` for background Vines, and
>         `Chan[T]` for coordination.
> ```

How suspension is intended to work per target:

| Target | Ready-Vine scheduling |
| :--- | :--- |
| Servers (Linux/macOS/Windows) | `epoll` / `kqueue` / `IOCP` event loop: a blocked Vine parks and the worker runs another |
| Microcontrollers | cooperative fibers or FreeRTOS tasks over a small static Tuber |
| WebAssembly | stack switching, or promises lowered through Asyncify |

A suspended Vine needs only its registers and its **Tuber watermark**. Its scratch memory is fixed and never relocated, so there is no stack copying, no pointer fix-up pass, and no GC scan.

### Cancellation and Timeouts

Cancellation is explicit rather than an implicit future drop: spawn the work, then race its channel against a timer with `select`. `select`, timers, and non-blocking I/O remain roadmap items. Until they land, the pthread prototype simply blocks the worker thread; `recv` is synchronous and direct-style.

> **Prototype status:** the language rule is final — direct style only. The current backend maps each Vine to a detached pthread, so a blocking `recv` parks an OS thread rather than a lightweight fiber. The event-loop and cooperative-fiber backends above are the next concurrency milestone.

---

## 6. Game Development: Frame Arenas and Flat Data

Ube’s memory model maps onto the scratchpad pattern used by game engines: allocate temporary data for one frame, then reclaim all of it with a single store. There is no collector to trigger a hitch, and no per-frame `malloc`/`free` when the data is built inside a `tuber` block.

```ube
fn simulate_particles(frame: i32) -> i32 {
    let mut particles = []i32{}
    let mut i = 0
    while i < 4 {
        particles.push(frame * 10 + i)
        i = i + 1
    }
    let mut total = 0
    for p in particles {
        total = total + p
    }
    return total
}

fn main() {
    let mut frame = 0
    while frame < 3 {
        tuber {
            let total = simulate_particles(frame)
            println(total)
        } // tuber: the whole frame scratchpad is reclaimed here
        frame = frame + 1
    }
}
```

This is the runnable `test_game_loop.ube`. The generated C saves a watermark on entering the `tuber` block and restores it on exit — one pointer write, no scanning.

### What works today

| Capability | Status |
| :--- | :--- |
| Per-frame arenas via `tuber { ... }` | **Yes** — save/restore watermark |
| Contiguous dynamic arrays | **Yes** for `[]i32` |
| `while` loops, assignment, `break`, `continue` | **Yes** |
| Flat structs, enums, and `match` | **Yes** |
| Bounded, predictable frame cost (no GC) | **Yes** — no collector exists |
| Generic slices `[]T` for structs/floats | **No** — roadmap |
| `extern fn`, pointer types, `.ptr` | **No** — roadmap |
| `Pool[T]` / generational slot pools | **No** — design/library stage |
| References into a growing array | **Not exposed** — design intent only |
| Real windowing/input/event loop | **No** — pthread prototype only |

The demo uses `[]i32` as the particle buffer because generic `[]T` is not implemented yet. The essay’s `[]Particle`, `[]Enemy`, and `Pool[Entity]` examples are the target design, not current syntax.

### C interop is a target, not a shipped feature

Generated Ube code is C99/GNU-C, so the data model is compatible with C libraries. But there is currently no `extern fn`, `*u8`, or string `.ptr`, so the raylib/SDL2 example cannot be compiled by this prototype. When the FFI surface lands, flat structs and slices should cross the boundary without marshaling; until then, treat those snippets as design targets.

### An honest comparison

Ube’s advantage is not that other languages cannot make games. C++ and Rust also achieve no-GC frame arenas; C# and Go can avoid most GC hitches with pooling and careful allocation. Ube’s bet is that the arena pattern is the **default memory model** instead of a hand-rolled subsystem, with a smaller, Go-like surface than C++ or Rust. The remaining gap is implementation: generic slices, FFI, references, slot pools, and a non-pthread scheduler.

---

## 7. Architectural Pipeline

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

## 8. Implementation Roadmap

### Phase 1: The Core Foundation (Current Milestone)
- [x] Minimal Tuber runtime (`ube_runtime.h`) with watermark save/restore.
- [x] End-to-end prototype compiler emitting C (`ubec.py`).
- [x] Basic types: `i32`, `bool`, and dynamic slices `[]i32`.
- [x] Monotone `push()` semantics and `tuber { ... }` scoped memory resets.
- [x] `while` loops, assignment/compound assignment, `break`, and `continue`.

### Phase 2: Ergonomics & Object Model
- [x] Go-style receiver functions: `fn (s Slice) sum() -> i32`.
- [x] Struct declarations and field access.
- [x] Tuber-allocated `string` values: literals, `+` concatenation, printing.
- [x] Implicit caller-allocated return values (`fn make_list() -> []i32`).
- [ ] Generic slices `[]T` for structs, floats, and other element types.

### Phase 3: Error Handling & Modern Types
- [x] Result types (`i32 ! string`) with error bubbling via the `?` operator.
- [x] `or_else` inline fallback defaults for results and optionals.
- [x] Null-safe Optionals (`T?`) with `null` and `if let val = opt` unwrapping.
- [ ] Optionals as function return values and `if let ... else` chains.
- [x] Tagged Unions / Enums (`enum Shape { Circle(f32), Rectangle(f32, f32) }`).
- [x] Pattern matching (`match` expressions).

### Phase 4: Standard Library & Concurrency
- [ ] Standard I/O, File System, and Math modules.
- [ ] Zero-copy C foreign function interface (`extern fn`).
- [x] Vines (lightweight coroutines/fibers) with private per-Vine Tubers.
- [x] Typed flat channels (`Chan[T]`) with `new`, `send`, `recv`, and `close`.
- [x] `spawn fn(args)` and the compile-time flat-only rule for payloads and arguments.
- [x] Direct-style concurrency: no `async`/`await`, no colored functions (rejected by `ubec`).
- [ ] Event-loop/fiber scheduler so blocking operations park a Vine instead of an OS thread.
- [ ] `select` multiplexing, channel timeouts, and non-pthread backends.

### Phase 5: Self-Hosting & Native Tooling
- [ ] Rewrite `ubec` in Ube itself (Self-hosting compiler).
- [ ] Instant-compilation dev workflow via in-process TinyCC (TCC).
- [ ] Direct WebAssembly (`.wasm`) generation mode.

---

## 9. Quickstart (Testing the Prototype)

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
Also try `demo.ube` (slices and Tuber scopes), `test_strings.ube`
(Tuber-allocated strings and concatenation), `test_v3.ube`
(tagged enums, `match`, and zero-GC dynamic returns), `test_v4.ube`
(Vines, `Chan[T]`, and `spawn`), `test_optionals.ube` (optionals,
`if let`, and optional `or_else`), `test_game_loop.ube` (per-frame
`tuber` arenas), and `test_full.ube` (the grand tour:
concurrency, optionals, `or_else`, enums, and `match`):

```bash
python3 ubec.py test_full.ube
```

### 3. Build & Run the Native Binary
```bash
gcc -O2 -pthread output.c -o ube_app
./ube_app
```

Concurrency uses the pthreads backend in this prototype, so builds are linked
with `-pthread`.

---

## 10. Guiding Principles

1. **Pragmatism Over Purity:** If a feature makes the compiler slightly more complex but saves the developer hours of frustration, we implement it.
2. **Explicit Intent, Implicit Safety:** Mutability is explicit (`let mut`), but memory cleanup is implicit and guaranteed.
3. **No Hidden Costs:** No invisible allocations, no background GC threads, no stop-the-world pauses.
4. **Small Conceptual Surface:** You should be able to learn the entire language syntax and semantics over a weekend.
5. **One Kind of Function:** Concurrency never changes a function’s type or call syntax. There is no `async`/`await`; blocking code is just code, and `spawn` is how you run it elsewhere.

---

## 11. License

Ube is open-source software licensed under the [MIT License](LICENSE).
