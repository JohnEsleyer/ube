// File: pkg/compiler/wasm_stmt.go
package compiler

import (
	"bytes"
	"fmt"
)

func (ctx *fnContext) compileStmt(buf *bytes.Buffer, stmt Stmt) error {
	switch s := stmt.(type) {
	case *BlockStmt:
		return ctx.compileBlock(buf, s)
	case *LetStmt:
		if len(s.Names) > 0 {
			tupLocal := ctx.allocLocal(valTypeI32)
			if err := ctx.compileExpr(buf, s.Value); err != nil {
				return err
			}
			buf.WriteByte(0x21)
			writeUleb128(buf, tupLocal)

			var elemTypes []UbeType
			var elemOffsets []int
			if tt, ok := s.ValueType.(TupleType); ok {
				offset := 0
				for _, el := range tt.Elems {
					elemTypes = append(elemTypes, el)
					sz := 4
					if el != nil {
						if el.ByteSize() > 4 {
							sz = el.ByteSize()
							offset = alignUpInt(offset, el.Align())
						}
					}
					elemOffsets = append(elemOffsets, offset)
					offset += sz
				}
			} else if ct, ok := s.ValueType.(CustomType); ok {
				if meta, ok := ctx.emitter.structs[ct.Name]; ok {
					fields := ctx.emitter.structFields[ct.Name]
					for _, f := range fields {
						elemTypes = append(elemTypes, f.Type)
						elemOffsets = append(elemOffsets, meta.fieldOffsets[f.Name])
					}
				}
			}

			for i, name := range s.Names {
				var elemT UbeType
				offset := i * 4
				if i < len(elemTypes) {
					elemT = elemTypes[i]
				}
				if i < len(elemOffsets) {
					offset = elemOffsets[i]
				}
				wasmLocalType := wasmTypeOrDefault(elemT)
				loadOp, _, align := fieldLoadStoreInfo(elemT)

				varLocal := ctx.allocLocal(wasmLocalType)
				ctx.locals[name] = varLocal
				if elemT != nil {
					ctx.localTypes[name] = elemT
				}

				buf.WriteByte(0x20) // local.get tupLocal
				writeUleb128(buf, tupLocal)
				buf.WriteByte(loadOp)
				writeUleb128(buf, align)
				writeUleb128(buf, uint32(offset))
				buf.WriteByte(0x21) // local.set varLocal
				writeUleb128(buf, varLocal)
			}
			return nil
		}

		var t UbeType
		if s.Type != nil {
			t = s.Type
		} else if s.ValueType != nil {
			t = s.ValueType
		} else if s.Value != nil {
			if inferred := ctx.inferExprType(s.Value); inferred != "" {
				t = typeFromString(inferred)
			}
		}
		localType := wasmTypeOrDefault(t)
		valIdx := ctx.allocLocal(localType)
		ctx.locals[s.Name] = valIdx
		if t != nil {
			ctx.localTypes[s.Name] = t
		} else if s.Type != nil {
			ctx.localTypes[s.Name] = s.Type
		} else if s.ValueType != nil {
			ctx.localTypes[s.Name] = s.ValueType
		}
		if s.Value != nil {
			if err := ctx.compileExpr(buf, s.Value); err != nil {
				return err
			}
			buf.WriteByte(0x21)
			writeUleb128(buf, valIdx)
		}

	case *AssignStmt:
		if ident, ok := s.LHS.(*IdentExpr); ok {
			localIdx, exists := ctx.locals[ident.Name]
			if !exists {
				return fmt.Errorf("unknown variable %s in assignment", ident.Name)
			}
			if s.Op != "=" {
				buf.WriteByte(0x20)
				writeUleb128(buf, localIdx)
				if err := ctx.compileExpr(buf, s.RHS); err != nil {
					return err
				}
				opType := ctx.inferExprType(s.LHS)
				if opType == "" {
					opType = ctx.inferExprType(s.RHS)
				}
				if err := emitCompoundOp(buf, s.Op, opType); err != nil {
					return err
				}
			} else {
				if err := ctx.compileExpr(buf, s.RHS); err != nil {
					return err
				}
			}
			buf.WriteByte(0x21)
			writeUleb128(buf, localIdx)
		} else if mem, ok := s.LHS.(*MemberExpr); ok {
			structName := mem.TargetType.String()
			meta, ok := ctx.emitter.structs[structName]
			if !ok {
				return fmt.Errorf("unknown struct type %s", structName)
			}
			fieldKey := memberFieldKey(mem)
			offset, ok := meta.fieldOffsets[fieldKey]
			if !ok {
				return fmt.Errorf("unknown field %s on struct %s", fieldKey, structName)
			}
			fieldType := meta.fieldTypes[fieldKey]
			loadOp, storeOp, align := fieldLoadStoreInfo(fieldType)

			if err := ctx.compileExpr(buf, mem.Target); err != nil {
				return err
			}
			if s.Op != "=" {
				targetLocal := ctx.allocLocal(valTypeI32)
				buf.WriteByte(0x22)
				writeUleb128(buf, targetLocal)
				buf.WriteByte(0x20)
				writeUleb128(buf, targetLocal)
				buf.WriteByte(loadOp)
				writeUleb128(buf, align)
				writeUleb128(buf, uint32(offset))

				if err := ctx.compileExpr(buf, s.RHS); err != nil {
					return err
				}
				opType := ctx.inferExprType(s.LHS)
				if opType == "" {
					opType = ctx.inferExprType(s.RHS)
				}
				if err := emitCompoundOp(buf, s.Op, opType); err != nil {
					return err
				}
			} else if ctx.isInlineAggregate(fieldType) {
				// mem.copy(base+offset, src_ptr, sizeofInline).
				// base was already pushed by the outer compileExpr(mem.Target);
				// move it into a local so offset can be added before RHS runs.
				baseLocal := ctx.allocLocal(valTypeI32)
				buf.WriteByte(0x21)
				writeUleb128(buf, baseLocal)
				buf.WriteByte(0x20)
				writeUleb128(buf, baseLocal)
				if offset != 0 {
					buf.WriteByte(0x41)
					writeSleb128(buf, int32(offset))
					buf.WriteByte(0x6a)
				}
				if err := ctx.compileExpr(buf, s.RHS); err != nil {
					return err
				}
				buf.WriteByte(0x41)
				writeSleb128(buf, int32(ctx.emitter.sizeofInline(fieldType)))
				ctx.emitMemoryCopy(buf)
				return nil
			} else {
				if err := ctx.compileExpr(buf, s.RHS); err != nil {
					return err
				}
			}
			buf.WriteByte(storeOp)
			writeUleb128(buf, align)
			writeUleb128(buf, uint32(offset))
		} else if idx, ok := s.LHS.(*IndexExpr); ok {
			addrLocal := ctx.allocLocal(valTypeI32)
			if err := ctx.compileExpr(buf, idx.Target); err != nil {
				return err
			}
			width := int32(4)
			storeOp := byte(0x36)
			align := uint32(2)
			if st, isSlice := idx.TargetType.(SliceType); isSlice {
				buf.WriteByte(0x28)
				writeUleb128(buf, 2)
				writeUleb128(buf, 0)
				width, _, storeOp, align = sliceElemInfo(st.Elem)
			} else if at, isArr := idx.TargetType.(ArrayType); isArr {
				width, _, storeOp, align = sliceElemInfo(at.Elem)
			}
			if err := ctx.compileExpr(buf, idx.Index); err != nil {
				return err
			}
			if width != 1 {
				buf.WriteByte(0x41)
				writeSleb128(buf, width)
				buf.WriteByte(0x6c)
			}
			buf.WriteByte(0x6a)
			buf.WriteByte(0x21)
			writeUleb128(buf, addrLocal)

			buf.WriteByte(0x20)
			writeUleb128(buf, addrLocal)
			if err := ctx.compileExpr(buf, s.RHS); err != nil {
				return err
			}
			buf.WriteByte(storeOp)
			writeUleb128(buf, align)
			writeUleb128(buf, 0)
		}

	case *ExprStmt:
		if err := ctx.compileExpr(buf, s.Expr); err != nil {
			return err
		}
		if ctx.exprLeavesValue(s.Expr) {
			buf.WriteByte(0x1a)
		}

	case *ReturnStmt:
		// Evaluate the return expression FIRST, while the state it reads
		// (open file descriptor, live arena slice, etc.) is still valid.
		// Then run the deferred calls, then emit return. Defers must run
		// at true scope exit, AFTER the return value is computed — not
		// before, as this case previously did. The prior order caused
		// `defer file.close(); return file.read_to_string()` to close
		// the file before the read, so read_to_string always returned
		// "attempt to read from closed file".
		if s.Value != nil {
			wrap := false
			switch ctx.fn.RetType.(type) {
			case ResultType, OptionalType:
				wrap = true
			}
			if _, isErr := s.Value.(*ErrorExpr); isErr {
				wrap = false
			}
			if lit, isLit := s.Value.(*LiteralExpr); isLit && lit.Val == "none" {
				wrap = false
			}
			switch s.ValueType.(type) {
			case ResultType, OptionalType:
				wrap = false
			}
			// Store the return value in a local before running defers. Defers
			// may leave their own values on the operand stack, so keeping the
			// result there would corrupt the function's return value.
			retValWasm := wasmTypeOrDefault(ctx.fn.RetType)
			retLocal := ctx.allocLocal(retValWasm)
			if wrap {
				if err := ctx.compileWrappedSuccess(buf, s.Value); err != nil {
					return err
				}
			} else if err := ctx.compileExpr(buf, s.Value); err != nil {
				return err
			}
			buf.WriteByte(0x21)
			writeUleb128(buf, retLocal)

			ctx.emitAllDefers(buf)

			buf.WriteByte(0x20)
			writeUleb128(buf, retLocal)
		} else {
			ctx.emitAllDefers(buf)
		}
		buf.WriteByte(0x0f)

	case *DeferStmt:
		ctx.addDefer(s.Call)

	case *IfStmt:
		if err := ctx.compileExpr(buf, s.Cond); err != nil {
			return err
		}
		ctx.openLabel(buf, 0x04)
		buf.WriteByte(valTypeVoid)
		if err := ctx.compileBlock(buf, s.Then); err != nil {
			return err
		}
		if s.Else != nil {
			buf.WriteByte(0x05)
			if elseBlock, ok := s.Else.(*BlockStmt); ok {
				if err := ctx.compileBlock(buf, elseBlock); err != nil {
					return err
				}
			} else if elseIf, ok := s.Else.(*IfStmt); ok {
				if err := ctx.compileStmt(buf, elseIf); err != nil {
					return err
				}
			}
		}
		ctx.closeLabel(buf)

	case *WhileStmt:
		ctx.openLabel(buf, 0x02)
		buf.WriteByte(valTypeVoid)
		ctx.openLabel(buf, 0x03)
		buf.WriteByte(valTypeVoid)
		ctx.loopBodyDepths = append(ctx.loopBodyDepths, ctx.labelDepth)
		ctx.loopDeferDepths = append(ctx.loopDeferDepths, len(ctx.deferScopes))

		if err := ctx.compileExpr(buf, s.Cond); err != nil {
			return err
		}
		buf.WriteByte(0x45)
		buf.WriteByte(0x0d)
		writeUleb128(buf, 1)

		if err := ctx.compileBlock(buf, s.Body); err != nil {
			return err
		}

		ctx.loopBodyDepths = ctx.loopBodyDepths[:len(ctx.loopBodyDepths)-1]
		ctx.loopDeferDepths = ctx.loopDeferDepths[:len(ctx.loopDeferDepths)-1]

		buf.WriteByte(0x0c)
		writeUleb128(buf, 0)

		ctx.closeLabel(buf)
		ctx.closeLabel(buf)

	case *ArenaStmt:
		savedLocal := ctx.allocLocal(valTypeI32)
		buf.WriteByte(0x23)
		writeUleb128(buf, 0)
		buf.WriteByte(0x21)
		writeUleb128(buf, savedLocal)

		if err := ctx.compileBlock(buf, s.Body); err != nil {
			return err
		}

		buf.WriteByte(0x20)
		writeUleb128(buf, savedLocal)
		buf.WriteByte(0x24)
		writeUleb128(buf, 0)

	case *ForStmt:
		if rng, ok := s.Iter.(*RangeExpr); ok {
			itemLocal := ctx.allocLocal(valTypeI32)
			ctx.locals[s.ItemName] = itemLocal
			ctx.localTypes[s.ItemName] = s.ItemType
			endLocal := ctx.allocLocal(valTypeI32)

			if err := ctx.compileExpr(buf, rng.Start); err != nil {
				return err
			}
			buf.WriteByte(0x21)
			writeUleb128(buf, itemLocal)

			if err := ctx.compileExpr(buf, rng.End); err != nil {
				return err
			}
			buf.WriteByte(0x21)
			writeUleb128(buf, endLocal)

			ctx.openLabel(buf, 0x02)
			buf.WriteByte(valTypeVoid)
			ctx.openLabel(buf, 0x03)
			buf.WriteByte(valTypeVoid)
			ctx.loopBodyDepths = append(ctx.loopBodyDepths, ctx.labelDepth)
			ctx.loopDeferDepths = append(ctx.loopDeferDepths, len(ctx.deferScopes))

			buf.WriteByte(0x20)
			writeUleb128(buf, itemLocal)
			buf.WriteByte(0x20)
			writeUleb128(buf, endLocal)
			buf.WriteByte(0x4e)
			buf.WriteByte(0x0d)
			writeUleb128(buf, 1)

			if err := ctx.compileBlock(buf, s.Body); err != nil {
				return err
			}

			ctx.loopBodyDepths = ctx.loopBodyDepths[:len(ctx.loopBodyDepths)-1]
			ctx.loopDeferDepths = ctx.loopDeferDepths[:len(ctx.loopDeferDepths)-1]

			buf.WriteByte(0x20)
			writeUleb128(buf, itemLocal)
			buf.WriteByte(0x41)
			writeSleb128(buf, 1)
			buf.WriteByte(0x6a)
			buf.WriteByte(0x21)
			writeUleb128(buf, itemLocal)

			buf.WriteByte(0x0c)
			writeUleb128(buf, 0)

			ctx.closeLabel(buf)
			ctx.closeLabel(buf)
		} else {
			targetLocal := ctx.allocLocal(valTypeI32)
			if err := ctx.compileExpr(buf, s.Iter); err != nil {
				return err
			}
			buf.WriteByte(0x21)
			writeUleb128(buf, targetLocal)

			lenLocal := ctx.allocLocal(valTypeI32)
			dataLocal := ctx.allocLocal(valTypeI32)

			if arr, ok := s.IterType.(ArrayType); ok {
				buf.WriteByte(0x41)
				writeSleb128(buf, int32(arr.Size))
				buf.WriteByte(0x21)
				writeUleb128(buf, lenLocal)

				buf.WriteByte(0x20)
				writeUleb128(buf, targetLocal)
				buf.WriteByte(0x21)
				writeUleb128(buf, dataLocal)
			} else {
				buf.WriteByte(0x20)
				writeUleb128(buf, targetLocal)
				buf.WriteByte(0x28)
				writeUleb128(buf, 2)
				writeUleb128(buf, 4)
				buf.WriteByte(0x21)
				writeUleb128(buf, lenLocal)

				buf.WriteByte(0x20)
				writeUleb128(buf, targetLocal)
				buf.WriteByte(0x28)
				writeUleb128(buf, 2)
				writeUleb128(buf, 0)
				buf.WriteByte(0x21)
				writeUleb128(buf, dataLocal)
			}

			idxLocal := ctx.allocLocal(valTypeI32)
			buf.WriteByte(0x41)
			writeSleb128(buf, 0)
			buf.WriteByte(0x21)
			writeUleb128(buf, idxLocal)

			// Element width and load opcode depend on the slice/array
			// element type. []u8 (and string) iterate byte-wise, while
			// []i64/f64 need 8-byte loads.
			elemWidth := int32(4)
			loadOp := byte(0x28) // i32.load
			loadAlign := uint32(2)
			itemWasm := byte(valTypeI32)
			if s.ItemType != nil {
				switch s.ItemType.String() {
				case "u8", "i8", "bool":
					elemWidth = 1
					loadOp = 0x2d // i32.load8_u
					loadAlign = 0
				case "i64", "u64":
					elemWidth = 8
					loadOp = 0x29 // i64.load
					loadAlign = 3
					itemWasm = valTypeI64
				case "f64":
					elemWidth = 8
					loadOp = 0x2b // f64.load
					loadAlign = 3
					itemWasm = valTypeF64
				case "f32":
					loadOp = 0x2a // f32.load
					itemWasm = valTypeF32
				}
			}
			itemLocal := ctx.allocLocal(itemWasm)
			ctx.locals[s.ItemName] = itemLocal
			ctx.localTypes[s.ItemName] = s.ItemType

			ctx.openLabel(buf, 0x02)
			buf.WriteByte(valTypeVoid)
			ctx.openLabel(buf, 0x03)
			buf.WriteByte(valTypeVoid)
			ctx.loopBodyDepths = append(ctx.loopBodyDepths, ctx.labelDepth)
			ctx.loopDeferDepths = append(ctx.loopDeferDepths, len(ctx.deferScopes))

			buf.WriteByte(0x20)
			writeUleb128(buf, idxLocal)
			buf.WriteByte(0x20)
			writeUleb128(buf, lenLocal)
			buf.WriteByte(0x49)
			buf.WriteByte(0x45)
			buf.WriteByte(0x0d)
			writeUleb128(buf, 1)

			buf.WriteByte(0x20)
			writeUleb128(buf, dataLocal)
			buf.WriteByte(0x20)
			writeUleb128(buf, idxLocal)
			buf.WriteByte(0x41)
			writeSleb128(buf, elemWidth)
			buf.WriteByte(0x6c)
			buf.WriteByte(0x6a)
			buf.WriteByte(loadOp)
			writeUleb128(buf, loadAlign)
			writeUleb128(buf, 0)
			buf.WriteByte(0x21)
			writeUleb128(buf, itemLocal)

			if err := ctx.compileBlock(buf, s.Body); err != nil {
				return err
			}

			ctx.loopBodyDepths = ctx.loopBodyDepths[:len(ctx.loopBodyDepths)-1]
			ctx.loopDeferDepths = ctx.loopDeferDepths[:len(ctx.loopDeferDepths)-1]

			buf.WriteByte(0x20)
			writeUleb128(buf, idxLocal)
			buf.WriteByte(0x41)
			writeSleb128(buf, 1)
			buf.WriteByte(0x6a)
			buf.WriteByte(0x21)
			writeUleb128(buf, idxLocal)

			buf.WriteByte(0x0c)
			writeUleb128(buf, 0)

			ctx.closeLabel(buf)
			ctx.closeLabel(buf)
		}

	case *IfLetStmt:
		optLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, s.Expr); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, optLocal)

		buf.WriteByte(0x20)
		writeUleb128(buf, optLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

		ctx.openLabel(buf, 0x04)
		buf.WriteByte(valTypeVoid)

		bindingType := s.VarType
		if bindingType == nil {
			bindingType = unwrapBindingType(ctx.exprStaticType(s.Expr))
		}
		varLocalWasm := byte(valTypeI32)
		if bindingType != nil {
			switch bindingType.String() {
			case "f32":
				varLocalWasm = valTypeF32
			case "f64":
				varLocalWasm = valTypeF64
			case "i64":
				varLocalWasm = valTypeI64
			}
		}
		varLocal := ctx.allocLocal(varLocalWasm)
		ctx.locals[s.VarName] = varLocal
		ctx.localTypes[s.VarName] = bindingType
		buf.WriteByte(0x20)
		writeUleb128(buf, optLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)
		switch varLocalWasm {
		case valTypeF32:
			buf.WriteByte(0xbe) // f32.reinterpret_i32
		case valTypeF64:
			buf.WriteByte(0xad) // i64.extend_i32_u
			buf.WriteByte(0xbf) // f64.reinterpret_i64
		case valTypeI64:
			buf.WriteByte(0xad) // i64.extend_i32_u
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, varLocal)

		if err := ctx.compileBlock(buf, s.Then); err != nil {
			return err
		}

		if s.Else != nil {
			buf.WriteByte(0x05)
			if elseBlock, ok := s.Else.(*BlockStmt); ok {
				if err := ctx.compileBlock(buf, elseBlock); err != nil {
					return err
				}
			} else if elseIf, ok := s.Else.(*IfStmt); ok {
				if err := ctx.compileStmt(buf, elseIf); err != nil {
					return err
				}
			}
		}
		ctx.closeLabel(buf)

	case *WhileLetStmt:
		ctx.openLabel(buf, 0x02)
		buf.WriteByte(valTypeVoid)
		ctx.openLabel(buf, 0x03)
		buf.WriteByte(valTypeVoid)
		ctx.loopBodyDepths = append(ctx.loopBodyDepths, ctx.labelDepth)
		ctx.loopDeferDepths = append(ctx.loopDeferDepths, len(ctx.deferScopes))

		optLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, s.Call); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, optLocal)

		buf.WriteByte(0x20)
		writeUleb128(buf, optLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)
		buf.WriteByte(0x45)
		buf.WriteByte(0x0d)
		writeUleb128(buf, 1)

		bindingType := s.VarType
		if bindingType == nil {
			bindingType = unwrapBindingType(ctx.exprStaticType(s.Call))
		}
		varLocalWasm := byte(valTypeI32)
		if bindingType != nil {
			switch bindingType.String() {
			case "f32":
				varLocalWasm = valTypeF32
			case "f64":
				varLocalWasm = valTypeF64
			case "i64":
				varLocalWasm = valTypeI64
			}
		}
		varLocal := ctx.allocLocal(varLocalWasm)
		ctx.locals[s.VarName] = varLocal
		ctx.localTypes[s.VarName] = bindingType
		buf.WriteByte(0x20)
		writeUleb128(buf, optLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)
		switch varLocalWasm {
		case valTypeF32:
			buf.WriteByte(0xbe) // f32.reinterpret_i32
		case valTypeF64:
			buf.WriteByte(0xad) // i64.extend_i32_u
			buf.WriteByte(0xbf) // f64.reinterpret_i64
		case valTypeI64:
			buf.WriteByte(0xad) // i64.extend_i32_u
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, varLocal)

		if err := ctx.compileBlock(buf, s.Body); err != nil {
			return err
		}

		ctx.loopBodyDepths = ctx.loopBodyDepths[:len(ctx.loopBodyDepths)-1]
		ctx.loopDeferDepths = ctx.loopDeferDepths[:len(ctx.loopDeferDepths)-1]

		buf.WriteByte(0x0c)
		writeUleb128(buf, 0)

		ctx.closeLabel(buf)
		ctx.closeLabel(buf)

	case *MatchStmt:
		targetLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, s.Expr); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, targetLocal)

		tagLocal := ctx.allocLocal(valTypeI32)
		buf.WriteByte(0x20)
		writeUleb128(buf, targetLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)
		buf.WriteByte(0x21)
		writeUleb128(buf, tagLocal)

		ctx.openLabel(buf, 0x02)
		buf.WriteByte(valTypeVoid)

		enumVariants := ctx.emitter.enums[s.EnumName]
		for _, arm := range s.Arms {
			ctx.openLabel(buf, 0x02)
			buf.WriteByte(valTypeVoid)

			if arm.Variant != "_" {
				tagVal := -1
				for idx, v := range enumVariants {
					if v.Name == arm.Variant {
						tagVal = idx
						break
					}
				}
				buf.WriteByte(0x20)
				writeUleb128(buf, tagLocal)
				buf.WriteByte(0x41)
				writeSleb128(buf, int32(tagVal))
				buf.WriteByte(0x47)
				buf.WriteByte(0x0d)
				writeUleb128(buf, 0)
			}

			for bIdx, bName := range arm.Bindings {
				var bindingType UbeType
				if variants, ok := ctx.emitter.enums[s.EnumName]; ok {
					for _, v := range variants {
						if v.Name == arm.Variant && bIdx < len(v.Types) {
							bindingType = v.Types[bIdx]
							break
						}
					}
				}
				bindingWasm := byte(valTypeI32)
				loadOp := byte(0x28)
				loadAlign := uint32(2)
				if bindingType != nil {
					switch bindingType.String() {
					case "f32":
						bindingWasm = valTypeF32
						loadOp = 0x2a
					case "f64":
						bindingWasm = valTypeF64
						loadOp = 0x2b
						loadAlign = 3
					case "i64":
						bindingWasm = valTypeI64
						loadOp = 0x29
						loadAlign = 3
					}
				}
				bLocal := ctx.allocLocal(bindingWasm)
				ctx.locals[bName] = bLocal
				ctx.localTypes[bName] = bindingType
				buf.WriteByte(0x20)
				writeUleb128(buf, targetLocal)
				buf.WriteByte(loadOp)
				writeUleb128(buf, loadAlign)
				writeUleb128(buf, uint32(4+bIdx*4))
				buf.WriteByte(0x21)
				writeUleb128(buf, bLocal)
			}

			if blk, ok := arm.Body.(*BlockStmt); ok {
				if err := ctx.compileBlock(buf, blk); err != nil {
					return err
				}
			} else if st := arm.Body; st != nil {
				if err := ctx.compileStmt(buf, st); err != nil {
					return err
				}
			}

			buf.WriteByte(0x0c)
			writeUleb128(buf, 1)

			ctx.closeLabel(buf)
		}

		ctx.closeLabel(buf)

	case *SpawnStmt:
		if err := ctx.compileExpr(buf, s.Call); err != nil {
			return err
		}
		if ctx.exprLeavesValue(s.Call) {
			buf.WriteByte(0x1a)
		}

	case *SelectStmt:
		buf.WriteByte(0x02)
		buf.WriteByte(valTypeVoid)

		for _, sc := range s.Cases {
			buf.WriteByte(0x02)
			buf.WriteByte(valTypeVoid)

			chanLocal := ctx.allocLocal(valTypeI32)
			if err := ctx.compileExpr(buf, sc.Chan); err != nil {
				return err
			}
			buf.WriteByte(0x21)
			writeUleb128(buf, chanLocal)

			buf.WriteByte(0x20)
			writeUleb128(buf, chanLocal)
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 12)
			buf.WriteByte(0x45)
			buf.WriteByte(0x0d)
			writeUleb128(buf, 0)

			varLocal := ctx.allocLocal(valTypeI32)
			ctx.locals[sc.VarName] = varLocal
			ctx.localTypes[sc.VarName] = sc.ChanElem

			buf.WriteByte(0x20)
			writeUleb128(buf, chanLocal)
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 4)

			buf.WriteByte(0x20)
			writeUleb128(buf, chanLocal)
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 20)

			buf.WriteByte(0x6a)
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 0)
			buf.WriteByte(0x21)
			writeUleb128(buf, varLocal)

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

			if err := ctx.compileBlock(buf, sc.Body); err != nil {
				return err
			}

			buf.WriteByte(0x0c)
			writeUleb128(buf, 1)

			buf.WriteByte(0x0b)
		}

		if s.Default != nil {
			if err := ctx.compileBlock(buf, s.Default); err != nil {
				return err
			}
		}

		buf.WriteByte(0x0b)

	case *LoopControlStmt:
		if len(ctx.loopBodyDepths) == 0 {
			return fmt.Errorf("%s: %s outside of any loop", s.Loc(), loopControlName(s.Continue))
		}
		if len(ctx.loopDeferDepths) > 0 {
			ctx.emitLoopDefers(buf, ctx.loopDeferDepths[len(ctx.loopDeferDepths)-1])
		}
		depth := ctx.labelDepth - ctx.loopBodyDepths[len(ctx.loopBodyDepths)-1]
		buf.WriteByte(0x0c)
		if s.Continue {
			writeUleb128(buf, uint32(depth))
		} else {
			writeUleb128(buf, uint32(depth+1))
		}
	}
	return nil
}

func loopControlName(cont bool) string {
	if cont {
		return "continue"
	}
	return "break"
}

// emitCompoundOp emits the wasm arithmetic opcode for a compound assignment
// operator, chosen by both the operator and the operand type. Wasm numeric
// opcodes are type-specific: i32.add = 0x6a, i64.add = 0x7c, f32.add = 0x92,
// f64.add = 0xa0. The same offset pattern holds for sub/mul/div/rem.
func emitCompoundOp(buf *bytes.Buffer, op string, operandType string) error {
	// Base offsets within each family: add=0x00, sub=0x01, mul=0x02,
	// div=0x03, rem=0x05. The family base is the opcode for that family's
	// add (i32=0x6a, i64=0x7c, f32=0x92, f64=0xa0).
	var familyBase byte
	switch operandType {
	case "f32":
		familyBase = 0x92
	case "f64":
		familyBase = 0xa0
	case "i64":
		familyBase = 0x7c
	default:
		familyBase = 0x6a
	}
	// f32/f64 have no integer remainder; i32/i64 have no float remainder
	// either, but the numeric family layout is: add, sub, mul, div, rem
	// where each is one byte apart. For f32/f64 the gap is: add, sub, mul,
	// div, min, max, copysign — so we cannot use base+5 for rem. The demo
	// does not use %= on floats; reject that combination explicitly rather
	// than emitting the wrong opcode.
	isFloat := operandType == "f32" || operandType == "f64"
	switch op {
	case "+=":
		buf.WriteByte(familyBase)
	case "-=":
		buf.WriteByte(familyBase + 0x01)
	case "*=":
		buf.WriteByte(familyBase + 0x02)
	case "/=":
		buf.WriteByte(familyBase + 0x03)
	case "%=":
		if isFloat {
			return fmt.Errorf("'%%=' is not supported on '%s'", operandType)
		}
		buf.WriteByte(familyBase + 0x05)
	case "&=":
		buf.WriteByte(0x71)
	case "|=":
		buf.WriteByte(0x72)
	case "^=":
		buf.WriteByte(0x73)
	case "<<=":
		buf.WriteByte(0x74)
	case ">>=":
		buf.WriteByte(0x75)
	default:
		return fmt.Errorf("unsupported assignment operator: %s", op)
	}
	return nil
}

