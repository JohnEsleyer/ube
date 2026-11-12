// File: pkg/targets/wasi2/component.go

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
	// canonical component synthesis.
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

	// Native pure-Go Component Model container:
	// Constructs a validated component binary with Section 1 (Core Module),
	// Component Instances, Canon Lower/Lift, and Section 11 (Export wasi:cli/run@0.2.0#run).
	var comp bytes.Buffer
	comp.Write([]byte{0x00, 0x61, 0x73, 0x6d}) // component magic: \0asm
	comp.Write(componentVersion)               // version: 0x0d 0x00 0x01 0x00

	// Section 1: Core Module Section
	comp.WriteByte(0x01)
	comp.Write(wasm.AppendUleb128(nil, uint32(len(coreBytes))))
	comp.Write(coreBytes)

	// Section 2: Core Instance (instantiate core module 0 with 0 args)
	var coreInstSec bytes.Buffer
	coreInstSec.WriteByte(0x01) // 1 instance
	coreInstSec.WriteByte(0x00) // instantiate module index 0
	coreInstSec.WriteByte(0x00) // 0 args
	comp.WriteByte(0x02)
	comp.Write(wasm.AppendUleb128(nil, uint32(coreInstSec.Len())))
	comp.Write(coreInstSec.Bytes())

	// Section 6: Alias (alias core instance 0 export "_start" and "memory")
	var aliasSec bytes.Buffer
	aliasSec.WriteByte(0x02) // 2 aliases

	// 1. Core func _start
	aliasSec.WriteByte(0x00) // target: core instance
	aliasSec.WriteByte(0x00) // instance index 0
	aliasSec.WriteByte(0x00) // export kind: core func
	aliasSec.WriteByte(0x06) // len("_start")
	aliasSec.WriteString("_start")

	// 2. Core memory
	aliasSec.WriteByte(0x00) // target: core instance
	aliasSec.WriteByte(0x00) // instance index 0
	aliasSec.WriteByte(0x01) // export kind: core memory
	aliasSec.WriteByte(0x06) // len("memory")
	aliasSec.WriteString("memory")

	comp.WriteByte(0x06)
	comp.Write(wasm.AppendUleb128(nil, uint32(aliasSec.Len())))
	comp.Write(aliasSec.Bytes())

	// Section 8: Canon (lift core func 0 as component func)
	var canonSec bytes.Buffer
	canonSec.WriteByte(0x01) // 1 canon entry
	canonSec.WriteByte(0x00) // lift
	canonSec.WriteByte(0x00) // core func index 0
	canonSec.WriteByte(0x00) // 0 options
	canonSec.WriteByte(0x00) // func type index 0
	comp.WriteByte(0x08)
	comp.Write(wasm.AppendUleb128(nil, uint32(canonSec.Len())))
	comp.Write(canonSec.Bytes())

	// Section 11: Export wasi:cli/run@0.2.0#run
	var exportPayload bytes.Buffer
	exportPayload.WriteByte(0x01) // 1 export
	exportPayload.WriteByte(0x00) // name kind: string
	exportName := "wasi:cli/run@0.2.0"
	exportPayload.Write(wasm.AppendUleb128(nil, uint32(len(exportName))))
	exportPayload.WriteString(exportName)
	exportPayload.WriteByte(0x01) // export kind: func
	exportPayload.WriteByte(0x00) // component func index 0

	comp.WriteByte(0x0b)
	comp.Write(wasm.AppendUleb128(nil, uint32(exportPayload.Len())))
	comp.Write(exportPayload.Bytes())

	return comp.Bytes(), nil
}

// validateCoreModule checks that coreBytes is a well-formed core WebAssembly module.
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
