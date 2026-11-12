package wasi2

import (
	"github.com/JohnEsleyer/ube/pkg/compiler"
	"github.com/JohnEsleyer/ube/pkg/targets"
)

// Wasi2Backend emits WASI 0.2 (Preview 2) Component Model binaries.
type Wasi2Backend struct{}

// NewBackend creates the WASI 0.2 component-model backend.
func NewBackend() targets.Backend {
	return &Wasi2Backend{}
}

// Name implements targets.Backend.
func (b *Wasi2Backend) Name() string {
	return targets.TargetWasi02
}

// Compile lowers the checked program to a core WebAssembly module (with
// Canonical ABI cabi_realloc support) and wraps it into a WASI 0.2
// Component Model binary.
func (b *Wasi2Backend) Compile(prog *compiler.Program, cfg targets.TargetConfig) ([]byte, error) {
	coreBytes, err := compiler.EmitWasmWithConfig(prog, compiler.EmitWasmConfig{
		MemTrace: cfg.MemTrace,
	})
	if err != nil {
		return nil, err
	}
	return WrapCoreModuleIntoComponent(coreBytes)
}

// StdlibOverlay implements targets.Backend: target-sensitive packages
// resolve from the std/wasi2 component-model tree.
func (b *Wasi2Backend) StdlibOverlay() map[string]string {
	return map[string]string{
		"wasi": "wasi2/wasi",
		"fs":   "wasi2/fs",
		"io":   "wasi2/io",
	}
}
