package compiler_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

// compileLoadedFile runs the full pipeline on a file that may contain imports:
// module loading, generic monomorphization, type checking, C generation,
// native compilation, and execution.
func compileLoadedFile(t *testing.T, mainFile string) string {
	t.Helper()
	cc, err := exec.LookPath("gcc")
	if err != nil {
		cc, err = exec.LookPath("clang")
	}
	if err != nil {
		t.Skip("gcc or clang required for native execution tests")
	}

	prog, err := compiler.NewModuleLoader().Load(mainFile)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if err := compiler.Monomorphize(prog); err != nil {
		t.Fatalf("monomorphize error: %v", err)
	}
	if err := compiler.NewTypechecker().Check(prog); err != nil {
		t.Fatalf("typecheck error: %v", err)
	}

	tmp := t.TempDir()
	cfile := filepath.Join(tmp, "test_out.c")
	binary := filepath.Join(tmp, "test_bin")
	if err := os.WriteFile(cfile, []byte(compiler.NewGenerator(prog).Generate()), 0600); err != nil {
		t.Fatal(err)
	}
	runtimePath, err := filepath.Abs("../../runtime")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cc, "-std=gnu11", "-O2", "-pthread", "-I"+runtimePath, cfile, "-o", binary)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("C compilation error: %v\n%s", err, out)
	}
	out, err := exec.Command(binary).CombinedOutput()
	if err != nil {
		t.Fatalf("runtime error: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeUbeFile(t *testing.T, dir, name, src string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStdlib_U8PrimitiveType(t *testing.T) {
	src := `
fn main() {
    let b: u8 = 255
    let mut s = []u8{}
    s.push(b)
    s.push(42)
    println(s.len)
    println(s[0])
    println(s[1])
}
`
	out := runSourceNative(t, src)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected 3 lines of output, got: %q", out)
	}
	if lines[0] != "2" || lines[1] != "255" || lines[2] != "42" {
		t.Errorf("unexpected u8 outputs: %v", lines)
	}
}

func TestStdlib_U8StringBytes(t *testing.T) {
	src := `
fn main() {
    let text = "Ube"
    println(text[0])
    println(text[1])
    println(text[2])
}
`
	out := runSourceNative(t, src)
	if out != "85\n98\n101" {
		t.Fatalf("expected string bytes 85/98/101, got %q", out)
	}
}

func TestStdlib_ImportMath(t *testing.T) {
	dir := t.TempDir()
	mainFile := writeUbeFile(t, dir, "main.ube", `
import "std/math"

fn main() {
    let c = clamp(15, 0, 10)
    let m = max(42, 99)
    println(c)
    println(m)
    println(pow(2.0, 10))
}
`)

	prog, err := compiler.NewModuleLoader().Load(mainFile)
	if err != nil {
		t.Fatalf("failed to load with std/math: %v", err)
	}
	if err := compiler.Monomorphize(prog); err != nil {
		t.Fatalf("monomorphize failed: %v", err)
	}
	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typecheck failed: %v", err)
	}
	cCode := compiler.NewGenerator(prog).Generate()
	if !strings.Contains(cCode, "clamp") || !strings.Contains(cCode, "max") {
		t.Fatalf("expected generated C to contain std/math functions")
	}

	if out := compileLoadedFile(t, mainFile); out != "10\n99\n1024" {
		t.Fatalf("unexpected math output: %q", out)
	}
}

func TestStdlib_ImportStringsBytesAndCollections(t *testing.T) {
	dir := t.TempDir()
	mainFile := writeUbeFile(t, dir, "main.ube", `
import "std/strings"
import "std/bytes"
import "std/collections"

fn main() {
    let has = str_contains("hello world", "world")
    println(has)

    let mut st = stack_new[i32]()
    st.push(100)
    st.push(200)
    println(st.len())
    if let v = st.pop() {
        println(v)
    }

    let mut b = buffer_new(8)
    b.write_u8(65)
    b.write_u8(66)
    println(b.len())
    println(b.get(0))

    let mut m = map_new[string, i32]()
    m.put("answer", 42)
    if let x = m.get("answer") {
        println(x)
    }
}
`)

	prog, err := compiler.NewModuleLoader().Load(mainFile)
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}
	if err := compiler.Monomorphize(prog); err != nil {
		t.Fatalf("monomorphize failed: %v", err)
	}
	if err := compiler.NewTypechecker().Check(prog); err != nil {
		t.Fatalf("typecheck failed: %v", err)
	}

	want := "true\n2\n200\n2\n65\n42"
	if out := compileLoadedFile(t, mainFile); out != want {
		t.Fatalf("unexpected stdlib output:\n got: %q\nwant: %q", out, want)
	}
}

func TestStdlib_ImportWasi(t *testing.T) {
	dir := t.TempDir()
	mainFile := writeUbeFile(t, dir, "main.ube", `
import "std/wasi"

fn main() {
    println(WASI_STDOUT())
    println(WASI_STDIN())
}
`)
	prog, err := compiler.NewModuleLoader().Load(mainFile)
	if err != nil {
		t.Fatalf("failed to load std/wasi: %v", err)
	}
	if err := compiler.NewTypechecker().Check(prog); err != nil {
		t.Fatalf("typecheck failed: %v", err)
	}
	if out := compileLoadedFile(t, mainFile); out != "1\n0" {
		t.Fatalf("unexpected wasi output: %q", out)
	}
}

func TestStdlib_ImportIO(t *testing.T) {
	dir := t.TempDir()
	mainFile := writeUbeFile(t, dir, "main.ube", `
import "std/io"

fn main() {
    let mut w = bytes_writer_new()
    let data = []u8{72, 101, 108, 108, 111}
    let n = w.write(data)
    if let written = n {
        println(written)
    }

    let mut r = bytes_reader_new(w.to_slice())
    let mut buf = []u8{0, 0, 0, 0, 0}
    let rn = r.read(buf)
    if let read_len = rn {
        println(read_len)
        println(buf[0])
        println(buf[4])
    }

    let mut src = bytes_reader_new([]u8{65, 66, 67})
    let mut dst = bytes_writer_new()
    let copied = copy(dst, src)
    println(copied)
    println(dst.len())
}
`)
	prog, err := compiler.NewModuleLoader().Load(mainFile)
	if err != nil {
		t.Fatalf("failed to load std/io: %v", err)
	}
	if err := compiler.Monomorphize(prog); err != nil {
		t.Fatalf("monomorphize failed: %v", err)
	}
	if err := compiler.NewTypechecker().Check(prog); err != nil {
		t.Fatalf("typecheck failed: %v", err)
	}
	want := "5\n5\n72\n111\n3\n3"
	if out := compileLoadedFile(t, mainFile); out != want {
		t.Fatalf("unexpected io output:\n got: %q\nwant: %q", out, want)
	}
}

func TestStdlib_ImportWithUbeSuffix(t *testing.T) {
	dir := t.TempDir()
	mainFile := writeUbeFile(t, dir, "main.ube", `
import "std/math.ube"

fn main() {
    println(clamp(5, 0, 3))
}
`)
	if out := compileLoadedFile(t, mainFile); out != "3" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestStdlib_UnknownModuleIsRejected(t *testing.T) {
	dir := t.TempDir()
	mainFile := writeUbeFile(t, dir, "main.ube", `
import "std/does_not_exist"

fn main() {}
`)
	_, err := compiler.NewModuleLoader().Load(mainFile)
	if err == nil || !strings.Contains(err.Error(), "unknown standard library module") {
		t.Fatalf("expected unknown std module error, got: %v", err)
	}
}

func TestStdlib_LocalProjectOverride(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "std"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "std", "local.ube"),
		[]byte("pub fn local_value() -> i32 { return 7 }\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}
	mainFile := writeUbeFile(t, dir, "main.ube", `
import "std/local"

fn main() {
    println(local_value())
}
`)
	if out := compileLoadedFile(t, mainFile); out != "7" {
		t.Fatalf("unexpected output from local std override: %q", out)
	}
}

func TestStdlib_UBEPathResolution(t *testing.T) {
	stdRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stdRoot, "std"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(stdRoot, "std", "envmod.ube"),
		[]byte("pub fn env_value() -> i32 { return 11 }\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UBE_PATH", stdRoot)

	dir := t.TempDir()
	mainFile := writeUbeFile(t, dir, "main.ube", `
import "std/envmod"

fn main() {
    println(env_value())
}
`)
	if out := compileLoadedFile(t, mainFile); out != "11" {
		t.Fatalf("unexpected output from UBE_PATH module: %q", out)
	}
}
