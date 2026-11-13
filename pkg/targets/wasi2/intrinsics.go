// File: pkg/targets/wasi2/intrinsics.go
//
// WASI 0.2 (Preview 2) WIT syscall intrinsics.

package wasi2

import (
	"bytes"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func emitMemoryCopy(buf *bytes.Buffer) {
	buf.WriteByte(0xfc)
	buf.WriteByte(0x0a)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
}

func emitLoad8U(buf *bytes.Buffer, offset uint32) {
	buf.WriteByte(0x2d)
	compiler.WriteUleb128(buf, 0)
	compiler.WriteUleb128(buf, offset)
}

func emitI64Load(buf *bytes.Buffer, offset uint32) {
	buf.WriteByte(0x29)
	compiler.WriteUleb128(buf, 3)
	compiler.WriteUleb128(buf, offset)
}

func emitI64Store(buf *bytes.Buffer, offset uint32) {
	buf.WriteByte(0x37)
	compiler.WriteUleb128(buf, 3)
	compiler.WriteUleb128(buf, offset)
}

// emitStoreNegatedErrCode loads a u8 error-code at (retLocal + offset) and
// leaves -(code + 1) on the stack so error-code 0 ('access') never aliases 0.
func emitStoreNegatedErrCode(buf *bytes.Buffer, retLocal, offset uint32) {
	compiler.EmitI32Const(buf, -1)
	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, offset)
	buf.WriteByte(0x6b) // -1 - err_code == -(err_code + 1)
}

func (w *wasi2Syscalls) compileWallNowMs(ctx *compiler.FnContext, buf *bytes.Buffer) error {
	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 16, retLocal)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:clocks/wall-clock@0.2.0", "now"))

	compiler.EmitLocalGet(buf, retLocal)
	emitI64Load(buf, 0)
	buf.WriteByte(0x42) // i64.const 1000
	compiler.WriteSleb128_64(buf, 1000)
	buf.WriteByte(0x7e) // i64.mul

	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 8, 2)
	buf.WriteByte(0xad) // i64.extend_i32_u
	buf.WriteByte(0x42) // i64.const 1_000_000
	compiler.WriteSleb128_64(buf, 1000000)
	buf.WriteByte(0x80) // i64.div_u
	buf.WriteByte(0x7c) // i64.add
	return nil
}

func (w *wasi2Syscalls) compileSleepMs(ctx *compiler.FnContext, buf *bytes.Buffer, args []compiler.Expr) error {
	if len(args) != 1 {
		return nil
	}
	nsLocal := ctx.AllocLocal(compiler.ValTypeI64)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	buf.WriteByte(0xad) // i64.extend_i32_u
	buf.WriteByte(0x42)
	compiler.WriteSleb128_64(buf, 1000000)
	buf.WriteByte(0x7e)
	buf.WriteByte(0x21) // local.set
	compiler.WriteUleb128(buf, nsLocal)

	startLocal := ctx.AllocLocal(compiler.ValTypeI64)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:clocks/monotonic-clock@0.2.0", "now"))
	buf.WriteByte(0x21)
	compiler.WriteUleb128(buf, startLocal)

	buf.WriteByte(0x02) // block
	buf.WriteByte(compiler.ValTypeVoid)
	buf.WriteByte(0x03) // loop
	buf.WriteByte(compiler.ValTypeVoid)

	compiler.EmitCallIdx(buf, w.importIndex("wasi:clocks/monotonic-clock@0.2.0", "now"))
	buf.WriteByte(0x20)
	compiler.WriteUleb128(buf, startLocal)
	buf.WriteByte(0x7d) // i64.sub
	buf.WriteByte(0x20)
	compiler.WriteUleb128(buf, nsLocal)
	buf.WriteByte(0x59) // i64.ge_s
	buf.WriteByte(0x0d) // br_if exit
	compiler.WriteUleb128(buf, 1)
	buf.WriteByte(0x0c) // br loop
	compiler.WriteUleb128(buf, 0)
	buf.WriteByte(0x0b)
	buf.WriteByte(0x0b)
	return nil
}

func (w *wasi2Syscalls) compileRandomU64(ctx *compiler.FnContext, buf *bytes.Buffer) error {
	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 16, retLocal)
	buf.WriteByte(0x42)
	compiler.WriteSleb128_64(buf, 8)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:random/random@0.2.0", "get-random-bytes"))
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 0, 2)
	emitI64Load(buf, 0)
	return nil
}

// compileOsArgs lowers os_args() via wasi:cli/environment@0.2.0#get-arguments.
// Canonical ABI returns { ptr, len } of { str_ptr: i32, str_len: i32 } pairs,
// which matches Ube's []string backing buffer directly.
func (w *wasi2Syscalls) compileOsArgs(ctx *compiler.FnContext, buf *bytes.Buffer) error {
	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 8, retLocal)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:cli/environment@0.2.0", "get-arguments"))

	sliceLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 12, sliceLocal)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitI32Store(buf, 0, 2)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitI32Store(buf, 4, 2)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitI32Store(buf, 8, 2)
	compiler.EmitLocalGet(buf, sliceLocal)
	return nil
}

// compileOsEnviron lowers os_environ() via wasi:cli/environment@0.2.0#get-environment,
// joining each (key, val) tuple into "key=val" strings.
func (w *wasi2Syscalls) compileOsEnviron(ctx *compiler.FnContext, buf *bytes.Buffer) error {
	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 8, retLocal)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:cli/environment@0.2.0", "get-environment"))

	listPtr := ctx.AllocLocal(compiler.ValTypeI32)
	countLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalSet(buf, listPtr)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalSet(buf, countLocal)

	hdrSize := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, countLocal)
	compiler.EmitI32Const(buf, 8)
	buf.WriteByte(0x6c)
	compiler.EmitLocalSet(buf, hdrSize)

	headersLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAllocDyn(buf, hdrSize, headersLocal)

	idxLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, 0)
	compiler.EmitLocalSet(buf, idxLocal)

	entryPtr := ctx.AllocLocal(compiler.ValTypeI32)
	kLen := ctx.AllocLocal(compiler.ValTypeI32)
	vLen := ctx.AllocLocal(compiler.ValTypeI32)
	totLen := ctx.AllocLocal(compiler.ValTypeI32)
	strBuf := ctx.AllocLocal(compiler.ValTypeI32)

	buf.WriteByte(0x02) // block
	buf.WriteByte(compiler.ValTypeVoid)
	buf.WriteByte(0x03) // loop
	buf.WriteByte(compiler.ValTypeVoid)

	compiler.EmitLocalGet(buf, idxLocal)
	compiler.EmitLocalGet(buf, countLocal)
	buf.WriteByte(0x4f) // i32.ge_u
	buf.WriteByte(0x0d)
	compiler.WriteUleb128(buf, 1)

	// entryPtr = listPtr + idx * 16
	compiler.EmitLocalGet(buf, listPtr)
	compiler.EmitLocalGet(buf, idxLocal)
	compiler.EmitI32Const(buf, 16)
	buf.WriteByte(0x6c)
	buf.WriteByte(0x6a)
	compiler.EmitLocalSet(buf, entryPtr)

	compiler.EmitLocalGet(buf, entryPtr)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalSet(buf, kLen)

	compiler.EmitLocalGet(buf, entryPtr)
	compiler.EmitI32Load(buf, 12, 2)
	compiler.EmitLocalSet(buf, vLen)

	// totLen = kLen + 1 + vLen
	compiler.EmitLocalGet(buf, kLen)
	compiler.EmitI32Const(buf, 1)
	buf.WriteByte(0x6a)
	compiler.EmitLocalGet(buf, vLen)
	buf.WriteByte(0x6a)
	compiler.EmitLocalSet(buf, totLen)

	ctx.EmitAllocDyn(buf, totLen, strBuf)

	// copy key
	compiler.EmitLocalGet(buf, strBuf)
	compiler.EmitLocalGet(buf, entryPtr)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalGet(buf, kLen)
	emitMemoryCopy(buf)

	// write '='
	compiler.EmitLocalGet(buf, strBuf)
	compiler.EmitLocalGet(buf, kLen)
	buf.WriteByte(0x6a)
	compiler.EmitI32Const(buf, 61)
	buf.WriteByte(0x3a) // i32.store8
	compiler.WriteUleb128(buf, 0)
	compiler.WriteUleb128(buf, 0)

	// copy val
	compiler.EmitLocalGet(buf, strBuf)
	compiler.EmitLocalGet(buf, kLen)
	buf.WriteByte(0x6a)
	compiler.EmitI32Const(buf, 1)
	buf.WriteByte(0x6a)
	compiler.EmitLocalGet(buf, entryPtr)
	compiler.EmitI32Load(buf, 8, 2)
	compiler.EmitLocalGet(buf, vLen)
	emitMemoryCopy(buf)

	// headers[idx] = { strBuf, totLen }
	compiler.EmitLocalGet(buf, headersLocal)
	compiler.EmitLocalGet(buf, idxLocal)
	compiler.EmitI32Const(buf, 8)
	buf.WriteByte(0x6c)
	buf.WriteByte(0x6a)
	compiler.EmitLocalGet(buf, strBuf)
	compiler.EmitI32Store(buf, 0, 2)

	compiler.EmitLocalGet(buf, headersLocal)
	compiler.EmitLocalGet(buf, idxLocal)
	compiler.EmitI32Const(buf, 8)
	buf.WriteByte(0x6c)
	buf.WriteByte(0x6a)
	compiler.EmitLocalGet(buf, totLen)
	compiler.EmitI32Store(buf, 4, 2)

	compiler.EmitLocalGet(buf, idxLocal)
	compiler.EmitI32Const(buf, 1)
	buf.WriteByte(0x6a)
	compiler.EmitLocalSet(buf, idxLocal)
	buf.WriteByte(0x0c)
	compiler.WriteUleb128(buf, 0)
	buf.WriteByte(0x0b)
	buf.WriteByte(0x0b)

	sliceLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 12, sliceLocal)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitLocalGet(buf, headersLocal)
	compiler.EmitI32Store(buf, 0, 2)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitLocalGet(buf, countLocal)
	compiler.EmitI32Store(buf, 4, 2)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitLocalGet(buf, countLocal)
	compiler.EmitI32Store(buf, 8, 2)
	compiler.EmitLocalGet(buf, sliceLocal)
	return nil
}

func (w *wasi2Syscalls) compileSingleDrop(ctx *compiler.FnContext, buf *bytes.Buffer, mod, name string, args []compiler.Expr) error {
	if len(args) != 1 {
		return nil
	}
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitCallIdx(buf, w.importIndex(mod, name))
	return nil
}

func (w *wasi2Syscalls) compileSingleDropResult(ctx *compiler.FnContext, buf *bytes.Buffer, mod, name string, args []compiler.Expr) error {
	if err := w.compileSingleDrop(ctx, buf, mod, name, args); err != nil {
		return err
	}
	compiler.EmitI32Const(buf, 0)
	return nil
}

// compileStreamWrite lowers wasi2_stream_write_raw(handle, data []u8) -> i32.
// Chunks writes at 4096 bytes so large slices do not trap in blocking-write-and-flush.
// Returns 0 on success, -1 if stream closed, -2 on stream error.
func (w *wasi2Syscalls) compileStreamWrite(ctx *compiler.FnContext, buf *bytes.Buffer, args []compiler.Expr) error {
	if len(args) != 2 {
		return nil
	}
	handleLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, handleLocal)

	sliceLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[1]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, sliceLocal)

	ptrLocal := ctx.AllocLocal(compiler.ValTypeI32)
	lenLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalSet(buf, ptrLocal)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalSet(buf, lenLocal)

	return w.emitStreamWritePtrLen(ctx, buf, handleLocal, ptrLocal, lenLocal)
}

func (w *wasi2Syscalls) emitStreamWritePtrLen(ctx *compiler.FnContext, buf *bytes.Buffer, handleLocal, ptrLocal, lenLocal uint32) error {
	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 16, retLocal)

	offLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, 0)
	compiler.EmitLocalSet(buf, offLocal)

	statusLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, 0)
	compiler.EmitLocalSet(buf, statusLocal)

	chunkLocal := ctx.AllocLocal(compiler.ValTypeI32)

	buf.WriteByte(0x02) // block exit
	buf.WriteByte(compiler.ValTypeVoid)
	buf.WriteByte(0x03) // loop
	buf.WriteByte(compiler.ValTypeVoid)

	// if off >= len { break }
	compiler.EmitLocalGet(buf, offLocal)
	compiler.EmitLocalGet(buf, lenLocal)
	buf.WriteByte(0x4e) // i32.ge_s
	buf.WriteByte(0x0d)
	compiler.WriteUleb128(buf, 1)

	// chunk = min(len - off, 4096)
	compiler.EmitLocalGet(buf, lenLocal)
	compiler.EmitLocalGet(buf, offLocal)
	buf.WriteByte(0x6b)
	compiler.EmitLocalSet(buf, chunkLocal)

	compiler.EmitLocalGet(buf, chunkLocal)
	compiler.EmitI32Const(buf, 4096)
	buf.WriteByte(0x4a) // i32.gt_s
	buf.WriteByte(0x04)
	bodyVoid := byte(compiler.ValTypeVoid)
	buf.WriteByte(bodyVoid)
	compiler.EmitI32Const(buf, 4096)
	compiler.EmitLocalSet(buf, chunkLocal)
	buf.WriteByte(0x0b)

	// blocking-write-and-flush(handle, ptr + off, chunk, retLocal)
	compiler.EmitLocalGet(buf, handleLocal)
	compiler.EmitLocalGet(buf, ptrLocal)
	compiler.EmitLocalGet(buf, offLocal)
	buf.WriteByte(0x6a)
	compiler.EmitLocalGet(buf, chunkLocal)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/streams@0.2.0", "[method]output-stream.blocking-write-and-flush"))

	// check retLocal[0]
	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 0)
	buf.WriteByte(0x04) // if err
	buf.WriteByte(compiler.ValTypeVoid)
	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 4)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04) // if last-operation-failed
	buf.WriteByte(compiler.ValTypeVoid)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 8, 2)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/error@0.2.0", "[resource-drop]error"))
	compiler.EmitI32Const(buf, -2)
	compiler.EmitLocalSet(buf, statusLocal)
	buf.WriteByte(0x05) // else closed
	compiler.EmitI32Const(buf, -1)
	compiler.EmitLocalSet(buf, statusLocal)
	buf.WriteByte(0x0b)
	buf.WriteByte(0x0c) // br exit
	compiler.WriteUleb128(buf, 2)
	buf.WriteByte(0x0b)

	compiler.EmitLocalGet(buf, offLocal)
	compiler.EmitLocalGet(buf, chunkLocal)
	buf.WriteByte(0x6a)
	compiler.EmitLocalSet(buf, offLocal)
	buf.WriteByte(0x0c)
	compiler.WriteUleb128(buf, 0)
	buf.WriteByte(0x0b)
	buf.WriteByte(0x0b)

	compiler.EmitLocalGet(buf, statusLocal)
	return nil
}

// compileStreamRead lowers wasi2_stream_read_raw(handle, mut buf []u8) -> i32.
// Returns bytes read >= 0, -1 if closed, -2 on stream error.
func (w *wasi2Syscalls) compileStreamRead(ctx *compiler.FnContext, buf *bytes.Buffer, args []compiler.Expr) error {
	if len(args) != 2 {
		return nil
	}
	handleLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, handleLocal)

	sliceLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[1]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, sliceLocal)

	bufPtr := ctx.AllocLocal(compiler.ValTypeI32)
	bufLen := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalSet(buf, bufPtr)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalSet(buf, bufLen)

	compiler.EmitLocalGet(buf, bufLen)
	compiler.EmitI32Const(buf, 0)
	buf.WriteByte(0x4c) // i32.le_s
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, 0)
	buf.WriteByte(0x05)

	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 16, retLocal)
	compiler.EmitLocalGet(buf, handleLocal)
	compiler.EmitLocalGet(buf, bufLen)
	buf.WriteByte(0xad) // i64.extend_i32_u
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/streams@0.2.0", "[method]input-stream.blocking-read"))

	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 0)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)

	copyLen := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 8, 2)
	compiler.EmitLocalSet(buf, copyLen)

	compiler.EmitLocalGet(buf, copyLen)
	compiler.EmitLocalGet(buf, bufLen)
	buf.WriteByte(0x4a) // i32.gt_s
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeVoid)
	compiler.EmitLocalGet(buf, bufLen)
	compiler.EmitLocalSet(buf, copyLen)
	buf.WriteByte(0x0b)

	compiler.EmitLocalGet(buf, bufPtr)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalGet(buf, copyLen)
	emitMemoryCopy(buf)
	compiler.EmitLocalGet(buf, copyLen)

	buf.WriteByte(0x05) // err
	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 4)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 8, 2)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/error@0.2.0", "[resource-drop]error"))
	compiler.EmitI32Const(buf, -2)
	buf.WriteByte(0x05)
	compiler.EmitI32Const(buf, -1)
	buf.WriteByte(0x0b)

	buf.WriteByte(0x0b)
	buf.WriteByte(0x0b)
	return nil
}

// compilePreopenDir returns the first preopened directory descriptor handle,
// caching it in static linear memory so repeated fs calls do not reallocate.
func (w *wasi2Syscalls) compilePreopenDir(ctx *compiler.FnContext, buf *bytes.Buffer) error {
	compiler.EmitI32Const(buf, int32(w.preopenCacheOff))
	compiler.EmitI32Load(buf, 4, 2)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, int32(w.preopenCacheOff))
	compiler.EmitI32Load(buf, 0, 2)
	buf.WriteByte(0x05)

	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 8, retLocal)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:filesystem/preopens@0.2.0", "get-directories"))

	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, -1)
	buf.WriteByte(0x05)

	fdLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitI32Load(buf, 0, 2) // first tuple's descriptor handle
	compiler.EmitLocalSet(buf, fdLocal)

	compiler.EmitI32Const(buf, int32(w.preopenCacheOff))
	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitI32Store(buf, 0, 2)
	compiler.EmitI32Const(buf, int32(w.preopenCacheOff))
	compiler.EmitI32Const(buf, 1)
	compiler.EmitI32Store(buf, 4, 2)
	compiler.EmitLocalGet(buf, fdLocal)
	buf.WriteByte(0x0b)

	buf.WriteByte(0x0b)
	return nil
}

func (w *wasi2Syscalls) compileOpenAt(ctx *compiler.FnContext, buf *bytes.Buffer, args []compiler.Expr) error {
	if len(args) != 5 {
		return nil
	}
	dirLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, dirLocal)

	pathHdr := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[1]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, pathHdr)

	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 8, retLocal)

	compiler.EmitLocalGet(buf, dirLocal)
	if err := ctx.CompileExpr(buf, args[2]); err != nil { // path_flags
		return err
	}
	compiler.EmitLocalGet(buf, pathHdr)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalGet(buf, pathHdr)
	compiler.EmitI32Load(buf, 4, 2)
	if err := ctx.CompileExpr(buf, args[3]); err != nil { // open_flags
		return err
	}
	if err := ctx.CompileExpr(buf, args[4]); err != nil { // desc_flags
		return err
	}
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:filesystem/types@0.2.0", "[method]descriptor.open-at"))

	fdLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 0)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalSet(buf, fdLocal)

	// Reset fdOffsets[fd] = 0 if 0 <= fd < maxFdSlots
	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitI32Const(buf, maxFdSlots)
	buf.WriteByte(0x49) // i32.lt_u
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeVoid)
	compiler.EmitI32Const(buf, int32(w.fdOffsetsBase))
	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitI32Const(buf, 8)
	buf.WriteByte(0x6c)
	buf.WriteByte(0x6a)
	buf.WriteByte(0x42)
	compiler.WriteSleb128_64(buf, 0)
	emitI64Store(buf, 0)
	buf.WriteByte(0x0b)

	compiler.EmitLocalGet(buf, fdLocal)
	buf.WriteByte(0x05)
	emitStoreNegatedErrCode(buf, retLocal, 4)
	buf.WriteByte(0x0b)
	return nil
}

func (w *wasi2Syscalls) compileDescRead(ctx *compiler.FnContext, buf *bytes.Buffer, args []compiler.Expr) error {
	if len(args) != 3 {
		return nil
	}
	fdLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, fdLocal)

	sliceLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[1]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, sliceLocal)

	offLocal := ctx.AllocLocal(compiler.ValTypeI64)
	if err := ctx.CompileExpr(buf, args[2]); err != nil {
		return err
	}
	buf.WriteByte(0x21)
	compiler.WriteUleb128(buf, offLocal)

	return w.emitDescReadAtOffset(ctx, buf, fdLocal, sliceLocal, offLocal)
}

func (w *wasi2Syscalls) emitDescReadAtOffset(ctx *compiler.FnContext, buf *bytes.Buffer, fdLocal, sliceLocal, offLocal uint32) error {
	bufPtr := ctx.AllocLocal(compiler.ValTypeI32)
	bufLen := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalSet(buf, bufPtr)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalSet(buf, bufLen)

	compiler.EmitLocalGet(buf, bufLen)
	compiler.EmitI32Const(buf, 0)
	buf.WriteByte(0x4c)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, 0)
	buf.WriteByte(0x05)

	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 16, retLocal)
	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitLocalGet(buf, bufLen)
	buf.WriteByte(0xad) // i64.extend_i32_u
	buf.WriteByte(0x20)
	compiler.WriteUleb128(buf, offLocal)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:filesystem/types@0.2.0", "[method]descriptor.read"))

	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 0)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)

	copyLen := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 8, 2)
	compiler.EmitLocalSet(buf, copyLen)

	compiler.EmitLocalGet(buf, copyLen)
	compiler.EmitLocalGet(buf, bufLen)
	buf.WriteByte(0x4a)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeVoid)
	compiler.EmitLocalGet(buf, bufLen)
	compiler.EmitLocalSet(buf, copyLen)
	buf.WriteByte(0x0b)

	compiler.EmitLocalGet(buf, bufPtr)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalGet(buf, copyLen)
	emitMemoryCopy(buf)
	compiler.EmitLocalGet(buf, copyLen)

	buf.WriteByte(0x05)
	emitStoreNegatedErrCode(buf, retLocal, 4)
	buf.WriteByte(0x0b)

	buf.WriteByte(0x0b)
	return nil
}

func (w *wasi2Syscalls) compileDescWrite(ctx *compiler.FnContext, buf *bytes.Buffer, args []compiler.Expr) error {
	if len(args) != 3 {
		return nil
	}
	fdLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, fdLocal)

	sliceLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[1]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, sliceLocal)

	offLocal := ctx.AllocLocal(compiler.ValTypeI64)
	if err := ctx.CompileExpr(buf, args[2]); err != nil {
		return err
	}
	buf.WriteByte(0x21)
	compiler.WriteUleb128(buf, offLocal)

	dataPtr := ctx.AllocLocal(compiler.ValTypeI32)
	dataLen := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalSet(buf, dataPtr)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalSet(buf, dataLen)

	return w.emitDescWritePtrLenAtOffset(ctx, buf, fdLocal, dataPtr, dataLen, offLocal)
}

func (w *wasi2Syscalls) emitDescWritePtrLenAtOffset(ctx *compiler.FnContext, buf *bytes.Buffer, fdLocal, dataPtr, dataLen, offLocal uint32) error {
	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 16, retLocal)
	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitLocalGet(buf, dataPtr)
	compiler.EmitLocalGet(buf, dataLen)
	buf.WriteByte(0x20)
	compiler.WriteUleb128(buf, offLocal)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:filesystem/types@0.2.0", "[method]descriptor.write"))

	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 0)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, retLocal)
	emitI64Load(buf, 8)
	buf.WriteByte(0xa7) // i32.wrap_i64
	buf.WriteByte(0x05)
	emitStoreNegatedErrCode(buf, retLocal, 8)
	buf.WriteByte(0x0b)
	return nil
}

func (w *wasi2Syscalls) compileDescStat(ctx *compiler.FnContext, buf *bytes.Buffer, byPath bool, args []compiler.Expr) error {
	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 112, retLocal)
	outBufLocal := ctx.AllocLocal(compiler.ValTypeI32)

	if byPath {
		if len(args) != 3 {
			return nil
		}
		dirLocal := ctx.AllocLocal(compiler.ValTypeI32)
		if err := ctx.CompileExpr(buf, args[0]); err != nil {
			return err
		}
		compiler.EmitLocalSet(buf, dirLocal)
		pathHdr := ctx.AllocLocal(compiler.ValTypeI32)
		if err := ctx.CompileExpr(buf, args[1]); err != nil {
			return err
		}
		compiler.EmitLocalSet(buf, pathHdr)
		if err := ctx.CompileExpr(buf, args[2]); err != nil {
			return err
		}
		compiler.EmitLocalSet(buf, outBufLocal)

		compiler.EmitLocalGet(buf, dirLocal)
		compiler.EmitI32Const(buf, 1) // symlink-follow
		compiler.EmitLocalGet(buf, pathHdr)
		compiler.EmitI32Load(buf, 0, 2)
		compiler.EmitLocalGet(buf, pathHdr)
		compiler.EmitI32Load(buf, 4, 2)
		compiler.EmitLocalGet(buf, retLocal)
		compiler.EmitCallIdx(buf, w.importIndex("wasi:filesystem/types@0.2.0", "[method]descriptor.stat-at"))
	} else {
		if len(args) != 2 {
			return nil
		}
		fdLocal := ctx.AllocLocal(compiler.ValTypeI32)
		if err := ctx.CompileExpr(buf, args[0]); err != nil {
			return err
		}
		compiler.EmitLocalSet(buf, fdLocal)
		if err := ctx.CompileExpr(buf, args[1]); err != nil {
			return err
		}
		compiler.EmitLocalSet(buf, outBufLocal)

		compiler.EmitLocalGet(buf, fdLocal)
		compiler.EmitLocalGet(buf, retLocal)
		compiler.EmitCallIdx(buf, w.importIndex("wasi:filesystem/types@0.2.0", "[method]descriptor.stat"))
	}

	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 0)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)

	// out[0] = i64(type)
	compiler.EmitLocalGet(buf, outBufLocal)
	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 8)
	buf.WriteByte(0xad)
	emitI64Store(buf, 0)

	// out[1] = link_count
	compiler.EmitLocalGet(buf, outBufLocal)
	compiler.EmitLocalGet(buf, retLocal)
	emitI64Load(buf, 16)
	emitI64Store(buf, 8)

	// out[2] = size
	compiler.EmitLocalGet(buf, outBufLocal)
	compiler.EmitLocalGet(buf, retLocal)
	emitI64Load(buf, 24)
	emitI64Store(buf, 16)

	compiler.EmitI32Const(buf, 0)
	buf.WriteByte(0x05)
	emitStoreNegatedErrCode(buf, retLocal, 8)
	buf.WriteByte(0x0b)
	return nil
}

func (w *wasi2Syscalls) compileDescSync(ctx *compiler.FnContext, buf *bytes.Buffer, args []compiler.Expr) error {
	if len(args) != 1 {
		return nil
	}
	fdLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, fdLocal)

	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 8, retLocal)
	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:filesystem/types@0.2.0", "[method]descriptor.sync"))

	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 0)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, 0)
	buf.WriteByte(0x05)
	emitStoreNegatedErrCode(buf, retLocal, 1)
	buf.WriteByte(0x0b)
	return nil
}

func (w *wasi2Syscalls) compileDescPathOp(ctx *compiler.FnContext, buf *bytes.Buffer, method string, args []compiler.Expr) error {
	if len(args) != 2 {
		return nil
	}
	dirLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, dirLocal)

	pathHdr := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[1]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, pathHdr)

	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 8, retLocal)
	compiler.EmitLocalGet(buf, dirLocal)
	compiler.EmitLocalGet(buf, pathHdr)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalGet(buf, pathHdr)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:filesystem/types@0.2.0", method))

	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 0)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, 0)
	buf.WriteByte(0x05)
	emitStoreNegatedErrCode(buf, retLocal, 1)
	buf.WriteByte(0x0b)
	return nil
}

func (w *wasi2Syscalls) compileViaStream(ctx *compiler.FnContext, buf *bytes.Buffer, method string, args []compiler.Expr) error {
	if len(args) != 2 {
		return nil
	}
	fdLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, fdLocal)

	offLocal := ctx.AllocLocal(compiler.ValTypeI64)
	if err := ctx.CompileExpr(buf, args[1]); err != nil {
		return err
	}
	buf.WriteByte(0x21)
	compiler.WriteUleb128(buf, offLocal)

	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 8, retLocal)
	compiler.EmitLocalGet(buf, fdLocal)
	buf.WriteByte(0x20)
	compiler.WriteUleb128(buf, offLocal)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:filesystem/types@0.2.0", method))

	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 0)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 4, 2)
	buf.WriteByte(0x05)
	emitStoreNegatedErrCode(buf, retLocal, 4)
	buf.WriteByte(0x0b)
	return nil
}

// compileLegacyFdWrite lowers wasi_fd_write_raw / wasi_fd_write_string_raw
// for shared std/io callers: fd 1/2 route to stdout/stderr streams; fd >= 3
// route to descriptor.write using the per-fd static offset table.
func (w *wasi2Syscalls) compileLegacyFdWrite(ctx *compiler.FnContext, buf *bytes.Buffer, isStr bool, args []compiler.Expr) error {
	if len(args) != 2 {
		return nil
	}
	fdLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, fdLocal)

	hdrLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[1]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, hdrLocal)

	dataPtr := ctx.AllocLocal(compiler.ValTypeI32)
	dataLen := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, hdrLocal)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalSet(buf, dataPtr)
	compiler.EmitLocalGet(buf, hdrLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalSet(buf, dataLen)

	_ = isStr

	streamLocal := ctx.AllocLocal(compiler.ValTypeI32)
	resLocal := ctx.AllocLocal(compiler.ValTypeI32)

	// if fd == 1 || fd == 2
	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitI32Const(buf, 1)
	buf.WriteByte(0x46)
	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitI32Const(buf, 2)
	buf.WriteByte(0x46)
	buf.WriteByte(0x72) // i32.or
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)

	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitI32Const(buf, 1)
	buf.WriteByte(0x46)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:cli/stdout@0.2.0", "get-stdout"))
	buf.WriteByte(0x05)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:cli/stderr@0.2.0", "get-stderr"))
	buf.WriteByte(0x0b)
	compiler.EmitLocalSet(buf, streamLocal)

	if err := w.emitStreamWritePtrLen(ctx, buf, streamLocal, dataPtr, dataLen); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, resLocal)
	compiler.EmitLocalGet(buf, streamLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/streams@0.2.0", "[resource-drop]output-stream"))
	compiler.EmitLocalGet(buf, resLocal)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, dataLen)
	buf.WriteByte(0x05)
	compiler.EmitLocalGet(buf, resLocal)
	buf.WriteByte(0x0b)

	buf.WriteByte(0x05) // descriptor path

	offLocal := ctx.AllocLocal(compiler.ValTypeI64)
	compiler.EmitI32Const(buf, int32(w.fdOffsetsBase))
	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitI32Const(buf, maxFdSlots-1)
	buf.WriteByte(0x71)
	compiler.EmitI32Const(buf, 8)
	buf.WriteByte(0x6c)
	buf.WriteByte(0x6a)
	emitI64Load(buf, 0)
	buf.WriteByte(0x21)
	compiler.WriteUleb128(buf, offLocal)

	if err := w.emitDescWritePtrLenAtOffset(ctx, buf, fdLocal, dataPtr, dataLen, offLocal); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, resLocal)

	compiler.EmitLocalGet(buf, resLocal)
	compiler.EmitI32Const(buf, 0)
	buf.WriteByte(0x4e) // >= 0
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeVoid)
	compiler.EmitI32Const(buf, int32(w.fdOffsetsBase))
	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitI32Const(buf, maxFdSlots-1)
	buf.WriteByte(0x71)
	compiler.EmitI32Const(buf, 8)
	buf.WriteByte(0x6c)
	buf.WriteByte(0x6a)
	buf.WriteByte(0x20)
	compiler.WriteUleb128(buf, offLocal)
	compiler.EmitLocalGet(buf, resLocal)
	buf.WriteByte(0xac)
	buf.WriteByte(0x7c)
	emitI64Store(buf, 0)
	buf.WriteByte(0x0b)

	compiler.EmitLocalGet(buf, resLocal)
	buf.WriteByte(0x0b)
	return nil
}

func (w *wasi2Syscalls) compileLegacyFdRead(ctx *compiler.FnContext, buf *bytes.Buffer, args []compiler.Expr) error {
	if len(args) != 2 {
		return nil
	}
	fdLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, fdLocal)

	sliceLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[1]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, sliceLocal)

	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)

	// stdin stream read
	stdinLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:cli/stdin@0.2.0", "get-stdin"))
	compiler.EmitLocalSet(buf, stdinLocal)

	bufPtr := ctx.AllocLocal(compiler.ValTypeI32)
	bufLen := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalSet(buf, bufPtr)
	compiler.EmitLocalGet(buf, sliceLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalSet(buf, bufLen)

	retLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 16, retLocal)
	compiler.EmitLocalGet(buf, stdinLocal)
	compiler.EmitLocalGet(buf, bufLen)
	buf.WriteByte(0xad)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/streams@0.2.0", "[method]input-stream.blocking-read"))

	compiler.EmitLocalGet(buf, stdinLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/streams@0.2.0", "[resource-drop]input-stream"))

	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 0)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	copyLen := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 8, 2)
	compiler.EmitLocalSet(buf, copyLen)
	compiler.EmitLocalGet(buf, copyLen)
	compiler.EmitLocalGet(buf, bufLen)
	buf.WriteByte(0x4a)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeVoid)
	compiler.EmitLocalGet(buf, bufLen)
	compiler.EmitLocalSet(buf, copyLen)
	buf.WriteByte(0x0b)
	compiler.EmitLocalGet(buf, bufPtr)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalGet(buf, copyLen)
	emitMemoryCopy(buf)
	compiler.EmitLocalGet(buf, copyLen)
	buf.WriteByte(0x05)
	compiler.EmitLocalGet(buf, retLocal)
	emitLoad8U(buf, 4)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, retLocal)
	compiler.EmitI32Load(buf, 8, 2)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/error@0.2.0", "[resource-drop]error"))
	compiler.EmitI32Const(buf, -1)
	buf.WriteByte(0x05)
	compiler.EmitI32Const(buf, 0) // closed -> 0 bytes (EOF)
	buf.WriteByte(0x0b)
	buf.WriteByte(0x0b)

	buf.WriteByte(0x05) // descriptor read

	offLocal := ctx.AllocLocal(compiler.ValTypeI64)
	compiler.EmitI32Const(buf, int32(w.fdOffsetsBase))
	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitI32Const(buf, maxFdSlots-1)
	buf.WriteByte(0x71)
	compiler.EmitI32Const(buf, 8)
	buf.WriteByte(0x6c)
	buf.WriteByte(0x6a)
	emitI64Load(buf, 0)
	buf.WriteByte(0x21)
	compiler.WriteUleb128(buf, offLocal)

	resLocal := ctx.AllocLocal(compiler.ValTypeI32)
	if err := w.emitDescReadAtOffset(ctx, buf, fdLocal, sliceLocal, offLocal); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, resLocal)

	compiler.EmitLocalGet(buf, resLocal)
	compiler.EmitI32Const(buf, 0)
	buf.WriteByte(0x4a)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeVoid)
	compiler.EmitI32Const(buf, int32(w.fdOffsetsBase))
	compiler.EmitLocalGet(buf, fdLocal)
	compiler.EmitI32Const(buf, maxFdSlots-1)
	buf.WriteByte(0x71)
	compiler.EmitI32Const(buf, 8)
	buf.WriteByte(0x6c)
	buf.WriteByte(0x6a)
	buf.WriteByte(0x20)
	compiler.WriteUleb128(buf, offLocal)
	compiler.EmitLocalGet(buf, resLocal)
	buf.WriteByte(0xac)
	buf.WriteByte(0x7c)
	emitI64Store(buf, 0)
	buf.WriteByte(0x0b)

	compiler.EmitLocalGet(buf, resLocal)
	buf.WriteByte(0x0b)
	return nil
}
