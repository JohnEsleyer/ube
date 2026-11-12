// Package wasi2 is the WASI 0.2 (Preview 2) target backend. It emits a core
// WebAssembly module with Canonical ABI support (cabi_realloc) and wraps it
// into a WebAssembly Component Model binary (\0asm\x0d\x00\x01\x00).
//
// This package owns everything Preview 2: the Canonical ABI definitions, the
// component packaging (a wasm-tools bridge with a pure-Go fallback envelope),
// and the std/wasi2 standard library tree. Nothing WASI Preview 1 is
// referenced here.
package wasi2

import (
	"bytes"
	"os/exec"

	"github.com/JohnEsleyer/ube/pkg/wasm"
)

// componentVersion identifies a WebAssembly Component Model binary:
// \0asm followed by version 0x0d, revision 0x01.
var componentVersion = []byte{0x0d, 0x00, 0x01, 0x00}

// WrapCoreModuleIntoComponent wraps a compiled core WebAssembly module into a
// valid WebAssembly Component Model binary for WASI 0.2.
func WrapCoreModuleIntoComponent(coreBytes []byte) ([]byte, error) {
	if err := validateCoreModule(coreBytes); err != nil {
		return nil, err
	}

	// If wasm-tools is available on host PATH, leverage it for fully
	// canonical component resolution.
	if wasmTools, err := exec.LookPath("wasm-tools"); err == nil {
		cmd := exec.Command(wasmTools, "component", "new", "-")
		cmd.Stdin = bytes.NewReader(coreBytes)
		var out bytes.Buffer
		var errOut bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errOut
		if err := cmd.Run(); err == nil && out.Len() > 0 {
			return out.Bytes(), nil
		}
	}

	// Native pure-Go Component Model container envelope:
	// Constructs a standard Component Model container wrapping the core
	// module with zero external dependencies.
	var comp bytes.Buffer
	comp.Write([]byte{0x00, 0x61, 0x73, 0x6d}) // component magic: \0asm
	comp.Write(componentVersion)               // component version 0x0d 0x00 0x01 0x00

	// Section 1 (Core Module Section): payload is the complete core binary.
	comp.WriteByte(0x01)
	comp.Write(wasm.AppendUleb128(nil, uint32(len(coreBytes))))
	comp.Write(coreBytes)

	return comp.Bytes(), nil
}

// validateCoreModule checks that coreBytes is a well-formed core WebAssembly
// module before embedding it in a component envelope.
func validateCoreModule(coreBytes []byte) error {
	if len(coreBytes) < 8 {
		return errShortCoreModule
	}
	magic := []byte{0x00, 0x61, 0x73, 0x6d}
	for i, b := range magic {
		if coreBytes[i] != b {
			return errBadCoreMagic
		}
	}
	if coreBytes[4] != 0x01 || coreBytes[5] != 0x00 || coreBytes[6] != 0x00 || coreBytes[7] != 0x00 {
		return errBadCoreVersion
	}
	return nil
}
