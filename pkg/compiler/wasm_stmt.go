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
			ctx.ir.SetLocal(tupLocal)
			ctx.ir.FlushToBuffer(buf)

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

				ctx.ir.GetLocal(tupLocal)
				ctx.ir.FlushToBuffer(buf)
				buf.WriteByte(loadOp)
				writeUleb128(buf, align)
				writeUleb128(buf, uint32(offset))
				ctx.ir.SetLocal(varLocal)
				ctx.ir.FlushToBuffer(buf)
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
			ctx.ir.SetLocal(valIdx)
			ctx.ir.FlushToBuffer(buf)
		}

	case *AssignStmt:
		if ident, ok := s.LHS.(*IdentExpr); ok {
			localIdx, exists := ctx.locals[ident.Name]
			if !exists {
				return fmt.Errorf("unknown variable %s in assignment", ident.Name)
			}
			if s.Op != "=" {
				ctx.ir.GetLocal(localIdx)
				ctx.ir.FlushToBuffer(buf)
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
			ctx.ir.SetLocal(localIdx)
			ctx.ir.FlushToBuffer(buf)
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
				ctx.ir.TeeLocal(targetLocal)
				ctx.ir.FlushToBuffer(buf)
				ctx.ir.GetLocal(targetLocal)
				ctx.ir.FlushToBuffer(buf)
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
				ctx.ir.SetLocal(baseLocal)
				ctx.ir.FlushToBuffer(buf)
				ctx.ir.GetLocal(baseLocal)
				ctx.ir.FlushToBuffer(buf)
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
			ctx.ir.SetLocal(addrLocal)
			ctx.ir.FlushToBuffer(buf)

			ctx.ir.GetLocal(addrLocal)
			ctx.ir.FlushToBuffer(buf)
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
			ctx.emitDrop()
			ctx.ir.FlushToBuffer(buf)
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
			ctx.emitSetLocal(retLocal)
			ctx.ir.FlushToBuffer(buf)

			ctx.emitAllDefers(buf)

			ctx.emitGetLocal(retLocal)
			ctx.ir.FlushToBuffer(buf)
		} else {
			ctx.emitAllDefers(buf)
		}
		ctx.emitReturn()
		ctx.ir.FlushToBuffer(buf)

	case *DeferStmt:
		ctx.addDefer(s.Call)

	case *IfStmt:
		if err := ctx.compileExpr(buf, s.Cond); err != nil {
			return err
		}
		ctx.openIRLabel(buf, 0x04)
		if err := ctx.compileBlock(buf, s.Then); err != nil {
			return err
		}
		if s.Else != nil {
			ctx.ir.Else()
			ctx.ir.FlushToBuffer(buf)
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
		ctx.closeIRLabel(buf)

	case *WhileStmt:
		var loopID int32
		prevScope := ctx.currentScopeID
		if ctx.emitter.memTrace {
			loopID = ctx.emitter.allocScopeID("while loop", s.At.File, s.At.Line, 2, prevScope)
			emitI32Const(buf, loopID)
			emitCallIdx(buf, ctx.emitter.traceImports["loop_enter"])
		}

		var savedArenaLocal uint32
		if s.IsArena {
			savedArenaLocal = ctx.allocLocal(valTypeI32)
		}
		ctx.loopArenaLocals = append(ctx.loopArenaLocals, savedArenaLocal)

		ctx.openIRLabel(buf, 0x02)
		breakDepth := ctx.labelDepth
		ctx.openIRLabel(buf, 0x03)
		continueDepth := ctx.labelDepth
		ctx.loopBreakDepths = append(ctx.loopBreakDepths, breakDepth)
		ctx.loopContinueDepths = append(ctx.loopContinueDepths, continueDepth)
		ctx.loopDeferDepths = append(ctx.loopDeferDepths, len(ctx.deferScopes))

		if s.IsArena {
			ctx.emitGetGlobal(0)
			ctx.ir.FlushToBuffer(buf)
			ctx.emitSetLocal(savedArenaLocal)
			ctx.ir.FlushToBuffer(buf)
		}

		if err := ctx.compileExpr(buf, s.Cond); err != nil {
			return err
		}
		ctx.emitI32Eqz()
		ctx.emitBrIf(uint32(ctx.labelDepth - breakDepth))
		ctx.ir.FlushToBuffer(buf)

		if ctx.emitter.memTrace {
			emitI32Const(buf, loopID)
			emitCallIdx(buf, ctx.emitter.traceImports["loop_iter"])
			ctx.currentScopeID = loopID
		}

		if err := ctx.compileBlock(buf, s.Body); err != nil {
			return err
		}

		if s.IsArena {
			ctx.emitGetLocal(savedArenaLocal)
			ctx.ir.FlushToBuffer(buf)
			ctx.emitSetGlobal(0)
			ctx.ir.FlushToBuffer(buf)
		}

		if ctx.emitter.memTrace {
			ctx.currentScopeID = prevScope
		}

		ctx.loopBreakDepths = ctx.loopBreakDepths[:len(ctx.loopBreakDepths)-1]
		ctx.loopContinueDepths = ctx.loopContinueDepths[:len(ctx.loopContinueDepths)-1]
		ctx.loopDeferDepths = ctx.loopDeferDepths[:len(ctx.loopDeferDepths)-1]
		ctx.loopArenaLocals = ctx.loopArenaLocals[:len(ctx.loopArenaLocals)-1]

		ctx.emitBr(uint32(ctx.labelDepth - continueDepth))
		ctx.ir.FlushToBuffer(buf)

		ctx.closeIRLabel(buf)
		ctx.closeIRLabel(buf)

		if s.IsArena {
			ctx.emitGetLocal(savedArenaLocal)
			ctx.ir.FlushToBuffer(buf)
			ctx.emitSetGlobal(0)
			ctx.ir.FlushToBuffer(buf)
		}

		if ctx.emitter.memTrace {
			emitI32Const(buf, loopID)
			emitCallIdx(buf, ctx.emitter.traceImports["loop_exit"])
		}

	case *ArenaStmt:
		savedLocal := ctx.allocLocal(valTypeI32)
		ctx.emitGetGlobal(0)
		ctx.ir.FlushToBuffer(buf)
		ctx.emitSetLocal(savedLocal)
		ctx.ir.FlushToBuffer(buf)

		var scopeID int32
		prevScope := ctx.currentScopeID
		if ctx.emitter.memTrace {
			scopeID = ctx.emitter.allocScopeID("arena scratch", s.At.File, s.At.Line, 1, prevScope)
			ctx.currentScopeID = scopeID
			emitI32Const(buf, scopeID)
			emitI32Const(buf, 1)
			emitCallIdx(buf, ctx.emitter.traceImports["arena_enter"])
		}

		if err := ctx.compileBlock(buf, s.Body); err != nil {
			return err
		}

		if ctx.emitter.memTrace {
			emitI32Const(buf, scopeID)
			emitCallIdx(buf, ctx.emitter.traceImports["arena_exit"])
			ctx.currentScopeID = prevScope
		}

		ctx.emitGetLocal(savedLocal)
		ctx.ir.FlushToBuffer(buf)
		ctx.emitSetGlobal(0)
		ctx.ir.FlushToBuffer(buf)

	case *ForStmt:
		if rng, ok := s.Iter.(*RangeExpr); ok {
			itemLocal := ctx.allocLocal(valTypeI32)
			ctx.locals[s.ItemName] = itemLocal
			ctx.localTypes[s.ItemName] = s.ItemType
			endLocal := ctx.allocLocal(valTypeI32)

			if err := ctx.compileExpr(buf, rng.Start); err != nil {
				return err
			}
			ctx.emitSetLocal(itemLocal)
			ctx.ir.FlushToBuffer(buf)

			if err := ctx.compileExpr(buf, rng.End); err != nil {
				return err
			}
			ctx.emitSetLocal(endLocal)
			ctx.ir.FlushToBuffer(buf)

			var savedArenaLocal uint32
			if s.IsArena {
				savedArenaLocal = ctx.allocLocal(valTypeI32)
			}
			ctx.loopArenaLocals = append(ctx.loopArenaLocals, savedArenaLocal)

			ctx.openIRLabel(buf, 0x02) // outer block: break target
			breakDepth := ctx.labelDepth
			ctx.openIRLabel(buf, 0x03) // loop head
			loopHeadDepth := ctx.labelDepth

			if s.IsArena {
				ctx.emitGetGlobal(0)
				ctx.ir.FlushToBuffer(buf)
				ctx.emitSetLocal(savedArenaLocal)
				ctx.ir.FlushToBuffer(buf)
			}

			ctx.emitGetLocal(itemLocal)
			ctx.ir.FlushToBuffer(buf)
			ctx.emitGetLocal(endLocal)
			ctx.ir.FlushToBuffer(buf)
			buf.WriteByte(0x4e) // i32.ge_s: item >= end → exit loop
			ctx.emitBrIf(uint32(ctx.labelDepth - breakDepth))
			ctx.ir.FlushToBuffer(buf)

			ctx.openIRLabel(buf, 0x02) // continue target: block around body
			continueDepth := ctx.labelDepth

			ctx.loopBreakDepths = append(ctx.loopBreakDepths, breakDepth)
			ctx.loopContinueDepths = append(ctx.loopContinueDepths, continueDepth)
			ctx.loopDeferDepths = append(ctx.loopDeferDepths, len(ctx.deferScopes))

			if err := ctx.compileBlock(buf, s.Body); err != nil {
				return err
			}

			ctx.loopBreakDepths = ctx.loopBreakDepths[:len(ctx.loopBreakDepths)-1]
			ctx.loopContinueDepths = ctx.loopContinueDepths[:len(ctx.loopContinueDepths)-1]
			ctx.loopDeferDepths = ctx.loopDeferDepths[:len(ctx.loopDeferDepths)-1]
			ctx.loopArenaLocals = ctx.loopArenaLocals[:len(ctx.loopArenaLocals)-1]

			ctx.closeIRLabel(buf) // close continue block

			if s.IsArena {
				ctx.emitGetLocal(savedArenaLocal)
				ctx.ir.FlushToBuffer(buf)
				ctx.emitSetGlobal(0)
				ctx.ir.FlushToBuffer(buf)
			}

			ctx.emitGetLocal(itemLocal)
			ctx.ir.FlushToBuffer(buf)
			buf.WriteByte(0x41)
			writeSleb128(buf, 1)
			buf.WriteByte(0x6a)
			ctx.emitSetLocal(itemLocal)
			ctx.ir.FlushToBuffer(buf)

			ctx.emitBr(uint32(ctx.labelDepth - loopHeadDepth))
			ctx.ir.FlushToBuffer(buf)

			ctx.closeIRLabel(buf) // close loop head
			ctx.closeIRLabel(buf) // close break target

			if s.IsArena {
				ctx.emitGetLocal(savedArenaLocal)
				ctx.ir.FlushToBuffer(buf)
				ctx.emitSetGlobal(0)
				ctx.ir.FlushToBuffer(buf)
			}
		} else {
			targetLocal := ctx.allocLocal(valTypeI32)
			if err := ctx.compileExpr(buf, s.Iter); err != nil {
				return err
			}
			ctx.emitSetLocal(targetLocal)
			ctx.ir.FlushToBuffer(buf)

			lenLocal := ctx.allocLocal(valTypeI32)
			dataLocal := ctx.allocLocal(valTypeI32)

			if arr, ok := s.IterType.(ArrayType); ok {
				buf.WriteByte(0x41)
				writeSleb128(buf, int32(arr.Size))
				ctx.emitSetLocal(lenLocal)
				ctx.ir.FlushToBuffer(buf)

				ctx.emitGetLocal(targetLocal)
				ctx.ir.FlushToBuffer(buf)
				ctx.emitSetLocal(dataLocal)
				ctx.ir.FlushToBuffer(buf)
			} else {
				ctx.emitGetLocal(targetLocal)
				ctx.ir.FlushToBuffer(buf)
				buf.WriteByte(0x28)
				writeUleb128(buf, 2)
				writeUleb128(buf, 4)
				ctx.emitSetLocal(lenLocal)
				ctx.ir.FlushToBuffer(buf)

				ctx.emitGetLocal(targetLocal)
				ctx.ir.FlushToBuffer(buf)
				buf.WriteByte(0x28)
				writeUleb128(buf, 2)
				writeUleb128(buf, 0)
				ctx.emitSetLocal(dataLocal)
				ctx.ir.FlushToBuffer(buf)
			}

			idxLocal := ctx.allocLocal(valTypeI32)
			buf.WriteByte(0x41)
			writeSleb128(buf, 0)
			ctx.emitSetLocal(idxLocal)
			ctx.ir.FlushToBuffer(buf)

			// Element width and load opcode depend on the slice/array
			// element type via sliceElemInfo.
			elemWidth := int32(4)
			loadOp := byte(0x28) // i32.load
			loadAlign := uint32(2)
			itemWasm := byte(valTypeI32)
			if s.ItemType != nil {
				elemWidth, loadOp, _, loadAlign = sliceElemInfo(s.ItemType)
				itemWasm = wasmTypeOrDefault(s.ItemType)
			}
			itemLocal := ctx.allocLocal(itemWasm)
			ctx.locals[s.ItemName] = itemLocal
			ctx.localTypes[s.ItemName] = s.ItemType

			var savedArenaLocal uint32
			if s.IsArena {
				savedArenaLocal = ctx.allocLocal(valTypeI32)
			}
			ctx.loopArenaLocals = append(ctx.loopArenaLocals, savedArenaLocal)

			var loopID int32
			prevScope := ctx.currentScopeID
			if ctx.emitter.memTrace {
				loopID = ctx.emitter.allocScopeID("for loop", s.At.File, s.At.Line, 2, prevScope)
				emitI32Const(buf, loopID)
				emitCallIdx(buf, ctx.emitter.traceImports["loop_enter"])
			}

			ctx.openIRLabel(buf, 0x02) // outer block: break target
			breakDepth := ctx.labelDepth
			ctx.openIRLabel(buf, 0x03) // loop head
			loopHeadDepth := ctx.labelDepth

			if s.IsArena {
				ctx.emitGetGlobal(0)
				ctx.ir.FlushToBuffer(buf)
				ctx.emitSetLocal(savedArenaLocal)
				ctx.ir.FlushToBuffer(buf)
			}

			ctx.emitGetLocal(idxLocal)
			ctx.ir.FlushToBuffer(buf)
			ctx.emitGetLocal(lenLocal)
			ctx.ir.FlushToBuffer(buf)
			buf.WriteByte(0x49)
			ctx.emitI32Eqz()
			ctx.emitBrIf(uint32(ctx.labelDepth - breakDepth))
			ctx.ir.FlushToBuffer(buf)

			if ctx.emitter.memTrace {
				emitI32Const(buf, loopID)
				emitCallIdx(buf, ctx.emitter.traceImports["loop_iter"])
				ctx.currentScopeID = loopID
			}

			ctx.emitGetLocal(dataLocal)
			ctx.ir.FlushToBuffer(buf)
			ctx.emitGetLocal(idxLocal)
			ctx.ir.FlushToBuffer(buf)
			buf.WriteByte(0x41)
			writeSleb128(buf, elemWidth)
			buf.WriteByte(0x6c)
			buf.WriteByte(0x6a)
			buf.WriteByte(loadOp)
			writeUleb128(buf, loadAlign)
			writeUleb128(buf, 0)
			ctx.emitSetLocal(itemLocal)
			ctx.ir.FlushToBuffer(buf)

			ctx.openIRLabel(buf, 0x02) // continue target: block around body
			continueDepth := ctx.labelDepth

			ctx.loopBreakDepths = append(ctx.loopBreakDepths, breakDepth)
			ctx.loopContinueDepths = append(ctx.loopContinueDepths, continueDepth)
			ctx.loopDeferDepths = append(ctx.loopDeferDepths, len(ctx.deferScopes))

			if err := ctx.compileBlock(buf, s.Body); err != nil {
				return err
			}

			ctx.loopBreakDepths = ctx.loopBreakDepths[:len(ctx.loopBreakDepths)-1]
			ctx.loopContinueDepths = ctx.loopContinueDepths[:len(ctx.loopContinueDepths)-1]
			ctx.loopDeferDepths = ctx.loopDeferDepths[:len(ctx.loopDeferDepths)-1]
			ctx.loopArenaLocals = ctx.loopArenaLocals[:len(ctx.loopArenaLocals)-1]

			ctx.closeIRLabel(buf) // close continue block

			if s.IsArena {
				ctx.emitGetLocal(savedArenaLocal)
				ctx.ir.FlushToBuffer(buf)
				ctx.emitSetGlobal(0)
				ctx.ir.FlushToBuffer(buf)
			}

			ctx.emitGetLocal(idxLocal)
			ctx.ir.FlushToBuffer(buf)
			buf.WriteByte(0x41)
			writeSleb128(buf, 1)
			buf.WriteByte(0x6a)
			ctx.emitSetLocal(idxLocal)
			ctx.ir.FlushToBuffer(buf)

			ctx.emitBr(uint32(ctx.labelDepth - loopHeadDepth))
			ctx.ir.FlushToBuffer(buf)

			if ctx.emitter.memTrace {
				ctx.currentScopeID = prevScope
			}

			ctx.closeIRLabel(buf) // close loop head
			ctx.closeIRLabel(buf) // close break target

			if s.IsArena {
				ctx.emitGetLocal(savedArenaLocal)
				ctx.ir.FlushToBuffer(buf)
				ctx.emitSetGlobal(0)
				ctx.ir.FlushToBuffer(buf)
			}

			if ctx.emitter.memTrace {
				emitI32Const(buf, loopID)
				emitCallIdx(buf, ctx.emitter.traceImports["loop_exit"])
			}
		}

	case *IfLetStmt:
		optLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, s.Expr); err != nil {
			return err
		}
		ctx.emitSetLocal(optLocal)
		ctx.ir.FlushToBuffer(buf)

		ctx.emitGetLocal(optLocal)
		ctx.ir.FlushToBuffer(buf)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

		ctx.openIRLabel(buf, 0x04)

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
		ctx.emitGetLocal(optLocal)
		ctx.ir.FlushToBuffer(buf)
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
		ctx.emitSetLocal(varLocal)
		ctx.ir.FlushToBuffer(buf)

		if err := ctx.compileBlock(buf, s.Then); err != nil {
			return err
		}

		if s.Else != nil {
			ctx.ir.Else()
			ctx.ir.FlushToBuffer(buf)
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
		ctx.closeIRLabel(buf)

	case *WhileLetStmt:
		var savedArenaLocal uint32
		if s.IsArena {
			savedArenaLocal = ctx.allocLocal(valTypeI32)
		}
		ctx.loopArenaLocals = append(ctx.loopArenaLocals, savedArenaLocal)

		ctx.openIRLabel(buf, 0x02)
		breakDepth := ctx.labelDepth
		ctx.openIRLabel(buf, 0x03)
		continueDepth := ctx.labelDepth
		ctx.loopBreakDepths = append(ctx.loopBreakDepths, breakDepth)
		ctx.loopContinueDepths = append(ctx.loopContinueDepths, continueDepth)
		ctx.loopDeferDepths = append(ctx.loopDeferDepths, len(ctx.deferScopes))

		if s.IsArena {
			ctx.emitGetGlobal(0)
			ctx.ir.FlushToBuffer(buf)
			ctx.emitSetLocal(savedArenaLocal)
			ctx.ir.FlushToBuffer(buf)
		}

		optLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, s.Call); err != nil {
			return err
		}
		ctx.emitSetLocal(optLocal)
		ctx.ir.FlushToBuffer(buf)

		ctx.emitGetLocal(optLocal)
		ctx.ir.FlushToBuffer(buf)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)
		ctx.emitI32Eqz()
		ctx.emitBrIf(uint32(ctx.labelDepth - breakDepth))
		ctx.ir.FlushToBuffer(buf)

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
		ctx.emitGetLocal(optLocal)
		ctx.ir.FlushToBuffer(buf)
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
		ctx.emitSetLocal(varLocal)
		ctx.ir.FlushToBuffer(buf)

		if err := ctx.compileBlock(buf, s.Body); err != nil {
			return err
		}

		if s.IsArena {
			ctx.emitGetLocal(savedArenaLocal)
			ctx.ir.FlushToBuffer(buf)
			ctx.emitSetGlobal(0)
			ctx.ir.FlushToBuffer(buf)
		}

		ctx.loopBreakDepths = ctx.loopBreakDepths[:len(ctx.loopBreakDepths)-1]
		ctx.loopContinueDepths = ctx.loopContinueDepths[:len(ctx.loopContinueDepths)-1]
		ctx.loopDeferDepths = ctx.loopDeferDepths[:len(ctx.loopDeferDepths)-1]
		ctx.loopArenaLocals = ctx.loopArenaLocals[:len(ctx.loopArenaLocals)-1]

		ctx.emitBr(uint32(ctx.labelDepth - continueDepth))
		ctx.ir.FlushToBuffer(buf)

		ctx.closeIRLabel(buf)
		ctx.closeIRLabel(buf)

		if s.IsArena {
			ctx.emitGetLocal(savedArenaLocal)
			ctx.ir.FlushToBuffer(buf)
			ctx.emitSetGlobal(0)
			ctx.ir.FlushToBuffer(buf)
		}

	case *MatchStmt:
		targetLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, s.Expr); err != nil {
			return err
		}
		ctx.emitSetLocal(targetLocal)
		ctx.ir.FlushToBuffer(buf)

		tagLocal := ctx.allocLocal(valTypeI32)
		ctx.emitGetLocal(targetLocal)
		ctx.ir.FlushToBuffer(buf)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)
		ctx.emitSetLocal(tagLocal)
		ctx.ir.FlushToBuffer(buf)

		ctx.openIRLabel(buf, 0x02)

		enumVariants := ctx.emitter.enums[s.EnumName]
		for _, arm := range s.Arms {
			ctx.openIRLabel(buf, 0x02)

			if arm.Variant != "_" {
				tagVal := -1
				for idx, v := range enumVariants {
					if v.Name == arm.Variant {
						tagVal = idx
						break
					}
				}
				ctx.emitGetLocal(tagLocal)
				ctx.ir.FlushToBuffer(buf)
				buf.WriteByte(0x41)
				writeSleb128(buf, int32(tagVal))
				buf.WriteByte(0x47) // i32.ne
				ctx.emitBrIf(0)
				ctx.ir.FlushToBuffer(buf)
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
				ctx.emitGetLocal(targetLocal)
				ctx.ir.FlushToBuffer(buf)
				buf.WriteByte(loadOp)
				writeUleb128(buf, loadAlign)
				writeUleb128(buf, uint32(4+bIdx*4))
				ctx.emitSetLocal(bLocal)
				ctx.ir.FlushToBuffer(buf)
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

			ctx.emitBr(1)
			ctx.ir.FlushToBuffer(buf)

			ctx.closeIRLabel(buf)
		}

		ctx.closeIRLabel(buf)

	case *SpawnStmt:
		if err := ctx.compileExpr(buf, s.Call); err != nil {
			return err
		}
		if ctx.exprLeavesValue(s.Call) {
			ctx.emitDrop()
			ctx.ir.FlushToBuffer(buf)
		}

	case *SelectStmt:
		ctx.openIRLabel(buf, 0x02) // outer block

		for _, sc := range s.Cases {
			ctx.openIRLabel(buf, 0x02) // per-case block

			chanLocal := ctx.allocLocal(valTypeI32)
			if err := ctx.compileExpr(buf, sc.Chan); err != nil {
				return err
			}
			ctx.emitSetLocal(chanLocal)
			ctx.ir.FlushToBuffer(buf)

			ctx.emitGetLocal(chanLocal)
			ctx.ir.FlushToBuffer(buf)
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 12)
			ctx.emitI32Eqz()
			ctx.emitBrIf(0)
			ctx.ir.FlushToBuffer(buf)

			varLocal := ctx.allocLocal(valTypeI32)
			ctx.locals[sc.VarName] = varLocal
			ctx.localTypes[sc.VarName] = sc.ChanElem

			ctx.emitGetLocal(chanLocal)
			ctx.ir.FlushToBuffer(buf)
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 4)

			ctx.emitGetLocal(chanLocal)
			ctx.ir.FlushToBuffer(buf)
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 20)

			buf.WriteByte(0x6a)
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 0)
			ctx.emitSetLocal(varLocal)
			ctx.ir.FlushToBuffer(buf)

			ctx.emitGetLocal(chanLocal)
			ctx.ir.FlushToBuffer(buf)
			ctx.emitGetLocal(chanLocal)
			ctx.ir.FlushToBuffer(buf)
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

			ctx.emitBr(1)
			ctx.ir.FlushToBuffer(buf)

			ctx.closeIRLabel(buf) // end per-case block
		}

		if s.Default != nil {
			if err := ctx.compileBlock(buf, s.Default); err != nil {
				return err
			}
		}

		ctx.closeIRLabel(buf) // end outer block

	case *LoopControlStmt:
		if len(ctx.loopBreakDepths) == 0 {
			return fmt.Errorf("%s: %s outside of any loop", s.Loc(), loopControlName(s.Continue))
		}
		if len(ctx.loopDeferDepths) > 0 {
			ctx.emitLoopDefers(buf, ctx.loopDeferDepths[len(ctx.loopDeferDepths)-1])
		}
		if len(ctx.loopArenaLocals) > 0 {
			if arenaLoc := ctx.loopArenaLocals[len(ctx.loopArenaLocals)-1]; arenaLoc != 0 {
				ctx.emitGetLocal(arenaLoc)
				ctx.ir.FlushToBuffer(buf)
				ctx.emitSetGlobal(0)
				ctx.ir.FlushToBuffer(buf)
			}
		}
		if s.Continue {
			depth := ctx.labelDepth - ctx.loopContinueDepths[len(ctx.loopContinueDepths)-1]
			ctx.emitBr(uint32(depth))
		} else {
			depth := ctx.labelDepth - ctx.loopBreakDepths[len(ctx.loopBreakDepths)-1]
			ctx.emitBr(uint32(depth))
		}
		ctx.ir.FlushToBuffer(buf)
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
