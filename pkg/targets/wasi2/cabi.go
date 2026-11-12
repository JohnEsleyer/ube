// File: pkg/targets/wasi2/cabi.go

package wasi2

// cabi.go defines the Canonical ABI contract the wasi2 core module must
// honor so the Component Model can lift its exports and lower host
// interfaces into it.
//
// The core emitter (pkg/compiler) guarantees every wasi2-bound core module
// carries the functions and memory required by the Canonical ABI:
//
//   - (memory (export "memory"))  — shared linear memory addressed by
//     Canonical ABI pointers (i32 offsets).
//   - (func (export "cabi_realloc")
//        (param $orig_ptr i32) (param $orig_size i32)
//        (param $align i32)    (param $new_size i32)
//        (result i32))         — the Canonical ABI reallocation entry point,
//     backed by Ube's regional bump allocator (global 0).
//
// The component packaging step (component.go) embeds the core module into
// the component binary; wasmtime and wasm-tools use these exports to lift
// core functions into component-level functions.

// ReallocName is the canonical export name of the Canonical ABI
// reallocation function required by the Component Model.
const ReallocName = "cabi_realloc"

// MemoryExportName is the canonical export name of the linear memory
// shared between the core module and lifted component functions.
const MemoryExportName = "memory"

// Core value type encodings used by the Canonical ABI realloc signature.
const (
	valTypeI32  byte = 0x7f
	valTypeVoid byte = 0x40
)

// ReallocParams is the Canonical ABI realloc parameter list:
// (i32, i32, i32, i32) -> (orig_ptr, orig_size, align, new_size).
var ReallocParams = []byte{valTypeI32, valTypeI32, valTypeI32, valTypeI32}

// ReallocResults is the Canonical ABI realloc result list: (new_ptr i32).
var ReallocResults = []byte{valTypeI32}
