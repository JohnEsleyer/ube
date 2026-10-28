<div align="center">

<img src="editors/vscode/icons/ube.svg" width="120" height="120" alt="Ube Logo">

# The Ube Programming Language

**The sweet, grounded systems language.**
*Easy to program like Go. Fast like C. Zero Garbage Collection.*

**Latest version: `0.5.5`**

</div>

---

## 🍠 Overview

Ube is a modern, high-performance systems programming language designed to bridge the gap between low-level control and high-level ergonomics. It delivers the speed and deterministic performance of C with the clean syntax, safety features, and concurrency primitives of Go.

Ube is built with **Zero Garbage Collection (GC)**, making it ideal for game engines, embedded systems, real-time networking, and high-performance CLI tools where pause-free execution is mandatory.

---

## 🚀 Installation

You can install Ube instantly using our installation script:

```sh
curl -sSL https://raw.githubusercontent.com/JohnEsleyer/ube/main/install.sh | bash
```

### Manual Build from Source
If you prefer to build from source (requires Go 1.18+ and GCC/Clang):
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

## 🧁 How It Works

Ube uses a fast, reliable **transpilation and compilation pipeline**:
1. **Frontend Parsing & Type Checking**: The Go compiler frontend parses `.ube` source files, monomorphizes generics, validates types, and runs compile-time safety and lifetime audits.
2. **C Code Generation**: Valid programs are compiled into optimized, readable standard C code (`.c`).
3. **Native Compilation**: Ube invokes your system's C compiler (`gcc` or `clang`) with optimizations (`-O2`) to produce a standalone, lightning-fast native binary with zero runtime dependencies.

---

## 🧠 Memory Management Concepts

Ube achieves **Zero Garbage Collection** without requiring complex manual `malloc`/`free` or strict lifetime annotations like Rust through two core pillars:

### 1. Regional Scratch Arenas (`arena { ... }`) & Safe Promotion
Ube introduces regional arena allocation via `arena` (or `scope`) blocks. Any dynamic collection (like `[]i32` slices or strings) allocated inside an arena lives in a lightweight, thread-local bump-allocated memory chunk.

To guarantee complete memory safety without complex Rust-style lifetime annotations:
* **The Upward Reference Invariant:** Inner arenas cannot mutate outer reference containers (e.g., calling `.push()` on an outer slice from inside an arena is rejected at compile time).
* **Explicit Promotion (`promote`):** To pass dynamic memory from an arena to an outer scope, use `promote(...)` to copy the data directly into the parent arena before the child arena is wiped.

```fn main() {
    let mut final_list = []i32{}

    arena {
        let mut temp_list = []i32{}
        temp_list.push(42)
        temp_list.push(100)

        // Safely promotes memory into the parent arena:
        final_list = promote(temp_list)
    } // 💥 The entire child arena is recycled in 1 instruction. Zero fragmentation, zero GC!

    println(final_list.len) // 2
}
```

### 2. Shared-Nothing Concurrency (Tasks & Channels)
Ube implements thread-safe communication channels (`Chan[T]`) and lightweight concurrent background tasks (`spawn`). To prevent data races and eliminate garbage collection across threads, Ube enforces a **flat type restriction**: complex pointers cannot cross channel boundaries or spawn arguments. Values are safely copied via `memcpy`, maintaining complete memory isolation between worker threads.

---

## 💡 Quickstart

Initialize a starter project:
```sh
ube init
```

Run your program directly:
```sh
ube run main.ube
```

Build an optimized standalone native executable:
```sh
ube build main.ube -o my_app
```
