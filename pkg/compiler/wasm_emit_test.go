package compiler_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func TestDirectWasm_ArithmeticAndPrint(t *testing.T) {
	src := `
fn add(a: i32, b: i32) -> i32 {
	return a + b
}

fn main() {
	let x = 10
	let y = 20
	let z = add(x, y)
	println(z)
}
`
	lexer := compiler.NewLexer("test.ube", src)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("tokenize error: %v", err)
	}

	parser := compiler.NewParser(tokens)
	prog, err := parser.ParseProgram()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typecheck error: %v", err)
	}

	wasmData, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("EmitWasm failed: %v", err)
	}

	if len(wasmData) < 8 {
		t.Fatalf("Wasm binary too small: %d bytes", len(wasmData))
	}
	// Verify Wasm magic header \0asm\1\0\0\0
	if wasmData[0] != 0x00 || wasmData[1] != 0x61 || wasmData[2] != 0x73 || wasmData[3] != 0x6d {
		t.Fatalf("Invalid wasm magic bytes: %x", wasmData[:4])
	}
}

func TestDirectWasm_StringLiteralsAndArena(t *testing.T) {
	src := `
struct Point {
	x: i32,
	y: i32
}

fn main() {
	println("Direct Wasm 0.8.1")
	arena {
		let p = Point{ x: 42, y: 84 }
		println(p.x)
		println(p.y)
	}
}
`
	lexer := compiler.NewLexer("arena.ube", src)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("tokenize error: %v", err)
	}

	parser := compiler.NewParser(tokens)
	prog, err := parser.ParseProgram()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typecheck error: %v", err)
	}

	wasmData, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("EmitWasm failed: %v", err)
	}

	tmpFile := filepath.Join(t.TempDir(), "test_arena.wasm")
	if err := os.WriteFile(tmpFile, wasmData, 0644); err != nil {
		t.Fatalf("failed to write test wasm: %v", err)
	}
}

func TestDirectWasm_WhileLoop(t *testing.T) {
	src := `
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
	lexer := compiler.NewLexer("while_test.ube", src)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("tokenize error: %v", err)
	}

	parser := compiler.NewParser(tokens)
	prog, err := parser.ParseProgram()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typecheck error: %v", err)
	}

	wasmData, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("EmitWasm error: %v", err)
	}

	if len(wasmData) < 8 {
		t.Fatalf("wasm binary too small: %d bytes", len(wasmData))
	}
	if wasmData[0] != 0x00 || wasmData[1] != 0x61 || wasmData[2] != 0x73 || wasmData[3] != 0x6d {
		t.Fatalf("invalid wasm magic bytes: %x", wasmData[:4])
	}

	tmpFile := filepath.Join(t.TempDir(), "while_loop.wasm")
	if err := os.WriteFile(tmpFile, wasmData, 0644); err != nil {
		t.Fatalf("failed to write test wasm: %v", err)
	}

	// If the wasmtime binary is available, execute and verify output.
	if wasmtimePath, err2 := exec.LookPath("wasmtime"); err2 == nil {
		out, runErr := exec.Command(wasmtimePath, tmpFile).CombinedOutput()
		if runErr != nil {
			t.Fatalf("wasmtime execution failed: %v\nOutput: %s", runErr, string(out))
		}
		if strings.TrimSpace(string(out)) != "15" {
			t.Fatalf("expected '15', got %q", string(out))
		}
	}
}

func TestDirectWasm_CompoundAssignAndUnary(t *testing.T) {
	src := `
fn main() {
    let mut a = 10
    a += 5
    a -= 3
    a *= 2
    let b = -a
    let c = !false
    println(a)
    println(b)
    println(c)
}
`
	lexer := compiler.NewLexer("compound.ube", src)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("tokenize error: %v", err)
	}

	parser := compiler.NewParser(tokens)
	prog, err := parser.ParseProgram()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typecheck error: %v", err)
	}

	wasmData, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("EmitWasm error: %v", err)
	}
	if len(wasmData) < 8 {
		t.Fatalf("binary too small: %d", len(wasmData))
	}
}

func TestDirectWasm_ArraysAndSlices(t *testing.T) {
	src := `
fn main() {
	let arr = [3]i32{10, 20, 30}
	println(arr.len)
	println(arr[0])
	println(arr[2])

	let mut s = []i32{100, 200, 300}
	println(s.len)
	s[1] = 999
	println(s[1])

	let mut total = 0
	for val in s {
		total += val
	}
	println(total)
}
`
	lexer := compiler.NewLexer("arrays_slices.ube", src)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("tokenize error: %v", err)
	}

	parser := compiler.NewParser(tokens)
	prog, err := parser.ParseProgram()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typecheck error: %v", err)
	}

	wasmData, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("EmitWasm error: %v", err)
	}
	if len(wasmData) < 8 {
		t.Fatalf("binary too small: %d", len(wasmData))
	}

	tmpFile := filepath.Join(t.TempDir(), "arrays_slices.wasm")
	if err := os.WriteFile(tmpFile, wasmData, 0644); err != nil {
		t.Fatalf("failed to write test wasm: %v", err)
	}

	if wasmtimePath, err2 := exec.LookPath("wasmtime"); err2 == nil {
		out, runErr := exec.Command(wasmtimePath, tmpFile).CombinedOutput()
		if runErr != nil {
			t.Fatalf("wasmtime execution failed: %v\nOutput: %s", runErr, string(out))
		}
		expected := "3\n10\n30\n3\n999\n1399"
		if strings.TrimSpace(string(out)) != expected {
			t.Fatalf("expected:\n%s\ngot:\n%s", expected, string(out))
		}
	}
}

func TestDirectWasm_MethodsAndForLoop(t *testing.T) {
	src := `
struct Counter {
    val: i32
}

fn (c Counter) double() -> i32 {
    return c.val * 2
}

fn main() {
    let mut total = 0
    for i in 1..4 {
        total += i
    }
    let c = Counter{ val: total }
    let ans = c.double()
    println(ans)
}
`
	lexer := compiler.NewLexer("methods.ube", src)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("tokenize error: %v", err)
	}

	parser := compiler.NewParser(tokens)
	prog, err := parser.ParseProgram()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typecheck error: %v", err)
	}

	wasmData, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("EmitWasm error: %v", err)
	}
	if len(wasmData) < 8 {
		t.Fatalf("binary too small: %d", len(wasmData))
	}
}

func TestDirectWasm_BitwiseAndLogicalOps(t *testing.T) {
	src := `
fn main() {
	let a = 12 & 10
	let b = 12 | 10
	let c = 12 ^ 10
	let d = 2 << 3
	let e = 16 >> 2
	let f = 17 % 5
	let g = true && false
	let h = true || false
	println(a)
	println(b)
	println(c)
	println(d)
	println(e)
	println(f)
	println(g)
	println(h)
}
`
	lexer := compiler.NewLexer("bitwise.ube", src)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("tokenize error: %v", err)
	}

	parser := compiler.NewParser(tokens)
	prog, err := parser.ParseProgram()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typecheck error: %v", err)
	}

	wasmData, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("EmitWasm error: %v", err)
	}
	if len(wasmData) < 8 {
		t.Fatalf("binary too small: %d", len(wasmData))
	}
}
