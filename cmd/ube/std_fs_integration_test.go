// File: cmd/ube/std_fs_integration_test.go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func compileSourceForTest(t *testing.T, entry, source string) []byte {
	t.Helper()

	loader := compiler.NewModuleLoader()
	prog, err := loader.LoadSource(entry, source)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typecheck failed: %v", err)
	}

	wasmBytes, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	return wasmBytes
}

func runSourceForTest(t *testing.T, name, source string) string {
	t.Helper()

	wasmBytes := compileSourceForTest(t, filepath.Join(t.TempDir(), name+".ube"), source)
	wasmPath := filepath.Join(t.TempDir(), name+".wasm")
	if err := os.WriteFile(wasmPath, wasmBytes, 0755); err != nil {
		t.Fatalf("failed to write wasm: %v", err)
	}

	out, err := runWasmCapture(wasmPath, nil)
	if err != nil {
		t.Fatalf("wasm execution failed: %v\noutput: %s", err, out)
	}
	return out
}

// TestResizeCapacityInProcess locks in the max(newLen, cap*2+8) growth rule.
// Pushing the select fallback first under-allocates the buffer, so subsequent
// arena allocations overwrite it. That was the cause of the corrupted
// std/fs.read_all output after it switched to chunk.resize(1024).
func TestResizeCapacityInProcess(t *testing.T) {
	source := `package main

fn main() {
    let mut chunk = []u8{}
    chunk.resize(1024)
    println(chunk.len)
    println(chunk.cap)
}
`
	out := runSourceForTest(t, "resize_capacity", source)
	if got := strings.TrimSpace(out); got != "1024\n1024" {
		t.Fatalf("expected len/cap 1024/1024, got %q", got)
	}
}

// TestStdFsRoundTripInProcess exercises the real std/fs package end to end:
// free-function/module-method name resolution, write/append/read helpers,
// File.read_all's resized chunk, OpenOptions.open, seeking, and cleanup.
func TestStdFsRoundTripInProcess(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	source := `package main

import "std/fs" as fs

fn main() -> bool ! string {
    println("before write_string")
    fs.write_string("e2e.txt", "first line\n")?
    println("after write_string")
    fs.append_string("e2e.txt", "second line\n")?
    println("after append_string")

    let content = fs.read_to_string("e2e.txt")?
    println(content)

    let mut opts = fs.open_options()
    opts.read(true)
    opts.write(true)
    let mut file = opts.open("e2e.txt")?
    file.seek(0, fs.SEEK_SET())?

    let mut buf = []u8{}
    buf.resize(8)
    let n = file.read(mut buf)?
    println(f"read {n} bytes")
    file.close()?

    fs.remove_file("e2e.txt")?
    let still_there = fs.exists("e2e.txt")
    println(f"exists {still_there}")
    return true
}
`
	out := runSourceForTest(t, "fs_round_trip", source)
	for _, want := range []string{"first line\nsecond line", "read 8 bytes", "exists false"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}
