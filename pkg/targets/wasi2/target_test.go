// File: pkg/targets/wasi2/target_test.go

package wasi2_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func compileWasi2Source(t *testing.T, src string) []byte {
	t.Helper()
	compiler.SetStdlibRoutes(wasi2.NewBackend().StdlibOverlay())
	defer compiler.SetStdlibRoutes(wasi1.NewBackend().StdlibOverlay())
	compiler.RegisterHostLayer(wasi2.NewHostLayer())
	defer compiler.RegisterHostLayer(wasi1.NewHostLayer())
	compiler.RegisterAbiLayer(wasi2.NewAbiLayer())

	tmpDir := t.TempDir()
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
	out, err := wasi2.NewBackend().Compile(prog, targets.TargetConfig{})
	if err != nil {
		t.Fatalf("wasi2 compile: %v", err)
	}
	if wasmTools, err := exec.LookPath("wasm-tools"); err == nil {
		compPath := filepath.Join(tmpDir, "out.wasm")
		if err := os.WriteFile(compPath, out, 0644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(wasmTools, "validate", "--features", "component-model", compPath)
		if valOut, valErr := cmd.CombinedOutput(); valErr != nil {
			t.Fatalf("wasm-tools validate failed: %v\n%s", valErr, string(valOut))
		}
	}
	return out
}

func TestWasi2BackendName(t *testing.T) {
	if got := wasi2.NewBackend().Name(); got != "wasi2" {
		t.Fatalf("expected backend name wasi2, got %q", got)
	}
}

func TestWasi2CompileProducesComponentBinary(t *testing.T) {
	out := compileWasi2Source(t, `package main

fn main() {
	println("hello wasi2")
}
`)
	if len(out) < 8 {
		t.Fatalf("component binary too small: %d bytes", len(out))
	}
	if !bytes.Equal(out[:8], componentMagic) {
		t.Fatalf("expected component magic %x, got %x", componentMagic, out[:8])
	}
	if !bytes.Contains(out, coreMagic) {
		t.Fatal("component binary does not embed a core WebAssembly module")
	}
	if !bytes.Contains(out, []byte("wasi:cli/run@0.2.0")) {
		t.Fatal("component binary does not export wasi:cli/run@0.2.0")
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

func TestWasi2StdlibPackagesCompile(t *testing.T) {
	out := compileWasi2Source(t, `package main

import "std/wasi" as wasi
import "std/fs" as fs
import "std/io" as io
import "std/time" as time
import "std/env" as env
import "std/random" as random

fn main() {
	let handle = wasi.wasi_stdout()
	let preopen = fs.PREOPEN_BASE_FD()
	let stream = io.stdout()
	let closed = stream.is_closed(wasi.WASI_02_STREAM_CLOSED())
	stream.close()
	wasi.resource_drop_output_stream(handle)

	let out = io.io_stdout()
	let _io = out.write_line("io works") or_else 0
	let _time = time.unix_now()
	let _env = env.args()
	let _rand = random.entropy_u64()
	let p = fs.path_join("dir", "file.txt")
	println(preopen)
	println(closed)
	println(p)
}
`)
	if !bytes.Equal(out[:8], componentMagic) {
		t.Fatalf("expected component magic %x, got %x", componentMagic, out[:8])
	}
}

func TestWasi2MainResultErrorPrintCompiles(t *testing.T) {
	out := compileWasi2Source(t, `package main

fn main() -> i32 ! string {
	return error("boom")
}
`)
	if !bytes.Equal(out[:8], componentMagic) {
		t.Fatalf("expected component magic %x, got %x", componentMagic, out[:8])
	}
}

func TestWasi2ExecuteWithWasmtimeIfAvailable(t *testing.T) {
	wasmtimeBin, err := exec.LookPath("wasmtime")
	if err != nil {
		t.Skip("wasmtime not installed on PATH; skipping execution check")
	}
	out := compileWasi2Source(t, `package main

import "std/fs" as fs

fn main() -> i32 ! string {
	fs.write_string("wasi2_test.txt", "hello component")?
	let got = fs.read_to_string("wasi2_test.txt")?
	fs.remove_file("wasi2_test.txt")?
	println(got)
	return 0
}
`)
	tmpDir := t.TempDir()
	compPath := filepath.Join(tmpDir, "app.wasm")
	if err := os.WriteFile(compPath, out, 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(wasmtimeBin, "run", "--dir="+tmpDir, "--wasm", "component-model=y", compPath)
	cmd.Dir = tmpDir
	runOut, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Fatalf("wasmtime run failed: %v\n%s", runErr, string(runOut))
	}
	if !strings.Contains(string(runOut), "hello component") {
		t.Fatalf("expected 'hello component' in output, got %q", string(runOut))
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
