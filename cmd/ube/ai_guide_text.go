package main

import "strings"

// getUbeLanguageGuide returns a self-contained Ube reference aimed at LLMs.
// "@@@" is replaced with a Markdown code fence and "@@" with a backtick so the
// raw string can stay readable.
func getUbeLanguageGuide() string {
	guide := strings.ReplaceAll(ubeLanguageGuide, "@@@", "```")
	return strings.ReplaceAll(guide, "@@", "`")
}

const ubeLanguageGuide = `# The Ube Programming Language: AI Reference Guide

This document is a self-contained specification of Ube intended for AI models.
Read it before writing or modifying Ube code.

## 1. What is Ube? (The Ube Philosophy)

Ube is a statically typed, memory-safe programming language engineered specifically
for high-level games, modern applications, CLI tools, and high-throughput backends.

Ube is **not** a low-level systems language. It does not manipulate hardware
registers, device drivers, or raw pointer arithmetic.

Its foundational pillars are:

- **High-Level Focus, Real-World Pragmatism:** Engineered for game creators, product builders,
  and backend engineers where low latency, fast iteration, and clean state management matter most.
- **Safety Without the Lifetime Tax:** Zero Garbage Collection achieved without lifetime annotations
  ('a) or complex borrow-check syntax. Memory safety is guaranteed via affine move semantics,
  lexical bump arenas (@@arena@@), and generational entity pools (@@Pool[T]@@).
- **Zero Magic:** No implicit conversions, no operator overloading, and no background GC pauses.
  Every control flow, memory scope, and movement is explicit in the source code.
- **Inspired by Go:** Approachable syntax, @@fn@@ / @@let@@ bindings, shared-nothing concurrency
  with @@spawn@@, @@select@@, and typed channels @@Chan[T]@@.
- **Fully Sandboxed, 100% WebAssembly:** Ube targets WebAssembly and WASI natively.
  Programs run safely, portably, and start in sub-milliseconds with ultra-low p99
  latency. The @@unsafe@@ keyword and raw pointer types do not exist; host access
  happens only through typed @@extern fn@@ Wasm imports.
- **AI-Native Clarity:** Clean grammar and explicit scoping enable LLMs to analyze, reason about,
  and generate Ube code with near-zero hallucinations.

## 2. Source files and the CLI

- Programs live in @@.ube@@ files. A file may pull in another file with
  @@import "relative/path.ube"@@; imported declarations become available
  globally (there are no namespaces).
- Comments use @@//@@.
- Typical commands:

@@@sh
ube new myapp          # scaffold a project
ube check main.ube     # parse + typecheck
ube build main.ube     # build main.wasm by default
ube build main.ube --html -o game.html
ube run main.ube       # compile to Wasm and run in a Wasm runtime
ube test               # discover and run test files
ube fmt -w .           # format sources
ube doc main.ube       # print declarations and signatures
ube context            # write an AI-oriented codebase summary
ube guide              # write this guide to ube_guide.md
@@@

## 3. Variables and types

Variables are immutable unless declared with @@mut@@. Types are inferred when
omitted.

@@@ube
fn main() {
    let name = "Ube"          // string (immutable binding)
    let mut count: i32 = 2    // mutable binding
    count = count + 1
    count += 3
    println(f"{name}: {count}")
}
@@@

### Primitive types

- Signed integers: @@i8@@, @@i16@@, @@i32@@, @@i64@@
- Unsigned integers: @@u8@@, @@u16@@, @@u32@@, @@u64@@
- Floats: @@f32@@, @@f64@@
- @@bool@@ with literals @@true@@ and @@false@@
- @@string@@ (UTF-8 bytes, heap/arena allocated, **not flat**)
- @@void@@ for functions that return nothing

### Composite types

- Slices: @@[]T@@ (dynamic, arena allocated)
- Arrays: @@[N]T@@ (fixed size, value type)
- Optionals: @@T?@@ (value or @@none@@)
- Results: @@T ! E@@ (success value or error value)
- Channels: @@Chan[T]@@
- Pools: @@Pool[T]@@
- @@StringBuilder@@
- User structs and enums by name

### Strings

String literals use double quotes. Prefix a literal with @@f@@ to enable
interpolation with @@{expr}@@; plain strings are literal:

@@@ube
fn main() {
    let who = "world"
    println("hello {who}")            // prints: hello {who}
    println(f"hello {who}, 1 + 1 = {1 + 1}")   // prints: hello world, 1 + 1 = 2
}
@@@

@@import@@ example:

@@@ube
import "lib.ube"
@@@

Strings are indexed as bytes (@@s[i]@@ yields @@u8@@) and sliced with
@@s[start:end]@@ (end exclusive), which returns a new string:

@@@ube
fn main() {
    let s = "active_item"
    println(s[0:6])   // active
}
@@@

## 4. Functions and methods

@@@ube
fn add(a: i32, b: i32) -> i32 {
    return a + b
}

struct Rect {
    width: i32,
    height: i32
}

// Value receiver: the struct is copied.
fn (r Rect) area() -> i32 {
    return r.width * r.height
}

// Mutable receiver: the struct is mutated in place.
fn (mut r Rect) scale(factor: i32) {
    r.width *= factor
    r.height *= factor
}
@@@

Functions are declared at top level. There are no closures; anonymous
functions do not exist. Use @@defer@@ to schedule a call at scope exit:

@@@ube
fn cleanup() { println("done") }

fn main() {
    defer cleanup()
    println("working")
}
@@@

## 5. Structs, enums, and pattern matching

Structs are value types. Literals use @@Name { field: value, ... }@@.

@@@ube
struct Point {
    x: f32,
    y: f32
}

fn main() {
    let p = Point { x: 1.0, y: 2.5 }
    println(p.x)
}
@@@

Enums are tagged unions. Variants may carry zero or more payload types, and
@@match@@ destructures them exhaustively:

@@@ube
enum Shape {
    Circle(f32),
    Rect(f32, f32),
    Empty
}

fn describe(s: Shape) {
    match s {
        Circle(r) => println(f"circle radius {r}"),
        Rect(w, h) => println(f"rect {w}x{h}"),
        Empty => println("empty")
    }
}
@@@

Loops:

@@@ube
fn main() {
    for i in 0..10 {           // range loop, end exclusive
        println(i)
    }

    for item in []i32{1, 2, 3} {
        println(item)
    }

    let mut n = 0
    while n < 3 {
        n += 1
    }
}
@@@

## 6. Errors and optionals

A function that can fail returns a result type written @@Ok ! Err@@:

@@@ube
fn parse_port(code: i32) -> i32 ! string {
    if code < 0 {
        return error("invalid port")
    }
    return code
}
@@@

- @@expr?@@ propagates an error to the caller (only valid in a function whose
  return type is a result).
- @@expr or_else fallback@@ extracts the success value or uses the fallback.

@@@ube
fn boot() -> i32 ! string {
    let port = parse_port(8080)?
    return port
}

fn fallback_port() -> i32 {
    return parse_port(-1) or_else 80
}
@@@

Optionals are written @@T?@@ and use @@none@@:

@@@ube
fn find_user(id: i32) -> string? {
    if id == 1 {
        return "Alice"
    }
    return none
}

fn main() {
    if let name = find_user(1) {
        println(f"found {name}")
    } else {
        println("not found")
    }
}
@@@

@@while let@@ also unwraps optionals and reads channels until they close:

@@@ube
fn drain(queue: Chan[i32]) {
    while let item = queue.recv() {
        println(item)
    }
}
@@@

## 7. Memory model: arenas, scopes, and promote (CRITICAL)

Ube has **no garbage collector**. Heap-like data (slices, strings produced at
runtime, string builders) is allocated from a bump arena associated with the
current region.

### Arena blocks

@@arena { ... }@@ enters a new arena level; every allocation made inside is
reclaimed in one instruction when the block exits. The block may be used as a
statement or as an expression:

@@@ube
fn main() {
    // Statement form
    arena {
        let mut temp = []i32{}
        temp.push(1)
        println(temp.len)
    } // temp's memory is reclaimed here

    // Expression form: the final expression is the block's value
    let doubled = arena {
        let mut local = []i32{1, 2, 3}
        promote(local)
    }
    println(doubled.len)
}
@@@

### The upward reference invariant

An inner arena must not store or mutate data owned by an outer region, because
the inner arena is released first. The compiler rejects such code at
typecheck time, for example:

- @@outer_slice.push(x)@@ inside an arena where @@outer_slice@@ was declared
  outside it;
- assigning an inner slice or string to an outer variable;
- returning a value allocated in an inner arena.

Diagnostics mention: "Outer slice mutation attempted within inner arena ...
use promote(...) to export data".

### promote

To move data out of an arena, copy it into the parent region with
@@promote(value)@@. Promotion is a deep copy:

- slices are reallocated in the parent arena (nested slices, structs, and
  optionals are promoted recursively);
- strings are copied;
- heap-backed values are promoted into the parent arena.

@@@ube
fn main() {
    let mut output = []i32{}

    arena {
        let mut temp = []i32{}
        temp.push(10)
        temp.push(20)
        output = promote(temp)
    }

    println(output.len)  // 2
    println(output[0])   // 10
}
@@@

Rule of thumb: **anything that outlives an arena block must be promoted**.

## 8. Concurrency: tasks, channels, select, pools

- @@spawn f(args...)@@ starts a concurrent task. The target must be a direct
  call, and all arguments must be flat.
- @@Chan[T].new(capacity)@@ creates a buffered channel.
- @@ch.send(v)@@, @@ch.recv()@@, @@ch.close()@@.
- @@select@@ waits on channel reads, with an optional @@default@@ arm.

@@@ube
struct Job {
    id: i32,
    weight: i32
}

fn worker(jobs: Chan[Job], results: Chan[i32]) {
    while let j = jobs.recv() {
        results.send(j.weight * 2)
    }
}

fn main() {
    let jobs = Chan[Job].new(4)
    let results = Chan[i32].new(4)

    spawn worker(jobs, results)

    jobs.send(Job { id: 1, weight: 50 })
    jobs.close()

    select {
        res = results => println(f"result {res}"),
        default => println("no result yet")
    }
}
@@@

### Flat type restriction

Values sent through a channel or passed to a spawned task must be **flat**:
primitives, fixed arrays of flat types, enums/structs whose fields are all
flat, and channels of flat types. The following are **not** flat and are
rejected at compile time:

- @@string@@, @@StringBuilder@@
- slices @@[]T@@
- structs or enums containing any of the above
- @@Pool[T]@@

This guarantees no heap-backed value ever crosses a task boundary, so there is
no shared mutable state and no locking.

### Object pools

@@Pool[T]@@ provides generational handles to reusable slots:

@@@ube
struct Item { id: i32 }

fn main() {
    let mut pool = Pool[Item].new(4)
    let h = pool.spawn(Item { id: 101 })

    if let item = pool.get(h) {
        println(item.id)
    }

    pool.despawn(h)
}
@@@

## 9. Generics and traits

Generics are monomorphized at compile time:

@@@ube
struct Box[T] {
    val: T
}

fn identity[T](x: T) -> T {
    return x
}

fn main() {
    // Generic arguments are explicit at call sites.
    let b = Box[i32] { val: 5 }
    println(identity[i32](b.val))
}
@@@

Traits provide dynamic dispatch. A value can be passed where a trait is
expected and is boxed automatically:

@@@ube
trait Greeter {
    fn greet() -> string
}

struct Human {
    name: string
}

fn (h Human) greet() -> string {
    return f"Hello, {h.name}"
}

fn announce(g: Greeter) {
    println(g.greet())
}

fn main() {
    announce(Human { name: "Ada" })
}
@@@

## 10. WebAssembly & Host Imports

Ube runs in a sandboxed WebAssembly execution environment. System features,
rendering, networking, and platform services are provided through clean
@@extern fn@@ host imports (WASI or JavaScript/host bindings). Host imports are
ordinary functions at the call site and never require an unsafe wrapper:

@@@ube
// Host environment declarations (browser, WASI, or game runtime)
extern fn host_print(msg: string)
extern fn host_fetch(url: string) -> string

fn main() {
    let res = host_fetch("https://api.example.com")
    host_print(res)
}
@@@

Ube is 100% WebAssembly and fully sandboxed: the @@unsafe@@ keyword and raw
pointer types (@@*T@@) do not exist in the language. There are no OS-specific C
headers, native syscall assembly, address-of operations, pointer dereferences,
or @@.ptr@@ escapes. Host capabilities enter only through typed @@extern fn@@
imports, which the Wasm runtime can grant or deny.

## 11. Built-in functions and methods

- Printing: @@print(...)@@, @@println(...)@@, @@println_str(s)@@
- Slices: @@s.len@@, @@s.cap@@, @@s.push(v)@@, @@s.pop()@@, @@s.clear()@@,
  @@s.contains(v)@@, @@s[i]@@
- Strings: @@s.len@@, @@s[i]@@, @@s[a:b]@@, @@str_trim@@, @@str_split@@,
  @@str_replace@@, @@str_contains@@, @@str_starts_with@@, @@str_ends_with@@,
  @@str_index_of@@, @@str_to_int@@, @@int_to_str@@
- Host I/O: @@read_file@@, @@write_file@@, @@file_exists@@, @@file_remove@@,
  @@path_join@@, @@env_get@@, @@env_set@@, @@os_args@@ (capabilities depend on
  the host; sandboxed hosts may deny directory or process operations)
- Math: @@abs@@, @@min@@, @@max@@, @@sqrt@@, @@pow@@, @@floor@@, @@ceil@@
- Time and scheduling: @@time_now_ms@@, @@sleep_ms@@, @@yield@@, @@exit_process@@
- StringBuilder: @@format@@, @@append@@, @@append_str@@, @@append_int@@,
  @@append_i64@@, @@append_char@@, @@append_bool@@, @@to_string@@

## 12. Summary of rules an AI must respect

1. Prefer @@let@@; add @@mut@@ only when reassignment or mutation is needed.
2. Never assume a GC: data escaping an @@arena@@ must be @@promote@@d.
3. Never mutate outer-owned slices or strings from inside an inner arena.
4. Only flat values may cross @@spawn@@ and @@Chan@@ boundaries; strings and
   slices are not flat.
5. Use @@?@@ and @@or_else@@ for results, @@if let@@/@@while let@@ for optionals.
6. Use @@extern fn@@ for Wasm host imports; keep platform access explicit and
   capability-driven. Never use @@unsafe@@, raw pointers, or native syscalls:
   they are rejected by the compiler because Ube is 100% sandboxed WebAssembly.
7. There are no closures or exceptions; split code across files with
   @@import "file.ube"@@ when needed.
`
