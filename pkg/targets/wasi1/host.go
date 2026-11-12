// File: pkg/targets/wasi1/host.go
//
// The WASI Preview 1 HostLayer. Everything Preview 1 that the core-module
// emitter consults lives here: syscall imports, syscall intrinsics, the
// print runtime helpers, and the _start error-print sequence.

package wasi1

import (
	"bytes"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

// wasiPreview1Module is the WASI Preview 1 import namespace.
const wasiPreview1Module = "wasi_snapshot_preview1"

// syscalls implements compiler.HostLayer for WASI Preview 1.
type syscalls struct {
	// imports maps syscall names to their import function indices.
	imports map[string]uint32
}

func init() {
	compiler.RegisterHostLayer(&syscalls{
		imports: make(map[string]uint32),
	})
}

// NewHostLayer returns a fresh WASI Preview 1 host layer. The layer is
// registered globally via init(); this constructor exists for tests and
// explicit configuration.
func NewHostLayer() compiler.HostLayer {
	return &syscalls{imports: make(map[string]uint32)}
}

// Name implements compiler.HostLayer.
func (w *syscalls) Name() string {
	return "wasi1/wasi_snapshot_preview1"
}

// RegisterImports implements compiler.HostLayer: registers the
// wasi_snapshot_preview1 syscall surface.
func (w *syscalls) RegisterImports(e *compiler.WasmEmitter) {
	add := func(name string, params, results []byte) {
		w.imports[name] = e.AddImport(wasiPreview1Module, name, params, results)
	}

	add("fd_write", []byte{compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("clock_time_get", []byte{compiler.ValTypeI32, compiler.ValTypeI64, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})

	// Additional WASI preview1 imports used by std/wasi1/fs and std/io.
	// Registering them up-front keeps the syscall intrinsics simple.
	add("fd_read", []byte{compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("fd_close", []byte{compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("fd_sync", []byte{compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("fd_seek", []byte{compiler.ValTypeI32, compiler.ValTypeI64, compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("fd_filestat_get", []byte{compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("path_open", []byte{compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI64, compiler.ValTypeI64, compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("path_create_directory", []byte{compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("path_unlink_file", []byte{compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("path_remove_directory", []byte{compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("path_filestat_get", []byte{compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("proc_exit", []byte{compiler.ValTypeI32}, []byte{})
	// Process/environment primitives used by std/env and std/random.
	add("args_sizes_get", []byte{compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("args_get", []byte{compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("environ_sizes_get", []byte{compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("environ_get", []byte{compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
	add("random_get", []byte{compiler.ValTypeI32, compiler.ValTypeI32}, []byte{compiler.ValTypeI32})
}

// importIndex returns the registered index of a syscall import.
func (w *syscalls) importIndex(name string) uint32 {
	return w.imports[name]
}

// CompileIntrinsic implements compiler.HostLayer: lowers the builtin calls
// that map to WASI Preview 1 syscalls. Returns handled=false for names the
// layer does not own.
func (w *syscalls) CompileIntrinsic(ctx *compiler.FnContext, buf *bytes.Buffer, name string, args []compiler.Expr) (bool, error) {
	switch name {
	case "time_now_ms":
		return true, w.compileClockNow(ctx, buf, 0, true) // CLOCK_REALTIME, milliseconds
	case "time_now_ns":
		return true, w.compileClockNow(ctx, buf, 1, false) // CLOCK_MONOTONIC, nanoseconds
	case "exit_process":
		return true, w.compileExitProcess(ctx, buf, args)
	case "sleep_ms":
		return true, w.compileSleepMs(ctx, buf, args)
	case "os_args":
		return true, w.compileWasiStringList(ctx, buf, "args_sizes_get", "args_get")
	case "os_environ":
		return true, w.compileWasiStringList(ctx, buf, "environ_sizes_get", "environ_get")
	case "random_get_u64":
		return true, w.compileRandomU64(ctx, buf)
	case "wasi_fd_write_raw", "wasi_fd_read_raw", "wasi_fd_write_string_raw":
		return true, w.compileFdIO(ctx, buf, name, args)
	case "wasi_path_open_raw":
		return true, w.compilePathOpen(ctx, buf, args)
	case "wasi_fd_close_raw":
		return true, w.compileSimpleWasiCall(ctx, buf, "fd_close", args)
	case "wasi_fd_sync_raw":
		return true, w.compileSimpleWasiCall(ctx, buf, "fd_sync", args)
	case "wasi_fd_seek_raw":
		return true, w.compileFdSeek(ctx, buf, args)
	case "wasi_path_create_dir_raw":
		return true, w.compilePathCall(ctx, buf, "path_create_directory", args)
	case "wasi_path_unlink_raw":
		return true, w.compilePathCall(ctx, buf, "path_unlink_file", args)
	case "wasi_path_remove_dir_raw":
		return true, w.compilePathCall(ctx, buf, "path_remove_directory", args)
	case "wasi_path_filestat_raw":
		return true, w.compilePathStat(ctx, buf, false, args)
	case "wasi_fd_filestat_raw":
		return true, w.compilePathStat(ctx, buf, true, args)
	}
	return false, nil
}

// Helpers implements compiler.HostLayer: the print runtime helpers. Their
// bodies write to stdout via fd_write, which makes them Preview 1 runtime,
// not shared frontend code.
func (w *syscalls) Helpers() []compiler.HelperSpec {
	i32 := compiler.ValTypeI32
	return []compiler.HelperSpec{
		{Name: "__print_i32", Params: []byte{i32, i32}, Results: []byte{}},
		{Name: "__print_i64", Params: []byte{compiler.ValTypeI64, i32}, Results: []byte{}},
		{Name: "__print_str", Params: []byte{i32, i32, i32}, Results: []byte{}},
		{Name: "__print_bool", Params: []byte{i32, i32}, Results: []byte{}},
		{Name: "__print_f64", Params: []byte{compiler.ValTypeF64, i32}, Results: []byte{}},
	}
}

// BuildHelper implements compiler.HostLayer.
func (w *syscalls) BuildHelper(e *compiler.WasmEmitter, name string) []byte {
	switch name {
	case "__print_i32":
		return w.buildPrintIntFunc(e)
	case "__print_i64":
		return w.buildPrintInt64Func(e)
	case "__print_str":
		return w.buildPrintStrFunc(e)
	case "__print_bool":
		return w.buildPrintBoolFunc(e)
	case "__print_f64":
		return w.buildPrintF64Func(e)
	}
	return nil
}

// EmitStartErrorPrint implements compiler.HostLayer: the _start epilogue that
// prints a string-typed main error to stderr and exits non-zero. The caller
// (buildStartFunc) has already stored main's result header in local 0 and
// opened the `if (errno == 0)` frame; this method emits only the frame body
// and must leave it open — the caller closes it.
func (w *syscalls) EmitStartErrorPrint(e *compiler.WasmEmitter, buf *bytes.Buffer) bool {
	// error string header: ciovec = { ptr: hdr.ptr, len: hdr.len }
	compiler.EmitI32Const(buf, scratchCiovec)
	compiler.EmitLocalGet(buf, 0)
	compiler.EmitI32Load(buf, 4, 2) // err header pointer
	compiler.EmitI32Load(buf, 0, 2) // header.ptr
	compiler.EmitI32Store(buf, 0, 2)

	compiler.EmitI32Const(buf, scratchCiovec+4)
	compiler.EmitLocalGet(buf, 0)
	compiler.EmitI32Load(buf, 4, 2) // err header pointer
	compiler.EmitI32Load(buf, 4, 2) // header.len
	compiler.EmitI32Store(buf, 0, 2)

	compiler.EmitI32Const(buf, 2) // stderr
	compiler.EmitI32Const(buf, scratchCiovec)
	compiler.EmitI32Const(buf, 1)
	compiler.EmitI32Const(buf, scratchNwrite)
	compiler.EmitCallIdx(buf, w.importIndex("fd_write"))
	buf.WriteByte(0x1a) // drop errno

	// newline after the error message
	compiler.EmitI32Const(buf, scratchCiovec+48)
	compiler.EmitI32Const(buf, 10)
	buf.WriteByte(0x3a) // i32.store8
	compiler.WriteUleb128(buf, 0)
	compiler.WriteUleb128(buf, 0)
	compiler.EmitI32Const(buf, scratchCiovec)
	compiler.EmitI32Const(buf, scratchCiovec+48)
	compiler.EmitI32Store(buf, 0, 2)
	compiler.EmitI32Const(buf, scratchCiovec+4)
	compiler.EmitI32Const(buf, 1)
	compiler.EmitI32Store(buf, 0, 2)
	compiler.EmitI32Const(buf, 2)
	compiler.EmitI32Const(buf, scratchCiovec)
	compiler.EmitI32Const(buf, 1)
	compiler.EmitI32Const(buf, scratchNwrite)
	compiler.EmitCallIdx(buf, w.importIndex("fd_write"))
	buf.WriteByte(0x1a)

	compiler.EmitI32Const(buf, 1)
	compiler.EmitCallIdx(buf, w.importIndex("proc_exit"))
	return true
}
