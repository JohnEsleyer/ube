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
    let mut x = 10
    x += 5
    let res = add(x, 20)
    println(res)
}
`
	lexer := compiler.NewLexer("test.ube", src)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("tokenize failed: %v", err)
	}

	parser := compiler.NewParser(tokens)
	prog, err := parser.ParseProgram()
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typecheck failed: %v", err)
	}

	wasmBytes, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("direct wasm emission failed: %v", err)
	}

	// Verify header magic (\0asm) and version (0x01)
	if len(wasmBytes) < 8 {
		t.Fatalf("wasm bytes too short: %d", len(wasmBytes))
	}
	if string(wasmBytes[0:4]) != "\x00asm" {
		t.Fatalf("invalid wasm magic header: %v", wasmBytes[0:4])
	}

	// If wasmtime or node is available, execute the generated wasm directly
	tmpWasm := filepath.Join(t.TempDir(), "direct.wasm")
	if err := os.WriteFile(tmpWasm, wasmBytes, 0755); err != nil {
		t.Fatalf("write wasm failed: %v", err)
	}

	if wasmtimePath, err := exec.LookPath("wasmtime"); err == nil {
		out, err := exec.Command(wasmtimePath, tmpWasm).CombinedOutput()
		if err != nil {
			t.Fatalf("wasmtime execution failed: %v\nOutput: %s", err, string(out))
		}
		if strings.TrimSpace(string(out)) != "35" {
			t.Fatalf("expected output '35', got: %q", string(out))
		}
	}
}
