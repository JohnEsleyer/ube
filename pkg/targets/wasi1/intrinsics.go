// File: pkg/targets/wasi1/intrinsics.go
//
// WASI Preview 1 syscall intrinsics. These lower builtin calls (wasi_*_raw,
// time_now_ms, os_args, random_get_u64, ...) into wasi_snapshot_preview1
// imports plus scratch-memory plumbing.

package wasi1

import (
	"bytes"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

// compileClockNow emits clock_time_get(clockID, precision, out) and leaves
// the resulting i64 nanosecond timestamp on the stack. When millis is true
// the value is divided by 1_000_000 to produce milliseconds.
func (w *syscalls) compileClockNow(ctx *compiler.FnContext, buf *bytes.Buffer, clockID int32, millis bool) error {
	compiler.EmitI32Const(buf, clockID)
	buf.WriteByte(0x42) // i64.const precision (1ns)
	compiler.WriteSleb128_64(buf, 1)
	compiler.EmitI32Const(buf, scratchSeekPos)
	compiler.EmitCallIdx(buf, w.importIndex("clock_time_get"))
	buf.WriteByte(0x1a) // drop errno

	compiler.EmitI32Const(buf, scratchSeekPos)
	buf.WriteByte(0x29) // i64.load
	compiler.WriteUleb128(buf, 3)
	compiler.WriteUleb128(buf, 0)
	if millis {
		buf.WriteByte(0x42) // i64.const 1_000_000
		compiler.WriteSleb128_64(buf, 1000000)
		buf.WriteByte(0x80) // i64.div_u
	}
	return nil
}

// compileExitProcess calls WASI proc_exit and marks the following code
// unreachable. proc_exit is noreturn, so the unreachable instruction keeps
// the enclosing block well-typed.
func (w *syscalls) compileExitProcess(ctx *compiler.FnContext, buf *bytes.Buffer, args []compiler.Expr) error {
	if len(args) != 1 {
		return nil
	}
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitCallIdx(buf, w.importIndex("proc_exit"))
	buf.WriteByte(0x00) // unreachable
	return nil
}

// compileSleepMs busy-waits on CLOCK_MONOTONIC. WASI preview1 exposes no
// portable sleep syscall, but poll_oneoff is overkill for the short delays
// used by benchmarks and tests; a monotonic spin is deterministic and keeps
// the runtime surface small.
func (w *syscalls) compileSleepMs(ctx *compiler.FnContext, buf *bytes.Buffer, args []compiler.Expr) error {
	if len(args) != 1 {
		return nil
	}
	msLocal := ctx.AllocLocal(compiler.ValTypeI64)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	buf.WriteByte(0xad) // i64.extend_i32_s
	buf.WriteByte(0x42) // i64.const 1_000_000
	compiler.WriteSleb128_64(buf, 1000000)
	buf.WriteByte(0x7e) // i64.mul
	buf.WriteByte(0x21)
	compiler.WriteUleb128(buf, msLocal)

	startLocal := ctx.AllocLocal(compiler.ValTypeI64)
	compiler.EmitI32Const(buf, 1) // CLOCK_MONOTONIC
	buf.WriteByte(0x42)
	compiler.WriteSleb128_64(buf, 1)
	compiler.EmitI32Const(buf, scratchSeekPos)
	compiler.EmitCallIdx(buf, w.importIndex("clock_time_get"))
	buf.WriteByte(0x1a)
	compiler.EmitI32Const(buf, scratchSeekPos)
	buf.WriteByte(0x29) // i64.load
	compiler.WriteUleb128(buf, 3)
	compiler.WriteUleb128(buf, 0)
	buf.WriteByte(0x21)
	compiler.WriteUleb128(buf, startLocal)

	buf.WriteByte(0x02) // block
	buf.WriteByte(compiler.ValTypeVoid)
	buf.WriteByte(0x03) // loop
	buf.WriteByte(compiler.ValTypeVoid)

	compiler.EmitI32Const(buf, 1)
	buf.WriteByte(0x42)
	compiler.WriteSleb128_64(buf, 1)
	compiler.EmitI32Const(buf, scratchSeekPos)
	compiler.EmitCallIdx(buf, w.importIndex("clock_time_get"))
	buf.WriteByte(0x1a)
	compiler.EmitI32Const(buf, scratchSeekPos)
	buf.WriteByte(0x29)
	compiler.WriteUleb128(buf, 3)
	compiler.WriteUleb128(buf, 0)
	buf.WriteByte(0x20) // local.get start
	compiler.WriteUleb128(buf, startLocal)
	buf.WriteByte(0x7d) // i64.sub
	buf.WriteByte(0x20) // local.get target
	compiler.WriteUleb128(buf, msLocal)
	buf.WriteByte(0x59) // i64.ge_s
	buf.WriteByte(0x0d) // br_if exit
	compiler.WriteUleb128(buf, 1)
	buf.WriteByte(0x0c) // br loop
	compiler.WriteUleb128(buf, 0)
	buf.WriteByte(0x0b) // end loop
	buf.WriteByte(0x0b) // end block
	return nil
}

// compileRandomU64 reads eight bytes from WASI random_get and returns them
// as a u64. Used by std/random's entropy-seeded constructor.
func (w *syscalls) compileRandomU64(ctx *compiler.FnContext, buf *bytes.Buffer) error {
	ptr := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 8, ptr)
	compiler.EmitLocalGet(buf, ptr)
	compiler.EmitI32Const(buf, 8)
	compiler.EmitCallIdx(buf, w.importIndex("random_get"))
	buf.WriteByte(0x1a) // drop errno
	compiler.EmitLocalGet(buf, ptr)
	buf.WriteByte(0x29) // i64.load
	compiler.WriteUleb128(buf, 3)
	compiler.WriteUleb128(buf, 0)
	return nil
}

// compileWasiStringList reads a WASI string vector (argv or environ) into a
// freshly allocated []string. The returned slice header is {ptr, len, cap};
// each element is a 4-byte pointer to an 8-byte string header {ptr, len}.
func (w *syscalls) compileWasiStringList(ctx *compiler.FnContext, buf *bytes.Buffer, sizesName, getName string) error {
	sizesLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAlloc(buf, 8, sizesLocal)
	compiler.EmitLocalGet(buf, sizesLocal)
	compiler.EmitLocalGet(buf, sizesLocal)
	compiler.EmitI32Const(buf, 4)
	buf.WriteByte(0x6a) // i32.add
	compiler.EmitCallIdx(buf, w.importIndex(sizesName))
	buf.WriteByte(0x1a) // drop errno

	countLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, sizesLocal)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalSet(buf, countLocal)

	bufSizeLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, sizesLocal)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitLocalSet(buf, bufSizeLocal)

	ptrArraySizeLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, countLocal)
	compiler.EmitI32Const(buf, 4)
	buf.WriteByte(0x6c) // i32.mul
	compiler.EmitLocalSet(buf, ptrArraySizeLocal)

	ptrArrayLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAllocDyn(buf, ptrArraySizeLocal, ptrArrayLocal)

	rawBufLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAllocDyn(buf, bufSizeLocal, rawBufLocal)

	compiler.EmitLocalGet(buf, ptrArrayLocal)
	compiler.EmitLocalGet(buf, rawBufLocal)
	compiler.EmitCallIdx(buf, w.importIndex(getName))
	buf.WriteByte(0x1a) // drop errno

	headerSizeLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, countLocal)
	compiler.EmitI32Const(buf, 8)
	buf.WriteByte(0x6c) // i32.mul
	compiler.EmitLocalSet(buf, headerSizeLocal)

	headersLocal := ctx.AllocLocal(compiler.ValTypeI32)
	ctx.EmitAllocDyn(buf, headerSizeLocal, headersLocal)

	idxLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, 0)
	compiler.EmitLocalSet(buf, idxLocal)

	buf.WriteByte(0x02) // block exit
	buf.WriteByte(compiler.ValTypeVoid)
	buf.WriteByte(0x03) // loop
	buf.WriteByte(compiler.ValTypeVoid)

	// if idx >= count { break }
	compiler.EmitLocalGet(buf, idxLocal)
	compiler.EmitLocalGet(buf, countLocal)
	buf.WriteByte(0x4f) // i32.ge_u
	buf.WriteByte(0x0d) // br_if
	compiler.WriteUleb128(buf, 1)

	strPtrLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalGet(buf, ptrArrayLocal)
	compiler.EmitLocalGet(buf, idxLocal)
	compiler.EmitI32Const(buf, 4)
	buf.WriteByte(0x6c) // i32.mul
	buf.WriteByte(0x6a) // i32.add
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalSet(buf, strPtrLocal)

	lenLocal := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, 0)
	compiler.EmitLocalSet(buf, lenLocal)

	buf.WriteByte(0x02) // block strlen-exit
	buf.WriteByte(compiler.ValTypeVoid)
	buf.WriteByte(0x03) // loop strlen
	buf.WriteByte(compiler.ValTypeVoid)
	compiler.EmitLocalGet(buf, strPtrLocal)
	compiler.EmitLocalGet(buf, lenLocal)
	buf.WriteByte(0x6a) // i32.add
	buf.WriteByte(0x2d) // i32.load8_u
	compiler.WriteUleb128(buf, 0)
	compiler.WriteUleb128(buf, 0)
	buf.WriteByte(0x45) // i32.eqz
	buf.WriteByte(0x0d) // br_if strlen-exit
	compiler.WriteUleb128(buf, 1)
	compiler.EmitLocalGet(buf, lenLocal)
	compiler.EmitI32Const(buf, 1)
	buf.WriteByte(0x6a)
	compiler.EmitLocalSet(buf, lenLocal)
	buf.WriteByte(0x0c) // continue
	compiler.WriteUleb128(buf, 0)
	buf.WriteByte(0x0b) // end loop
	buf.WriteByte(0x0b) // end block

	// headers[idx] = { strPtr, len }
	compiler.EmitLocalGet(buf, headersLocal)
	compiler.EmitLocalGet(buf, idxLocal)
	compiler.EmitI32Const(buf, 8)
	buf.WriteByte(0x6c)
	buf.WriteByte(0x6a)
	compiler.EmitLocalGet(buf, strPtrLocal)
	compiler.EmitI32Store(buf, 0, 2)
	compiler.EmitLocalGet(buf, headersLocal)
	compiler.EmitLocalGet(buf, idxLocal)
	compiler.EmitI32Const(buf, 8)
	buf.WriteByte(0x6c)
	buf.WriteByte(0x6a)
	compiler.EmitLocalGet(buf, lenLocal)
	compiler.EmitI32Store(buf, 4, 2)

	compiler.EmitLocalGet(buf, idxLocal)
	compiler.EmitI32Const(buf, 1)
	buf.WriteByte(0x6a)
	compiler.EmitLocalSet(buf, idxLocal)
	buf.WriteByte(0x0c) // continue outer
	compiler.WriteUleb128(buf, 0)
	buf.WriteByte(0x0b) // end loop
	buf.WriteByte(0x0b) // end block

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

// compileFdIO lowers wasi_fd_write_raw / wasi_fd_read_raw /
// wasi_fd_write_string_raw. Both take (fd, buffer) and return the transferred
// byte count, or the negated errno on failure.
func (w *syscalls) compileFdIO(ctx *compiler.FnContext, buf *bytes.Buffer, name string, args []compiler.Expr) error {
	if len(args) != 2 {
		return nil
	}
	if name == "wasi_fd_write_string_raw" {
		// fd, string-header
		hdr := ctx.AllocLocal(compiler.ValTypeI32)
		if err := ctx.CompileExpr(buf, args[1]); err != nil {
			return err
		}
		compiler.EmitLocalSet(buf, hdr)

		compiler.EmitI32Const(buf, scratchCiovec)
		compiler.EmitLocalGet(buf, hdr)
		compiler.EmitI32Load(buf, 0, 2)
		compiler.EmitI32Store(buf, 0, 2)
		compiler.EmitI32Const(buf, scratchCiovec+4)
		compiler.EmitLocalGet(buf, hdr)
		compiler.EmitI32Load(buf, 4, 2)
		compiler.EmitI32Store(buf, 0, 2)
	} else {
		// fd, slice-header
		ptr := ctx.AllocLocal(compiler.ValTypeI32)
		if err := ctx.CompileExpr(buf, args[1]); err != nil {
			return err
		}
		compiler.EmitLocalSet(buf, ptr)

		compiler.EmitI32Const(buf, scratchCiovec)
		compiler.EmitLocalGet(buf, ptr)
		compiler.EmitI32Load(buf, 0, 2)
		compiler.EmitI32Store(buf, 0, 2)
		compiler.EmitI32Const(buf, scratchCiovec+4)
		compiler.EmitLocalGet(buf, ptr)
		compiler.EmitI32Load(buf, 4, 2)
		compiler.EmitI32Store(buf, 0, 2)
	}

	importName := "fd_write"
	if name == "wasi_fd_read_raw" {
		importName = "fd_read"
	}
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitI32Const(buf, scratchCiovec)
	compiler.EmitI32Const(buf, 1)
	compiler.EmitI32Const(buf, scratchNwrite)
	compiler.EmitCallIdx(buf, w.importIndex(importName))

	errno := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalSet(buf, errno)
	compiler.EmitLocalGet(buf, errno)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, scratchNwrite)
	compiler.EmitI32Load(buf, 0, 2)
	buf.WriteByte(0x05)
	compiler.EmitI32Const(buf, 0)
	compiler.EmitLocalGet(buf, errno)
	buf.WriteByte(0x6b) // i32.sub
	buf.WriteByte(0x0b)
	return nil
}

// compilePathOpen lowers wasi_path_open_raw(path, oflags, fdflags,
// rights_base, rights_inheriting). The path is resolved against the
// preopened working directory (descriptor 3).
func (w *syscalls) compilePathOpen(ctx *compiler.FnContext, buf *bytes.Buffer, args []compiler.Expr) error {
	if len(args) != 5 {
		return nil
	}
	hdr := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, hdr)

	compiler.EmitI32Const(buf, 3) // preopened current directory
	compiler.EmitI32Const(buf, 1) // follow symlinks
	compiler.EmitLocalGet(buf, hdr)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalGet(buf, hdr)
	compiler.EmitI32Load(buf, 4, 2)
	if err := ctx.CompileExpr(buf, args[1]); err != nil { // oflags
		return err
	}
	if err := ctx.CompileExpr(buf, args[3]); err != nil { // rights_base
		return err
	}
	buf.WriteByte(0xad)                                   // i64.extend_i32_u
	if err := ctx.CompileExpr(buf, args[4]); err != nil { // rights_inheriting
		return err
	}
	buf.WriteByte(0xad)
	if err := ctx.CompileExpr(buf, args[2]); err != nil { // fdflags
		return err
	}
	compiler.EmitI32Const(buf, scratchOpenedFd)
	compiler.EmitCallIdx(buf, w.importIndex("path_open"))

	errno := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalSet(buf, errno)
	compiler.EmitLocalGet(buf, errno)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, scratchOpenedFd)
	compiler.EmitI32Load(buf, 0, 2)
	buf.WriteByte(0x05)
	compiler.EmitI32Const(buf, 0)
	compiler.EmitLocalGet(buf, errno)
	buf.WriteByte(0x6b)
	buf.WriteByte(0x0b)
	return nil
}

// compileFdSeek lowers wasi_fd_seek_raw(fd, offset, whence), returning the
// new position or the negated errno.
func (w *syscalls) compileFdSeek(ctx *compiler.FnContext, buf *bytes.Buffer, args []compiler.Expr) error {
	if len(args) != 3 {
		return nil
	}
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	if err := ctx.CompileExpr(buf, args[1]); err != nil {
		return err
	}
	buf.WriteByte(0xac) // i64.extend_i32_s
	if err := ctx.CompileExpr(buf, args[2]); err != nil {
		return err
	}
	compiler.EmitI32Const(buf, scratchSeekPos)
	compiler.EmitCallIdx(buf, w.importIndex("fd_seek"))

	errno := ctx.AllocLocal(compiler.ValTypeI32)
	compiler.EmitLocalSet(buf, errno)
	compiler.EmitLocalGet(buf, errno)
	compiler.EmitI32Eqz(buf)
	buf.WriteByte(0x04)
	buf.WriteByte(compiler.ValTypeI32)
	compiler.EmitI32Const(buf, scratchSeekPos)
	compiler.EmitI32Load(buf, 0, 2)
	buf.WriteByte(0x05)
	compiler.EmitI32Const(buf, 0)
	compiler.EmitLocalGet(buf, errno)
	buf.WriteByte(0x6b)
	buf.WriteByte(0x0b)
	return nil
}

// compileSimpleWasiCall lowers one-arg-per-stack-slot syscall passthroughs
// (fd_close, fd_sync).
func (w *syscalls) compileSimpleWasiCall(ctx *compiler.FnContext, buf *bytes.Buffer, importName string, args []compiler.Expr) error {
	for _, arg := range args {
		if err := ctx.CompileExpr(buf, arg); err != nil {
			return err
		}
	}
	compiler.EmitCallIdx(buf, w.importIndex(importName))
	return nil
}

// compilePathCall lowers path mutation syscalls taking a single string
// (path_create_directory, path_unlink_file, path_remove_directory).
func (w *syscalls) compilePathCall(ctx *compiler.FnContext, buf *bytes.Buffer, importName string, args []compiler.Expr) error {
	if len(args) != 1 {
		return nil
	}
	hdr := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, hdr)
	compiler.EmitI32Const(buf, 3)
	compiler.EmitLocalGet(buf, hdr)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalGet(buf, hdr)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitCallIdx(buf, w.importIndex(importName))
	return nil
}

// compilePathStat lowers filestat queries (path_filestat_get when byPath is
// false, fd_filestat_get otherwise). The 16-i32 stat buffer comes from the
// caller.
func (w *syscalls) compilePathStat(ctx *compiler.FnContext, buf *bytes.Buffer, byFd bool, args []compiler.Expr) error {
	if len(args) != 2 {
		return nil
	}
	if byFd {
		if err := ctx.CompileExpr(buf, args[0]); err != nil {
			return err
		}
		if err := ctx.CompileExpr(buf, args[1]); err != nil {
			return err
		}
		compiler.EmitCallIdx(buf, w.importIndex("fd_filestat_get"))
		return nil
	}

	hdr := ctx.AllocLocal(compiler.ValTypeI32)
	if err := ctx.CompileExpr(buf, args[0]); err != nil {
		return err
	}
	compiler.EmitLocalSet(buf, hdr)
	compiler.EmitI32Const(buf, 3)
	compiler.EmitI32Const(buf, 1)
	compiler.EmitLocalGet(buf, hdr)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitLocalGet(buf, hdr)
	compiler.EmitI32Load(buf, 4, 2)
	if err := ctx.CompileExpr(buf, args[1]); err != nil {
		return err
	}
	compiler.EmitCallIdx(buf, w.importIndex("path_filestat_get"))
	return nil
}
