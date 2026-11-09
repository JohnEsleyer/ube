package compiler

import (
	"bytes"

	"github.com/JohnEsleyer/ube/pkg/wasm"
)

func (ctx *fnContext) emitGetLocal(idx uint32) {
	ctx.ir.GetLocal(idx)
}

func (ctx *fnContext) emitSetLocal(idx uint32) {
	ctx.ir.SetLocal(idx)
}

func (ctx *fnContext) emitTeeLocal(idx uint32) {
	ctx.ir.TeeLocal(idx)
}

func (ctx *fnContext) emitConstI32(val int32) {
	ctx.ir.ConstI32(val)
}

func (ctx *fnContext) emitConstI64(val int64) {
	ctx.ir.ConstI64(val)
}

func (ctx *fnContext) emitConstF32(val float32) {
	ctx.ir.ConstF32(val)
}

func (ctx *fnContext) emitConstF64(val float64) {
	ctx.ir.ConstF64(val)
}

func (ctx *fnContext) emitCall(funcIdx uint32) {
	ctx.ir.Call(funcIdx)
}

func (ctx *fnContext) emitDrop() {
	ctx.ir.Drop()
}

func (ctx *fnContext) emitReturn() {
	ctx.ir.Return()
}

func (ctx *fnContext) emitBr(depth uint32) {
	ctx.ir.Br(depth)
}

func (ctx *fnContext) emitBrIf(depth uint32) {
	ctx.ir.BrIf(depth)
}

func (ctx *fnContext) emitI32Eqz() {
	ctx.ir.Emit(IRInstruction{Op: OpI32Eqz})
}

func (ctx *fnContext) emitSelect() {
	ctx.ir.Emit(IRInstruction{Op: OpSelect})
}

func (ctx *fnContext) emitGetGlobal(idx uint32) {
	ctx.ir.GetGlobal(idx)
}

func (ctx *fnContext) emitSetGlobal(idx uint32) {
	ctx.ir.SetGlobal(idx)
}

// openIRLabel routes a block/loop/if opcode through IRBuilder and flushes it to
// buf, then increments ctx.labelDepth. This replaces the openLabel + trailing
// buf.WriteByte(valTypeVoid) pattern throughout compileStmt.
func (ctx *fnContext) openIRLabel(buf *bytes.Buffer, op byte) {
	vt := wasm.ValType(valTypeVoid)
	switch op {
	case 0x02:
		ctx.ir.Block(vt)
	case 0x03:
		ctx.ir.Loop(vt)
	case 0x04:
		ctx.ir.If(vt)
	default:
		// Unknown structured opcode — emit as raw bytes for safety.
		ctx.ir.EmitBytes([]byte{op, valTypeVoid})
	}
	ctx.ir.FlushToBuffer(buf)
	// IRBuilder.Block/Loop/If already increment b.depth internally;
	// keep ctx.labelDepth in sync.
	ctx.labelDepth++
}

// closeIRLabel routes the end opcode through IRBuilder, flushes it to buf,
// and decrements ctx.labelDepth. This replaces every closeLabel call.
func (ctx *fnContext) closeIRLabel(buf *bytes.Buffer) {
	ctx.ir.End()
	ctx.ir.FlushToBuffer(buf)
	ctx.labelDepth--
}
