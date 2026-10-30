
<div align="center">

<img src="editors/vscode/icons/ube.svg" width="120" height="120" alt="Ube Logo">

# The Ube Programming Language

**The sweet, grounded systems language.**  
*Easy to program like Go. Fast like C. Zero Garbage Collection.*

**Latest version: `0.5.5`**

</div>

---

## 🍠 Overview

Ube is a modern, statically typed systems programming language designed to bridge the gap between low-level performance and developer ergonomics. It combines the readable, approachable syntax and concurrency model of Go with the pause-free speed, deterministic memory layout, and zero runtime overhead of C.

Ube delivers **Zero Garbage Collection (GC)** without requiring manual `malloc`/`free` or complex Rust-style lifetime annotations. It is built from the ground up for real-time game engines, CLI tools, embedded systems, networking servers, and performance-critical software.

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

1. **Frontend**: Lexing, parsing, monomorphization of generics, visibility checks, affine ownership tracking, and arena lifetime escape analysis.
2. **C Code Generation**: Emits clean, deterministic ANSI C99 code (`.c`).
3. **Native Compilation**: Invokes system `clang` or `gcc` with `-O2` optimization to generate standalone, dependency-free executables.

---

## 🧠 Memory Architecture: Zero GC

Ube does not use a tracing or reference-counting garbage collector. Memory is organized into deterministic regional bump arenas:

1. **The Regional Bump Arena (`arena { ... }`)**: Every allocation (dynamic slices, strings, string builders) defaults to the active arena level. Exiting an arena rewinds the watermark pointer in **1 CPU instruction**, freeing all allocations instantly without heap fragmentation.
2. **Explicit Promotion (`promote(...)`)**: When memory allocated inside an arena must outlive it, `promote` copies the value into the parent arena before the child arena is rewound.
3. **The Upward Reference Invariant**: Inner arenas cannot mutate outer references (e.g. calling `.push()` on an outer slice from inside an arena is rejected at compile time).
4. **Generational Handle Pools (`Pool[T]`)**: For long-lived entity graphs, pooled allocations are referenced via generational index handles (`Handle[T]`), preventing use-after-free and dangling pointers.

---

## 📖 Complete Syntax & Language Tour

### 1. Variables & Mutability

Variables are immutable by default using `let`. Use `let mut` for mutable bindings:

```ube
let pi: f32 = 3.14159
let name = "Ube"        // Type inferred as string

let mut counter = 0
counter = counter + 1
counter += 5            // Compound assignment: +=, -=, *=, /=, %=, &=, |=, ^=, <<=, >>=
```

### 2. Primitive Types

| Type Category | Types |
| :--- | :--- |
| **Integers** | `i8`, `i16`, `i32`, `i64`, `u8`, `u16`, `u32`, `u64` |
| **Floating Point** | `f32`, `f64` |
| **Boolean** | `bool` (`true`, `false`) |
| **String** | `string` (UTF-8 arena-allocated string slices) |
| **Void** | `void` |
| **Literals** | Dec (`42`), Hex (`0xFF`), Binary (`0b1010_0001`), Float (`3.14`) |

---

### 3. Functions & Methods

#### Standard Functions
```ube
fn add(a: i32, b: i32) -> i32 {
    return a + b
}

fn log_msg(msg: string) {
    println_str(msg)
}
```

#### Struct Receivers (Value & Mutable)
```ube
struct Counter {
    val: i32
}

// Value receiver (read-only copy)
fn (c Counter) read() -> i32 {
    return c.val
}

// Mutable receiver (mutates original)
fn (mut c Counter) bump(amount: i32) {
    c.val = c.val + amount
}
```

---

### 4. Structs & Field Visibility

Public declarations and fields use the `pub` keyword:

```ube
pub struct Player {
    pub name: string,
    pub score: i32,
    secret_seed: i32    // Private to its own module/file
}

let p = Player{
    name: "Aria",
    score: 100,
    secret_seed: 42
}
```

---

### 5. Tagged Enums & Pattern Matching

Enums are type-safe algebraic data types (discriminated unions). Match arms must be exhaustive:

```ube
enum Shape {
    Circle(f32),
    Rect(f32, f32),
    Point
}

fn describe(s: Shape) {
    match s {
        Circle(r) => {
            print_str("Circle with radius: ")
            println(r)
        }
        Rect(w, h) => {
            print_str("Rectangle area: ")
            println(w * h)
        }
        Point => {
            println_str("Zero-dimensional point")
        }
    }
}
```

---

### 6. Fixed Arrays, Dynamic Slices & Strings

#### Fixed-Size Arrays (`[N]T`)
Stack-allocated contiguous elements with fixed length:
```ube
let mut buffer: [4]i32 = [4]i32{10, 20, 30, 40}
buffer[0] = 99
println(buffer[0])
```

#### Dynamic Slices (`[]T`)
Arena-backed growable slices with built-in methods:
```ube
let mut items = []i32{}
items.push(10)
items.push(20)
items.push(30)

println(items.len)      // 3
println(items.cap)      // Underlying capacity
let last = items.pop()  // 30

// Slicing sub-ranges
let sub = items[0..1]
```

#### Passing Arrays as Slices
Arrays automatically coerce to read slices when passing to functions:
```ube
fn sum_slice(vals: []i32) -> i32 {
    let mut total = 0
    for v in vals { total += v }
    return total
}

let arr = [3]i32{1, 2, 3}
sum_slice(arr) // Automatically converted to []i32
```

---

### 7. Control Flow

#### `if` / `else`
```ube
if count > 10 {
    println_str("High")
} else if count == 10 {
    println_str("Target")
} else {
    println_str("Low")
}
```

#### `while` Loops
```ube
let mut i = 0
while i < 10 {
    i += 1
    if i == 5 { continue }
    if i == 8 { break }
}
```

#### `for..in` Iteration
Iterate over dynamic slices, arrays, or ranges:
```ube
// Range iteration
for idx in 0..5 {
    println(idx)
}

// Slice / Array iteration
for item in items {
    println(item)
}
```

---

### 8. Optionals (`T?`) & Results (`T ! E`)

#### Optionals (`T?`)
Represents an optional value (`null` or `T`):
```ube
fn find_user(id: i32) -> string? {
    if id == 1 {
        return "Alice"
    }
    return null
}

// If-let unwrapping
if let name = find_user(1) {
    println_str(name)
}

// Fallback defaulting with or_else
let username = find_user(99) or_else "Guest"
```

#### Error Handling with Results (`T ! E`)
No runtime exceptions. Functions return typed success or error:
```ube
fn parse_port(s: string) -> i32 ! string {
    if s == "invalid" {
        return error("Malformed port string")
    }
    return 8080
}

// Error propagation with the try (?) operator
fn start_server() -> bool ! string {
    let port = parse_port("8080")?
    println(port)
    return true
}

// Fallback handling with or_else
let active_port = parse_port("invalid") or_else 3000
```

---

### 9. Generics & Trait Constraints

Ube monomorphizes all generics during compilation—zero runtime overhead:

```ube
struct Box[T] {
    val: T
}

fn (b Box[T]) unwrap() -> T {
    return b.val
}

fn identity[T](val: T) -> T {
    return val
}

// Trait Constraints
trait Printable {
    fn to_str() -> string
}

fn display[T: Printable](item: T) {
    println_str(item.to_str())
}
```

---

### 10. Traits & Dynamic Dispatch

When dynamic polymorphism is required, traits can be passed as fat pointers (`vtables`):

```ube
trait Speaker {
    fn speak() -> string
}

struct Dog { name: string }
fn (d Dog) speak() -> string { return "Woof!" }

struct Cat { name: string }
fn (c Cat) speak() -> string { return "Meow!" }

fn make_noise(s: Speaker) {
    println_str(s.speak())
}

fn main() {
    let d = Dog{name: "Rex"}
    let c = Cat{name: "Luna"}
    make_noise(d)
    make_noise(c)
}
```

---

### 11. Regional Memory Arenas (`arena` & `promote`)

Use `arena` blocks to guarantee immediate memory reclamation without pauses:

```ube
fn main() {
    let mut persistent_slice = []i32{}

    // Arena statement
    arena {
        let mut scratch = []i32{}
        scratch.push(10)
        scratch.push(20)

        // Promotes scratch data into parent arena before child wipes
        persistent_slice = promote(scratch)
    } // 💥 The entire scratch arena is wiped clean in 1 instruction.

    println(persistent_slice.len) // 2

    // Arena expression
    let doubled = arena {
        let mut list = []i32{}
        list.push(42)
        promote(list)
    }
}
```

---

### 12. Channels & Concurrency (`spawn` & `select`)

Ube uses shared-nothing concurrency. Complex pointers cannot cross thread boundaries; data is safely moved across worker threads:

```ube
fn worker(jobs: Chan[i32], done: Chan<bool>) {
    while let job = jobs.recv() {
        print_str("Processing job: ")
        println(job)
    }
    done.send(true)
}

fn main() {
    let jobs = Chan[i32](10)
    let done = Chan[bool](1)

    spawn worker(jobs, done)

    jobs.send(101)
    jobs.send(102)
    jobs.close()

    // Multiplexing with select
    select {
        case is_done = <-done: {
            println_str("Worker completed task.")
        }
        default: {
            println_str("Waiting for worker...")
        }
    }
}
```

---

### 13. Resource Management (`defer`)

The `defer` statement ensures code runs whenever the enclosing function or loop block exits:

```ube
fn process_file() {
    println_str("Opening file resource")
    defer println_str("Closing file resource") // Runs at function exit

    let mut i = 0
    while i < 3 {
        defer println_str("Loop step cleanup") // Runs on every iteration, break, or continue
        i += 1
    }
}
```

---

### 14. Generational Pool Allocator (`Pool[T]`)

For persistent entity systems (e.g. game objects) where objects have unpredictable lifecycles:

```ube
struct Bullet {
    x: f32,
    y: f32
}

fn main() {
    let mut bullets = Pool[Bullet](128)

    let h1 = bullets.alloc(Bullet{x: 0.0, y: 10.0})
    println(bullets.get(h1).y) // 10.0

    bullets.free(h1)

    // Stale handle detection prevents dangling references / use-after-free:
    // bullets.get(h1) -> Triggers handle invalidation panic
}
```

---

### 15. Low-Level Control (`unsafe` & `extern`)

#### Unsafe Blocks
Enables raw pointer manipulation and hardware-level operations:
```ube
let x: i32 = 42
unsafe {
    let ptr: *i32 = &x
    println(*ptr)
}
```

#### Foreign Function Interface (C FFI)
Directly link and invoke C standard library functions or native dynamic libraries:
```ube
extern fn puts(str: string) -> i32
extern fn sin(rad: f64) -> f64

fn main() {
    puts("Hello from native libc puts!")
    let val = sin(3.14159 / 2.0)
}
```

---

### 16. Modules & Multi-File Imports

Split projects cleanly across separate files:

```ube
// math.ube
pub fn square(x: i32) -> i32 {
    return x * x
}

fn internal_debug() {
    // Only visible in math.ube
}
```

```ube
// main.ube
import "math.ube"

fn main() {
    println(square(5)) // 25
}
```

---

## 🛠️ CLI Toolchain Reference

The `ube` binary provides a complete developer workflow:

| Command | Usage | Description |
| :--- | :--- | :--- |
| `ube run <file>` | `ube run main.ube` | Compiles and executes your project in-memory. |
| `ube build <file>` | `ube build main.ube -o app` | Builds an optimized native executable (`-O2`). |
| `ube test [target]` | `ube test tests/` | Runs all test files (`test_*.ube` or functions named `test_*`). |
| `ube check <file>` | `ube check main.ube` | Audits syntax, types, and arena escapes without invoking C compiler. |
| `ube fmt <target>` | `ube fmt .` | Formats Ube source files to canonical indentation & spacing. |
| `ube doc [target]` | `ube doc main.ube` | Generates documentation for public structs, functions, enums, and traits. |
| `ube ai context` | `ube ai context . -o ctx.txt`| Generates a complete codebase architecture dump for LLM prompts. |
| `ube ai guide` | `ube ai guide` | Prints the complete Ube grammar reference for LLMs. |
| `ube lsp` | `ube lsp` | Starts the Language Server Protocol (hover, symbols, auto-complete). |

---

## 🧩 VS Code Extension

Official language support for Visual Studio Code is located under `editors/vscode`:
- Syntax highlighting for all Ube keywords, types, and operators (`->`, `!`, `?`, `arena`).
- Code snippets (`arena`, `fn`, `struct`, `match`, `spawn`, `select`).
- LSP integration for Hover types, Go-to Definition, Document Symbols, and Diagnostics.

---

## 📄 License

Distributed under the MIT License. See [LICENSE](LICENSE) for details.
