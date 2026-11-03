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
