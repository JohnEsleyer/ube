package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func TestDefaultWasmOutputForDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hello_app")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if got := defaultWasmOutput(dir); got != "hello_app.wasm" {
		t.Fatalf("expected hello_app.wasm, got %q", got)
	}
}

func TestDefaultWasmOutputForFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.ube")
	if err := os.WriteFile(file, []byte("package main\n\nfn main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := defaultWasmOutput(file); got != "main.wasm" {
		t.Fatalf("expected main.wasm, got %q", got)
	}
}

func TestParseBuildArgsDefaultsToCurrentDirectory(t *testing.T) {
	target, output, ai, err := parseBuildArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if target != "." || output != "" || ai {
		t.Fatalf("unexpected defaults: target=%q output=%q ai=%v", target, output, ai)
	}
}

func TestRequireExecutablePackage(t *testing.T) {
	library := &compiler.Program{Package: "util"}
	if err := requireExecutablePackage(library); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("expected non-executable package error, got %v", err)
	}

	noMain := &compiler.Program{Package: "main"}
	if err := requireExecutablePackage(noMain); err == nil || !strings.Contains(err.Error(), "must define") {
		t.Fatalf("expected missing-main error, got %v", err)
	}

	withMain := &compiler.Program{
		Package: "main",
		Decls:   []compiler.Decl{&compiler.FnDecl{Name: "main"}},
	}
	if err := requireExecutablePackage(withMain); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRecursiveTarget(t *testing.T) {
	root, recursive := recursiveTarget("/tmp/project/...")
	if !recursive || root != "/tmp/project" {
		t.Fatalf("unexpected result: root=%q recursive=%v", root, recursive)
	}
	if _, recursive := recursiveTarget("/tmp/project"); recursive {
		t.Fatal("plain directory must not be recursive")
	}
}
