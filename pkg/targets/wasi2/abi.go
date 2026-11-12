// File: pkg/targets/wasi2/abi.go
//
// The Canonical ABI (AbiLayer) for the WASI 0.2 target. It contributes the
// cabi_realloc helper and its core-module export so component tooling
// (wasm-tools, wasmtime) can lift core functions into the component world.

package wasi2

import (
	"bytes"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

// cabiABI implements compiler.AbiLayer.
type cabiABI struct{}

func init() {
	compiler.RegisterAbiLayer(cabiABI{})
}

// NewAbiLayer returns a fresh Canonical ABI layer. The layer is registered
// globally via init(); this constructor exists for tests and explicit
// configuration.
func NewAbiLayer() compiler.AbiLayer {
	return cabiABI{}
}

// Name implements compiler.AbiLayer.
func (a cabiABI) Name() string { return "wasi2/canonical-abi" }

// Helpers implements compiler.AbiLayer.
func (a cabiABI) Helpers() []compiler.HelperSpec {
	return []compiler.HelperSpec{
		{
			Name:    ReallocName,
			Params:  ReallocParams,
			Results: ReallocResults,
		},
	}
}

// BuildHelper implements compiler.AbiLayer.
func (a cabiABI) BuildHelper(e *compiler.WasmEmitter, name string) []byte {
	if name == ReallocName {
		return a.buildCabiReallocFunc(e)
	}
	return nil
}

// Exports implements compiler.AbiLayer.
func (a cabiABI) Exports() []compiler.ExportSpec {
	return []compiler.ExportSpec{
		{Export: ReallocName, Helper: ReallocName},
	}
}

// buildCabiReallocFunc implements the WebAssembly Canonical ABI cabi_realloc function:
// (func (export "cabi_realloc") (param $orig_ptr i32) (param $orig_size i32) (param $align i32) (param $new_size i32) (result i32))
// Backed directly by Ube's regional bump allocator (Global 0).
func (a cabiABI) buildCabiReallocFunc(e *compiler.WasmEmitter) []byte {
	var body bytes.Buffer

	// if new_size == 0 { return orig_ptr }
	compiler.EmitLocalGet(&body, 3)
	compiler.EmitI32Eqz(&body)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeVoid)
	compiler.EmitLocalGet(&body, 0)
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	// if align < 1 { align = 1 }
	compiler.EmitLocalGet(&body, 2)
	compiler.EmitI32Const(&body, 1)
	body.WriteByte(0x48)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeVoid)
	compiler.EmitI32Const(&body, 1)
	compiler.EmitLocalSet(&body, 2)
	body.WriteByte(0x0b)

	// align_mask = align - 1 (local 5)
	compiler.EmitLocalGet(&body, 2)
	compiler.EmitI32Const(&body, 1)
	body.WriteByte(0x6b)
	compiler.EmitLocalSet(&body, 5)

	// Check if orig_ptr == 0 (fresh allocation)
	compiler.EmitLocalGet(&body, 0)
	compiler.EmitI32Eqz(&body)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeI32)

	// aligned_ptr = (global0 + align_mask) & ~align_mask (local 4)
	body.WriteByte(0x23)
	compiler.WriteUleb128(&body, 0)
	compiler.EmitLocalGet(&body, 5)
	body.WriteByte(0x6a)
	compiler.EmitLocalGet(&body, 5)
	compiler.EmitI32Const(&body, -1)
	body.WriteByte(0x73)
	body.WriteByte(0x71)
	compiler.EmitLocalSet(&body, 4)

	// global0 = aligned_ptr + new_size
	compiler.EmitLocalGet(&body, 4)
	compiler.EmitLocalGet(&body, 3)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	compiler.WriteUleb128(&body, 0)

	compiler.EmitLocalGet(&body, 4)

	body.WriteByte(0x05) // else (reallocation)

	// if new_size <= orig_size { return orig_ptr }
	compiler.EmitLocalGet(&body, 3)
	compiler.EmitLocalGet(&body, 1)
	body.WriteByte(0x4d)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeVoid)
	compiler.EmitLocalGet(&body, 0)
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	// if orig_ptr + orig_size == global0 { global0 = orig_ptr + new_size; return orig_ptr }
	compiler.EmitLocalGet(&body, 0)
	compiler.EmitLocalGet(&body, 1)
	body.WriteByte(0x6a)
	body.WriteByte(0x23)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x46)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeVoid)
	compiler.EmitLocalGet(&body, 0)
	compiler.EmitLocalGet(&body, 3)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	compiler.WriteUleb128(&body, 0)
	compiler.EmitLocalGet(&body, 0)
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	// Otherwise allocate new buffer and copy
	body.WriteByte(0x23)
	compiler.WriteUleb128(&body, 0)
	compiler.EmitLocalGet(&body, 5)
	body.WriteByte(0x6a)
	compiler.EmitLocalGet(&body, 5)
	compiler.EmitI32Const(&body, -1)
	body.WriteByte(0x73)
	body.WriteByte(0x71)
	compiler.EmitLocalSet(&body, 4)

	compiler.EmitLocalGet(&body, 4)
	compiler.EmitLocalGet(&body, 3)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	compiler.WriteUleb128(&body, 0)

	// memory.copy: dest, src, len
	compiler.EmitLocalGet(&body, 4)
	compiler.EmitLocalGet(&body, 0)
	compiler.EmitLocalGet(&body, 1)
	body.WriteByte(0xfc)
	body.WriteByte(0x0a)
	body.WriteByte(0x00)
	body.WriteByte(0x00)

	compiler.EmitLocalGet(&body, 4)

	body.WriteByte(0x0b) // end if/else

	body.WriteByte(0x0b) // end function

	var full bytes.Buffer
	compiler.WriteUleb128(&full, 1) // 1 local group
	compiler.WriteUleb128(&full, 2) // 2 locals (local 4, local 5)
	full.WriteByte(compiler.ValTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	compiler.WriteUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}
