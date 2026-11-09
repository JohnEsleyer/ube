// File: pkg/compiler/wasm_emit_code.go
package compiler

import (
	"bytes"
	"encoding/binary"
	"math"

	"github.com/JohnEsleyer/ube/pkg/wasm"
)

type rleLocalGroup struct {
	Count uint32
	Type  wasm.ValType
}

func groupLocalsRLE(locals []wasm.ValType) []rleLocalGroup {
	if len(locals) == 0 {
		return nil
	}
	var groups []rleLocalGroup
	currType := locals[0]
	currCount := uint32(1)
	for i := 1; i < len(locals); i++ {
		if locals[i] == currType {
			currCount++
		} else {
			groups = append(groups, rleLocalGroup{Count: currCount, Type: currType})
			currType = locals[i]
			currCount = 1
		}
	}
	groups = append(groups, rleLocalGroup{Count: currCount, Type: currType})
	return groups
}

// EmitFunctionBody emits binary WebAssembly for an IRFunction.
func EmitFunctionBody(fn *IRFunction) []byte {
	var codeBuf bytes.Buffer

	// 1. Group contiguous locals of matching types (RLE format for Wasm)
	rleLocals := groupLocalsRLE(fn.Locals)
	writeUleb128(&codeBuf, uint32(len(rleLocals)))
	for _, l := range rleLocals {
		writeUleb128(&codeBuf, l.Count)
		codeBuf.WriteByte(byte(l.Type))
	}

	// 2. Linear instruction output
	for _, ins := range fn.Body {
		emitIRInstruction(&codeBuf, ins)
	}

	// 3. Delimiter
	codeBuf.WriteByte(wasm.OpEnd)

	var fullBuf bytes.Buffer
	writeUleb128(&fullBuf, uint32(codeBuf.Len()))
	fullBuf.Write(codeBuf.Bytes())
	return fullBuf.Bytes()
}

func emitIRInstruction(buf *bytes.Buffer, ins IRInstruction) {
	switch ins.Op {
	case OpConstI32:
		buf.WriteByte(wasm.OpI32Const)
		writeSleb128(buf, ins.ImmI32)
	case OpConstI64:
		buf.WriteByte(wasm.OpI64Const)
		writeSleb128_64(buf, ins.ImmI64)
	case OpConstF32:
		buf.WriteByte(wasm.OpF32Const)
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(ins.ImmF32))
		buf.Write(b[:])
	case OpConstF64:
		buf.WriteByte(wasm.OpF64Const)
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(ins.ImmF64))
		buf.Write(b[:])
	case OpGetLocal:
		buf.WriteByte(wasm.OpLocalGet)
		writeUleb128(buf, ins.LocalIdx)
	case OpSetLocal:
		buf.WriteByte(wasm.OpLocalSet)
		writeUleb128(buf, ins.LocalIdx)
	case OpTeeLocal:
		buf.WriteByte(wasm.OpLocalTee)
		writeUleb128(buf, ins.LocalIdx)
	case OpGetGlobal:
		buf.WriteByte(wasm.OpGlobalGet)
		writeUleb128(buf, ins.GlobalIdx)
	case OpSetGlobal:
		buf.WriteByte(wasm.OpGlobalSet)
		writeUleb128(buf, ins.GlobalIdx)
	case OpLoad:
		switch ins.Type {
		case wasm.ValI32:
			buf.WriteByte(wasm.OpI32Load)
		case wasm.ValI64:
			buf.WriteByte(wasm.OpI64Load)
		case wasm.ValF32:
			buf.WriteByte(wasm.OpF32Load)
		case wasm.ValF64:
			buf.WriteByte(wasm.OpF64Load)
		default:
			buf.WriteByte(wasm.OpI32Load)
		}
		writeUleb128(buf, ins.Align)
		writeUleb128(buf, ins.Offset)
	case OpStore:
		switch ins.Type {
		case wasm.ValI32:
			buf.WriteByte(wasm.OpI32Store)
		case wasm.ValI64:
			buf.WriteByte(wasm.OpI64Store)
		case wasm.ValF32:
			buf.WriteByte(wasm.OpF32Store)
		case wasm.ValF64:
			buf.WriteByte(wasm.OpF64Store)
		default:
			buf.WriteByte(wasm.OpI32Store)
		}
		writeUleb128(buf, ins.Align)
		writeUleb128(buf, ins.Offset)
	case OpMemoryCopy:
		buf.WriteByte(0xfc)
		writeUleb128(buf, 10)
		writeUleb128(buf, 0)
		writeUleb128(buf, 0)
	case OpMemoryFill:
		buf.WriteByte(0xfc)
		writeUleb128(buf, 11)
		writeUleb128(buf, 0)
	case OpAdd:
		switch ins.Type {
		case wasm.ValI32:
			buf.WriteByte(wasm.OpI32Add)
		case wasm.ValI64:
			buf.WriteByte(wasm.OpI64Add)
		case wasm.ValF32:
			buf.WriteByte(wasm.OpF32Add)
		case wasm.ValF64:
			buf.WriteByte(wasm.OpF64Add)
		}
	case OpSub:
		switch ins.Type {
		case wasm.ValI32:
			buf.WriteByte(wasm.OpI32Sub)
		case wasm.ValI64:
			buf.WriteByte(wasm.OpI64Sub)
		case wasm.ValF32:
			buf.WriteByte(wasm.OpF32Sub)
		case wasm.ValF64:
			buf.WriteByte(wasm.OpF64Sub)
		}
	case OpMul:
		switch ins.Type {
		case wasm.ValI32:
			buf.WriteByte(wasm.OpI32Mul)
		case wasm.ValI64:
			buf.WriteByte(wasm.OpI64Mul)
		case wasm.ValF32:
			buf.WriteByte(wasm.OpF32Mul)
		case wasm.ValF64:
			buf.WriteByte(wasm.OpF64Mul)
		}
	case OpDivS:
		switch ins.Type {
		case wasm.ValI32:
			buf.WriteByte(wasm.OpI32DivS)
		case wasm.ValI64:
			buf.WriteByte(wasm.OpI64DivS)
		case wasm.ValF32:
			buf.WriteByte(wasm.OpF32Div)
		case wasm.ValF64:
			buf.WriteByte(wasm.OpF64Div)
		}
	case OpDivU:
		if ins.Type == wasm.ValI32 {
			buf.WriteByte(wasm.OpI32DivU)
		} else {
			buf.WriteByte(wasm.OpI64DivU)
		}
	case OpRemS:
		if ins.Type == wasm.ValI32 {
			buf.WriteByte(wasm.OpI32RemS)
		} else {
			buf.WriteByte(wasm.OpI64RemS)
		}
	case OpRemU:
		if ins.Type == wasm.ValI32 {
			buf.WriteByte(wasm.OpI32RemU)
		} else {
			buf.WriteByte(wasm.OpI64RemU)
		}
	case OpAnd:
		if ins.Type == wasm.ValI32 {
			buf.WriteByte(wasm.OpI32And)
		} else {
			buf.WriteByte(wasm.OpI64And)
		}
	case OpOr:
		if ins.Type == wasm.ValI32 {
			buf.WriteByte(wasm.OpI32Or)
		} else {
			buf.WriteByte(wasm.OpI64Or)
		}
	case OpXor:
		if ins.Type == wasm.ValI32 {
			buf.WriteByte(wasm.OpI32Xor)
		} else {
			buf.WriteByte(wasm.OpI64Xor)
		}
	case OpShl:
		if ins.Type == wasm.ValI32 {
			buf.WriteByte(wasm.OpI32Shl)
		} else {
			buf.WriteByte(wasm.OpI64Shl)
		}
	case OpShrS:
		if ins.Type == wasm.ValI32 {
			buf.WriteByte(wasm.OpI32ShrS)
		} else {
			buf.WriteByte(wasm.OpI64ShrS)
		}
	case OpShrU:
		if ins.Type == wasm.ValI32 {
			buf.WriteByte(wasm.OpI32ShrU)
		} else {
			buf.WriteByte(wasm.OpI64ShrU)
		}
	case OpEq:
		switch ins.Type {
		case wasm.ValI32:
			buf.WriteByte(wasm.OpI32Eq)
		case wasm.ValI64:
			buf.WriteByte(wasm.OpI64Eq)
		case wasm.ValF32:
			buf.WriteByte(wasm.OpF32Eq)
		case wasm.ValF64:
			buf.WriteByte(wasm.OpF64Eq)
		}
	case OpNe:
		switch ins.Type {
		case wasm.ValI32:
			buf.WriteByte(wasm.OpI32Ne)
		case wasm.ValI64:
			buf.WriteByte(wasm.OpI64Ne)
		case wasm.ValF32:
			buf.WriteByte(wasm.OpF32Ne)
		case wasm.ValF64:
			buf.WriteByte(wasm.OpF64Ne)
		}
	case OpLtS:
		switch ins.Type {
		case wasm.ValI32:
			buf.WriteByte(wasm.OpI32LtS)
		case wasm.ValI64:
			buf.WriteByte(wasm.OpI64LtS)
		case wasm.ValF32:
			buf.WriteByte(wasm.OpF32Lt)
		case wasm.ValF64:
			buf.WriteByte(wasm.OpF64Lt)
		}
	case OpLtU:
		if ins.Type == wasm.ValI32 {
			buf.WriteByte(wasm.OpI32LtU)
		} else {
			buf.WriteByte(wasm.OpI64LtU)
		}
	case OpLeS:
		switch ins.Type {
		case wasm.ValI32:
			buf.WriteByte(wasm.OpI32LeS)
		case wasm.ValI64:
			buf.WriteByte(wasm.OpI64LeS)
		case wasm.ValF32:
			buf.WriteByte(wasm.OpF32Le)
		case wasm.ValF64:
			buf.WriteByte(wasm.OpF64Le)
		}
	case OpLeU:
		if ins.Type == wasm.ValI32 {
			buf.WriteByte(wasm.OpI32LeU)
		} else {
			buf.WriteByte(wasm.OpI64LeU)
		}
	case OpGtS:
		switch ins.Type {
		case wasm.ValI32:
			buf.WriteByte(wasm.OpI32GtS)
		case wasm.ValI64:
			buf.WriteByte(wasm.OpI64GtS)
		case wasm.ValF32:
			buf.WriteByte(wasm.OpF32Gt)
		case wasm.ValF64:
			buf.WriteByte(wasm.OpF64Gt)
		}
	case OpGtU:
		if ins.Type == wasm.ValI32 {
			buf.WriteByte(wasm.OpI32GtU)
		} else {
			buf.WriteByte(wasm.OpI64GtU)
		}
	case OpGeS:
		switch ins.Type {
		case wasm.ValI32:
			buf.WriteByte(wasm.OpI32GeS)
		case wasm.ValI64:
			buf.WriteByte(wasm.OpI64GeS)
		case wasm.ValF32:
			buf.WriteByte(wasm.OpF32Ge)
		case wasm.ValF64:
			buf.WriteByte(wasm.OpF64Ge)
		}
	case OpGeU:
		if ins.Type == wasm.ValI32 {
			buf.WriteByte(wasm.OpI32GeU)
		} else {
			buf.WriteByte(wasm.OpI64GeU)
		}
	case OpBlock:
		buf.WriteByte(wasm.OpBlock)
		buf.WriteByte(byte(ins.Type))
	case OpLoop:
		buf.WriteByte(wasm.OpLoop)
		buf.WriteByte(byte(ins.Type))
	case OpIf:
		buf.WriteByte(wasm.OpIf)
		buf.WriteByte(byte(ins.Type))
	case OpElse:
		buf.WriteByte(wasm.OpElse)
	case OpEnd:
		buf.WriteByte(wasm.OpEnd)
	case OpBr:
		buf.WriteByte(wasm.OpBr)
		writeUleb128(buf, ins.Offset)
	case OpBrIf:
		buf.WriteByte(wasm.OpBrIf)
		writeUleb128(buf, ins.Offset)
	case OpReturn:
		buf.WriteByte(wasm.OpReturn)
	case OpCall:
		buf.WriteByte(wasm.OpCall)
		writeUleb128(buf, ins.FuncIdx)
	case OpCallIndirect:
		buf.WriteByte(wasm.OpCallIndirect)
		writeUleb128(buf, ins.TypeIdx)
		writeUleb128(buf, 0)
	case OpUnreachable:
		buf.WriteByte(wasm.OpUnreachable)
	case OpDrop:
		buf.WriteByte(wasm.OpDrop)
	case OpSelect:
		buf.WriteByte(wasm.OpSelect)
	case OpI32Eqz:
		buf.WriteByte(wasm.OpI32Eqz)
	case OpI64Eqz:
		buf.WriteByte(wasm.OpI64Eqz)
	case OpI32WrapI64:
		buf.WriteByte(wasm.OpI32WrapI64)
	case OpI64ExtendI32S:
		buf.WriteByte(wasm.OpI64ExtendI32S)
	case OpI64ExtendI32U:
		buf.WriteByte(wasm.OpI64ExtendI32U)
	case OpF32DemoteF64:
		buf.WriteByte(wasm.OpF32DemoteF64)
	case OpF64PromoteF32:
		buf.WriteByte(wasm.OpF64PromoteF32)
	case OpF32ConvertI32S:
		buf.WriteByte(wasm.OpF32ConvertI32S)
	case OpF32ConvertI32U:
		buf.WriteByte(wasm.OpF32ConvertI32U)
	case OpF32ConvertI64S:
		buf.WriteByte(wasm.OpF32ConvertI64S)
	case OpF32ConvertI64U:
		buf.WriteByte(wasm.OpF32ConvertI64U)
	case OpF64ConvertI32S:
		buf.WriteByte(wasm.OpF64ConvertI32S)
	case OpF64ConvertI32U:
		buf.WriteByte(wasm.OpF64ConvertI32U)
	case OpF64ConvertI64S:
		buf.WriteByte(wasm.OpF64ConvertI64S)
	case OpF64ConvertI64U:
		buf.WriteByte(wasm.OpF64ConvertI64U)
	case OpI32TruncF32S:
		buf.WriteByte(wasm.OpI32TruncF32S)
	case OpI32TruncF32U:
		buf.WriteByte(wasm.OpI32TruncF32U)
	case OpI32TruncF64S:
		buf.WriteByte(wasm.OpI32TruncF64S)
	case OpI32TruncF64U:
		buf.WriteByte(wasm.OpI32TruncF64U)
	case OpI64TruncF32S:
		buf.WriteByte(wasm.OpI64TruncF32S)
	case OpI64TruncF32U:
		buf.WriteByte(wasm.OpI64TruncF32U)
	case OpI64TruncF64S:
		buf.WriteByte(wasm.OpI64TruncF64S)
	case OpI64TruncF64U:
		buf.WriteByte(wasm.OpI64TruncF64U)
	case OpF32Abs:
		buf.WriteByte(wasm.OpF32Abs)
	case OpF64Abs:
		buf.WriteByte(wasm.OpF64Abs)
	case OpF32Neg:
		buf.WriteByte(wasm.OpF32Neg)
	case OpF64Neg:
		buf.WriteByte(wasm.OpF64Neg)
	case OpI32ReinterpretF32:
		buf.WriteByte(wasm.OpI32ReinterpretF32)
	case OpF32ReinterpretI32:
		buf.WriteByte(wasm.OpF32ReinterpretI32)
	case OpI64ReinterpretF64:
		buf.WriteByte(wasm.OpI64ReinterpretF64)
	case OpF64ReinterpretI64:
		buf.WriteByte(wasm.OpF64ReinterpretI64)
	}
}
