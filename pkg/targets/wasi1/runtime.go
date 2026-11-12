// File: pkg/targets/wasi1/runtime.go
//
// Preview 1 print runtime helpers (__print_*). These write to stdout via the
// fd_write syscall and reserve the scratch layout, so they are Preview 1
// runtime, not shared frontend code.

package wasi1

import (
	"bytes"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func (w *syscalls) buildPrintStrFunc(e *compiler.WasmEmitter) []byte {
	var body bytes.Buffer

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, scratchCiovec)
	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x36)
	compiler.WriteUleb128(&body, 2)
	compiler.WriteUleb128(&body, 0)

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, scratchCiovec+4)
	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 1)
	body.WriteByte(0x36)
	compiler.WriteUleb128(&body, 2)
	compiler.WriteUleb128(&body, 0)

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 1)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, scratchCiovec)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 1)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, scratchNwrite)
	body.WriteByte(0x10)
	compiler.WriteUleb128(&body, w.importIndex("fd_write"))
	body.WriteByte(0x1a)

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 2)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeVoid)

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, scratchCiovec+48)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 10)
	body.WriteByte(0x3a)
	compiler.WriteUleb128(&body, 0)
	compiler.WriteUleb128(&body, 0)

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, scratchCiovec)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 48)
	body.WriteByte(0x36)
	compiler.WriteUleb128(&body, 2)
	compiler.WriteUleb128(&body, 0)

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, scratchCiovec+4)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 1)
	body.WriteByte(0x36)
	compiler.WriteUleb128(&body, 2)
	compiler.WriteUleb128(&body, 0)

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 1)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, scratchCiovec)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 1)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, scratchNwrite)
	body.WriteByte(0x10)
	compiler.WriteUleb128(&body, w.importIndex("fd_write"))
	body.WriteByte(0x1a)

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	var full bytes.Buffer
	compiler.WriteUleb128(&full, 0)
	full.Write(body.Bytes())

	var res bytes.Buffer
	compiler.WriteUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (w *syscalls) buildPrintBoolFunc(e *compiler.WasmEmitter) []byte {
	var body bytes.Buffer

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeVoid)

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, int32(e.TrueHeader()))
	body.WriteByte(0x28)
	compiler.WriteUleb128(&body, 2)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 4)
	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 1)
	body.WriteByte(0x10)
	compiler.WriteUleb128(&body, e.FnIndex("__print_str"))

	body.WriteByte(0x05)

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, int32(e.FalseHeader()))
	body.WriteByte(0x28)
	compiler.WriteUleb128(&body, 2)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 5)
	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 1)
	body.WriteByte(0x10)
	compiler.WriteUleb128(&body, e.FnIndex("__print_str"))

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	var full bytes.Buffer
	compiler.WriteUleb128(&full, 0)
	full.Write(body.Bytes())

	var res bytes.Buffer
	compiler.WriteUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (w *syscalls) buildPrintIntFunc(e *compiler.WasmEmitter) []byte {
	var body bytes.Buffer

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 47)
	body.WriteByte(0x21)
	compiler.WriteUleb128(&body, 2)

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 0)
	body.WriteByte(0x21)
	compiler.WriteUleb128(&body, 3)

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x45)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeVoid)

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 2)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 48)
	body.WriteByte(0x3a)
	compiler.WriteUleb128(&body, 0)
	compiler.WriteUleb128(&body, 0)

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 2)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 1)
	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 1)
	body.WriteByte(0x10)
	compiler.WriteUleb128(&body, e.FnIndex("__print_str"))
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 0)
	body.WriteByte(0x48)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeVoid)

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 0)
	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x6b)
	body.WriteByte(0x21)
	compiler.WriteUleb128(&body, 0)

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 1)
	body.WriteByte(0x21)
	compiler.WriteUleb128(&body, 3)

	body.WriteByte(0x0b)

	body.WriteByte(0x02)
	body.WriteByte(compiler.ValTypeVoid)
	body.WriteByte(0x03)
	body.WriteByte(compiler.ValTypeVoid)

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x45)
	body.WriteByte(0x0d)
	compiler.WriteUleb128(&body, 1)

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 10)
	body.WriteByte(0x6f)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 48)
	body.WriteByte(0x6a)
	body.WriteByte(0x21)
	compiler.WriteUleb128(&body, 4)

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 2)
	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 4)
	body.WriteByte(0x3a)
	compiler.WriteUleb128(&body, 0)
	compiler.WriteUleb128(&body, 0)

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 2)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x21)
	compiler.WriteUleb128(&body, 2)

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 10)
	body.WriteByte(0x6d)
	body.WriteByte(0x21)
	compiler.WriteUleb128(&body, 0)

	body.WriteByte(0x0c)
	compiler.WriteUleb128(&body, 0)

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 3)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeVoid)
	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 2)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 45)
	body.WriteByte(0x3a)
	compiler.WriteUleb128(&body, 0)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 2)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x21)
	compiler.WriteUleb128(&body, 2)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 2)
	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 1)
	body.WriteByte(0x6a)

	body.WriteByte(0x41)
	compiler.WriteSleb128(&body, 47)
	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 2)
	body.WriteByte(0x6b)

	body.WriteByte(0x20)
	compiler.WriteUleb128(&body, 1)
	body.WriteByte(0x10)
	compiler.WriteUleb128(&body, e.FnIndex("__print_str"))

	body.WriteByte(0x0b)

	var full bytes.Buffer
	compiler.WriteUleb128(&full, 1)
	compiler.WriteUleb128(&full, 3)
	full.WriteByte(compiler.ValTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	compiler.WriteUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (w *syscalls) buildPrintInt64Func(e *compiler.WasmEmitter) []byte {
	var body bytes.Buffer

	compiler.EmitLocalGet(&body, 0)
	compiler.EmitCallIdx(&body, e.FnIndex("__i64_to_str"))
	compiler.EmitLocalSet(&body, 2)

	compiler.EmitLocalGet(&body, 2)
	compiler.EmitI32Load(&body, 0, 2)
	compiler.EmitLocalGet(&body, 2)
	compiler.EmitI32Load(&body, 4, 2)
	compiler.EmitLocalGet(&body, 1)
	compiler.EmitCallIdx(&body, e.FnIndex("__print_str"))

	body.WriteByte(0x0b)

	var full bytes.Buffer
	compiler.WriteUleb128(&full, 1)
	compiler.WriteUleb128(&full, 1)
	full.WriteByte(compiler.ValTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	compiler.WriteUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (w *syscalls) buildPrintF64Func(e *compiler.WasmEmitter) []byte {
	var body bytes.Buffer

	compiler.EmitLocalGet(&body, 0)
	compiler.EmitCallIdx(&body, e.FnIndex("__f64_to_str"))
	compiler.EmitLocalSet(&body, 2)

	compiler.EmitLocalGet(&body, 2)
	compiler.EmitI32Load(&body, 0, 2)
	compiler.EmitLocalGet(&body, 2)
	compiler.EmitI32Load(&body, 4, 2)
	compiler.EmitLocalGet(&body, 1)
	compiler.EmitCallIdx(&body, e.FnIndex("__print_str"))

	body.WriteByte(0x0b)

	var full bytes.Buffer
	compiler.WriteUleb128(&full, 1)
	compiler.WriteUleb128(&full, 1)
	full.WriteByte(compiler.ValTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	compiler.WriteUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}
