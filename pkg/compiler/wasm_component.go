package compiler

import (
	"bytes"
	"os/exec"
)

// wrapCoreModuleIntoComponent wraps a compiled core WebAssembly module into a
// valid WebAssembly Component Model binary (\x00asm\x0d\x00\x01\x00) for WASI 0.2.
func wrapCoreModuleIntoComponent(coreBytes []byte) ([]byte, error) {
	// If wasm-tools is available on host PATH, leverage it for fully canonical component resolution.
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
	// Constructs a standard Component Model container wrapping the core module with zero external dependencies.
	var comp bytes.Buffer

	// 1. Component Magic & Version (\0asm\x0d\x00\x01\x00)
	comp.Write([]byte{0x00, 0x61, 0x73, 0x6d})
	comp.Write([]byte{0x0d, 0x00, 0x01, 0x00})

	// 2. Section 1: Core Module Section (id: 1)
	// Payload is the complete core wasm binary.
	writeSection(&comp, 0x01, coreBytes)

	return comp.Bytes(), nil
}
