package wasm

import (
	"encoding/binary"
	"math"
)

// Buffer provides an allocation-friendly byte stream for assembling WebAssembly instructions.
type Buffer struct {
	Bytes []byte
}

// NewBuffer creates an empty Buffer with pre-allocated capacity.
func NewBuffer(capacity int) *Buffer {
	return &Buffer{
		Bytes: make([]byte, 0, capacity),
	}
}

// Reset clears the buffer while preserving the underlying storage.
func (b *Buffer) Reset() {
	b.Bytes = b.Bytes[:0]
}

// Len returns the number of written bytes.
func (b *Buffer) Len() int {
	return len(b.Bytes)
}

// Emit appends a single byte.
func (b *Buffer) Emit(val byte) {
	b.Bytes = append(b.Bytes, val)
}

// EmitBytes appends a raw byte slice.
func (b *Buffer) EmitBytes(data []byte) {
	b.Bytes = append(b.Bytes, data...)
}

// EmitUleb128 appends an unsigned 32-bit integer in LEB128 format.
func (b *Buffer) EmitUleb128(val uint32) {
	b.Bytes = AppendUleb128(b.Bytes, val)
}

// EmitSleb128 appends a signed 32-bit integer in LEB128 format.
func (b *Buffer) EmitSleb128(val int32) {
	b.Bytes = AppendSleb128(b.Bytes, val)
}

// EmitI32Const emits an i32.const instruction with immediate value.
func (b *Buffer) EmitI32Const(val int32) {
	b.Emit(OpI32Const)
	b.EmitSleb128(val)
}

// EmitI64Const emits an i64.const instruction with immediate value.
func (b *Buffer) EmitI64Const(val int64) {
	b.Emit(OpI64Const)
	b.Bytes = AppendSleb128_64(b.Bytes, val)
}

// EmitF32Const emits an f32.const instruction with immediate value (IEEE 754 little-endian).
func (b *Buffer) EmitF32Const(val float32) {
	b.Emit(OpF32Const)
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], math.Float32bits(val))
	b.EmitBytes(raw[:])
}

// EmitF64Const emits an f64.const instruction with immediate value (IEEE 754 little-endian).
func (b *Buffer) EmitF64Const(val float64) {
	b.Emit(OpF64Const)
	var raw [8]byte
	binary.LittleEndian.PutUint64(raw[:], math.Float64bits(val))
	b.EmitBytes(raw[:])
}

// EmitString emits a UTF-8 string prefixed by its ULEB128 length.
func (b *Buffer) EmitString(s string) {
	b.EmitUleb128(uint32(len(s)))
	b.EmitBytes([]byte(s))
}

// EmitMemArg emits alignment and offset parameters for memory instructions.
func (b *Buffer) EmitMemArg(align, offset uint32) {
	b.EmitUleb128(align)
	b.EmitUleb128(offset)
}
