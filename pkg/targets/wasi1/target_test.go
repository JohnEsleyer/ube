package wasi1_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
	"github.com/JohnEsleyer/ube/pkg/targets"
	"github.com/JohnEsleyer/ube/pkg/targets/wasi1"
)

// coreMagic is the core WebAssembly module prefix (\0asm + version 1).
var coreMagic = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

func helloProgram(t *testing.T) *compiler.Program {
	t.Helper()
	tmpDir := t.TempDir()
	src := `package main

fn main() {
	println("hello wasi1")
}
`
	mainFile := filepath.Join(tmpDir, "main.ube")
	if err := os.WriteFile(mainFile, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	prog, err := compiler.NewModuleLoader().Load(mainFile)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typecheck: %v", err)
	}
	return prog
}

func TestWasi1BackendName(t *testing.T) {
	if got := wasi1.NewBackend().Name(); got != "wasi1" {
		t.Fatalf("expected backend name wasi1, got %q", got)
	}
}

func TestWasi1BackendProducesCoreModule(t *testing.T) {
	prog := helloProgram(t)
	out, err := wasi1.NewBackend().Compile(prog, targets.TargetConfig{})
	if err != nil {
		t.Fatalf("wasi1 compile: %v", err)
	}
	if !bytes.Equal(out[:8], coreMagic) {
		t.Fatalf("expected core module magic %x, got %x", coreMagic, out[:8])
	}
	if !bytes.Contains(out, []byte("wasi_snapshot_preview1")) {
		t.Fatal("wasi1 core module does not import wasi_snapshot_preview1")
	}
}

func TestWasi1StdlibOverlayRoutesTargetPackages(t *testing.T) {
	overlay := wasi1.NewBackend().StdlibOverlay()
	if got := overlay["wasi"]; got != "wasi1/wasi" {
		t.Fatalf("overlay[wasi] = %q, want wasi1/wasi", got)
	}
	if got := overlay["fs"]; got != "wasi1/fs" {
		t.Fatalf("overlay[fs] = %q, want wasi1/fs", got)
	}
	if _, routed := overlay["io"]; routed {
		t.Fatal("wasi1 must not route std/io: it is shared and imports std/wasi")
	}
}

// TestWasi1StdlibPackagesCompile verifies the std/wasi1 tree typechecks and
// emits cleanly when the wasi1 overlay is active (the compiler default).
func TestWasi1StdlibPackagesCompile(t *testing.T) {
	tmpDir := t.TempDir()
	src := `package main

import "std/wasi" as wasi
import "std/fs" as fs

fn main() {
	let cwd = wasi.WASI_AT_FDCWD()
	let mut opts = fs.open_options()
	opts.read(true)
	let mut file = opts.open("wasi1_fs_test.txt")?
	file.close()?
	println(cwd)
}
`
	mainFile := filepath.Join(tmpDir, "main.ube")
	if err := os.WriteFile(mainFile, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	prog, err := compiler.NewModuleLoader().Load(mainFile)
	if err != nil {
		t.Fatalf("loading std/wasi1 packages: %v", err)
	}
	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typechecking std/wasi1 packages: %v", err)
	}
	out, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("emitting std/wasi1 packages: %v", err)
	}
	if len(out) < 8 {
		t.Fatal("emitted module too small")
	}
}
