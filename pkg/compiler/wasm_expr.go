// File: pkg/compiler/wasm_expr.go
package compiler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

func (ctx *fnContext) compileExpr(buf *bytes.Buffer, expr Expr) error {
	switch e := expr.(type) {
	case *MoveExpr:
		return ctx.compileExpr(buf, e.Value)
	case *CastExpr:
		if err := ctx.compileExpr(buf, e.Expr); err != nil {
			return err
		}
		srcType := ctx.inferExprType(e.Expr)
		dstType := e.TargetType.String()
		emitCastOp(buf, srcType, dstType)
		return nil
	case *IfExpr:
		if err := ctx.compileExpr(buf, e.Cond); err != nil {
			return err
		}
		resultVal := byte(valTypeVoid)
		if e.Type != nil {
			switch e.Type.String() {
			case "f32":
				resultVal = valTypeF32
			case "f64":
				resultVal = valTypeF64
			case "i64":
				resultVal = valTypeI64
			case "void":
				resultVal = valTypeVoid
			default:
				resultVal = valTypeI32
			}
		} else {
			resultVal = valTypeI32
		}
		ctx.openLabel(buf, 0x04)
		buf.WriteByte(resultVal)
		if err := ctx.compileExprArm(buf, e.Then); err != nil {
			return err
		}
		buf.WriteByte(0x05)
		if err := ctx.compileExprArm(buf, e.Else); err != nil {
			return err
		}
		ctx.closeLabel(buf)
		return nil
	case *LiteralExpr:
		switch e.Type.String() {
		case "i8", "u8", "i16", "u16", "i32", "u32", "untyped int":
			val, _ := strconv.ParseInt(e.Val, 0, 64)
			buf.WriteByte(0x41)
			writeSleb128(buf, int32(val))
		case "i64", "u64":
			val, err := strconv.ParseInt(e.Val, 0, 64)
			if err != nil {
				uval, _ := strconv.ParseUint(e.Val, 0, 64)
				val = int64(uval)
			}
			buf.WriteByte(0x42)
			writeSleb128_64(buf, val)
		case "f32", "untyped float":
			var val float32
			fmt.Sscanf(e.Val, "%f", &val)
			buf.WriteByte(0x43)
			bits := math.Float32bits(val)
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], bits)
			buf.Write(b[:])
		case "f64":
			var val float64
			fmt.Sscanf(e.Val, "%f", &val)
			buf.WriteByte(0x44)
			bits := math.Float64bits(val)
			var b [8]byte
			binary.LittleEndian.PutUint64(b[:], bits)
			buf.Write(b[:])
		case "bool":
			buf.WriteByte(0x41)
			if e.Val == "true" {
				writeSleb128(buf, 1)
			} else {
				writeSleb128(buf, 0)
			}
		case "none":
			hdr := ctx.allocLocal(valTypeI32)
			ctx.emitAlloc(buf, 8, hdr)
			buf.WriteByte(0x20)
			writeUleb128(buf, hdr)
			buf.WriteByte(0x41)
			writeSleb128(buf, 0)
			buf.WriteByte(0x36)
			writeUleb128(buf, 2)
			writeUleb128(buf, 0)
			buf.WriteByte(0x20)
			writeUleb128(buf, hdr)
		case "string":
			off := ctx.emitter.internString(e.Val)
			buf.WriteByte(0x41)
			writeSleb128(buf, int32(off))
		default:
			buf.WriteByte(0x41)
			writeSleb128(buf, 0)
		}

	case *UnaryExpr:
		if err := ctx.compileExpr(buf, e.Operand); err != nil {
			return err
		}
		switch e.Op {
		case "!":
			buf.WriteByte(0x45)
		case "-":
			switch ctx.inferExprType(e.Operand) {
			case "f32":
				buf.WriteByte(0x8c)
			case "f64":
				buf.WriteByte(0x9a)
			case "i64", "u64":
				buf.WriteByte(0x42)
				writeSleb128_64(buf, -1)
				buf.WriteByte(0x7e) // i64.mul
			default:
				buf.WriteByte(0x41)
				writeSleb128(buf, -1)
				buf.WriteByte(0x6c)
			}
		case "~":
			if ctx.inferExprType(e.Operand) == "i64" || ctx.inferExprType(e.Operand) == "u64" {
				buf.WriteByte(0x42)
				writeSleb128_64(buf, -1)
				buf.WriteByte(0x85) // i64.xor
			} else {
				buf.WriteByte(0x41)
				writeSleb128(buf, -1)
				buf.WriteByte(0x73)
			}
		}

	case *IdentExpr:
		localIdx, exists := ctx.locals[e.Name]
		if !exists {
			return fmt.Errorf("unknown variable %s", e.Name)
		}
		buf.WriteByte(0x20)
		writeUleb128(buf, localIdx)

	case *BinaryExpr:
		if (e.Op == "==" || e.Op == "!=") && (e.Left != nil && (e.OperandType != nil && e.OperandType.String() == "string")) {
			if err := ctx.compileExpr(buf, e.Left); err != nil {
				return err
			}
			if err := ctx.compileExpr(buf, e.Right); err != nil {
				return err
			}
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__str_eq"])
			if e.Op == "!=" {
				buf.WriteByte(0x45)
			}
			return nil
		}

		if e.Op == "+" && (e.Type != nil && e.Type.String() == "string") {
			if err := ctx.compileExpr(buf, e.Left); err != nil {
				return err
			}
			if err := ctx.compileExpr(buf, e.Right); err != nil {
				return err
			}
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__str_concat"])
			return nil
		}

		opType := ""
		if e.OperandType != nil {
			opType = e.OperandType.String()
		} else if e.Type != nil {
			opType = e.Type.String()
		} else if lt := ctx.inferExprType(e.Left); lt != "" {
			opType = lt
		}

		if err := ctx.compileExpr(buf, e.Left); err != nil {
			return err
		}
		if err := ctx.compileExpr(buf, e.Right); err != nil {
			return err
		}

		// Mixed-width arithmetic: when the operation is evaluated at 64-bit
		// width but the right operand was emitted as a 32-bit integer (for
		// example a literal shift count or bit mask), widen it to i64.
		if opType == "i64" || opType == "u64" {
			rt := ctx.inferExprType(e.Right)
			switch rt {
			case "", "i64", "u64", "f32", "f64":
			default:
				if opType == "u64" {
					buf.WriteByte(0xad) // i64.extend_i32_u
				} else {
					buf.WriteByte(0xac) // i64.extend_i32_s
				}
			}
		}

		switch opType {
		case "f32":
			switch e.Op {
			case "+":
				buf.WriteByte(0x92)
			case "-":
				buf.WriteByte(0x93)
			case "*":
				buf.WriteByte(0x94)
			case "/":
				buf.WriteByte(0x95)
			case "==":
				buf.WriteByte(0x5b)
			case "!=":
				buf.WriteByte(0x5c)
			case "<":
				buf.WriteByte(0x5d)
			case "<=":
				buf.WriteByte(0x5f)
			case ">":
				buf.WriteByte(0x5e)
			case ">=":
				buf.WriteByte(0x60)
			}
		case "f64":
			switch e.Op {
			case "+":
				buf.WriteByte(0xa0)
			case "-":
				buf.WriteByte(0xa1)
			case "*":
				buf.WriteByte(0xa2)
			case "/":
				buf.WriteByte(0xa3)
			case "==":
				buf.WriteByte(0x61)
			case "!=":
				buf.WriteByte(0x62)
			case "<":
				buf.WriteByte(0x63)
			case "<=":
				buf.WriteByte(0x65)
			case ">":
				buf.WriteByte(0x64)
			case ">=":
				buf.WriteByte(0x66)
			}
		case "i64":
			switch e.Op {
			case "+":
				buf.WriteByte(0x7c)
			case "-":
				buf.WriteByte(0x7d)
			case "*":
				buf.WriteByte(0x7e)
			case "/":
				buf.WriteByte(0x7f)
			case "%":
				buf.WriteByte(0x81)
			case "==":
				buf.WriteByte(0x51)
			case "!=":
				buf.WriteByte(0x52)
			case "<":
				buf.WriteByte(0x53)
			case "<=":
				buf.WriteByte(0x57)
			case ">":
				buf.WriteByte(0x55)
			case ">=":
				buf.WriteByte(0x59)
			case "&":
				buf.WriteByte(0x83)
			case "|":
				buf.WriteByte(0x84)
			case "^":
				buf.WriteByte(0x85)
			case "<<":
				buf.WriteByte(0x86)
			case ">>":
				buf.WriteByte(0x87)
			}
		case "u64":
			switch e.Op {
			case "+":
				buf.WriteByte(0x7c)
			case "-":
				buf.WriteByte(0x7d)
			case "*":
				buf.WriteByte(0x7e)
			case "/":
				buf.WriteByte(0x80)
			case "%":
				buf.WriteByte(0x82)
			case "==":
				buf.WriteByte(0x51)
			case "!=":
				buf.WriteByte(0x52)
			case "<":
				buf.WriteByte(0x54)
			case "<=":
				buf.WriteByte(0x58)
			case ">":
				buf.WriteByte(0x56)
			case ">=":
				buf.WriteByte(0x5a)
			case "&":
				buf.WriteByte(0x83)
			case "|":
				buf.WriteByte(0x84)
			case "^":
				buf.WriteByte(0x85)
			case "<<":
				buf.WriteByte(0x86)
			case ">>":
				buf.WriteByte(0x88)
			}
		default:
			switch e.Op {
			case "+":
				buf.WriteByte(0x6a)
			case "-":
				buf.WriteByte(0x6b)
			case "*":
				buf.WriteByte(0x6c)
			case "/":
				if opType == "u32" || opType == "u16" || opType == "u8" {
					buf.WriteByte(0x6e)
				} else {
					buf.WriteByte(0x6d)
				}
			case "%":
				if opType == "u32" || opType == "u16" || opType == "u8" {
					buf.WriteByte(0x70)
				} else {
					buf.WriteByte(0x6f)
				}
			case "==":
				buf.WriteByte(0x46)
			case "!=":
				buf.WriteByte(0x47)
			case "<":
				if opType == "u32" || opType == "u16" || opType == "u8" {
					buf.WriteByte(0x49)
				} else {
					buf.WriteByte(0x48)
				}
			case "<=":
				if opType == "u32" || opType == "u16" || opType == "u8" {
					buf.WriteByte(0x4d)
				} else {
					buf.WriteByte(0x4c)
				}
			case ">":
				if opType == "u32" || opType == "u16" || opType == "u8" {
					buf.WriteByte(0x4b)
				} else {
					buf.WriteByte(0x4a)
				}
			case ">=":
				if opType == "u32" || opType == "u16" || opType == "u8" {
					buf.WriteByte(0x4f)
				} else {
					buf.WriteByte(0x4e)
				}
			case "&&", "&":
				buf.WriteByte(0x71)
			case "||", "|":
				buf.WriteByte(0x72)
			case "^":
				buf.WriteByte(0x73)
			case "<<":
				buf.WriteByte(0x74)
			case ">>":
				if opType == "u32" || opType == "u16" || opType == "u8" {
					buf.WriteByte(0x76)
				} else {
					buf.WriteByte(0x75)
				}
			}
		}

	case *IndexExpr:
		if err := ctx.compileExpr(buf, e.Target); err != nil {
			return err
		}
		if st, isSlice := e.TargetType.(SliceType); isSlice {
			// Deref the header pointer to get the data ptr.
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 0)
			width, loadOp, _, align := sliceElemInfo(st.Elem)
			if err := ctx.compileExpr(buf, e.Index); err != nil {
				return err
			}
			if width != 1 {
				buf.WriteByte(0x41)
				writeSleb128(buf, width)
				buf.WriteByte(0x6c) // i32.mul
			}
			buf.WriteByte(0x6a) // i32.add
			buf.WriteByte(loadOp)
			writeUleb128(buf, align)
			writeUleb128(buf, 0)
			return nil
		} else if _, isStr := e.TargetType.(StringType); isStr {
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 0)
			if err := ctx.compileExpr(buf, e.Index); err != nil {
				return err
			}
			buf.WriteByte(0x6a)
			buf.WriteByte(0x2d)
			writeUleb128(buf, 0)
			writeUleb128(buf, 0)
			return nil
		} else if at, isArr := e.TargetType.(ArrayType); isArr {
			width, loadOp, _, align := sliceElemInfo(at.Elem)
			if err := ctx.compileExpr(buf, e.Index); err != nil {
				return err
			}
			if width != 1 {
				buf.WriteByte(0x41)
				writeSleb128(buf, width)
				buf.WriteByte(0x6c) // i32.mul
			}
			buf.WriteByte(0x6a) // i32.add
			buf.WriteByte(loadOp)
			writeUleb128(buf, align)
			writeUleb128(buf, 0)
			return nil
		}
		if err := ctx.compileExpr(buf, e.Index); err != nil {
			return err
		}
		buf.WriteByte(0x41)
		writeSleb128(buf, 4)
		buf.WriteByte(0x6c)
		buf.WriteByte(0x6a)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

	case *MemberExpr:
		if e.Member == "len" {
			if arr, ok := e.TargetType.(ArrayType); ok {
				buf.WriteByte(0x41)
				writeSleb128(buf, int32(arr.Size))
				return nil
			}
			if err := ctx.compileExpr(buf, e.Target); err != nil {
				return err
			}
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 4)
			return nil
		}
		if e.Member == "cap" {
			if arr, ok := e.TargetType.(ArrayType); ok {
				buf.WriteByte(0x41)
				writeSleb128(buf, int32(arr.Size))
				return nil
			}
			if err := ctx.compileExpr(buf, e.Target); err != nil {
				return err
			}
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 8)
			return nil
		}

		if targetId, ok := e.Target.(*IdentExpr); ok {
			if variants, ok := ctx.emitter.enums[targetId.Name]; ok {
				for tagIdx, v := range variants {
					if v.Name == e.Member {
						enumHdr := ctx.allocLocal(valTypeI32)
						ctx.emitAlloc(buf, 4, enumHdr)
						buf.WriteByte(0x20)
						writeUleb128(buf, enumHdr)
						buf.WriteByte(0x41)
						writeSleb128(buf, int32(tagIdx))
						buf.WriteByte(0x36)
						writeUleb128(buf, 2)
						writeUleb128(buf, 0)
						buf.WriteByte(0x20)
						writeUleb128(buf, enumHdr)
						return nil
					}
				}
			}
		}

		structName := e.TargetType.String()
		meta, ok := ctx.emitter.structs[structName]
		if !ok {
			return fmt.Errorf("unknown struct type %s", structName)
		}
		fieldKey := memberFieldKey(e)
		offset, ok := meta.fieldOffsets[fieldKey]
		if !ok {
			return fmt.Errorf("unknown field %s on struct %s", fieldKey, structName)
		}
		fieldType := meta.fieldTypes[fieldKey]
		if ctx.isInlineAggregate(fieldType) {
			// Compound-valued field (nested struct or fixed array): return a
			// pointer into the owner's inline storage, not a scalar load.
			if err := ctx.compileExpr(buf, e.Target); err != nil {
				return err
			}
			if offset != 0 {
				buf.WriteByte(0x41)
				writeSleb128(buf, int32(offset))
				buf.WriteByte(0x6a)
			}
			return nil
		}
		if err := ctx.compileExpr(buf, e.Target); err != nil {
			return err
		}
		loadOp, _, loadAl := fieldLoadStoreInfo(fieldType)
		buf.WriteByte(loadOp)
		writeUleb128(buf, loadAl)
		writeUleb128(buf, uint32(offset))

	case *SliceSubExpr:
		// Determine element width and header size from the target type.
		// A string is {ptr, len} = 8 bytes with byte elements (width 1).
		// A []T is {ptr, len, cap} = 12 bytes with element width sizeof(T).
		// For []T we currently assume 4-byte elements; this matches the
		// ArrayInitExpr lowering and covers i32/f32/bool. Widen to 8 when
		// i64/f64 element slices are added.
		elemWidth := int32(1)
		hdrSize := int32(8)
		isSlice := false
		if e.TargetType != nil {
			switch tt := e.TargetType.(type) {
			case StringType:
				elemWidth = 1
				hdrSize = 8
				isSlice = false
			case SliceType:
				elemWidth = 4
				hdrSize = 12
				isSlice = true
				if tt.Elem != nil {
					switch tt.Elem.String() {
					case "i64", "u64", "f64":
						elemWidth = 8
					}
				}
			case ArrayType:
				elemWidth = 4
				hdrSize = 12
				isSlice = true
				if tt.Elem != nil {
					switch tt.Elem.String() {
					case "i64", "u64", "f64":
						elemWidth = 8
					}
				}
			}
		}

		targetLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, e.Target); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, targetLocal)

		startLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, e.Start); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, startLocal)

		endLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, e.End); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, endLocal)

		hdrLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, hdrSize, hdrLocal)

		// hdr[0] = target.ptr + start * elemWidth
		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)
		buf.WriteByte(0x20)
		writeUleb128(buf, targetLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)
		buf.WriteByte(0x20)
		writeUleb128(buf, startLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, elemWidth)
		buf.WriteByte(0x6c)
		buf.WriteByte(0x6a)
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

		// hdr[4] = end - start  (len)
		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)
		buf.WriteByte(0x20)
		writeUleb128(buf, endLocal)
		buf.WriteByte(0x20)
		writeUleb128(buf, startLocal)
		buf.WriteByte(0x6b)
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)

		// For []T only: hdr[8] = cap = end - start
		if isSlice {
			buf.WriteByte(0x20)
			writeUleb128(buf, hdrLocal)
			buf.WriteByte(0x20)
			writeUleb128(buf, endLocal)
			buf.WriteByte(0x20)
			writeUleb128(buf, startLocal)
			buf.WriteByte(0x6b)
			buf.WriteByte(0x36)
			writeUleb128(buf, 2)
			writeUleb128(buf, 8)
		}

		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)

	case *ArrayInitExpr:
		elemCount := len(e.Elements)
		elemSize := 4
		storeOp := byte(0x36)
		storeAlign := uint32(2)
		if e.Elem != nil {
			elemSize = e.Elem.ByteSize()
			if elemSize < 1 {
				elemSize = 4
			}
			_, storeOp, storeAlign = fieldLoadStoreInfo(e.Elem)
		}
		totalSize := elemCount * elemSize
		baseLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, int32(totalSize), baseLocal)

		for i, el := range e.Elements {
			buf.WriteByte(0x20)
			writeUleb128(buf, baseLocal)
			if err := ctx.compileExpr(buf, el); err != nil {
				return err
			}
			buf.WriteByte(storeOp)
			writeUleb128(buf, storeAlign)
			writeUleb128(buf, uint32(i*elemSize))
		}

		buf.WriteByte(0x20)
		writeUleb128(buf, baseLocal)

	case *SliceInitExpr:
		elemCount := len(e.Elements)
		capCount := elemCount
		if capCount < 4 {
			capCount = 4
		}
		dataLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, int32(capCount*4), dataLocal)

		for i, el := range e.Elements {
			buf.WriteByte(0x20)
			writeUleb128(buf, dataLocal)
			if err := ctx.compileExpr(buf, el); err != nil {
				return err
			}
			buf.WriteByte(0x36)
			writeUleb128(buf, 2)
			writeUleb128(buf, uint32(i*4))
		}

		headerLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, 12, headerLocal)

		buf.WriteByte(0x20)
		writeUleb128(buf, headerLocal)
		buf.WriteByte(0x20)
		writeUleb128(buf, dataLocal)
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

		buf.WriteByte(0x20)
		writeUleb128(buf, headerLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, int32(elemCount))
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)

		buf.WriteByte(0x20)
		writeUleb128(buf, headerLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, int32(capCount))
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 8)

		buf.WriteByte(0x20)
		writeUleb128(buf, headerLocal)

	case *ArrayToSliceExpr:
		arrLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, e.Value); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, arrLocal)

		hdrLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, 12, hdrLocal)

		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)
		buf.WriteByte(0x20)
		writeUleb128(buf, arrLocal)
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, int32(e.ArrayType.Size))
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)

		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, int32(e.ArrayType.Size))
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 8)

		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)

	case *StructInitExpr:
		meta, ok := ctx.emitter.structs[e.Name]
		if !ok {
			return fmt.Errorf("unknown struct %s in init", e.Name)
		}
		structBase := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, int32(meta.totalSize), structBase)

		for _, f := range e.Fields {
			offset := meta.fieldOffsets[f.Name]
			fieldType := meta.fieldTypes[f.Name]
			if ctx.isInlineAggregate(fieldType) {
				// mem.copy(structBase+offset, src_ptr, sizeofInline).
				// Stack order: [dst, src, len] then memory.copy.
				buf.WriteByte(0x20)
				writeUleb128(buf, structBase)
				if offset != 0 {
					buf.WriteByte(0x41)
					writeSleb128(buf, int32(offset))
					buf.WriteByte(0x6a)
				}
				if err := ctx.compileExpr(buf, f.Val); err != nil {
					return err
				}
				buf.WriteByte(0x41)
				writeSleb128(buf, int32(ctx.emitter.sizeofInline(fieldType)))
				ctx.emitMemoryCopy(buf)
				continue
			}
			buf.WriteByte(0x20)
			writeUleb128(buf, structBase)
			if err := ctx.compileExpr(buf, f.Val); err != nil {
				return err
			}
			_, storeOp, storeAl := fieldLoadStoreInfo(fieldType)
			buf.WriteByte(storeOp)
			writeUleb128(buf, storeAl)
			writeUleb128(buf, uint32(offset))
		}

		buf.WriteByte(0x20)
		writeUleb128(buf, structBase)

	case *TupleExpr:
		offset := 0
		type tupFieldInfo struct {
			t       UbeType
			offset  int
			storeOp byte
			align   uint32
		}
		var infos []tupFieldInfo
		for i, el := range e.Elems {
			var t UbeType
			if i < len(e.Types) {
				t = e.Types[i]
			} else {
				if inferred := ctx.inferExprType(el); inferred != "" {
					t = typeFromString(inferred)
				}
			}
			sz := 4
			al := uint32(2)
			storeOp := byte(0x36)
			if t != nil {
				_, storeOp, al = fieldLoadStoreInfo(t)
				if t.ByteSize() > 4 {
					sz = t.ByteSize()
					offset = alignUpInt(offset, t.Align())
				}
			}
			infos = append(infos, tupFieldInfo{t: t, offset: offset, storeOp: storeOp, align: al})
			offset += sz
		}
		totalSize := alignUpInt(offset, 4)
		if totalSize < 4 {
			totalSize = 4
		}
		tupBase := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, int32(totalSize), tupBase)
		for i, el := range e.Elems {
			buf.WriteByte(0x20)
			writeUleb128(buf, tupBase)
			if err := ctx.compileExpr(buf, el); err != nil {
				return err
			}
			buf.WriteByte(infos[i].storeOp)
			writeUleb128(buf, infos[i].align)
			writeUleb128(buf, uint32(infos[i].offset))
		}
		buf.WriteByte(0x20)
		writeUleb128(buf, tupBase)

	case *PromoteExpr:
		if err := ctx.compileExpr(buf, e.Value); err != nil {
			return err
		}
		if ctx.emitter.memTrace {
			sizeEst := int32(16)
			if e.Type != nil {
				sz := e.Type.ByteSize()
				if sz > 0 {
					sizeEst = int32(sz)
				}
			}
			emitI32Const(buf, sizeEst)
			emitCallIdx(buf, ctx.emitter.traceImports["arena_promote"])
		}

	case *ArenaExpr:
		savedLocal := ctx.allocLocal(valTypeI32)
		buf.WriteByte(0x23)
		writeUleb128(buf, 0)
		buf.WriteByte(0x21)
		writeUleb128(buf, savedLocal)

		ctx.pushDeferScope()
		terminated := false
		hasResult := e.Type != nil && e.Type.String() != "void" && len(e.Body.Stmts) > 0
		var resLocal uint32
		if hasResult {
			resWasm := wasmTypeOrDefault(e.Type)
			resLocal = ctx.allocLocal(resWasm)
		}

		for i, stmt := range e.Body.Stmts {
			if hasResult && i == len(e.Body.Stmts)-1 {
				es, ok := stmt.(*ExprStmt)
				if !ok {
					ctx.popDeferScope()
					return fmt.Errorf("%s: arena expression's last statement must be an expression", stmt.Loc())
				}
				if err := ctx.compileExpr(buf, es.Expr); err != nil {
					ctx.popDeferScope()
					return err
				}
				buf.WriteByte(0x21)
				writeUleb128(buf, resLocal)
				break
			}
			if err := ctx.compileStmt(buf, stmt); err != nil {
				ctx.popDeferScope()
				return err
			}
			if stmtTerminated(stmt) {
				terminated = true
				break
			}
		}
		if !terminated {
			ctx.emitCurrentScopeDefers(buf)
		}
		ctx.popDeferScope()

		buf.WriteByte(0x20)
		writeUleb128(buf, savedLocal)
		buf.WriteByte(0x24)
		writeUleb128(buf, 0)

		if hasResult {
			buf.WriteByte(0x20)
			writeUleb128(buf, resLocal)
		}

	case *TraitCoerceExpr:
		fatLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, 8, fatLocal)

		buf.WriteByte(0x20)
		writeUleb128(buf, fatLocal)
		if err := ctx.compileExpr(buf, e.Value); err != nil {
			return err
		}
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

		typeID := ctx.emitter.typeIDs[e.Concrete.String()]
		buf.WriteByte(0x20)
		writeUleb128(buf, fatLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, typeID)
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)

		buf.WriteByte(0x20)
		writeUleb128(buf, fatLocal)

	case *TryExpr:
		okType := unwrapBindingType(ctx.exprStaticType(e.Operand))
		resWasm := wasmTypeForUbe(okType)
		resLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, e.Operand); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, resLocal)

		buf.WriteByte(0x20)
		writeUleb128(buf, resLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)
		buf.WriteByte(0x45)
		buf.WriteByte(0x04)
		buf.WriteByte(valTypeVoid)

		ctx.emitAllDefers(buf)

		buf.WriteByte(0x20)
		writeUleb128(buf, resLocal)
		buf.WriteByte(0x0f)
		buf.WriteByte(0x0b)

		buf.WriteByte(0x20)
		writeUleb128(buf, resLocal)
		switch resWasm {
		case valTypeF32:
			buf.WriteByte(0x2a) // f32.load
			writeUleb128(buf, 2)
			writeUleb128(buf, 4)
		case valTypeF64:
			buf.WriteByte(0x2b) // f64.load
			writeUleb128(buf, 3)
			writeUleb128(buf, 4)
		case valTypeI64:
			buf.WriteByte(0x29) // i64.load
			writeUleb128(buf, 3)
			writeUleb128(buf, 4)
		default:
			buf.WriteByte(0x28) // i32.load
			writeUleb128(buf, 2)
			writeUleb128(buf, 4)
		}

	case *OrElseExpr:
		resWasm := byte(valTypeI32)
		switch ctx.inferExprType(e.Fallback) {
		case "f32":
			resWasm = valTypeF32
		case "f64":
			resWasm = valTypeF64
		case "i64":
			resWasm = valTypeI64
		}

		optLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, e.Primary); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, optLocal)

		resLocal := ctx.allocLocal(resWasm)

		buf.WriteByte(0x20)
		writeUleb128(buf, optLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)
		buf.WriteByte(0x04)
		buf.WriteByte(valTypeVoid)

		buf.WriteByte(0x20)
		writeUleb128(buf, optLocal)
		switch resWasm {
		case valTypeF32:
			buf.WriteByte(0x2a) // f32.load
			writeUleb128(buf, 2)
			writeUleb128(buf, 4)
		case valTypeF64:
			buf.WriteByte(0x2b) // f64.load
			writeUleb128(buf, 3)
			writeUleb128(buf, 4)
		case valTypeI64:
			buf.WriteByte(0x29) // i64.load
			writeUleb128(buf, 3)
			writeUleb128(buf, 4)
		default:
			buf.WriteByte(0x28) // i32.load
			writeUleb128(buf, 2)
			writeUleb128(buf, 4)
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, resLocal)

		buf.WriteByte(0x05)

		if err := ctx.compileExpr(buf, e.Fallback); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, resLocal)

		buf.WriteByte(0x0b)

		buf.WriteByte(0x20)
		writeUleb128(buf, resLocal)

	case *ErrorExpr:
		errLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, 8, errLocal)

		buf.WriteByte(0x20)
		writeUleb128(buf, errLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, 0)
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

		buf.WriteByte(0x20)
		writeUleb128(buf, errLocal)
		if err := ctx.compileExpr(buf, e.Arg); err != nil {
			return err
		}
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)

		buf.WriteByte(0x20)
		writeUleb128(buf, errLocal)

	case *InterpolatedStringExpr:
		resHdr := ctx.allocLocal(valTypeI32)
		emptyOff := ctx.emitter.internString("")
		buf.WriteByte(0x41)
		writeSleb128(buf, int32(emptyOff))
		buf.WriteByte(0x21)
		writeUleb128(buf, resHdr)

		for _, p := range e.Parts {
			buf.WriteByte(0x20)
			writeUleb128(buf, resHdr)
			if err := ctx.compileStringPart(buf, p); err != nil {
				return err
			}
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__str_concat"])
			buf.WriteByte(0x21)
			writeUleb128(buf, resHdr)
		}
		buf.WriteByte(0x20)
		writeUleb128(buf, resHdr)

	case *CallExpr:
		if handled, err := ctx.compileSpecialCall(buf, e); handled {
			return err
		}
		if mem, ok := e.Callee.(*MemberExpr); ok {
			if targetId, ok := mem.Target.(*IdentExpr); ok {
				if variants, ok := ctx.emitter.enums[targetId.Name]; ok {
					for tagIdx, v := range variants {
						if v.Name == mem.Member {
							size := 4 + len(e.Args)*4
							enumHdr := ctx.allocLocal(valTypeI32)
							ctx.emitAlloc(buf, int32(size), enumHdr)

							buf.WriteByte(0x20)
							writeUleb128(buf, enumHdr)
							buf.WriteByte(0x41)
							writeSleb128(buf, int32(tagIdx))
							buf.WriteByte(0x36)
							writeUleb128(buf, 2)
							writeUleb128(buf, 0)

							for aIdx, arg := range e.Args {
								buf.WriteByte(0x20)
								writeUleb128(buf, enumHdr)
								if err := ctx.compileExpr(buf, arg); err != nil {
									return err
								}
								storeOp := byte(0x36)
								storeAl := uint32(2)
								if aIdx < len(v.Types) && v.Types[aIdx] != nil {
									switch v.Types[aIdx].String() {
									case "f32":
										storeOp = 0x38
									case "f64":
										storeOp = 0x39
										storeAl = 3
									case "i64":
										storeOp = 0x37
										storeAl = 3
									}
								}
								buf.WriteByte(storeOp)
								writeUleb128(buf, storeAl)
								writeUleb128(buf, uint32(4+aIdx*4))
							}

							buf.WriteByte(0x20)
							writeUleb128(buf, enumHdr)
							return nil
						}
					}
				}
			}
		}

		if mem, ok := e.Callee.(*MemberExpr); ok {
			if _, isSlice := mem.TargetType.(SliceType); isSlice {
				switch mem.Member {
				case "resize":
					if len(e.Args) != 1 {
						return nil
					}
					width := int32(4)
					if st, isSlice := mem.TargetType.(SliceType); isSlice {
						width, _, _, _ = sliceElemInfo(st.Elem)
					}

					slLocal := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, mem.Target); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, slLocal)

					newLenL := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, e.Args[0]); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, newLenL)

					oldLenL := ctx.allocLocal(valTypeI32)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					buf.WriteByte(0x21)
					writeUleb128(buf, oldLenL)

					// if newLen > cap { grow }
					newCapL := ctx.allocLocal(valTypeI32)
					newSizeL := ctx.allocLocal(valTypeI32)
					newBufL := ctx.allocLocal(valTypeI32)

					buf.WriteByte(0x20)
					writeUleb128(buf, newLenL)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)
					buf.WriteByte(0x4b) // i32.gt_u
					buf.WriteByte(0x04) // if
					buf.WriteByte(valTypeVoid)

					// newCap = select(newLen, cap*2+8, newLen > cap*2+8)
					// Wasm select returns the first value when the
					// condition is true, so the true value (newLen) is
					// pushed first and the false value (cap*2+8) second.
					buf.WriteByte(0x20)
					writeUleb128(buf, newLenL)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)
					buf.WriteByte(0x41)
					writeSleb128(buf, 2)
					buf.WriteByte(0x6c)
					buf.WriteByte(0x41)
					writeSleb128(buf, 8)
					buf.WriteByte(0x6a)
					//   condition: newLen > cap*2+8
					buf.WriteByte(0x20)
					writeUleb128(buf, newLenL)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)
					buf.WriteByte(0x41)
					writeSleb128(buf, 2)
					buf.WriteByte(0x6c)
					buf.WriteByte(0x41)
					writeSleb128(buf, 8)
					buf.WriteByte(0x6a)
					buf.WriteByte(0x4b) // i32.gt_u
					buf.WriteByte(0x1b) // select
					buf.WriteByte(0x21)
					writeUleb128(buf, newCapL)

					// newSize = newCap * width
					buf.WriteByte(0x20)
					writeUleb128(buf, newCapL)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, newSizeL)

					if ctx.emitter.memTrace {
						buf.WriteByte(0x20)
						writeUleb128(buf, slLocal)
						buf.WriteByte(0x28)
						writeUleb128(buf, 2)
						writeUleb128(buf, 8) // old cap
						buf.WriteByte(0x20)
						writeUleb128(buf, newCapL)
						emitI32Const(buf, width)
						emitCallIdx(buf, ctx.emitter.traceImports["slice_realloc"])
					}

					// newBuf = __arena_alloc_dyn(newSize)
					ctx.emitAllocDyn(buf, newSizeL, newBufL)

					// memory.copy(newBuf, slLocal.ptr, oldLen * width)
					buf.WriteByte(0x20)
					writeUleb128(buf, newBufL)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)
					buf.WriteByte(0x20)
					writeUleb128(buf, oldLenL)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					ctx.emitMemoryCopy(buf)

					// slLocal.ptr = newBuf
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, newBufL)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					// slLocal.cap = newCap
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, newCapL)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)

					buf.WriteByte(0x0b) // end if

					// if newLen > oldLen { memory.fill tail with zeros }
					fillDstL := ctx.allocLocal(valTypeI32)
					fillLenL := ctx.allocLocal(valTypeI32)

					buf.WriteByte(0x20)
					writeUleb128(buf, newLenL)
					buf.WriteByte(0x20)
					writeUleb128(buf, oldLenL)
					buf.WriteByte(0x4b) // i32.gt_u
					buf.WriteByte(0x04)
					buf.WriteByte(valTypeVoid)

					// dst = ptr + oldLen*width
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)
					buf.WriteByte(0x20)
					writeUleb128(buf, oldLenL)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					buf.WriteByte(0x6a)
					buf.WriteByte(0x21)
					writeUleb128(buf, fillDstL)

					// fillLen = (newLen - oldLen) * width
					buf.WriteByte(0x20)
					writeUleb128(buf, newLenL)
					buf.WriteByte(0x20)
					writeUleb128(buf, oldLenL)
					buf.WriteByte(0x6b) // i32.sub
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, fillLenL)

					// memory.fill(dst, 0, fillLen)
					buf.WriteByte(0x20)
					writeUleb128(buf, fillDstL)
					buf.WriteByte(0x41)
					writeSleb128(buf, 0)
					buf.WriteByte(0x20)
					writeUleb128(buf, fillLenL)
					buf.WriteByte(0xfc)
					writeUleb128(buf, 0x0b)
					writeUleb128(buf, 0x00)

					buf.WriteByte(0x0b) // end if

					// slLocal.len = newLen
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, newLenL)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					return nil

				case "push":
					width := int32(4)
					storeOp := byte(0x36)
					storeAlign := uint32(2)
					if st, isSlice := mem.TargetType.(SliceType); isSlice {
						width, _, storeOp, storeAlign = sliceElemInfo(st.Elem)
					}

					slLocal := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, mem.Target); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, slLocal)

					// if header.len >= header.cap {
					//     newCap = header.cap*2 + 8
					//     newSize = newCap * width
					//     newBuf = __arena_alloc_dyn(newSize)
					//     memory.copy(newBuf, header.ptr, header.len * width)
					//     header.ptr = newBuf
					//     header.cap = newCap
					// }
					newCapL := ctx.allocLocal(valTypeI32)
					newSizeL := ctx.allocLocal(valTypeI32)
					newBufL := ctx.allocLocal(valTypeI32)

					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28) // header.len
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28) // header.cap
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)
					buf.WriteByte(0x4f) // i32.ge_u
					buf.WriteByte(0x04) // if
					buf.WriteByte(valTypeVoid)

					// newCap = cap*2 + 8
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)
					buf.WriteByte(0x41)
					writeSleb128(buf, 2)
					buf.WriteByte(0x6c)
					buf.WriteByte(0x41)
					writeSleb128(buf, 8)
					buf.WriteByte(0x6a)
					buf.WriteByte(0x21)
					writeUleb128(buf, newCapL)

					// newSize = newCap * width
					buf.WriteByte(0x20)
					writeUleb128(buf, newCapL)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, newSizeL)

					// newBuf = __arena_alloc_dyn(newSize)
					ctx.emitAllocDyn(buf, newSizeL, newBufL)

					// memory.copy(newBuf, header.ptr, header.len * width)
					buf.WriteByte(0x20)
					writeUleb128(buf, newBufL)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					ctx.emitMemoryCopy(buf)

					// header.ptr = newBuf
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, newBufL)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					// header.cap = newCap
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, newCapL)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)

					buf.WriteByte(0x0b) // end if

					// header.ptr + header.len * width
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					buf.WriteByte(0x6a)

					if err := ctx.compileExpr(buf, e.Args[0]); err != nil {
						return err
					}
					buf.WriteByte(storeOp)
					writeUleb128(buf, storeAlign)
					writeUleb128(buf, 0)

					// header.len = header.len + 1
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x6a)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					return nil

				case "pop":
					width := int32(4)
					loadOp := byte(0x28)
					loadAlign := uint32(2)
					if st, isSlice := mem.TargetType.(SliceType); isSlice {
						width, loadOp, _, loadAlign = sliceElemInfo(st.Elem)
					}

					slLocal := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, mem.Target); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, slLocal)

					optHdr := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 8, optHdr)

					// if header.len == 0 { optHdr[0] = 0 } else { ... }
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28) // i32.load header.len
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					buf.WriteByte(0x45) // i32.eqz
					buf.WriteByte(0x04) // if
					buf.WriteByte(valTypeVoid)

					// Empty slice: optHdr[0] = 0 (none)
					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 0)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					buf.WriteByte(0x05) // else

					// Non-empty: header.len = header.len - 1
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x6b)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)

					// optHdr[0] = 1 (some)
					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					// optHdr[4] = header.ptr[header.len] (width-aware load)
					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					buf.WriteByte(0x6a)
					buf.WriteByte(loadOp)
					writeUleb128(buf, loadAlign)
					writeUleb128(buf, 0)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)

					buf.WriteByte(0x0b) // end if

					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					return nil
				}
			}

			if _, isPool := mem.TargetType.(PoolType); isPool {
				switch mem.Member {
				case "new":
					poolHdr := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 16, poolHdr)

					capLocal := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, e.Args[0]); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, capLocal)

					slotsLocal := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 1024, slotsLocal)

					buf.WriteByte(0x20)
					writeUleb128(buf, poolHdr)
					buf.WriteByte(0x20)
					writeUleb128(buf, capLocal)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					buf.WriteByte(0x20)
					writeUleb128(buf, poolHdr)
					buf.WriteByte(0x20)
					writeUleb128(buf, slotsLocal)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)

					buf.WriteByte(0x20)
					writeUleb128(buf, poolHdr)
					return nil

				case "spawn":
					handleHdr := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 8, handleHdr)

					buf.WriteByte(0x20)
					writeUleb128(buf, handleHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					buf.WriteByte(0x20)
					writeUleb128(buf, handleHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)

					buf.WriteByte(0x20)
					writeUleb128(buf, handleHdr)
					return nil

				case "get":
					optHdr := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 8, optHdr)

					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 100)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)

					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					return nil

				case "despawn":
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					return nil
				}
			}

			if _, isChan := mem.TargetType.(ChanType); isChan {
				switch mem.Member {
				case "new":
					chanHdr := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 24, chanHdr)

					buf.WriteByte(0x20)
					writeUleb128(buf, chanHdr)
					if err := ctx.compileExpr(buf, e.Args[0]); err != nil {
						return err
					}
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					bufBuf := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 128, bufBuf)

					buf.WriteByte(0x20)
					writeUleb128(buf, chanHdr)
					buf.WriteByte(0x20)
					writeUleb128(buf, bufBuf)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 20)

					buf.WriteByte(0x20)
					writeUleb128(buf, chanHdr)
					return nil

				case "send":
					chanLocal := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, mem.Target); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, chanLocal)

					// head = chan.head
					// cap = chan.capacity
					// write_idx = (chan.head + chan.count) % chan.capacity
					// slot_offset = chan.buffer_ptr + write_idx * 4
					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 20) // chan.buffer_ptr

					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8) // chan.head

					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 12) // chan.count

					buf.WriteByte(0x6a) // head + count

					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0) // chan.capacity

					buf.WriteByte(0x70) // i32.rem_u => write_idx

					buf.WriteByte(0x41) // 4
					writeSleb128(buf, 4)
					buf.WriteByte(0x6c) // write_idx * 4
					buf.WriteByte(0x6a) // buffer_ptr + offset

					if err := ctx.compileExpr(buf, e.Args[0]); err != nil {
						return err
					}
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0) // store val in slot

					// chan.count++
					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 12)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x6a)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 12)
					return nil

				case "recv":
					chanLocal := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, mem.Target); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, chanLocal)

					optHdr := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 8, optHdr)

					// Check count > 0
					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 12) // count
					buf.WriteByte(0x41)
					writeSleb128(buf, 0)
					buf.WriteByte(0x4a) // i32.gt_s
					buf.WriteByte(0x04) // if
					buf.WriteByte(valTypeVoid)

					// optHdr.has_value = 1
					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					// read_ptr = chan.buffer_ptr + chan.head * 4
					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)

					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 20) // buffer_ptr

					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8) // head

					buf.WriteByte(0x41)
					writeSleb128(buf, 4)
					buf.WriteByte(0x6c) // head * 4
					buf.WriteByte(0x6a) // buffer_ptr + head*4

					buf.WriteByte(0x28) // load val
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					buf.WriteByte(0x36) // store into optHdr.value
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)

					// chan.head = (chan.head + 1) % chan.capacity
					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)

					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8) // head
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x6a) // head + 1

					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0) // capacity
					buf.WriteByte(0x70)  // rem_u

					buf.WriteByte(0x36) // store chan.head
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)

					// chan.count--
					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 12)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x6b)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 12)

					buf.WriteByte(0x05) // else (empty channel)

					// optHdr.has_value = 0
					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 0)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					buf.WriteByte(0x0b) // end if

					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					return nil

				case "close":
					chanLocal := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, mem.Target); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, chanLocal)

					// chan.is_closed = 1 (at offset 16)
					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 16)
					return nil
				}
			}

			if _, isTrait := mem.TargetType.(TraitType); isTrait {
				fatLocal := ctx.allocLocal(valTypeI32)
				if err := ctx.compileExpr(buf, mem.Target); err != nil {
					return err
				}
				buf.WriteByte(0x21)
				writeUleb128(buf, fatLocal)

				buf.WriteByte(0x20)
				writeUleb128(buf, fatLocal)
				buf.WriteByte(0x28)
				writeUleb128(buf, 2)
				writeUleb128(buf, 0)

				for k, fnIdx := range ctx.emitter.methodIndices {
					if len(k) > len(mem.Member) && k[len(k)-len(mem.Member):] == mem.Member {
						buf.WriteByte(0x10)
						writeUleb128(buf, fnIdx)
						return nil
					}
				}
			}

			if target, isIdent := mem.Target.(*IdentExpr); isIdent {
				if canonical, isModule := ctx.emitter.moduleAliases[target.Name]; isModule {
					name := canonical + "." + mem.Member
					fnIdx, exists := ctx.emitter.fnIndices[name]
					if !exists {
						return fmt.Errorf("unknown function '%s' in module '%s' (expected '%s')", mem.Member, target.Name, name)
					}
					for _, arg := range e.Args {
						if err := ctx.compileExpr(buf, arg); err != nil {
							return err
						}
					}
					buf.WriteByte(0x10)
					writeUleb128(buf, fnIdx)
					return nil
				}
			}

			if mem.TargetType == nil {
				return fmt.Errorf("cannot resolve method call '%s' on untyped target", mem.Member)
			}
			methodKey := mem.TargetType.String() + "." + mem.Member
			if methodIdx, exists := ctx.emitter.methodIndices[methodKey]; exists {
				if err := ctx.compileExpr(buf, mem.Target); err != nil {
					return err
				}
				if len(mem.EmbedPath) > 0 {
					rootType := ctx.inferExprType(mem.Target)
					off, _, err := ctx.embedReceiverOffset(rootType, mem.EmbedPath)
					if err != nil {
						return err
					}
					if off != 0 {
						buf.WriteByte(0x41)
						writeSleb128(buf, int32(off))
						buf.WriteByte(0x6a)
					}
				}
				for _, arg := range e.Args {
					if err := ctx.compileExpr(buf, arg); err != nil {
						return err
					}
				}
				buf.WriteByte(0x10)
				writeUleb128(buf, methodIdx)
				return nil
			}
			{
				tt := mem.TargetType.String()
				short := tt
				if i := strings.LastIndexByte(tt, '.'); i >= 0 {
					short = tt[i+1:]
				} else if i := strings.LastIndex(tt, "__"); i >= 0 {
					short = tt[i+2:]
				}
				shortKey := short + "." + mem.Member
				if methodIdx, exists := ctx.emitter.methodIndices[shortKey]; exists {
					if err := ctx.compileExpr(buf, mem.Target); err != nil {
						return err
					}
					if len(mem.EmbedPath) > 0 {
						rootType := ctx.inferExprType(mem.Target)
						off, _, err := ctx.embedReceiverOffset(rootType, mem.EmbedPath)
						if err != nil {
							return err
						}
						if off != 0 {
							buf.WriteByte(0x41)
							writeSleb128(buf, int32(off))
							buf.WriteByte(0x6a)
						}
					}
					for _, arg := range e.Args {
						if err := ctx.compileExpr(buf, arg); err != nil {
							return err
						}
					}
					buf.WriteByte(0x10)
					writeUleb128(buf, methodIdx)
					return nil
				}
			}
		}

		name := ""
		if ident, ok := e.Callee.(*IdentExpr); ok {
			name = ident.Name
		}
		if name != "" {
			if name == "print" || name == "println" {
				return ctx.compilePrint(buf, e.Args, name == "println")
			}
			if name == "time_now_ms" {
				buf.WriteByte(0x41)
				writeSleb128(buf, 0)
				buf.WriteByte(0x42)
				writeSleb128(buf, 1000)
				buf.WriteByte(0x41)
				writeSleb128(buf, scratchNwrite)
				buf.WriteByte(0x10)
				writeUleb128(buf, ctx.emitter.clockTimeIdx)
				buf.WriteByte(0x1a)

				buf.WriteByte(0x41)
				writeSleb128(buf, 100)
				return nil
			}

			fnIdx, exists := ctx.emitter.fnIndices[name]
			if !exists {
				return fmt.Errorf("unknown function %s", name)
			}
			for _, arg := range e.Args {
				if err := ctx.compileExpr(buf, arg); err != nil {
					return err
				}
			}
			buf.WriteByte(0x10)
			writeUleb128(buf, fnIdx)
		}
	}
	return nil
}

func (ctx *fnContext) emitAlloc(buf *bytes.Buffer, size int32, resultLocal uint32) {
	// Keep all heap allocations 8-byte aligned. WASI writes u64 fields (for
	// example in filestat) and requires the destination pointer to be aligned.
	buf.WriteByte(0x23)
	writeUleb128(buf, 0)
	buf.WriteByte(0x41)
	writeSleb128(buf, 7)
	buf.WriteByte(0x6a)
	buf.WriteByte(0x41)
	writeSleb128(buf, -8)
	buf.WriteByte(0x71)
	buf.WriteByte(0x21)
	writeUleb128(buf, resultLocal)

	buf.WriteByte(0x20)
	writeUleb128(buf, resultLocal)
	buf.WriteByte(0x41)
	writeSleb128(buf, size)
	buf.WriteByte(0x6a)
	buf.WriteByte(0x24)
	writeUleb128(buf, 0)

	if ctx.emitter.memTrace {
		emitI32Const(buf, size)
		emitI32Const(buf, 0)
		emitCallIdx(buf, ctx.emitter.traceImports["arena_alloc"])
	}
}

// emitAllocDyn is emitAlloc with a runtime size. sizeLocal must hold a
// non-negative i32 byte count; resultLocal receives the 8-byte-aligned
// allocation address. Used by slice growth paths where the required size
// depends on the current length and capacity.
func (ctx *fnContext) emitAllocDyn(buf *bytes.Buffer, sizeLocal, resultLocal uint32) {
	buf.WriteByte(0x23) // global.get __arena_pointer
	writeUleb128(buf, 0)
	buf.WriteByte(0x41) // i32.const 7
	writeSleb128(buf, 7)
	buf.WriteByte(0x6a) // i32.add
	buf.WriteByte(0x41) // i32.const -8
	writeSleb128(buf, -8)
	buf.WriteByte(0x71) // i32.and
	buf.WriteByte(0x21) // local.set resultLocal
	writeUleb128(buf, resultLocal)

	buf.WriteByte(0x20) // local.get resultLocal
	writeUleb128(buf, resultLocal)
	buf.WriteByte(0x20) // local.get sizeLocal
	writeUleb128(buf, sizeLocal)
	buf.WriteByte(0x6a) // i32.add
	buf.WriteByte(0x24) // global.set __arena_pointer
	writeUleb128(buf, 0)

	if ctx.emitter.memTrace {
		buf.WriteByte(0x20)
		writeUleb128(buf, sizeLocal)
		emitI32Const(buf, 1) // 1 = dynamic slice buffer
		emitCallIdx(buf, ctx.emitter.traceImports["arena_alloc"])
	}
}

// emitMemoryCopy emits wasm memory.copy (bulk-memory, 0xfc 0x0a). Stack
// consumption: dst, src, len, in that push order. Wasmtime/Cranelift enable
// bulk-memory by default, so no module feature declaration is required.
func (ctx *fnContext) emitMemoryCopy(buf *bytes.Buffer) {
	buf.WriteByte(0xfc)
	writeUleb128(buf, 0x0a)
	writeUleb128(buf, 0x00) // dst memory index 0
	writeUleb128(buf, 0x00) // src memory index 0
}

func (ctx *fnContext) compilePrint(buf *bytes.Buffer, args []Expr, newline bool) error {
	nlVal := int32(0)
	if newline {
		nlVal = 1
	}

	for i, arg := range args {
		isLast := i == len(args)-1
		currentNL := int32(0)
		if isLast {
			currentNL = nlVal
		}

		argT := ctx.inferExprType(arg)

		if argT == "string" {
			strLocal := ctx.allocLocal(valTypeI32)
			if err := ctx.compileExpr(buf, arg); err != nil {
				return err
			}
			buf.WriteByte(0x21)
			writeUleb128(buf, strLocal)

			buf.WriteByte(0x20)
			writeUleb128(buf, strLocal)
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 0)

			buf.WriteByte(0x20)
			writeUleb128(buf, strLocal)
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 4)

			buf.WriteByte(0x41)
			writeSleb128(buf, currentNL)
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__print_str"])
		} else if argT == "bool" {
			if err := ctx.compileExpr(buf, arg); err != nil {
				return err
			}
			buf.WriteByte(0x41)
			writeSleb128(buf, currentNL)
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__print_bool"])
		} else if argT == "i64" || argT == "u64" {
			if err := ctx.compileExpr(buf, arg); err != nil {
				return err
			}
			buf.WriteByte(0x41)
			writeSleb128(buf, currentNL)
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__print_i64"])
		} else if argT == "f32" {
			if err := ctx.compileExpr(buf, arg); err != nil {
				return err
			}
			buf.WriteByte(0xbb) // f64.promote_f32
			buf.WriteByte(0x41)
			writeSleb128(buf, currentNL)
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__print_f64"])
		} else if argT == "f64" {
			if err := ctx.compileExpr(buf, arg); err != nil {
				return err
			}
			buf.WriteByte(0x41)
			writeSleb128(buf, currentNL)
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__print_f64"])
		} else {
			if err := ctx.compileExpr(buf, arg); err != nil {
				return err
			}
			buf.WriteByte(0x41)
			writeSleb128(buf, currentNL)
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__print_i32"])
		}
	}
	return nil
}

// compileExprArm emits a block whose value is the final ExprStmt. Earlier
// statements are emitted as normal, with any values they leave on the stack
// dropped at the ExprStmt site (see compileStmt's *ExprStmt case).
func (ctx *fnContext) compileExprArm(buf *bytes.Buffer, b *BlockStmt) error {
	if b == nil || len(b.Stmts) == 0 {
		return fmt.Errorf("%s: if-expression branch has no value", b.Loc())
	}
	for i, stmt := range b.Stmts {
		if i == len(b.Stmts)-1 {
			es, ok := stmt.(*ExprStmt)
			if !ok {
				return fmt.Errorf("%s: if-expression branch's last statement must be an expression", stmt.Loc())
			}
			return ctx.compileExpr(buf, es.Expr)
		}
		if err := ctx.compileStmt(buf, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (ctx *fnContext) inferExprType(expr Expr) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *MoveExpr:
		if e.Type != nil {
			return e.Type.String()
		}
		return ctx.inferExprType(e.Value)
	case *CastExpr:
		if e.TargetType != nil {
			return e.TargetType.String()
		}
	case *IndexExpr:
		if e.TargetType != nil {
			if st, ok := e.TargetType.(SliceType); ok && st.Elem != nil {
				return st.Elem.String()
			}
			if at, ok := e.TargetType.(ArrayType); ok && at.Elem != nil {
				return at.Elem.String()
			}
			if _, ok := e.TargetType.(StringType); ok {
				return "u8"
			}
		}
	case *LiteralExpr:
		return e.Type.String()
	case *IdentExpr:
		if t, ok := ctx.localTypes[e.Name]; ok && t != nil {
			return t.String()
		}
	case *InterpolatedStringExpr:
		return "string"
	case *StructInitExpr:
		return e.Name
	case *MemberExpr:
		if e.TargetType != nil {
			if meta, ok := ctx.emitter.structs[e.TargetType.String()]; ok {
				if ft, ok := meta.fieldTypes[memberFieldKey(e)]; ok && ft != nil {
					return ft.String()
				}
			}
		}
	case *SliceSubExpr:
		if e.TargetType != nil {
			switch e.TargetType.(type) {
			case StringType:
				return "string"
			case SliceType:
				return e.TargetType.String()
			case ArrayType:
				// [N]T coerces to []T; the caller wraps in ArrayToSliceExpr,
				// so a bare ArrayType target here still means the result is
				// a slice of the same element.
				if at, ok := e.TargetType.(ArrayType); ok && at.Elem != nil {
					return SliceType{Elem: at.Elem}.String()
				}
			}
		}
	case *UnaryExpr:
		if t := ctx.inferExprType(e.Operand); t != "" {
			return t
		}
	case *BinaryExpr:
		if e.Type != nil {
			return e.Type.String()
		}
		if lt := ctx.inferExprType(e.Left); lt != "" {
			return lt
		}
		if rt := ctx.inferExprType(e.Right); rt != "" {
			return rt
		}
	case *IfExpr:
		if e.Type != nil {
			return e.Type.String()
		}
	case *ArenaExpr:
		if e.Type != nil {
			return e.Type.String()
		}
	case *CallExpr:
		if t := ctx.callReturnUbeType(e); t != nil {
			return t.String()
		}
	case *TryExpr:
		if call, ok := e.Operand.(*CallExpr); ok {
			if t := ctx.callReturnUbeType(call); t != nil {
				if res, ok := t.(ResultType); ok {
					return res.Ok.String()
				}
			}
		}
		if id, ok := e.Operand.(*IdentExpr); ok {
			if t, ok := ctx.localTypes[id.Name]; ok && t != nil {
				if res, ok := t.(ResultType); ok {
					return res.Ok.String()
				}
			}
		}
	case *OrElseExpr:
		if e.PrimaryType != nil {
			switch pt := e.PrimaryType.(type) {
			case ResultType:
				return pt.Ok.String()
			case OptionalType:
				return pt.Inner.String()
			}
		}
	}
	return ""
}
