package wasi2_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
	"github.com/JohnEsleyer/ube/pkg/targets"
	"github.com/JohnEsleyer/ube/pkg/targets/wasi1"
	wasi2 "github.com/JohnEsleyer/ube/pkg/targets/wasi2"
)

// componentMagic is the Component Model binary prefix (\0asm + 0x0d 0x00 0x01 0x00).
var componentMagic = []byte{0x00, 0x61, 0x73, 0x6d, 0x0d, 0x00, 0x01, 0x00}

// coreMagic is the core WebAssembly module prefix (\0asm + version 1).
var coreMagic = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

func helloProgram(t *testing.T) *compiler.Program {
	t.Helper()
	tmpDir := t.TempDir()
	src := `package main

fn main() {
	println("hello wasi2")
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

func TestWasi2BackendName(t *testing.T) {
	if got := wasi2.NewBackend().Name(); got != "wasi2" {
		t.Fatalf("expected backend name wasi2, got %q", got)
	}
}

func TestWasi2CompileProducesComponentBinary(t *testing.T) {
	prog := helloProgram(t)
	out, err := wasi2.NewBackend().Compile(prog, targets.TargetConfig{})
	if err != nil {
		t.Fatalf("wasi2 compile: %v", err)
	}
	if len(out) < 8 {
		t.Fatalf("component binary too small: %d bytes", len(out))
	}
	if !bytes.Equal(out[:8], componentMagic) {
		t.Fatalf("expected component magic %x, got %x", componentMagic, out[:8])
	}
	// The core module must be embedded as the component's core module
	// section: the wrapper emits section id 1 with the core binary inside.
	if !bytes.Contains(out, coreMagic) {
		t.Fatal("component binary does not embed a core WebAssembly module")
	}
}

func TestWasi2WrapRejectsNonCoreModules(t *testing.T) {
	if _, err := wasi2.WrapCoreModuleIntoComponent([]byte{0x00, 0x61}); err == nil {
		t.Fatal("expected error for truncated core module")
	}
	badMagic := append([]byte{}, coreMagic...)
	badMagic[0] = 0xff
	if _, err := wasi2.WrapCoreModuleIntoComponent(badMagic); err == nil {
		t.Fatal("expected error for invalid core magic")
	}
}

func TestWasi2StdlibOverlayRoutesTargetPackages(t *testing.T) {
	overlay := wasi2.NewBackend().StdlibOverlay()
	for key, want := range map[string]string{
		"wasi": "wasi2/wasi",
		"fs":   "wasi2/fs",
		"io":   "wasi2/io",
	} {
		if got := overlay[key]; got != want {
			t.Fatalf("overlay[%q] = %q, want %q", key, got, want)
		}
	}
}

// TestWasi2StdlibPackagesCompile verifies the std/wasi2 tree typechecks and
// emits cleanly when the wasi2 overlay is active.
func TestWasi2StdlibPackagesCompile(t *testing.T) {
	compiler.SetStdlibRoutes(wasi2.NewBackend().StdlibOverlay())
	defer compiler.SetStdlibRoutes(wasi1.NewBackend().StdlibOverlay())

	tmpDir := t.TempDir()
	src := `package main

import "std/wasi" as wasi
import "std/fs" as fs
import "std/io" as io

fn main() {
	let handle = wasi.wasi_stdout()
	let preopen = fs.PREOPEN_BASE_FD()
	let stream = io.stdout()
	let closed = stream.is_closed(wasi.WASI_02_STREAM_CLOSED())
	println(handle + preopen)
	println(closed)
}
`
	mainFile := filepath.Join(tmpDir, "main.ube")
	if err := os.WriteFile(mainFile, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	prog, err := compiler.NewModuleLoader().Load(mainFile)
	if err != nil {
		t.Fatalf("loading std/wasi2 packages: %v", err)
	}
	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typechecking std/wasi2 packages: %v", err)
	}
	out, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("emitting std/wasi2 packages: %v", err)
	}
	if len(out) < 8 {
		t.Fatal("emitted module too small")
	}
}

func TestCanonicalABIContract(t *testing.T) {
	if wasi2.ReallocName != "cabi_realloc" {
		t.Fatalf("unexpected Canonical ABI realloc name: %q", wasi2.ReallocName)
	}
	if wasi2.MemoryExportName != "memory" {
		t.Fatalf("unexpected Canonical ABI memory export: %q", wasi2.MemoryExportName)
	}
	if len(wasi2.ReallocParams) != 4 {
		t.Fatal("cabi_realloc must take four i32 parameters")
	}
	if len(wasi2.ReallocResults) != 1 {
		t.Fatal("cabi_realloc must return one i32 result")
	}
}
