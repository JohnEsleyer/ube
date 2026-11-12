// Package wasi1 is the WASI Preview 1 (0.1) target backend. It emits a core
// WebAssembly module importing the wasi_snapshot_preview1 syscall surface
// (fd_write, path_open, clock_time_get, ...) and is the default ube target.
//
// This package owns everything Preview 1: the linear memory scratch layout,
// the syscall intrinsics, and the std/wasi1 standard library tree. Nothing
// WASI 0.2 (component model, Canonical ABI, cabi_realloc) is referenced
// here.
package wasi1

import (
	"github.com/JohnEsleyer/ube/pkg/compiler"
	"github.com/JohnEsleyer/ube/pkg/targets"
)

// Wasi1Backend emits WASI Preview 1 core modules.
type Wasi1Backend struct{}

// NewBackend creates the WASI Preview 1 backend.
func NewBackend() targets.Backend {
	return &Wasi1Backend{}
}

// Name implements targets.Backend.
func (b *Wasi1Backend) Name() string {
	return targets.TargetWasiPreview1
}

// Compile lowers the checked program to a core WebAssembly module with
// wasi_snapshot_preview1 imports.
func (b *Wasi1Backend) Compile(prog *compiler.Program, cfg targets.TargetConfig) ([]byte, error) {
	return compiler.EmitWasmWithConfig(prog, compiler.EmitWasmConfig{
		MemTrace: cfg.MemTrace,
	})
}

// StdlibOverlay implements targets.Backend: target-sensitive packages
// resolve from the std/wasi1 Preview 1 tree. std/io stays shared: it imports
// "std/wasi", which routes to std/wasi1/wasi via the overlay below.
func (b *Wasi1Backend) StdlibOverlay() map[string]string {
	return map[string]string{
		"wasi": "wasi1/wasi",
		"fs":   "wasi1/fs",
	}
}
