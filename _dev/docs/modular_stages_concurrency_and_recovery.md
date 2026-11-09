# Modularized Compiler Pipeline, Concurrency Runtime & Parser Recovery

This document details the architecture and implementation of three major compiler and runtime advancements in Ube:
1. **Modularizing Wasm Code Emission into Discrete Stages (IR, Local Allocation, and Binary Emission)**
2. **Single-Threaded WASI Concurrency Runtime and Channel Rings**
3. **Foundational Precision IEEE-754 Float Formatting and Resilient Parser Error Recovery**

---

## 1. Modularizing `wasm_emit.go` into Discrete Stages

Historically, Ube lowered AST nodes directly into raw WebAssembly bytecode bytes within a single visitor pass. To decouple language semantics, register/local assignment, and binary serialization, the pipeline is now structured into three modular phases:

```
                    ┌────────────────────────┐
                    │      Typechecked       │
                    │       Program AST      │
                    └───────────┬────────────┘
                                │
                                ▼
                    ┌────────────────────────┐
                    │    IR Construction     │  (Lower AST to linear IROpcode sequence,
                    │     & Control Flow     │   desugar defer/promotions, flatten CFG)
                    └───────────┬────────────┘
                                │
                                ▼
                    ┌────────────────────────┐
                    │     Virtual Local /    │  (Compute local lifecycles, coalesce
                    │    Scratch Allocator   │   temporaries via LocalPool, assign indices)
                    └───────────┬────────────┘
                                │
                                ▼
                    ┌────────────────────────┐
                    │      Binary Wasm       │  (RLE local run grouping, LEB128 encoding,
                    │        Emitter         │   binary section assembly)
                    └────────────────────────┘
```

### Components

- **Intermediate Representation (`pkg/compiler/ir.go`)**:
  - `IROpcode`: Linear WebAssembly-focused stack, memory, arithmetic, comparison, and structured control flow opcodes (`OpBlock`, `OpLoop`, `OpIf`, `OpCallIndirect`, etc.).
  - `IRInstruction`: Encapsulates opcodes, value types, immediate constants (`i32`, `i64`, `f32`, `f64`), index references (`LocalIdx`, `GlobalIdx`, `FuncIdx`), memory alignments, and offsets.
  - `IRFunction`: Encapsulates function metadata (`Params`, `Results`, `Locals`, and `Body []IRInstruction`).

- **Virtual Local & Scratch Allocator (`pkg/compiler/local_alloc.go`)**:
  - `LocalPool`: Tracks parameter types and maps declared variable bindings to fixed Wasm local indices.
  - Temporary local leasing (`LeaseTemp`, `ReleaseTemp`): Reuses scratch locals across disjoint expression evaluation lifetimes to reduce total stack frame footprints and eliminate bloat in WebAssembly functions.

- **Isolated Binary Generation (`pkg/compiler/wasm_emit_code.go`)**:
  - `EmitFunctionBody(fn *IRFunction) []byte`: Compresses the function's local variables into WebAssembly's run-length-encoded (RLE) local declarations and transforms linear `IRInstruction` streams directly into validated byte arrays with proper LEB128 encoding and function delimiters.

---

## 2. Concurrency Runtime Architecture (WASI Single-Threaded Task Scoping)

In standard WebAssembly/WASI Preview 1, native OS kernel threads (`pthread`) do not exist without non-standard engine flags. Ube guarantees **data-race-free, deterministic concurrency** operating cooperatively within a single thread.

### Linear Memory Channel Layout

Channels (`Chan[T]`) are allocated in linear memory with the following 32-byte header:

```
Offset 0:  capacity (i32)
Offset 4:  element_size (i32)
Offset 8:  head (i32)
Offset 12: count (i32)
Offset 16: is_closed (i32)
Offset 20: buffer_ptr (i32 -> payload elements ring buffer)
Offset 24: waiting_reader_task_id (i32)
Offset 28: waiting_writer_task_id (i32)
```

### Channel Operations & Task Execution Semantics

- **Ring Buffer `send(val)`**:
  - Computes `write_idx = (head + count) % capacity`.
  - Writes the value into `buffer_ptr + write_idx * sizeof(T)` and increments `count`.
- **Ring Buffer `recv()`**:
  - When `count > 0`: extracts item at `head`, increments `head = (head + 1) % capacity`, decrements `count`, and returns `Option.Some(val)`.
  - When channel is empty: returns `Option.None` (allowing `while let` or `recv() or_else fallback` expressions to branch gracefully).
- **Channel `close()`**:
  - Marks `is_closed = 1` at offset 16.
- **Select Semantics (`select`)**:
  - Evaluates channel readiness (`count > 0`). If ready, consumes an element without blocking. If none are ready, falls back to `default => { ... }`.
- **Task Spawning (`spawn worker(...)`)**:
  - Concurrency parameters allow sendable flat types and channels across tasks (`isSendableAcrossVines`).
  - Tasks execute in single-threaded environments cooperatively and deterministically without runtime data races.

---

## 3. Precision IEEE-754 Float Formatting & Parser Error Recovery

### Precision Float-to-String Formatting (`buildF64ToStrFunc`)

Located in [`pkg/compiler/wasm_emit.go`](file:///media/john/4f4b301d-6085-48e0-85e2-a5f710b19884/Documents/030-ube/ube/pkg/compiler/wasm_emit.go):
- **NaN Detection**: Emits `f64.ne (val != val)` check returning static string `"NaN"`.
- **Infinities**: Reinterprets `f64` bits via `i64.reinterpret_f64` (opcode `0xbd`):
  - `0x7FF0000000000000` -> `"Inf"`
  - `0xFFF0000000000000` (sleb64 `-0x0010000000000000`) -> `"-Inf"`
- **Negative Zero (`-0.0`)**: Extracts sign bit `(i64.reinterpret_f64(val) >> 63) & 1` to correctly emit negative signs on `-0.0` before applying `f64.abs`.
- **Fractional Precision**: Multiplies fractional components without truncating precision or dropping fractional representation.

### Resilient Parser Error Recovery

Located in [`pkg/compiler/parser_stmt.go`](file:///media/john/4f4b301d-6085-48e0-85e2-a5f710b19884/Documents/030-ube/ube/pkg/compiler/parser_stmt.go) and [`pkg/compiler/parser_expr.go`](file:///media/john/4f4b301d-6085-48e0-85e2-a5f710b19884/Documents/030-ube/ube/pkg/compiler/parser_expr.go):
- **Statement Recovery (`parseStmtWithRecovery`)**: Statements inside blocks are wrapped in recovery handlers. Syntax panics are caught and converted to diagnostics in `p.errors`.
- **Statement Resynchronization (`synchronizeStmt`)**: Advances past malformed tokens until reaching a semicolon or statement boundary (`let`, `return`, `if`, `while`, `for`, `arena`, `spawn`, `break`, `continue`, `match`, `select`, `defer`, `}`).
- **Balanced Delimiter Skipping (`skipToMatchingDelimiter`)**: Skips matching `()`, `[]`, and `{}` pairs to prevent expression parsing errors from cascading into surrounding block scopes.
- **Multiple Diagnostics**: Compiling or running `ube check` reports multiple isolated syntax faults across different lines instead of halting on the first failure.
