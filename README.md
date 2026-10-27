<div align="center">

<img src="assets/icon.svg" alt="Ube Logo" width="130" height="130" />

# Ube Programming Language

**The sweet, grounded systems language.**  
*Easy to program like Go. Fast like C. Zero Garbage Collection.*

[![License: MIT](https://img.shields.io/badge/License-MIT-purple.svg)](LICENSE)
[![Status: Active](https://img.shields.io/badge/Status-v0.5.0-brightgreen.svg)]()
[![Target: C99_WASM](https://img.shields.io/badge/Target-C99%20%7C%20WASM%20%7C%20Embedded-blue.svg)]()

</div>

---

## 1. What is Ube?

**Ube** is a statically typed, compiled systems language designed for developer ergonomics, deterministic latency, and universal portability.

It solves a fundamental dilemma in modern systems engineering:

* **Go** is productive, but its **Garbage Collector (GC)** incurs latency spikes, unpredictable memory footprints, and unsuitability for microcontrollers or hard real-time systems.
* **Rust** gives high performance without GC, but its **borrow checker and explicit lifetime annotations (`'a`)** impose cognitive overhead that slows down iterative delivery.
* **C/C++** is everywhere, but lacks memory ergonomics, type safety, and modern standard primitives.

**Ube provides Go-style syntax and developer ergonomics with zero garbage collection.**

---

## 2. The Core Pillars

| Pillar | How Ube Achieves It |
| :--- | :--- |
| 🚀 **Fast as C/Rust** | Contiguous memory layouts, monotone bump growth on Tubers, and single-store deallocation. |
| 🧁 **Easy as Go** | Minimalist syntax, Go-style receiver methods, channels, fast compilation, and zero lifetime annotations. |
| 🛡️ **Memory Safe** | Eliminates heap churn, use-after-free, and double-free via static root analysis and scoped regional Tubers. |
| 🌍 **Runs Everywhere** | Transpiles to clean ANSI C99/C23: Desktops, WebAssembly (WASM), and Microcontrollers (ESP32, STM32, RISC-V). |
| ⚡ **Zero-Cost C Interop** | Flat data models and direct C FFI (`extern fn`, `.ptr`) with zero conversion overhead and no glue code. |

---

## 3. The Memory Model: "Tubers"

Ube rejects runtime garbage collection and manual `malloc`/`free`. Instead, it uses **Tubers** (scoped regional bump allocators):

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
Every function and lexical block saves a watermark representing the current top of the Tuber. When the scope exits, all allocated items inside that scope are reclaimed in a single instruction:
```c
current_tuber.top = watermark;
```

### 2. Caller-Allocated Returns
Callees that return dynamic slices do not reset their watermark; caller frames inherit the allocation directly without GC or copying.

### 3. Vines: Shared-Nothing Concurrency
A **Vine** is Ube's unit of execution. Every Vine owns a private, isolated Tuber via thread-local storage (`_Thread_local`). Cross-Vine communication occurs exclusively via typed channels (`Chan[T]`) copying data by value (`memcpy`).

---

## 4. Current Implementation Status

| Feature / Subsystem | Status | Description |
| :--- | :--- | :--- |
| **AST Parser & Lexer** | **Shipped** | Full tokenization, recursive-descent parser, line/column tracking. |
| **Static Typechecker** | **Shipped** | Scope resolution, type inference, struct/enum validation, flat type check for `Chan[T]`. |
| **Generic Slices (`[]T`)** | **Shipped** | Supports dynamic arrays of primitives, structs, and custom types via `.push()`. |
| **Null-Safe Optionals (`T?`)** | **Shipped** | `null`, `if let`, optional returns, and unwrap fallback (`or_else`). |
| **Result Error Handling (`T ! E`)** | **Shipped** | Fallible functions, `return error(...)`, `?` propagation, and `or_else`. |
| **Tagged Unions & Pattern Matching**| **Shipped** | Multi-variant data enums, multi-line `match` statements with bindings. |
| **C FFI & Pointer Interop** | **Shipped** | `extern fn`, `*u8`, `*void`, `**void`, and `.ptr` lowering. |
| **Generational Slot Pools (`Pool[T]`)**| **Shipped** | Fast handle-based entity pooling with generational index safety. |
| **Channels & Vines** | **Shipped** | `Chan[T].new()`, `.send()`, `.recv()`, `.close()`, `spawn`, `while let`, and `select`. |
| **Chunked Immutable Tubers** | Roadmap | Migration from reallocating buffer to non-moving chunk lists. |
| **M:N Fiber Scheduler** | Roadmap | Epoll/Kqueue/WASM cooperative fiber event loop. |
| **Self-Hosting Compiler** | Roadmap | Rewrite compiler frontend directly in Ube. |

---

## 5. Quickstart

### Prerequisites
* Python 3.8+
* Any C compiler (`gcc`, `clang`, or `tcc`)

### Automatic Install
```bash
curl -fsSL https://raw.githubusercontent.com/JohnEsleyer/ube/main/install.sh | bash
```

### Manual Build & Test
```bash
# 1. Run the frontend compiler test suite
python3 tests/test_frontend.py

# 2. Compile comprehensive end-to-end suite
python3 src/compiler/ubec.py tests/test_comprehensive.ube

# 3. Build with GCC and run
gcc -O2 -pthread -Isrc/runtime output.c -o ube_app
./ube_app
```

---

## 6. License

Ube is open-source software licensed under the [MIT License](LICENSE).