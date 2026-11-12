package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/compiler"
	"github.com/JohnEsleyer/ube/pkg/targets"
	"github.com/JohnEsleyer/ube/pkg/targets/wasi1"
	"github.com/JohnEsleyer/ube/pkg/targets/wasi2"
)

const (
	TargetWasiPreview1 = "wasi1"
	TargetWasi02       = "wasi2"
)

// getBackend routes a --target identifier to its backend implementation.
// The empty string and unrecognized values fall back to WASI Preview 1, the
// default target.
func getBackend(target string) targets.Backend {
	switch target {
	case targets.TargetWasi02:
		return wasi2.NewBackend()
	case "", targets.TargetWasiPreview1:
		return wasi1.NewBackend()
	default:
		return wasi1.NewBackend()
	}
}

// applyStdlibOverlay installs the backend's standard library routing so
// target-sensitive packages (std/wasi, std/fs, std/io) resolve from the
// active target's tree before any package is loaded.
func applyStdlibOverlay(backend targets.Backend) {
	if overlay := backend.StdlibOverlay(); len(overlay) > 0 {
		compiler.SetStdlibRoutes(overlay)
	}
}

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
