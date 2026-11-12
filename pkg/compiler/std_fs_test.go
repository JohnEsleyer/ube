package compiler_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
	// Register the WASI Preview 1/0.2 backend layers for emission.
	_ "github.com/JohnEsleyer/ube/pkg/targets/wasi1"
	_ "github.com/JohnEsleyer/ube/pkg/targets/wasi2"
)

// TestEndToEnd_StdFsOpenOptionsMethod reproduces the failure where
// `opts.open(path)` reported "Unsupported call target". std/fs declares both a
// free function `open` and a receiver method `(OpenOptions) open`; package
// qualification must qualify the free function while leaving the method name
// alone so the call site key stays `fs.OpenOptions.open`.
func TestEndToEnd_StdFsOpenOptionsMethod(t *testing.T) {
	tmpDir := t.TempDir()
	mainSrc := `package main

import "std/fs" as fs

fn touch(path: string) -> bool ! string {
    let mut opts = fs.open_options()
    opts.read(true)
    opts.write(true)
    let mut file = opts.open(path)?
    file.close()?
    return true
}

fn main() {
    let ok = touch("std_fs_method_test.txt") or_else false
    println(ok)
}
`
	mainFile := filepath.Join(tmpDir, "main.ube")
	if err := os.WriteFile(mainFile, []byte(mainSrc), 0644); err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	loader := compiler.NewModuleLoader()
	prog, err := loader.Load(mainFile)
	if err != nil {
		t.Fatalf("failed to load modules: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typechecker rejected std/fs OpenOptions.open: %v", err)
	}

	wasmBytes, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("EmitWasm failed on std/fs OpenOptions.open: %v", err)
	}
	if len(wasmBytes) < 8 {
		t.Fatalf("emitted wasm too small: %d bytes", len(wasmBytes))
	}
}
