<div align="center">

<img src="editors/vscode/icons/ube.svg" width="120" height="120" alt="Ube Logo">

# The Ube Programming Language

**The sweet, grounded language for games, apps, and backends.**  
*Fast like C. Clean like Go. Zero GC. 100% Wasm-Native.*

**Latest version: `0.5.6`**

</div>

---

## 🍠 The Ube Philosophy

Ube is a statically typed, memory-safe programming language engineered for a specific, pragmatic domain: **high-level games, modern applications, CLI tools, and high-throughput backends**.

Ube is **not** a low-level systems language. It does not write operating system kernels, manipulate hardware registers, or juggle raw pointer arithmetic. Those domains belong to C, C++, and Zig. 

Instead, Ube exists for developers who love the simplicity and readability of **Go**, but want the pause-free speed, predictable latency, and deterministic memory model of **C**—without the cognitive tax or compiler fights of **Rust**.

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

5. **Sandboxed, Secure, and 100% Wasm-First**  
   Ube embraces WebAssembly (Wasm) and WASI as its primary execution substrate. Programs are secure by default, start in sub-milliseconds, and compile into clean, portable bytecode that runs identically in browsers, servers, edge workers, and game runtimes.

6. **Designed for Both Humans and Artificial Intelligence**  
   Ube’s strict, unambiguous syntax, explicit data movement, and lack of hidden control flow make code exceptionally straightforward for Large Language Models to inspect, trace, reason about, and generate without hallucinations.

---

## 🚀 Installation & Toolchain

### Automated Install Script
```sh
curl -sSL https://raw.githubusercontent.com/JohnEsleyer/ube/main/install.sh | bash
```

### Build from Source (Requires Go 1.18+)
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

| Command | Usage | Description |
| :--- | :--- | :--- |
| `ube run` | `ube run main.ube` | Compiles to Wasm and runs it in `wasmtime`, `wasmer`, `node`, `deno`, or `wasm3`. |
| `ube build` | `ube build main.ube` | Builds a sandboxed WebAssembly/WASI module (`main.wasm`) by default. |
| `ube build` | `ube build main.ube --html` | Builds a Web Canvas/UI app (`main.html`) with Emscripten. |
| `ube build` | `ube build main.ube --emit-c` | Keeps the generated C source for inspection or custom toolchains. |
| `ube test` | `ube test tests/` | Runs all test files or `test_*` functions. |
| `ube check` | `ube check main.ube` | Audits types, regions, ownership, and host imports statically. |
| `ube fmt` | `ube fmt .` | Canonical auto-formatter for all `.ube` files. |
| `ube doc` | `ube doc main.ube` | Generates documentation for public exports. |
| `ube lsp` | `ube lsp` | Starts the Language Server Protocol. |
| `ube guide` | `ube guide` | Outputs the AI Reference Guide (`ube_guide.md`). |
| `ube context`| `ube context . -o ctx.txt`| Generates codebase architecture context for AI. |

> The compiler prefers `clang --target=wasm32-wasi`, `zig cc -target wasm32-wasi`,
> or `emcc`. If no Wasm SDK is installed, `ube build` and `ube run` fall back to
> a native C host executable so development can continue.

---

## 📄 License

Distributed under the MIT License. See [LICENSE](LICENSE) for details.
