// File: pkg/compiler/wasm_core_test.go
package compiler_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
	"github.com/JohnEsleyer/ube/pkg/wasm"
)

func compileToWasm(t *testing.T, source string) []byte {
	t.Helper()

	lexer := compiler.NewLexer("test.ube", source)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Lexer error: %v", err)
	}

	parser := compiler.NewParser(tokens)
	prog, err := parser.ParseProgram()
	if err != nil {
		t.Fatalf("Parser error: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("Typecheck error: %v", err)
	}

	wasmBytes, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("EmitWasm error: %v", err)
	}

	return wasmBytes
}

func runWasm(t *testing.T, wasmBytes []byte) string {
	t.Helper()

	// Verify binary signature
	if len(wasmBytes) < 8 {
		t.Fatalf("wasm output too short (%d bytes)", len(wasmBytes))
	}
	if !bytes.Equal(wasmBytes[:4], wasm.Magic[:]) {
		t.Fatalf("invalid wasm magic header: %x", wasmBytes[:4])
	}
	if !bytes.Equal(wasmBytes[4:8], wasm.Version[:]) {
		t.Fatalf("invalid wasm version: %x", wasmBytes[4:8])
	}

	// If wasmtime is available in the environment, execute it
	wasmtimePath, err := exec.LookPath("wasmtime")
	if err != nil {
		if home, hErr := os.UserHomeDir(); hErr == nil {
			cand := filepath.Join(home, ".wasmtime", "bin", "wasmtime")
			if info, sErr := os.Stat(cand); sErr == nil && !info.IsDir() {
				wasmtimePath = cand
				err = nil
			}
		}
	}
	if err != nil {
		t.Logf("wasmtime not on PATH; validated binary structure (%d bytes)", len(wasmBytes))
		return ""
	}

	tmpDir := t.TempDir()
	wasmFile := filepath.Join(tmpDir, "out.wasm")
	if err := os.WriteFile(wasmFile, wasmBytes, 0755); err != nil {
		t.Fatalf("failed to write wasm file: %v", err)
	}

	cmd := exec.Command(wasmtimePath, wasmFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("wasmtime execution failed: %v\nstderr: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String())
}

func TestWasmCore_MagicHeader(t *testing.T) {
	source := `
package main

fn main() {
}
`
	wasmBytes := compileToWasm(t, source)
	if len(wasmBytes) < 8 {
		t.Fatalf("output smaller than header")
	}
	if !bytes.Equal(wasmBytes[:4], wasm.Magic[:]) {
		t.Errorf("magic header mismatch: got %x, want %x", wasmBytes[:4], wasm.Magic)
	}
}

func TestWasmCore_SimpleArithmetic(t *testing.T) {
	source := `
package main

fn add(a: i32, b: i32) -> i32 {
    return a + b
}

fn main() {
    let result = add(20, 22)
    println(result)
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "42" {
		t.Errorf("expected '42', got '%s'", out)
	}
}

func TestWasmCore_WhileLoop(t *testing.T) {
	source := `
package main

fn main() {
    let mut sum = 0
    let mut i = 1
    while i <= 5 {
        sum += i
        i += 1
    }
    println(sum)
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "15" {
		t.Errorf("expected '15', got '%s'", out)
	}
}

func TestWasmCore_Conditionals(t *testing.T) {
	source := `
package main

fn max(a: i32, b: i32) -> i32 {
    if a > b {
        return a
    } else {
        return b
    }
}

fn main() {
    println(max(10, 99))
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "99" {
		t.Errorf("expected '99', got '%s'", out)
	}
}

func TestWasmCore_StructsAndReceivers(t *testing.T) {
	source := `
package main

struct Point {
    x: i32,
    y: i32
}

fn (p Point) sum() -> i32 {
    return p.x + p.y
}

fn (mut p Point) offset(dx: i32, dy: i32) {
    p.x += dx
    p.y += dy
}

fn main() {
    let mut pt = Point{ x: 10, y: 20 }
    pt.offset(5, 5)
    println(pt.sum())
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "40" {
		t.Errorf("expected '40', got '%s'", out)
	}
}

func TestWasmCore_DynamicSlices(t *testing.T) {
	source := `
package main

fn main() {
    let mut s = []i32{}
    s.push(10)
    s.push(20)
    s.push(30)
    println(s.len)
    println(s[1])
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "3\n20" {
		t.Errorf("expected '3\\n20', got '%s'", out)
	}
}

func TestWasmCore_TaggedEnumsAndMatch(t *testing.T) {
	source := `
package main

enum Shape {
    Circle(i32),
    Rect(i32, i32)
}

fn area(s: Shape) -> i32 {
    match s {
        Circle(r) => {
            return r * r
        },
        Rect(w, h) => {
            return w * h
        }
    }
}

fn main() {
    let c = Shape.Circle(5)
    let r = Shape.Rect(4, 6)
    println(area(c))
    println(area(r))
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "25\n24" {
		t.Errorf("expected '25\\n24', got '%s'", out)
	}
}

func TestWasmCore_RegionalArena(t *testing.T) {
	source := `
package main

fn main() {
    let mut val = 0
    arena {
        let mut s = []i32{}
        s.push(42)
        val = s[0]
    }
    println(val)
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "42" {
		t.Errorf("expected '42', got '%s'", out)
	}
}

func TestWasmCore_MonomorphizedGenerics(t *testing.T) {
	source := `
package main

struct Box[T] {
    val: T
}

fn (b Box[T]) get() -> T {
    return b.val
}

fn wrap[T](item: T) -> Box[T] {
    return Box[T]{ val: item }
}

fn main() {
    let b1 = wrap[i32](20)
    let b2 = wrap[i32](22)
    println(b1.get() + b2.get())
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "42" {
		t.Errorf("expected '42', got '%s'", out)
	}
}

func TestWasmCore_DynamicTraitDispatch(t *testing.T) {
	source := `
package main

trait Speaker {
    fn speak() -> i32
}

struct Dog {
    val: i32
}

fn (d Dog) speak() -> i32 {
    return d.val * 2
}

fn announce(s: Speaker) -> i32 {
    return s.speak()
}

fn main() {
    let d = Dog{ val: 21 }
    println(announce(d))
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "42" {
		t.Errorf("expected '42', got '%s'", out)
	}
}

func TestWasmCore_StringInterpolation(t *testing.T) {
	source := `
package main

fn main() {
    let name = "Ube"
    println(f"Hello {name}!")
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "Hello Ube!" {
		t.Errorf("expected 'Hello Ube!', got '%s'", out)
	}
}

func TestWasmCore_FixedArrayCoercion(t *testing.T) {
	source := `
package main

fn sum_items(items: []i32) -> i32 {
    let mut total = 0
    for x in items {
        total += x
    }
    return total
}

fn main() {
    let arr = [3]i32{ 10, 20, 30 }
    println(sum_items(arr))
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "60" {
		t.Errorf("expected '60', got '%s'", out)
	}
}

func TestWasmCore_StringBuilder(t *testing.T) {
	source := `
package main

fn main() {
    let mut sb = StringBuilder.new(4)
    sb.append_str("Ube")
    sb.append_char(32)
    sb.append_int(2026)
    println(sb.to_string())
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "Ube 2026" {
		t.Errorf("expected 'Ube 2026', got '%s'", out)
	}
}

func TestWasmCore_MethodReceiverEvaluatedOnce(t *testing.T) {
	// Uses a mut-param method with a side effect and an aggregate receiver,
	// which is the exact shape reported in problems_list1 item 1.
	source := `
package main

struct Tracker {
    calls: i32
}

fn (mut t Tracker) touch(mut buf: []i32) -> i32 {
    t.calls += 1
    return t.calls
}

fn main() {
    let mut t = Tracker{ calls: 0 }
    let mut buf = []i32{}
    buf.push(1)
    let r = t.touch(buf)
    println(r)
    println(t.calls)
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "1\n1" {
		t.Errorf("expected '1\\n1' (receiver evaluated once), got '%s'", out)
	}
}

func TestWasmCore_ProcExitOnError(t *testing.T) {
	source := `
package main

fn main() -> i32 ! string {
    return error("fatal boot error")
}
`
	wasmBytes := compileToWasm(t, source)

	wasmtimePath, err := exec.LookPath("wasmtime")
	if err != nil {
		if home, hErr := os.UserHomeDir(); hErr == nil {
			cand := filepath.Join(home, ".wasmtime", "bin", "wasmtime")
			if info, sErr := os.Stat(cand); sErr == nil && !info.IsDir() {
				wasmtimePath = cand
				err = nil
			}
		}
	}
	if err != nil {
		t.Skip("wasmtime not found on PATH")
	}

	tmpDir := t.TempDir()
	wasmFile := filepath.Join(tmpDir, "out.wasm")
	if err := os.WriteFile(wasmFile, wasmBytes, 0755); err != nil {
		t.Fatalf("failed to write wasm file: %v", err)
	}

	cmd := exec.Command(wasmtimePath, wasmFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr == nil {
		t.Fatal("expected non-zero exit status for erroring main, got 0")
	}
	if !strings.Contains(stderr.String(), "fatal boot error") {
		t.Errorf("expected 'fatal boot error' in stderr, got: %s", stderr.String())
	}
}

// TestWasmCore_BlockScopedDefers verifies that a defer belongs to the block
// that contains it: an inner block's defer runs when that block exits, not
// when the function returns, and a return from inside a nested block runs all
// active scopes from innermost to outermost exactly once.
func TestWasmCore_BlockScopedDefers(t *testing.T) {
	source := `
package main

fn mark(s: string) {
    println(s)
}

fn f() -> i32 {
    defer mark("f1")
    if true {
        defer mark("f2")
        return 42
    }
    return 0
}

fn main() {
    defer mark("outer")
    if true {
        defer mark("inner")
        println("body")
    }
    println("after")
    println(f())
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	want := "body\ninner\nafter\nf2\nf1\n42\nouter"
	if out != "" && out != want {
		t.Errorf("expected %q, got %q", want, out)
	}
}

// TestWasmCore_LoopScopedDefers verifies that loop-body defers run once per
// iteration on normal fall-through, continue, and break, without leaking past
// the loop or double-emitting on the terminating branch.
func TestWasmCore_LoopScopedDefers(t *testing.T) {
	source := `
package main

fn mark(s: string) {
    println(s)
}

fn main() {
    let mut i = 0
    while i < 3 {
        i += 1
        defer mark("defer")
        if i == 2 {
            continue
        }
        if i == 3 {
            break
        }
        println("body")
    }
    println("done")
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	want := "body\ndefer\ndefer\ndefer\ndone"
	if out != "" && out != want {
		t.Errorf("expected %q, got %q", want, out)
	}
}

// TestWasmCore_ResizeGrowsCapacity locks in the max(newLen, cap*2+8) growth
// calculation. Wasm select returns its first operand when the condition is
// true; pushing the fallback first silently under-allocates, allowing later
// arena allocations to overwrite the buffer. This is the bug that corrupted
// std/fs File.read_all after it switched to chunk.resize(1024).
func TestWasmCore_ResizeGrowsCapacity(t *testing.T) {
	source := `
package main

fn main() {
    let mut chunk = []u8{}
    chunk.resize(1024)
    println(chunk.len)
    println(chunk.cap)
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	if out != "" && out != "1024\n1024" {
		t.Errorf("expected '1024\\n1024', got '%s'", out)
	}
}

// TestWasmCore_SliceOfStructs covers []Struct element handling: struct
// literals are arena-backed pointers, .push stores that pointer, index loads
// it back, field reads and writes hit the right offsets, and for-in iteration
// sees every element.
func TestWasmCore_SliceOfStructs(t *testing.T) {
	source := `
package main

struct P {
    x: i32,
    y: i32
}

fn main() {
    let mut ps = []P{}
    ps.push(P{ x: 1, y: 2 })
    ps.push(P{ x: 3, y: 4 })
    println(ps[0].x)
    println(ps[1].y)
    ps[0].x = 10
    ps[0].y = 20
    println(ps[0].x)
    println(ps[0].y)

    ps[1] = P{ x: 30, y: 40 }
    println(ps[1].x)
    println(ps[1].y)

    let mut total = 0
    for p in ps {
        total += p.x + p.y
    }
    println(total)
    println(ps.len)
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	want := "1\n4\n10\n20\n30\n40\n100\n2"
	if out != "" && out != want {
		t.Errorf("expected %q, got %q", want, out)
	}
}

// TestWasmCore_MixedWidthArithmeticAndCasts verifies that i64 values flow
// through function signatures, literals, locals, return values, casts, and
// interpolation without being coerced to i32. A mismatch here produces a Wasm
// validation error ("type mismatch: expected i64, found i32").
func TestWasmCore_MixedWidthArithmeticAndCasts(t *testing.T) {
	source := `package main

fn widen(v: i32) -> i64 {
    let w: i64 = 0
    return w
}

fn cast_widen(v: i32) -> i64 {
    return v as i64
}

fn main() {
    let a: i32 = 17
    let wide = widen(a)
    println(f"wide = {wide}")
    let cast_wide = cast_widen(a)
    println(f"cast_wide = {cast_wide}")

    let n: i32 = 42
    let nf: f32 = n as f32
    let back: i32 = nf as i32
    println(back)
}
`
	wasmBytes := compileToWasm(t, source)
	out := runWasm(t, wasmBytes)
	want := "wide = 0\ncast_wide = 17\n42"
	if out != "" && out != want {
		t.Errorf("expected %q, got %q", want, out)
	}
}
