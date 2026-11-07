// File: pkg/compiler/wasm_intrinsics.go

package compiler

import "bytes"

// Scratch memory used by raw WASI intrinsics. Static data starts at
// staticDataOff (64).
const (
	scratchOpenedFd = 48
	// WASI fd_seek writes a u64 filesize to this out-pointer. Wasmtime
	// requires that pointer to be 8-byte aligned. 52 is not (52%8=4),
	// which produced "Pointer not aligned to 8: Region { start: 52,
	// len: 8 }" on every seek. Use the next free 8-aligned slot after
	// scratchOpenedFd; scratchCiovec (0..8), scratchNwrite (8..12),
	// scratchItoa (16..48) and scratchOpenedFd (48..52) are unchanged.
	scratchSeekPos = 56
)

func (e *wasmEmitter) registerWasiImport(name string, params []byte, results []byte) uint32 {
	typeIdx := e.registerType(params, results)
	idx := uint32(len(e.imports))
	e.imports = append(e.imports, importMeta{
		module:  "wasi_snapshot_preview1",
		name:    name,
		typeIdx: typeIdx,
	})
	e.wasiImports[name] = idx
	return idx
}

func emitI32Const(buf *bytes.Buffer, v int32) {
	buf.WriteByte(0x41)
	writeSleb128(buf, v)
}

func emitLocalGet(buf *bytes.Buffer, idx uint32) {
	buf.WriteByte(0x20)
	writeUleb128(buf, idx)
}

func emitLocalSet(buf *bytes.Buffer, idx uint32) {
	buf.WriteByte(0x21)
	writeUleb128(buf, idx)
}

func emitI32Load(buf *bytes.Buffer, offset, align uint32) {
	buf.WriteByte(0x28)
	writeUleb128(buf, align)
	writeUleb128(buf, offset)
}

func emitI32Store(buf *bytes.Buffer, offset, align uint32) {
	buf.WriteByte(0x36)
	writeUleb128(buf, align)
	writeUleb128(buf, offset)
}

func emitCallIdx(buf *bytes.Buffer, idx uint32) {
	buf.WriteByte(0x10)
	writeUleb128(buf, idx)
}

func emitI32Eqz(buf *bytes.Buffer) {
	buf.WriteByte(0x45)
}

func (ctx *fnContext) wasiIdx(name string) uint32 {
	return ctx.emitter.wasiImports[name]
}

// compileSpecialCall handles compiler-backed standard library intrinsics and
// the small set of string/slice bridge methods that need direct memory access.
// It returns handled=true when the call was fully emitted.
func (ctx *fnContext) compileSpecialCall(buf *bytes.Buffer, e *CallExpr) (bool, error) {
	if mem, ok := e.Callee.(*MemberExpr); ok {
		if id, ok := mem.Target.(*IdentExpr); ok && id.Name == "string" &&
			(mem.Member == "from_bytes" || mem.Member == "from_utf8") {
			if len(e.Args) != 1 {
				return true, nil
			}
			sliceLocal := ctx.allocLocal(valTypeI32)
			if err := ctx.compileExpr(buf, e.Args[0]); err != nil {
				return true, err
			}
			emitLocalSet(buf, sliceLocal)

			strLocal := ctx.allocLocal(valTypeI32)
			ctx.emitAlloc(buf, 8, strLocal)
			emitLocalGet(buf, strLocal)
			emitLocalGet(buf, sliceLocal)
			emitI32Load(buf, 0, 2)
			emitI32Store(buf, 0, 2)
			emitLocalGet(buf, strLocal)
			emitLocalGet(buf, sliceLocal)
			emitI32Load(buf, 4, 2)
			emitI32Store(buf, 4, 2)

			resLocal := ctx.allocLocal(valTypeI32)
			ctx.emitAlloc(buf, 8, resLocal)
			emitLocalGet(buf, resLocal)
			emitI32Const(buf, 1)
			emitI32Store(buf, 0, 2)
			emitLocalGet(buf, resLocal)
			emitLocalGet(buf, strLocal)
			emitI32Store(buf, 4, 2)
			emitLocalGet(buf, resLocal)
			return true, nil
		}

		if e.ReceiverType != nil && e.ReceiverType.String() == "string" &&
			(mem.Member == "bytes" || mem.Member == "as_bytes") {
			strLocal := ctx.allocLocal(valTypeI32)
			if err := ctx.compileExpr(buf, mem.Target); err != nil {
				return true, err
			}
			emitLocalSet(buf, strLocal)

			hdrLocal := ctx.allocLocal(valTypeI32)
			ctx.emitAlloc(buf, 12, hdrLocal)
			emitLocalGet(buf, hdrLocal)
			emitLocalGet(buf, strLocal)
			emitI32Load(buf, 0, 2)
			emitI32Store(buf, 0, 2)
			emitLocalGet(buf, hdrLocal)
			emitLocalGet(buf, strLocal)
			emitI32Load(buf, 4, 2)
			emitI32Store(buf, 4, 2)
			emitLocalGet(buf, hdrLocal)
			emitLocalGet(buf, strLocal)
			emitI32Load(buf, 4, 2)
			emitI32Store(buf, 8, 2)
			emitLocalGet(buf, hdrLocal)
			return true, nil
		}

		if id, ok := mem.Target.(*IdentExpr); ok && id.Name == "StringBuilder" && mem.Member == "new" {
			return true, ctx.compileStringBuilderNew(buf, e)
		}

		if isStringBuilderTarget(ctx, mem) {
			switch mem.Member {
			case "append", "append_str":
				if len(e.Args) != 1 {
					return true, nil
				}
				return true, ctx.compileStringBuilderAppend(buf, mem.Target, e.Args[0])
			case "append_int":
				if len(e.Args) != 1 {
					return true, nil
				}
				return true, ctx.compileStringBuilderAppendInt(buf, mem.Target, e.Args[0])
			case "append_char":
				if len(e.Args) != 1 {
					return true, nil
				}
				return true, ctx.compileStringBuilderAppendChar(buf, mem.Target, e.Args[0])
			case "to_string":
				return true, ctx.compileStringBuilderToString(buf, mem.Target)
			}
		}
	}

	id, ok := e.Callee.(*IdentExpr)
	if !ok {
		return false, nil
	}

	switch id.Name {
	case "wasi_fd_write_raw", "wasi_fd_read_raw", "wasi_fd_write_string_raw":
		return true, ctx.compileFdIO(buf, id.Name, e.Args)
	case "wasi_path_open_raw":
		return true, ctx.compilePathOpen(buf, e.Args)
	case "wasi_fd_close_raw":
		return true, ctx.compileSimpleWasiCall(buf, "fd_close", e.Args, false)
	case "wasi_fd_sync_raw":
		return true, ctx.compileSimpleWasiCall(buf, "fd_sync", e.Args, false)
	case "wasi_fd_seek_raw":
		return true, ctx.compileFdSeek(buf, e.Args)
	case "wasi_path_create_dir_raw":
		return true, ctx.compilePathCall(buf, "path_create_directory", e.Args)
	case "wasi_path_unlink_raw":
		return true, ctx.compilePathCall(buf, "path_unlink_file", e.Args)
	case "wasi_path_remove_dir_raw":
		return true, ctx.compilePathCall(buf, "path_remove_directory", e.Args)
	case "wasi_path_filestat_raw":
		return true, ctx.compilePathStat(buf, false, e.Args)
	case "wasi_fd_filestat_raw":
		return true, ctx.compilePathStat(buf, true, e.Args)
	case "int_to_str":
		if len(e.Args) != 1 {
			return true, nil
		}
		if err := ctx.compileExpr(buf, e.Args[0]); err != nil {
			return true, err
		}
		emitCallIdx(buf, ctx.emitter.fnIndices["__i32_to_str"])
		return true, nil
	}
	return false, nil
}

func (ctx *fnContext) compileFdIO(buf *bytes.Buffer, name string, args []Expr) error {
	if len(args) != 2 {
		return nil
	}
	if name == "wasi_fd_write_string_raw" {
		// fd, string-header
		hdr := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, args[1]); err != nil {
			return err
		}
		emitLocalSet(buf, hdr)

		emitI32Const(buf, scratchCiovec)
		emitLocalGet(buf, hdr)
		emitI32Load(buf, 0, 2)
		emitI32Store(buf, 0, 2)
		emitI32Const(buf, scratchCiovec+4)
		emitLocalGet(buf, hdr)
		emitI32Load(buf, 4, 2)
		emitI32Store(buf, 0, 2)
	} else {
		// fd, slice-header
		ptr := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, args[1]); err != nil {
			return err
		}
		emitLocalSet(buf, ptr)

		emitI32Const(buf, scratchCiovec)
		emitLocalGet(buf, ptr)
		emitI32Load(buf, 0, 2)
		emitI32Store(buf, 0, 2)
		emitI32Const(buf, scratchCiovec+4)
		emitLocalGet(buf, ptr)
		emitI32Load(buf, 4, 2)
		emitI32Store(buf, 0, 2)
	}

	importName := "fd_write"
	if name == "wasi_fd_read_raw" {
		importName = "fd_read"
	}
	if err := ctx.compileExpr(buf, args[0]); err != nil {
		return err
	}
	emitI32Const(buf, scratchCiovec)
	emitI32Const(buf, 1)
	emitI32Const(buf, scratchNwrite)
	emitCallIdx(buf, ctx.wasiIdx(importName))

	errno := ctx.allocLocal(valTypeI32)
	emitLocalSet(buf, errno)
	emitLocalGet(buf, errno)
	emitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(valTypeI32)
	emitI32Const(buf, scratchNwrite)
	emitI32Load(buf, 0, 2)
	buf.WriteByte(0x05)
	emitI32Const(buf, 0)
	emitLocalGet(buf, errno)
	buf.WriteByte(0x6b) // i32.sub
	buf.WriteByte(0x0b)
	return nil
}

func (ctx *fnContext) compilePathOpen(buf *bytes.Buffer, args []Expr) error {
	if len(args) != 5 {
		return nil
	}
	hdr := ctx.allocLocal(valTypeI32)
	if err := ctx.compileExpr(buf, args[0]); err != nil {
		return err
	}
	emitLocalSet(buf, hdr)

	emitI32Const(buf, 3) // preopened current directory
	emitI32Const(buf, 1) // follow symlinks
	emitLocalGet(buf, hdr)
	emitI32Load(buf, 0, 2)
	emitLocalGet(buf, hdr)
	emitI32Load(buf, 4, 2)
	if err := ctx.compileExpr(buf, args[1]); err != nil { // oflags
		return err
	}
	if err := ctx.compileExpr(buf, args[3]); err != nil { // rights_base
		return err
	}
	buf.WriteByte(0xad)                                   // i64.extend_i32_u
	if err := ctx.compileExpr(buf, args[4]); err != nil { // rights_inheriting
		return err
	}
	buf.WriteByte(0xad)
	if err := ctx.compileExpr(buf, args[2]); err != nil { // fdflags
		return err
	}
	emitI32Const(buf, scratchOpenedFd)
	emitCallIdx(buf, ctx.wasiIdx("path_open"))

	errno := ctx.allocLocal(valTypeI32)
	emitLocalSet(buf, errno)
	emitLocalGet(buf, errno)
	emitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(valTypeI32)
	emitI32Const(buf, scratchOpenedFd)
	emitI32Load(buf, 0, 2)
	buf.WriteByte(0x05)
	emitI32Const(buf, 0)
	emitLocalGet(buf, errno)
	buf.WriteByte(0x6b)
	buf.WriteByte(0x0b)
	return nil
}

func (ctx *fnContext) compileFdSeek(buf *bytes.Buffer, args []Expr) error {
	if len(args) != 3 {
		return nil
	}
	if err := ctx.compileExpr(buf, args[0]); err != nil {
		return err
	}
	if err := ctx.compileExpr(buf, args[1]); err != nil {
		return err
	}
	buf.WriteByte(0xac) // i64.extend_i32_s
	if err := ctx.compileExpr(buf, args[2]); err != nil {
		return err
	}
	emitI32Const(buf, scratchSeekPos)
	emitCallIdx(buf, ctx.wasiIdx("fd_seek"))

	errno := ctx.allocLocal(valTypeI32)
	emitLocalSet(buf, errno)
	emitLocalGet(buf, errno)
	emitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(valTypeI32)
	emitI32Const(buf, scratchSeekPos)
	emitI32Load(buf, 0, 2)
	buf.WriteByte(0x05)
	emitI32Const(buf, 0)
	emitLocalGet(buf, errno)
	buf.WriteByte(0x6b)
	buf.WriteByte(0x0b)
	return nil
}

func (ctx *fnContext) compileSimpleWasiCall(buf *bytes.Buffer, importName string, args []Expr, _ bool) error {
	for _, arg := range args {
		if err := ctx.compileExpr(buf, arg); err != nil {
			return err
		}
	}
	emitCallIdx(buf, ctx.wasiIdx(importName))
	return nil
}

func (ctx *fnContext) compilePathCall(buf *bytes.Buffer, importName string, args []Expr) error {
	if len(args) != 1 {
		return nil
	}
	hdr := ctx.allocLocal(valTypeI32)
	if err := ctx.compileExpr(buf, args[0]); err != nil {
		return err
	}
	emitLocalSet(buf, hdr)
	emitI32Const(buf, 3)
	emitLocalGet(buf, hdr)
	emitI32Load(buf, 0, 2)
	emitLocalGet(buf, hdr)
	emitI32Load(buf, 4, 2)
	emitCallIdx(buf, ctx.wasiIdx(importName))
	return nil
}

func (ctx *fnContext) compilePathStat(buf *bytes.Buffer, byFd bool, args []Expr) error {
	if len(args) != 2 {
		return nil
	}
	if byFd {
		if err := ctx.compileExpr(buf, args[0]); err != nil {
			return err
		}
		if err := ctx.compileExpr(buf, args[1]); err != nil {
			return err
		}
		emitCallIdx(buf, ctx.wasiIdx("fd_filestat_get"))
		return nil
	}

	hdr := ctx.allocLocal(valTypeI32)
	if err := ctx.compileExpr(buf, args[0]); err != nil {
		return err
	}
	emitLocalSet(buf, hdr)
	emitI32Const(buf, 3)
	emitI32Const(buf, 1)
	emitLocalGet(buf, hdr)
	emitI32Load(buf, 0, 2)
	emitLocalGet(buf, hdr)
	emitI32Load(buf, 4, 2)
	if err := ctx.compileExpr(buf, args[1]); err != nil {
		return err
	}
	emitCallIdx(buf, ctx.wasiIdx("path_filestat_get"))
	return nil
}

// compileStringPart evaluates one interpolation part and leaves a string
// header on the stack, converting scalars through the int-to-string helper.
func (ctx *fnContext) compileStringPart(buf *bytes.Buffer, part Expr) error {
	partType := ctx.inferExprType(part)
	if err := ctx.compileExpr(buf, part); err != nil {
		return err
	}
	switch partType {
	case "string":
		return nil
	case "bool":
		buf.WriteByte(0x04)
		buf.WriteByte(valTypeI32)
		emitI32Const(buf, int32(ctx.emitter.trueHeader))
		buf.WriteByte(0x05)
		emitI32Const(buf, int32(ctx.emitter.falseHeader))
		buf.WriteByte(0x0b)
	case "f32":
		buf.WriteByte(0xa8) // i32.trunc_f32_s
		emitCallIdx(buf, ctx.emitter.fnIndices["__i32_to_str"])
	case "f64":
		buf.WriteByte(0xaa) // i32.trunc_f64_s
		emitCallIdx(buf, ctx.emitter.fnIndices["__i32_to_str"])
	default:
		emitCallIdx(buf, ctx.emitter.fnIndices["__i32_to_str"])
	}
	return nil
}

func (ctx *fnContext) callReturnUbeType(call *CallExpr) UbeType {
	if call == nil {
		return nil
	}
	if id, ok := call.Callee.(*IdentExpr); ok {
		if id.Name == "int_to_str" {
			return StringType{}
		}
		if ret, ok := ctx.emitter.fnReturns[id.Name]; ok {
			return ret
		}
	}
	if mem, ok := call.Callee.(*MemberExpr); ok {
		if id, ok := mem.Target.(*IdentExpr); ok && id.Name == "StringBuilder" && mem.Member == "new" {
			return StringBuilderType{}
		}
		if mem.Member == "to_string" {
			return StringType{}
		}
		if call.ReceiverType != nil {
			if ret, ok := ctx.emitter.fnReturns[call.ReceiverType.String()+"."+mem.Member]; ok {
				return ret
			}
		}
		if target, ok := mem.Target.(*IdentExpr); ok {
			if prefix, ok := ctx.emitter.moduleAliases[target.Name]; ok {
				if ret, ok := ctx.emitter.fnReturns[prefix+"."+mem.Member]; ok {
					return ret
				}
			}
		}
	}
	return nil
}

// callReturnsValue reports whether a call leaves a value on the operand stack.
func (ctx *fnContext) callReturnsValue(call *CallExpr) bool {
	if call == nil {
		return false
	}
	if id, ok := call.Callee.(*IdentExpr); ok {
		switch id.Name {
		case "println", "print":
			return false
		}
		if ret, ok := ctx.emitter.fnReturns[id.Name]; ok {
			return ret != nil && ret.String() != "void"
		}
		return true
	}
	if mem, ok := call.Callee.(*MemberExpr); ok {
		switch mem.Member {
		case "push", "clear", "resize", "append", "append_str", "append_int", "append_char":
			return false
		}
		if call.ReceiverType != nil {
			if ret, ok := ctx.emitter.fnReturns[call.ReceiverType.String()+"."+mem.Member]; ok {
				return ret != nil && ret.String() != "void"
			}
		}
		if target, ok := mem.Target.(*IdentExpr); ok {
			if prefix, ok := ctx.emitter.moduleAliases[target.Name]; ok {
				if ret, ok := ctx.emitter.fnReturns[prefix+mem.Member]; ok {
					return ret != nil && ret.String() != "void"
				}
			}
		}
	}
	return true
}

// exprLeavesValue reports whether compiling expr leaves a value on the
// operand stack. Expression statements use this to discard unused results.
func (ctx *fnContext) exprLeavesValue(expr Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *CallExpr:
		return ctx.callReturnsValue(e)
	case *IfExpr:
		return e.Type == nil || e.Type.String() != "void"
	case *TryExpr, *OrElseExpr, *ErrorExpr, *InterpolatedStringExpr,
		*StructInitExpr, *SliceInitExpr, *ArrayInitExpr, *TupleExpr,
		*BinaryExpr, *UnaryExpr, *MemberExpr, *IndexExpr, *SliceSubExpr,
		*RangeExpr, *TraitCoerceExpr, *ArrayToSliceExpr, *PromoteExpr, *ArenaExpr, *CastExpr:
		return true
	case *LiteralExpr:
		return e.Type == nil || e.Type.String() != "void"
	case *IdentExpr:
		t, ok := ctx.localTypes[e.Name]
		return !ok || t == nil || t.String() != "void"
	}
	return false
}

func (ctx *fnContext) compileDeferredCall(buf *bytes.Buffer, expr Expr) error {
	if err := ctx.compileExpr(buf, expr); err != nil {
		return err
	}
	if call, ok := expr.(*CallExpr); ok && ctx.callReturnsValue(call) {
		buf.WriteByte(0x1a)
	}
	return nil
}

func (ctx *fnContext) compileWrappedSuccess(buf *bytes.Buffer, value Expr) error {
	valLocalType := byte(valTypeI32)
	switch ctx.inferExprType(value) {
	case "f32":
		valLocalType = valTypeF32
	case "f64":
		valLocalType = valTypeF64
	case "i64":
		valLocalType = valTypeI64
	}
	valLocal := ctx.allocLocal(valLocalType)
	if err := ctx.compileExpr(buf, value); err != nil {
		return err
	}
	emitLocalSet(buf, valLocal)

	resLocal := ctx.allocLocal(valTypeI32)
	ctx.emitAlloc(buf, 8, resLocal)
	emitLocalGet(buf, resLocal)
	emitI32Const(buf, 1)
	emitI32Store(buf, 0, 2)

	emitLocalGet(buf, resLocal)
	emitLocalGet(buf, valLocal)
	switch valLocalType {
	case valTypeF32:
		buf.WriteByte(0xbc) // i32.reinterpret_f32
	case valTypeF64:
		buf.WriteByte(0xbd) // i64.reinterpret_f64
		buf.WriteByte(0xa7) // i32.wrap_i64
	case valTypeI64:
		buf.WriteByte(0xa7) // i32.wrap_i64
	}
	emitI32Store(buf, 4, 2)

	emitLocalGet(buf, resLocal)
	return nil
}

// exprStaticType recovers the type of common expressions for codegen paths
// that need to know how a value is represented (for example printing an
// if-let binding).
func (ctx *fnContext) exprStaticType(expr Expr) UbeType {
	switch e := expr.(type) {
	case *LiteralExpr:
		return e.Type
	case *IdentExpr:
		if t, ok := ctx.localTypes[e.Name]; ok {
			return t
		}
	case *CallExpr:
		if id, ok := e.Callee.(*IdentExpr); ok {
			if t, ok := ctx.emitter.fnReturns[id.Name]; ok {
				return t
			}
		}
		if mem, ok := e.Callee.(*MemberExpr); ok {
			if e.ReceiverType != nil {
				if t, ok := ctx.emitter.fnReturns[e.ReceiverType.String()+"."+mem.Member]; ok {
					return t
				}
			}
			if target, ok := mem.Target.(*IdentExpr); ok {
				if prefix, ok := ctx.emitter.moduleAliases[target.Name]; ok {
					if t, ok := ctx.emitter.fnReturns[prefix+mem.Member]; ok {
						return t
					}
				}
			}
		}
	case *OrElseExpr:
		if e.PrimaryType != nil {
			switch t := e.PrimaryType.(type) {
			case OptionalType:
				return t.Inner
			case ResultType:
				return t.Ok
			}
		}
	}
	return nil
}

func unwrapBindingType(t UbeType) UbeType {
	switch v := t.(type) {
	case OptionalType:
		return v.Inner
	case ResultType:
		return v.Ok
	}
	return t
}

func isStringBuilderTarget(ctx *fnContext, mem *MemberExpr) bool {
	if mem.TargetType != nil && mem.TargetType.String() == "StringBuilder" {
		return true
	}
	if id, ok := mem.Target.(*IdentExpr); ok {
		if id.Name == "StringBuilder" {
			return true
		}
		if t, ok := ctx.localTypes[id.Name]; ok && t != nil && t.String() == "StringBuilder" {
			return true
		}
	}
	return false
}

func (ctx *fnContext) compileStringBuilderNew(buf *bytes.Buffer, e *CallExpr) error {
	capLocal := ctx.allocLocal(valTypeI32)
	if len(e.Args) > 0 {
		if err := ctx.compileExpr(buf, e.Args[0]); err != nil {
			return err
		}
	} else {
		emitI32Const(buf, 16)
	}
	emitLocalSet(buf, capLocal)

	// if cap < 16 { cap = 16 }
	emitLocalGet(buf, capLocal)
	emitI32Const(buf, 16)
	buf.WriteByte(0x48) // i32.lt_s
	buf.WriteByte(0x04)
	buf.WriteByte(valTypeVoid)
	emitI32Const(buf, 16)
	emitLocalSet(buf, capLocal)
	buf.WriteByte(0x0b)

	// dataLocal = align8(global0)
	dataLocal := ctx.allocLocal(valTypeI32)
	buf.WriteByte(0x23)
	writeUleb128(buf, 0)
	emitI32Const(buf, 7)
	buf.WriteByte(0x6a)
	emitI32Const(buf, -8)
	buf.WriteByte(0x71)
	emitLocalSet(buf, dataLocal)

	// global0 = dataLocal + capLocal
	emitLocalGet(buf, dataLocal)
	emitLocalGet(buf, capLocal)
	buf.WriteByte(0x6a)
	buf.WriteByte(0x24)
	writeUleb128(buf, 0)

	// Allocate 12-byte StringBuilder header { buf: i32, len: i32, cap: i32 }
	sbLocal := ctx.allocLocal(valTypeI32)
	ctx.emitAlloc(buf, 12, sbLocal)

	// sb.buf = dataLocal
	emitLocalGet(buf, sbLocal)
	emitLocalGet(buf, dataLocal)
	emitI32Store(buf, 0, 2)

	// sb.len = 0
	emitLocalGet(buf, sbLocal)
	emitI32Const(buf, 0)
	emitI32Store(buf, 4, 2)

	// sb.cap = capLocal
	emitLocalGet(buf, sbLocal)
	emitLocalGet(buf, capLocal)
	emitI32Store(buf, 8, 2)

	emitLocalGet(buf, sbLocal)
	return nil
}

func (ctx *fnContext) compileStringBuilderAppend(buf *bytes.Buffer, target Expr, arg Expr) error {
	sbLocal := ctx.allocLocal(valTypeI32)
	if err := ctx.compileExpr(buf, target); err != nil {
		return err
	}
	emitLocalSet(buf, sbLocal)

	strLocal := ctx.allocLocal(valTypeI32)
	if err := ctx.compileExpr(buf, arg); err != nil {
		return err
	}
	emitLocalSet(buf, strLocal)

	strPtr := ctx.allocLocal(valTypeI32)
	emitLocalGet(buf, strLocal)
	emitI32Load(buf, 0, 2)
	emitLocalSet(buf, strPtr)

	strLen := ctx.allocLocal(valTypeI32)
	emitLocalGet(buf, strLocal)
	emitI32Load(buf, 4, 2)
	emitLocalSet(buf, strLen)

	// req = sb.len + strLen
	reqLocal := ctx.allocLocal(valTypeI32)
	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 4, 2)
	emitLocalGet(buf, strLen)
	buf.WriteByte(0x6a)
	emitLocalSet(buf, reqLocal)

	// if req > sb.cap { realloc }
	emitLocalGet(buf, reqLocal)
	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 8, 2)
	buf.WriteByte(0x4a) // i32.gt_s
	buf.WriteByte(0x04)
	buf.WriteByte(valTypeVoid)

	newCap := ctx.allocLocal(valTypeI32)
	emitLocalGet(buf, reqLocal)
	emitI32Const(buf, 2)
	buf.WriteByte(0x6c)
	emitLocalSet(buf, newCap)

	newBuf := ctx.allocLocal(valTypeI32)
	buf.WriteByte(0x23)
	writeUleb128(buf, 0)
	emitI32Const(buf, 7)
	buf.WriteByte(0x6a)
	emitI32Const(buf, -8)
	buf.WriteByte(0x71)
	emitLocalSet(buf, newBuf)

	emitLocalGet(buf, newBuf)
	emitLocalGet(buf, newCap)
	buf.WriteByte(0x6a)
	buf.WriteByte(0x24)
	writeUleb128(buf, 0)

	copyIdx := ctx.allocLocal(valTypeI32)
	emitI32Const(buf, 0)
	emitLocalSet(buf, copyIdx)

	buf.WriteByte(0x02)
	buf.WriteByte(valTypeVoid)
	buf.WriteByte(0x03)
	buf.WriteByte(valTypeVoid)

	emitLocalGet(buf, copyIdx)
	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 4, 2)
	buf.WriteByte(0x4e)
	buf.WriteByte(0x0d)
	writeUleb128(buf, 1)

	emitLocalGet(buf, newBuf)
	emitLocalGet(buf, copyIdx)
	buf.WriteByte(0x6a)

	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 0, 2)
	emitLocalGet(buf, copyIdx)
	buf.WriteByte(0x6a)
	buf.WriteByte(0x2d)
	writeUleb128(buf, 0)
	writeUleb128(buf, 0)

	buf.WriteByte(0x3a)
	writeUleb128(buf, 0)
	writeUleb128(buf, 0)

	emitLocalGet(buf, copyIdx)
	emitI32Const(buf, 1)
	buf.WriteByte(0x6a)
	emitLocalSet(buf, copyIdx)

	buf.WriteByte(0x0c)
	writeUleb128(buf, 0)
	buf.WriteByte(0x0b)
	buf.WriteByte(0x0b)

	emitLocalGet(buf, sbLocal)
	emitLocalGet(buf, newBuf)
	emitI32Store(buf, 0, 2)

	emitLocalGet(buf, sbLocal)
	emitLocalGet(buf, newCap)
	emitI32Store(buf, 8, 2)

	buf.WriteByte(0x0b)

	// Copy strPtr[0..strLen] into sb.buf[sb.len..]
	appendIdx := ctx.allocLocal(valTypeI32)
	emitI32Const(buf, 0)
	emitLocalSet(buf, appendIdx)

	buf.WriteByte(0x02)
	buf.WriteByte(valTypeVoid)
	buf.WriteByte(0x03)
	buf.WriteByte(valTypeVoid)

	emitLocalGet(buf, appendIdx)
	emitLocalGet(buf, strLen)
	buf.WriteByte(0x4e)
	buf.WriteByte(0x0d)
	writeUleb128(buf, 1)

	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 0, 2)
	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 4, 2)
	buf.WriteByte(0x6a)
	emitLocalGet(buf, appendIdx)
	buf.WriteByte(0x6a)

	emitLocalGet(buf, strPtr)
	emitLocalGet(buf, appendIdx)
	buf.WriteByte(0x6a)
	buf.WriteByte(0x2d)
	writeUleb128(buf, 0)
	writeUleb128(buf, 0)

	buf.WriteByte(0x3a)
	writeUleb128(buf, 0)
	writeUleb128(buf, 0)

	emitLocalGet(buf, appendIdx)
	emitI32Const(buf, 1)
	buf.WriteByte(0x6a)
	emitLocalSet(buf, appendIdx)

	buf.WriteByte(0x0c)
	writeUleb128(buf, 0)
	buf.WriteByte(0x0b)
	buf.WriteByte(0x0b)

	// sb.len = reqLocal
	emitLocalGet(buf, sbLocal)
	emitLocalGet(buf, reqLocal)
	emitI32Store(buf, 4, 2)

	return nil
}

func (ctx *fnContext) compileStringBuilderAppendInt(buf *bytes.Buffer, target Expr, arg Expr) error {
	intStr := &CallExpr{
		Callee: &IdentExpr{Name: "int_to_str"},
		Args:   []Expr{arg},
	}
	return ctx.compileStringBuilderAppend(buf, target, intStr)
}

func (ctx *fnContext) compileStringBuilderAppendChar(buf *bytes.Buffer, target Expr, arg Expr) error {
	sbLocal := ctx.allocLocal(valTypeI32)
	if err := ctx.compileExpr(buf, target); err != nil {
		return err
	}
	emitLocalSet(buf, sbLocal)

	chLocal := ctx.allocLocal(valTypeI32)
	if err := ctx.compileExpr(buf, arg); err != nil {
		return err
	}
	emitLocalSet(buf, chLocal)

	reqLocal := ctx.allocLocal(valTypeI32)
	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 4, 2)
	emitI32Const(buf, 1)
	buf.WriteByte(0x6a)
	emitLocalSet(buf, reqLocal)

	emitLocalGet(buf, reqLocal)
	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 8, 2)
	buf.WriteByte(0x4a)
	buf.WriteByte(0x04)
	buf.WriteByte(valTypeVoid)

	newCap := ctx.allocLocal(valTypeI32)
	emitLocalGet(buf, reqLocal)
	emitI32Const(buf, 2)
	buf.WriteByte(0x6c)
	emitLocalSet(buf, newCap)

	newBuf := ctx.allocLocal(valTypeI32)
	buf.WriteByte(0x23)
	writeUleb128(buf, 0)
	emitI32Const(buf, 7)
	buf.WriteByte(0x6a)
	emitI32Const(buf, -8)
	buf.WriteByte(0x71)
	emitLocalSet(buf, newBuf)

	emitLocalGet(buf, newBuf)
	emitLocalGet(buf, newCap)
	buf.WriteByte(0x6a)
	buf.WriteByte(0x24)
	writeUleb128(buf, 0)

	copyIdx := ctx.allocLocal(valTypeI32)
	emitI32Const(buf, 0)
	emitLocalSet(buf, copyIdx)

	buf.WriteByte(0x02)
	buf.WriteByte(valTypeVoid)
	buf.WriteByte(0x03)
	buf.WriteByte(valTypeVoid)

	emitLocalGet(buf, copyIdx)
	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 4, 2)
	buf.WriteByte(0x4e)
	buf.WriteByte(0x0d)
	writeUleb128(buf, 1)

	emitLocalGet(buf, newBuf)
	emitLocalGet(buf, copyIdx)
	buf.WriteByte(0x6a)

	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 0, 2)
	emitLocalGet(buf, copyIdx)
	buf.WriteByte(0x6a)
	buf.WriteByte(0x2d)
	writeUleb128(buf, 0)
	writeUleb128(buf, 0)

	buf.WriteByte(0x3a)
	writeUleb128(buf, 0)
	writeUleb128(buf, 0)

	emitLocalGet(buf, copyIdx)
	emitI32Const(buf, 1)
	buf.WriteByte(0x6a)
	emitLocalSet(buf, copyIdx)

	buf.WriteByte(0x0c)
	writeUleb128(buf, 0)
	buf.WriteByte(0x0b)
	buf.WriteByte(0x0b)

	emitLocalGet(buf, sbLocal)
	emitLocalGet(buf, newBuf)
	emitI32Store(buf, 0, 2)

	emitLocalGet(buf, sbLocal)
	emitLocalGet(buf, newCap)
	emitI32Store(buf, 8, 2)

	buf.WriteByte(0x0b)

	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 0, 2)
	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 4, 2)
	buf.WriteByte(0x6a)
	emitLocalGet(buf, chLocal)
	buf.WriteByte(0x3a)
	writeUleb128(buf, 0)
	writeUleb128(buf, 0)

	emitLocalGet(buf, sbLocal)
	emitLocalGet(buf, reqLocal)
	emitI32Store(buf, 4, 2)

	return nil
}

func (ctx *fnContext) compileStringBuilderToString(buf *bytes.Buffer, target Expr) error {
	sbLocal := ctx.allocLocal(valTypeI32)
	if err := ctx.compileExpr(buf, target); err != nil {
		return err
	}
	emitLocalSet(buf, sbLocal)

	resHdr := ctx.allocLocal(valTypeI32)
	ctx.emitAlloc(buf, 8, resHdr)

	emitLocalGet(buf, resHdr)
	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 0, 2)
	emitI32Store(buf, 0, 2)

	emitLocalGet(buf, resHdr)
	emitLocalGet(buf, sbLocal)
	emitI32Load(buf, 4, 2)
	emitI32Store(buf, 4, 2)

	emitLocalGet(buf, resHdr)
	return nil
}
