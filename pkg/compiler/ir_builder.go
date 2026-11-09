package compiler

import (
	"bytes"

	"github.com/JohnEsleyer/ube/pkg/wasm"
)

// IRBuilder builds a linear IR sequence and manages virtual locals.
type IRBuilder struct {
	fn    *IRFunction
	pool  *LocalPool
	depth int // current label nesting depth
}

// NewIRBuilder creates an IRBuilder for an IRFunction and its LocalPool.
func NewIRBuilder(fn *IRFunction, pool *LocalPool) *IRBuilder {
	return &IRBuilder{
		fn:   fn,
		pool: pool,
	}
}

// Function returns the underlying IRFunction.
func (b *IRBuilder) Function() *IRFunction {
	if b.pool != nil {
		b.fn.Locals = b.pool.Locals()
	}
	return b.fn
}

// LocalPool returns the LocalPool managing virtual registers.
func (b *IRBuilder) LocalPool() *LocalPool {
	return b.pool
}

// LabelDepth returns the current label depth.
func (b *IRBuilder) LabelDepth() int {
	return b.depth
}

// Emit appends an IRInstruction to the function body.
func (b *IRBuilder) Emit(ins IRInstruction) {
	b.fn.Body = append(b.fn.Body, ins)
}

// EmitBytes appends raw WebAssembly bytes directly into the IR stream.
func (b *IRBuilder) EmitBytes(data []byte) {
	if len(data) == 0 {
		return
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	b.fn.Body = append(b.fn.Body, IRInstruction{
		Op:       OpRawBytes,
		RawBytes: cp,
	})
}

// ConstI32 emits an i32.const instruction.
func (b *IRBuilder) ConstI32(val int32) {
	b.Emit(IRInstruction{Op: OpConstI32, ImmI32: val})
}

// ConstI64 emits an i64.const instruction.
func (b *IRBuilder) ConstI64(val int64) {
	b.Emit(IRInstruction{Op: OpConstI64, ImmI64: val})
}

// ConstF32 emits an f32.const instruction.
func (b *IRBuilder) ConstF32(val float32) {
	b.Emit(IRInstruction{Op: OpConstF32, ImmF32: val})
}

// ConstF64 emits an f64.const instruction.
func (b *IRBuilder) ConstF64(val float64) {
	b.Emit(IRInstruction{Op: OpConstF64, ImmF64: val})
}

// GetLocal emits a local.get instruction.
func (b *IRBuilder) GetLocal(idx uint32) {
	b.Emit(IRInstruction{Op: OpGetLocal, LocalIdx: idx})
}

// SetLocal emits a local.set instruction.
func (b *IRBuilder) SetLocal(idx uint32) {
	b.Emit(IRInstruction{Op: OpSetLocal, LocalIdx: idx})
}

// TeeLocal emits a local.tee instruction.
func (b *IRBuilder) TeeLocal(idx uint32) {
	b.Emit(IRInstruction{Op: OpTeeLocal, LocalIdx: idx})
}

// GetGlobal emits a global.get instruction.
func (b *IRBuilder) GetGlobal(idx uint32) {
	b.Emit(IRInstruction{Op: OpGetGlobal, GlobalIdx: idx})
}

// SetGlobal emits a global.set instruction.
func (b *IRBuilder) SetGlobal(idx uint32) {
	b.Emit(IRInstruction{Op: OpSetGlobal, GlobalIdx: idx})
}

// Call emits a call instruction.
func (b *IRBuilder) Call(funcIdx uint32) {
	b.Emit(IRInstruction{Op: OpCall, FuncIdx: funcIdx})
}

// CallIndirect emits a call_indirect instruction.
func (b *IRBuilder) CallIndirect(typeIdx uint32) {
	b.Emit(IRInstruction{Op: OpCallIndirect, TypeIdx: typeIdx})
}

// Drop emits a drop instruction.
func (b *IRBuilder) Drop() {
	b.Emit(IRInstruction{Op: OpDrop})
}

// Return emits a return instruction.
func (b *IRBuilder) Return() {
	b.Emit(IRInstruction{Op: OpReturn})
}

// Block begins a block label scope.
func (b *IRBuilder) Block(vt wasm.ValType) {
	b.depth++
	b.Emit(IRInstruction{Op: OpBlock, Type: vt})
}

// Loop begins a loop label scope.
func (b *IRBuilder) Loop(vt wasm.ValType) {
	b.depth++
	b.Emit(IRInstruction{Op: OpLoop, Type: vt})
}

// If begins an if label scope.
func (b *IRBuilder) If(vt wasm.ValType) {
	b.depth++
	b.Emit(IRInstruction{Op: OpIf, Type: vt})
}

// Else emits an else instruction.
func (b *IRBuilder) Else() {
	b.Emit(IRInstruction{Op: OpElse})
}

// End closes a block, loop, or if scope.
func (b *IRBuilder) End() {
	if b.depth > 0 {
		b.depth--
	}
	b.Emit(IRInstruction{Op: OpEnd})
}

// Br emits an unconditional branch (br) to a relative label depth.
func (b *IRBuilder) Br(depth uint32) {
	b.Emit(IRInstruction{Op: OpBr, Offset: depth})
}

// BrIf emits a conditional branch (br_if) to a relative label depth.
func (b *IRBuilder) BrIf(depth uint32) {
	b.Emit(IRInstruction{Op: OpBrIf, Offset: depth})
}

// Load emits a typed memory load.
func (b *IRBuilder) Load(vt wasm.ValType, align, offset uint32) {
	b.Emit(IRInstruction{Op: OpLoad, Type: vt, Align: align, Offset: offset})
}

// Store emits a typed memory store.
func (b *IRBuilder) Store(vt wasm.ValType, align, offset uint32) {
	b.Emit(IRInstruction{Op: OpStore, Type: vt, Align: align, Offset: offset})
}

// Load8U emits an i32.load8_u memory load.
func (b *IRBuilder) Load8U(align, offset uint32) {
	b.Emit(IRInstruction{Op: OpLoad8U, Align: align, Offset: offset})
}

// Load16U emits an i32.load16_u memory load.
func (b *IRBuilder) Load16U(align, offset uint32) {
	b.Emit(IRInstruction{Op: OpLoad16U, Align: align, Offset: offset})
}

// Store8 emits an i32.store8 memory store.
func (b *IRBuilder) Store8(align, offset uint32) {
	b.Emit(IRInstruction{Op: OpStore8, Align: align, Offset: offset})
}

// Store16 emits an i32.store16 memory store.
func (b *IRBuilder) Store16(align, offset uint32) {
	b.Emit(IRInstruction{Op: OpStore16, Align: align, Offset: offset})
}

// MemoryCopy emits a memory.copy instruction.
func (b *IRBuilder) MemoryCopy() {
	b.Emit(IRInstruction{Op: OpMemoryCopy})
}

// MemoryFill emits a memory.fill instruction.
func (b *IRBuilder) MemoryFill() {
	b.Emit(IRInstruction{Op: OpMemoryFill})
}

// FlushToBuffer dumps all emitted IR instructions to a bytes.Buffer.
func (b *IRBuilder) FlushToBuffer(buf *bytes.Buffer) {
	for _, ins := range b.fn.Body {
		emitIRInstruction(buf, ins)
	}
	b.fn.Body = nil
}
