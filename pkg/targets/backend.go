// Package targets defines the backend contract between the target-agnostic
// compiler frontend (pkg/compiler) and the per-target code emitters.
//
// Each supported target lives in its own subpackage (wasi1 for WASI Preview
// 1 core modules, wasi2 for WASI 0.2 component-model binaries) and is
// selected at the CLI boundary with --target. The frontend never branches on
// the target: it always lowers the checked program to a core WebAssembly
// module, and the backend decides how to package or extend it.
package targets

import "github.com/JohnEsleyer/ube/pkg/compiler"

// TargetConfig carries the build settings that influence target emission.
type TargetConfig struct {
	MemTrace bool
	OptLevel string
}

// Backend is implemented by every compilation target. It owns all
// target-specific emission: the final binary shape, the syscalls, and the
// standard library tree used to resolve target-sensitive packages.
type Backend interface {
	// Name returns the --target identifier ("wasi1" or "wasi2").
	Name() string

	// Compile lowers the typechecked program to the target's binary format.
	// For wasi1 this is a core WebAssembly module; for wasi2 it is a
	// WebAssembly Component Model binary wrapping the core module.
	Compile(prog *compiler.Program, cfg TargetConfig) ([]byte, error)

	// StdlibOverlay provides target-specific standard library routing:
	// requested package keys mapped to the package key that should actually
	// be loaded (e.g. "wasi" -> "wasi2/wasi"). Entries are optional; packages
	// absent from the overlay resolve normally.
	StdlibOverlay() map[string]string
}

// Target identifiers shared by the CLI and the backend packages.
const (
	TargetWasiPreview1 = "wasi1"
	TargetWasi02       = "wasi2"
)
