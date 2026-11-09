package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

const (
	TargetWasiPreview1 = "wasi1"
	TargetWasi02       = "wasi2"
)

// targetLabel returns a short, human-readable name for a build/check target.
func targetLabel(target string) string {
	abs, err := filepath.Abs(target)
	if err != nil {
		abs = target
	}
	abs = filepath.Clean(abs)
	if base := filepath.Base(abs); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return target
}

// defaultWasmOutput picks a Go-style output name: directory targets use the
// directory name, file targets use the file stem.
func defaultWasmOutput(target string) string {
	abs, err := filepath.Abs(target)
	if err != nil {
		abs = target
	}
	abs = filepath.Clean(abs)

	if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
		return filepath.Base(abs) + ".wasm"
	}

	base := filepath.Base(abs)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "main.wasm"
	}
	return base + ".wasm"
}

// requireExecutablePackage enforces the package-main contract for `ube build`
// and `ube run`.
func requireExecutablePackage(prog *compiler.Program) error {
	if prog == nil {
		return fmt.Errorf("no program loaded")
	}
	if prog.Package != "main" {
		return fmt.Errorf("package %q is not executable: executable packages must declare 'package main'", prog.Package)
	}
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*compiler.FnDecl); ok && fn.Name == "main" {
			return nil
		}
	}
	return fmt.Errorf("package main must define 'fn main()'")
}
