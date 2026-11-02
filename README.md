<div align="center">

<img src="editors/vscode/icons/ube.svg" width="120" height="120" alt="Ube Logo">

# The Ube Programming Language

**The sweet, grounded language for games, apps, and backends.**  
*100% WebAssembly. Fully Sandboxed. Zero GC. Clean like Go.*

**Latest version: `0.7.3`**

</div>

---

## 🍠 The Ube Philosophy

Ube is a statically typed, memory-safe programming language engineered for a specific, pragmatic domain: **high-level games, modern applications, CLI tools, and high-throughput backends**.

Ube is **not** a low-level systems language. It does not write operating system kernels, manipulate hardware registers, or juggle raw pointer arithmetic. Those domains belong to C, C++, and Zig. 

Instead, Ube exists for developers who love the simplicity and readability of **Go**, but want the pause-free speed, predictable latency, and deterministic memory model of **C**—without the cognitive tax or compiler fights of **Rust**.

---

## 🛡️ Defining Trait: Fully Sandboxed WebAssembly

- **100% WebAssembly Only**: Ube targets Wasm/WASI natively. No C bindings, no native syscall assembly, and no hardware pointers.
- **Zero Unsafe**: The `unsafe` keyword and raw pointers (`*T`) are permanently eliminated from the language.
- **Host Imports (`extern fn`)**: Interfacing with the host (WebGPU, Canvas, fetch, DOM, or WASI runtime) occurs cleanly through typed Wasm host imports.
- **Affine Memory & Regional Arenas**: Zero GC without lifetime annotations.

---

### Core Tenets

1. **High-Level Focus, Real-World Pragmatism**  
   Built specifically for game creators, product builders, and backend engineers. By leaving raw pointer arithmetic and hardware tinkering behind, Ube frees developers to focus entirely on application logic, gameplay loops, and business value.

2. **Safety Without the Lifetime Tax**  
   Memory safety is a standard guarantee, not an academic puzzle. Ube has **no lifetime annotations (`'a`)** or complex borrow-checker gymnastics. Memory safety and zero garbage collection are achieved through three deterministic, compile-time mechanisms:
   - **Affine Move Semantics**: Values have clear single ownership.
   - **Regional Bump Arenas (`arena { ... }`)**: Scratch memory wiped in a single CPU instruction.
   - **Generational Entity Pools (`Pool[T]`)**: Graph structures and entity lifecycles validated via generation handles.

3. **Zero Magic: What You Read Is What You Get**  
   No hidden implicit type conversions. No operator overloading. No hidden runtime garbage collector kicking in mid-frame or spiking p99 latencies. Every allocation, movement, and boundary is explicit in the source code.

4. **Inspired by Go: Readability and Concurrency**  
   Clean, human-first syntax. Shared-nothing concurrency with tasks (`spawn`), typed message channels (`Chan[T]`), and event multiplexing (`select`) prevents data races by construction.

5. **Fully Sandboxed, 100% WebAssembly**  
   Ube targets WebAssembly (Wasm) and WASI natively. Programs are secure by default, start in sub-milliseconds, and compile into clean, portable bytecode that runs identically in browsers, servers, edge workers, and game runtimes. The language has no `unsafe` keyword and no raw pointer types; host capabilities enter only through typed Wasm host imports (`extern fn`).

6. **Designed for Both Humans and Artificial Intelligence**  
   Ube’s strict, unambiguous syntax, explicit data movement, and lack of hidden control flow make code exceptionally straightforward for Large Language Models to inspect, trace, reason about, and generate without hallucinations. The native `ube sig / load / patch` pipeline delivers complete LLM-assisted multi-file edits in **1–2 request turns** with zero intermediate files.

---

## 🚀 Installation & Toolchain

### Automated Install Script
```sh
curl -sSL https://raw.githubusercontent.com/JohnEsleyer/ube/main/install.sh | bash
```

### Build from Source (Requires Go 1.18+ and CGO)
```sh
git clone https://github.com/JohnEsleyer/ube.git
cd ube
CGO_ENABLED=1 go build -o bin/ube ./cmd/ube
sudo cp bin/ube /usr/local/bin/ube
```

> **Note:** `CGO_ENABLED=1` is required because `ube` embeds the [Wasmtime](https://wasmtime.dev) runtime via `wasmtime-go`. Precompiled native libraries for Linux (x86_64, aarch64), macOS (Intel & Apple Silicon), and Windows (x86_64) are downloaded automatically by `go build`.

Verify your installation:
```sh
ube version
```

---

## 🧠 Memory Architecture: Zero GC, Zero Pauses

Ube operates without a tracing garbage collector or reference counting:

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
│    Long-lived entities, game objects, graph nodes         │
│   - Generational index handles (Handle[T])                │
│   - Linear static consumption prevents stale access       │
└───────────────────────────────────────────────────────────┘
```

---

## 📖 Complete Syntax & Language Tour

### 1. Variables & Types
```ube
// Immutable by default
let release_year: i32 = 2026
let ratio: f32 = 3.14159
let name: string = "Ube"

// Mutable bindings
let mut score = 0
score += 10
score *= 2
println(score) // 20
```

### 2. Move Semantics (Ownership)
Affine types (`string`, slices `[]T`, structs with non-copy fields, `Pool[T]`) transfer ownership on assignment:
```ube
let a = "Grounded Game"
let b = a          // 'a' is moved to 'b'
// println(a)      // COMPILE ERROR: use of moved variable 'a'
println(b)         // OK

let mut s = "initial"
let old = s        // 's' moved
s = "reassigned"   // Reassignment reactivates 's'
println(s)         // OK
```

### 3. Functions & Methods
```ube
struct Player {
    score: i32
}

// Function
fn add(a: i32, b: i32) -> i32 {
    return a + b
}

// Value receiver
fn (p Player) read_score() -> i32 {
    return p.score
}

// Mutable receiver
fn (mut p Player) add_points(pts: i32) {
    p.score += pts
}
```

### 4. Tagged Enums & Pattern Matching
```ube
enum Action {
    Move(f32, f32),
    Attack(i32),
    Idle
}

fn handle_action(a: Action) {
    match a {
        Move(x, y) => println(f"Moving to {x}, {y}"),
        Attack(dmg) => println(f"Attacking with damage: {dmg}"),
        Idle => println("Player is idle")
    }
}
```

### 5. Regional Memory Arenas (`arena` & `promote`)
Scratch memory is instantly rewound upon exiting the block:
```ube
fn main() {
    let mut persistent = []i32{}

    arena {
        let mut scratch = []i32{}
        scratch.push(10)
        scratch.push(20)

        // Copies data safely into the outer parent arena
        persistent = promote(scratch)
    } // child arena wiped in 1 instruction

    println(persistent.len) // 2
}
```

### 6. Generational Entity Pools (`Pool[T]`)
For game objects, UI nodes, and dynamic entities:
```ube
struct Enemy {
    hp: i32
}

fn main() {
    let mut pool = Pool[Enemy].new(64)

    // Allocates handle
    let h1 = pool.spawn(Enemy{ hp: 100 })

    if let e = pool.get(h1) {
        println(e.hp)
    }

    // Linearly frees the handle
    pool.despawn(h1)
    // pool.get(h1) -> Compile error: use of moved variable
}
```

### 7. Concurrency & Channels (`spawn` & `select`)
Shared-nothing message passing ensures concurrency without data races:
```ube
struct Packet {
    id: i32,
    code: i32
}

fn worker(ch: Chan[Packet], done: Chan[bool]) {
    while let pkt = ch.recv() {
        println(f"Processed packet {pkt.id}")
    }
    done.send(true)
}

fn main() {
    let ch = Chan[Packet].new(8)
    let done = Chan[bool].new(1)

    spawn worker(ch, done)

    ch.send(Packet{ id: 1, code: 200 })
    ch.close()

    select {
        ok = done => println("Worker completed cleanly"),
        default => println("Waiting...")
    }
}
```

---

## 🛠️ Developer CLI Workflow

### Core Commands

| Command | Usage | Description |
| :--- | :--- | :--- |
| `ube run` | `ube run main.ube` | Compiles to Wasm and runs it via the **embedded Wasmtime** Cranelift JIT engine (no external runtime needed). |
| `ube build` | `ube build main.ube` | Builds a sandboxed WebAssembly/WASI module (`main.wasm`) by default. |
| `ube build` | `ube build main.ube --html` | Builds a Web Canvas/UI app (`main.html`) with Emscripten. |
| `ube build` | `ube build main.ube --emit-c` | Keeps the generated C source for inspection or custom toolchains. |
| `ube test` | `ube test tests/` | Runs all test files or `test_*` functions. |
| `ube check` | `ube check main.ube` | Audits types, regions, ownership, and host imports statically. |
| `ube fmt` | `ube fmt .` | Canonical auto-formatter for all `.ube` files. |
| `ube doc` | `ube doc main.ube` | Generates documentation for public exports. |
| `ube lsp` | `ube lsp` | Starts the Language Server Protocol. |

> The compiler prefers `clang --target=wasm32-wasi`, `zig cc -target wasm32-wasi`,
> or `emcc`. If no Wasm SDK is installed, `ube build` and `ube run` fall back to
> a native C host executable so development can continue.
>
> `ube run` uses the **embedded [Wasmtime](https://wasmtime.dev) runtime** (Cranelift JIT) to execute
> Wasm modules directly inside the `ube` process — no external `wasmtime` binary required.
> External runtimes (`wasmer`, `node`, `deno`, `wasm3`) are kept as secondary fallbacks.

---

## 🤖 Native AI Pipeline (`v0.7.2+`)

Ube ships a built-in **1-Click Clipboard Pipeline** that connects your codebase directly to any LLM chat window with zero intermediate files and 1–2 request turns.

```
[Local Codebase]                        [Claude / ChatGPT / DeepSeek]
       |                                              |
       |  1. ube sig -c                               |
       |-----------------------------------------------> Turn 1 (Map):
       |  (Symbol skeletons copied to clipboard)      | LLM returns <request>
       |                                              |
       |  2. ube load -ci -c                          |
       |-----------------------------------------------> Turn 2 (Solve):
       |  (Reads clipboard, traces AST,               | LLM returns <replace>
       |   copies deep context to clipboard)          |
       |                                              |
       |  3. ube patch -ci                            |
       v                                              |
 [Atomic Disk Update] <--------------------------------+
```

### AI Commands

| Command | Example | Description |
| :--- | :--- | :--- |
| `ube prompt` | `ube prompt -c` | Copies the token-efficient LLM system prompt to clipboard. Paste at session start. |
| `ube sig` | `ube sig -c` | Extracts all struct / enum / trait / fn signatures (no bodies). ~300 tokens for an entire project. |
| `ube load` | `ube load -ci -c` | Reads an LLM request from clipboard, resolves all directives against the AST, copies context back. |
| `ube patch` | `ube patch -ci` | Reads an LLM patch bundle from clipboard and applies it atomically via 2-pass in-memory VFS. |
| `ube guide` | `ube guide` | Writes the Ube language reference guide to `ube_guide.md`. |
| `ube context` | `ube context . -o ctx.txt` | Generates a full codebase architecture summary for AI. |

### `ube load` Directives

Pass a file or pipe from clipboard (`-ci`). The following XML directives are resolved against the live AST:

| Directive | Purpose |
| :--- | :--- |
| `<trace symbol="Fn" depth="2" />` | Recursively extracts the function body, its callees, and referenced types up to `depth`. |
| `<def symbol="StructOrFn" />` | Extracts a single function or type definition. |
| `<select path="..." fn="..." case="..." />` | Isolates one `match` arm or `switch` case without dumping the whole function. |
| `<select struct="T" methods="a,b" />` | Extracts specific method implementations for a type. |
| `<xref symbol="X" action="call\|write\|def" />` | Finds definition sites and all reference lines for a symbol across the codebase. |
| `<file path="..." search="..." context="5" for-patch="true" />` | Extracts a file slice around a search term; `for-patch="true"` omits line numbers. |
| `<grep pattern="keyword" glob="*.ube" />` | Finds every line containing a pattern across source files. |
| `<feature-context>...</feature-context>` | Composite bundle: type defs + entrypoint trace + touchpoint grep in one tag. |

### `ube patch` Directives

LLM responses containing any of the following are applied atomically (all-or-nothing, validated in memory before touching disk):

```xml
<!-- Surgical in-place replacement (optional function scope guard) -->
<replace path="src/game.ube" within="update_player">
<search>
    p.speed = 5
</search>
<with>
    p.speed = 10
</with>
</replace>

<!-- Create or fully overwrite a file -->
<file path="src/helpers.ube">
pub fn clamp(v: f32, lo: f32, hi: f32) -> f32 {
    if v < lo { return lo }
    if v > hi { return hi }
    return v
}
</file>

<!-- Delete a file -->
<delete path="src/old_module.ube" />
```

**Flags:** `-n` / `--dry-run` validates all anchors in memory without writing to disk. `-f` / `--fix` enables fuzzy whitespace-tolerant matching.

---

## 📄 License

Distributed under the MIT License. See [LICENSE](LICENSE) for details.
