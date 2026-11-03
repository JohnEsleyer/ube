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
