// File: pkg/targets/wasi2/host.go
//
// Direct WASI 0.2 (Preview 2) WIT HostLayer. Implements true component
// lowering for wasi:cli/*, wasi:io/*, wasi:filesystem/*, wasi:clocks/*,
// and wasi:random/* without preview1 shims.

package wasi2

import (
	"bytes"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

const maxFdSlots = 128

type wasi2Syscalls struct {
	imports         map[string]uint32
	preopenCacheOff uint32 // 8 bytes: { fd: i32, initialized: i32 }
	fdOffsetsBase   uint32 // maxFdSlots * 8 bytes of i64 file offsets
}

func init() {
	compiler.RegisterAbiLayer(NewAbiLayer())
}

// NewHostLayer returns the WASI 0.2 WIT host layer.
func NewHostLayer() compiler.HostLayer {
	return &wasi2Syscalls{
		imports: make(map[string]uint32),
	}
}

// Name implements compiler.HostLayer.
func (w *wasi2Syscalls) Name() string {
	return "wasi2/wit-interfaces"
}

// RegisterImports registers lowered WIT component interface functions and
// reserves static scratch slots for preopen caching and descriptor offsets.
func (w *wasi2Syscalls) RegisterImports(e *compiler.WasmEmitter) {
	w.preopenCacheOff = e.AllocStatic(8)
	w.fdOffsetsBase = e.AllocStatic(maxFdSlots * 8)

	add := func(module, name string, params, results []byte) {
		w.imports[module+"#"+name] = e.AddImport(module, name, params, results)
	}

	i32 := compiler.ValTypeI32
	i64 := compiler.ValTypeI64

	// wasi:cli/stdout, stderr, stdin, exit, environment
	add("wasi:cli/stdout@0.2.0", "get-stdout", []byte{}, []byte{i32})
	add("wasi:cli/stderr@0.2.0", "get-stderr", []byte{}, []byte{i32})
	add("wasi:cli/stdin@0.2.0", "get-stdin", []byte{}, []byte{i32})
	add("wasi:cli/exit@0.2.0", "exit", []byte{i32}, []byte{})
	add("wasi:cli/environment@0.2.0", "get-arguments", []byte{i32}, []byte{})
	add("wasi:cli/environment@0.2.0", "get-environment", []byte{i32}, []byte{})

	// wasi:io/error & wasi:io/streams
	add("wasi:io/error@0.2.0", "[resource-drop]error", []byte{i32}, []byte{})
	add("wasi:io/streams@0.2.0", "[method]output-stream.blocking-write-and-flush", []byte{i32, i32, i32, i32}, []byte{})
	add("wasi:io/streams@0.2.0", "[method]input-stream.blocking-read", []byte{i32, i64, i32}, []byte{})
	add("wasi:io/streams@0.2.0", "[resource-drop]output-stream", []byte{i32}, []byte{})
	add("wasi:io/streams@0.2.0", "[resource-drop]input-stream", []byte{i32}, []byte{})

	// wasi:clocks/monotonic-clock & wall-clock
	add("wasi:clocks/monotonic-clock@0.2.0", "now", []byte{}, []byte{i64})
	add("wasi:clocks/wall-clock@0.2.0", "now", []byte{i32}, []byte{})

	// wasi:random/random
	add("wasi:random/random@0.2.0", "get-random-bytes", []byte{i64, i32}, []byte{})

	// wasi:filesystem/preopens & types
	add("wasi:filesystem/preopens@0.2.0", "get-directories", []byte{i32}, []byte{})
	add("wasi:filesystem/types@0.2.0", "[method]descriptor.read-via-stream", []byte{i32, i64, i32}, []byte{})
	add("wasi:filesystem/types@0.2.0", "[method]descriptor.write-via-stream", []byte{i32, i64, i32}, []byte{})
	add("wasi:filesystem/types@0.2.0", "[method]descriptor.read", []byte{i32, i64, i64, i32}, []byte{})
	add("wasi:filesystem/types@0.2.0", "[method]descriptor.write", []byte{i32, i32, i32, i64, i32}, []byte{})
	add("wasi:filesystem/types@0.2.0", "[method]descriptor.open-at", []byte{i32, i32, i32, i32, i32, i32, i32}, []byte{})
	add("wasi:filesystem/types@0.2.0", "[method]descriptor.stat", []byte{i32, i32}, []byte{})
	add("wasi:filesystem/types@0.2.0", "[method]descriptor.stat-at", []byte{i32, i32, i32, i32, i32}, []byte{})
	add("wasi:filesystem/types@0.2.0", "[method]descriptor.create-directory-at", []byte{i32, i32, i32, i32}, []byte{})
	add("wasi:filesystem/types@0.2.0", "[method]descriptor.unlink-file-at", []byte{i32, i32, i32, i32}, []byte{})
	add("wasi:filesystem/types@0.2.0", "[method]descriptor.remove-directory-at", []byte{i32, i32, i32, i32}, []byte{})
	add("wasi:filesystem/types@0.2.0", "[method]descriptor.sync", []byte{i32, i32}, []byte{})
	add("wasi:filesystem/types@0.2.0", "[resource-drop]descriptor", []byte{i32}, []byte{})
}

func (w *wasi2Syscalls) importIndex(module, name string) uint32 {
	return w.imports[module+"#"+name]
}

// CompileIntrinsic implements compiler.HostLayer.
func (w *wasi2Syscalls) CompileIntrinsic(ctx *compiler.FnContext, buf *bytes.Buffer, name string, args []compiler.Expr) (bool, error) {
	switch name {
	case "time_now_ns", "wasi2_clock_now":
		compiler.EmitCallIdx(buf, w.importIndex("wasi:clocks/monotonic-clock@0.2.0", "now"))
		return true, nil
	case "time_now_ms":
		return true, w.compileWallNowMs(ctx, buf)
	case "sleep_ms":
		return true, w.compileSleepMs(ctx, buf, args)
	case "exit_process", "wasi2_exit":
		if len(args) == 1 {
			if err := ctx.CompileExpr(buf, args[0]); err != nil {
				return false, err
			}
		} else {
			compiler.EmitI32Const(buf, 0)
		}
		compiler.EmitCallIdx(buf, w.importIndex("wasi:cli/exit@0.2.0", "exit"))
		buf.WriteByte(0x00) // unreachable
		return true, nil
	case "random_get_u64":
		return true, w.compileRandomU64(ctx, buf)
	case "os_args":
		return true, w.compileOsArgs(ctx, buf)
	case "os_environ":
		return true, w.compileOsEnviron(ctx, buf)
	case "wasi2_stdin":
		compiler.EmitCallIdx(buf, w.importIndex("wasi:cli/stdin@0.2.0", "get-stdin"))
		return true, nil
	case "wasi2_stdout":
		compiler.EmitCallIdx(buf, w.importIndex("wasi:cli/stdout@0.2.0", "get-stdout"))
		return true, nil
	case "wasi2_stderr":
		compiler.EmitCallIdx(buf, w.importIndex("wasi:cli/stderr@0.2.0", "get-stderr"))
		return true, nil
	case "wasi2_stream_write_raw":
		return true, w.compileStreamWrite(ctx, buf, args)
	case "wasi2_stream_read_raw":
		return true, w.compileStreamRead(ctx, buf, args)
	case "wasi2_stream_drop_input_raw":
		return true, w.compileSingleDrop(ctx, buf, "wasi:io/streams@0.2.0", "[resource-drop]input-stream", args)
	case "wasi2_stream_drop_output_raw":
		return true, w.compileSingleDrop(ctx, buf, "wasi:io/streams@0.2.0", "[resource-drop]output-stream", args)
	case "wasi2_preopen_dir_raw":
		return true, w.compilePreopenDir(ctx, buf)
	case "wasi2_open_at_raw":
		return true, w.compileOpenAt(ctx, buf, args)
	case "wasi2_desc_read_raw":
		return true, w.compileDescRead(ctx, buf, args)
	case "wasi2_desc_write_raw":
		return true, w.compileDescWrite(ctx, buf, args)
	case "wasi2_desc_stat_raw":
		return true, w.compileDescStat(ctx, buf, false, args)
	case "wasi2_path_stat_raw":
		return true, w.compileDescStat(ctx, buf, true, args)
	case "wasi2_desc_sync_raw":
		return true, w.compileDescSync(ctx, buf, args)
	case "wasi2_desc_drop_raw":
		return true, w.compileSingleDrop(ctx, buf, "wasi:filesystem/types@0.2.0", "[resource-drop]descriptor", args)
	case "wasi2_create_dir_at_raw":
		return true, w.compileDescPathOp(ctx, buf, "[method]descriptor.create-directory-at", args)
	case "wasi2_unlink_at_raw":
		return true, w.compileDescPathOp(ctx, buf, "[method]descriptor.unlink-file-at", args)
	case "wasi2_remove_dir_at_raw":
		return true, w.compileDescPathOp(ctx, buf, "[method]descriptor.remove-directory-at", args)
	case "wasi2_read_via_stream_raw":
		return true, w.compileViaStream(ctx, buf, "[method]descriptor.read-via-stream", args)
	case "wasi2_write_via_stream_raw":
		return true, w.compileViaStream(ctx, buf, "[method]descriptor.write-via-stream", args)
	case "wasi_fd_write_raw", "wasi_fd_write_string_raw":
		return true, w.compileLegacyFdWrite(ctx, buf, name == "wasi_fd_write_string_raw", args)
	case "wasi_fd_read_raw":
		return true, w.compileLegacyFdRead(ctx, buf, args)
	case "wasi_fd_close_raw":
		return true, w.compileSingleDropResult(ctx, buf, "wasi:filesystem/types@0.2.0", "[resource-drop]descriptor", args)
	case "wasi_fd_sync_raw":
		return true, w.compileDescSync(ctx, buf, args)
	}
	return false, nil
}

// Helpers implements compiler.HostLayer: print helpers targeting wasi:cli/stdout and wasi:io/streams.
func (w *wasi2Syscalls) Helpers() []compiler.HelperSpec {
	i32 := compiler.ValTypeI32
	return []compiler.HelperSpec{
		{Name: "__print_i32", Params: []byte{i32, i32}, Results: []byte{}},
		{Name: "__print_i64", Params: []byte{compiler.ValTypeI64, i32}, Results: []byte{}},
		{Name: "__print_str", Params: []byte{i32, i32, i32}, Results: []byte{}},
		{Name: "__print_bool", Params: []byte{i32, i32}, Results: []byte{}},
		{Name: "__print_f64", Params: []byte{compiler.ValTypeF64, i32}, Results: []byte{}},
	}
}

func (w *wasi2Syscalls) buildPrintStrFunc(e *compiler.WasmEmitter) []byte {
	var body bytes.Buffer

	// params: local 0: ptr, local 1: len, local 2: newline
	stdoutLocal := uint32(3)
	retLocal := uint32(4)

	// stdout = wasi:cli/stdout@0.2.0#get-stdout()
	compiler.EmitCallIdx(&body, w.importIndex("wasi:cli/stdout@0.2.0", "get-stdout"))
	compiler.EmitLocalSet(&body, stdoutLocal)

	// ret_ptr = scratch 0..11 (12 bytes for result<_, stream-error>)
	compiler.EmitI32Const(&body, 0)
	compiler.EmitLocalSet(&body, retLocal)

	// blocking-write-and-flush(stdout, ptr, len, retLocal)
	compiler.EmitLocalGet(&body, stdoutLocal)
	compiler.EmitLocalGet(&body, 0)
	compiler.EmitLocalGet(&body, 1)
	compiler.EmitLocalGet(&body, retLocal)
	compiler.EmitCallIdx(&body, w.importIndex("wasi:io/streams@0.2.0", "[method]output-stream.blocking-write-and-flush"))

	// If error with own<error> (ret[0] != 0 && ret[4] == 0), drop error resource.
	compiler.EmitLocalGet(&body, retLocal)
	body.WriteByte(0x2d) // i32.load8_u
	compiler.WriteUleb128(&body, 0)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x04) // if err
	body.WriteByte(compiler.ValTypeVoid)
	compiler.EmitLocalGet(&body, retLocal)
	body.WriteByte(0x2d)
	compiler.WriteUleb128(&body, 0)
	compiler.WriteUleb128(&body, 4)
	compiler.EmitI32Eqz(&body)
	body.WriteByte(0x04) // if last-operation-failed
	body.WriteByte(compiler.ValTypeVoid)
	compiler.EmitLocalGet(&body, retLocal)
	compiler.EmitI32Load(&body, 8, 2)
	compiler.EmitCallIdx(&body, w.importIndex("wasi:io/error@0.2.0", "[resource-drop]error"))
	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	// if newline != 0: write newline byte
	compiler.EmitLocalGet(&body, 2)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeVoid)

	// write 10 ('\n') at linear memory 60
	compiler.EmitI32Const(&body, 60)
	compiler.EmitI32Const(&body, 10)
	body.WriteByte(0x3a) // i32.store8
	compiler.WriteUleb128(&body, 0)
	compiler.WriteUleb128(&body, 0)

	compiler.EmitLocalGet(&body, stdoutLocal)
	compiler.EmitI32Const(&body, 60)
	compiler.EmitI32Const(&body, 1)
	compiler.EmitLocalGet(&body, retLocal)
	compiler.EmitCallIdx(&body, w.importIndex("wasi:io/streams@0.2.0", "[method]output-stream.blocking-write-and-flush"))

	compiler.EmitLocalGet(&body, retLocal)
	body.WriteByte(0x2d)
	compiler.WriteUleb128(&body, 0)
	compiler.WriteUleb128(&body, 0)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeVoid)
	compiler.EmitLocalGet(&body, retLocal)
	body.WriteByte(0x2d)
	compiler.WriteUleb128(&body, 0)
	compiler.WriteUleb128(&body, 4)
	compiler.EmitI32Eqz(&body)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeVoid)
	compiler.EmitLocalGet(&body, retLocal)
	compiler.EmitI32Load(&body, 8, 2)
	compiler.EmitCallIdx(&body, w.importIndex("wasi:io/error@0.2.0", "[resource-drop]error"))
	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	body.WriteByte(0x0b) // end if newline

	// [resource-drop]output-stream(stdout)
	compiler.EmitLocalGet(&body, stdoutLocal)
	compiler.EmitCallIdx(&body, w.importIndex("wasi:io/streams@0.2.0", "[resource-drop]output-stream"))

	body.WriteByte(0x0b) // end function

	var full bytes.Buffer
	compiler.WriteUleb128(&full, 1)
	compiler.WriteUleb128(&full, 2)
	full.WriteByte(compiler.ValTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	compiler.WriteUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (w *wasi2Syscalls) buildPrintIntFunc(e *compiler.WasmEmitter) []byte {
	var body bytes.Buffer

	compiler.EmitLocalGet(&body, 0)
	compiler.EmitCallIdx(&body, e.FnIndex("__i32_to_str"))
	compiler.EmitLocalSet(&body, 2)

	compiler.EmitLocalGet(&body, 2)
	compiler.EmitI32Load(&body, 0, 2)
	compiler.EmitLocalGet(&body, 2)
	compiler.EmitI32Load(&body, 4, 2)
	compiler.EmitLocalGet(&body, 1)
	compiler.EmitCallIdx(&body, e.FnIndex("__print_str"))
	body.WriteByte(0x0b)

	var full bytes.Buffer
	compiler.WriteUleb128(&full, 1)
	compiler.WriteUleb128(&full, 1)
	full.WriteByte(compiler.ValTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	compiler.WriteUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (w *wasi2Syscalls) buildPrintInt64Func(e *compiler.WasmEmitter) []byte {
	var body bytes.Buffer

	compiler.EmitLocalGet(&body, 0)
	compiler.EmitCallIdx(&body, e.FnIndex("__i64_to_str"))
	compiler.EmitLocalSet(&body, 2)

	compiler.EmitLocalGet(&body, 2)
	compiler.EmitI32Load(&body, 0, 2)
	compiler.EmitLocalGet(&body, 2)
	compiler.EmitI32Load(&body, 4, 2)
	compiler.EmitLocalGet(&body, 1)
	compiler.EmitCallIdx(&body, e.FnIndex("__print_str"))
	body.WriteByte(0x0b)

	var full bytes.Buffer
	compiler.WriteUleb128(&full, 1)
	compiler.WriteUleb128(&full, 1)
	full.WriteByte(compiler.ValTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	compiler.WriteUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (w *wasi2Syscalls) buildPrintF64Func(e *compiler.WasmEmitter) []byte {
	var body bytes.Buffer

	compiler.EmitLocalGet(&body, 0)
	compiler.EmitCallIdx(&body, e.FnIndex("__f64_to_str"))
	compiler.EmitLocalSet(&body, 2)

	compiler.EmitLocalGet(&body, 2)
	compiler.EmitI32Load(&body, 0, 2)
	compiler.EmitLocalGet(&body, 2)
	compiler.EmitI32Load(&body, 4, 2)
	compiler.EmitLocalGet(&body, 1)
	compiler.EmitCallIdx(&body, e.FnIndex("__print_str"))
	body.WriteByte(0x0b)

	var full bytes.Buffer
	compiler.WriteUleb128(&full, 1)
	compiler.WriteUleb128(&full, 1)
	full.WriteByte(compiler.ValTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	compiler.WriteUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (w *wasi2Syscalls) buildPrintBoolFunc(e *compiler.WasmEmitter) []byte {
	var body bytes.Buffer

	compiler.EmitLocalGet(&body, 0)
	body.WriteByte(0x04)
	body.WriteByte(compiler.ValTypeVoid)

	compiler.EmitI32Const(&body, int32(e.TrueHeader()))
	body.WriteByte(0x28)
	compiler.WriteUleb128(&body, 2)
	compiler.WriteUleb128(&body, 0)
	compiler.EmitI32Const(&body, 4)
	compiler.EmitLocalGet(&body, 1)
	compiler.EmitCallIdx(&body, e.FnIndex("__print_str"))

	body.WriteByte(0x05)

	compiler.EmitI32Const(&body, int32(e.FalseHeader()))
	body.WriteByte(0x28)
	compiler.WriteUleb128(&body, 2)
	compiler.WriteUleb128(&body, 0)
	compiler.EmitI32Const(&body, 5)
	compiler.EmitLocalGet(&body, 1)
	compiler.EmitCallIdx(&body, e.FnIndex("__print_str"))

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	var full bytes.Buffer
	compiler.WriteUleb128(&full, 0)
	full.Write(body.Bytes())

	var res bytes.Buffer
	compiler.WriteUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

// BuildHelper implements compiler.HostLayer.
func (w *wasi2Syscalls) BuildHelper(e *compiler.WasmEmitter, name string) []byte {
	switch name {
	case "__print_str":
		return w.buildPrintStrFunc(e)
	case "__print_i32":
		return w.buildPrintIntFunc(e)
	case "__print_i64":
		return w.buildPrintInt64Func(e)
	case "__print_bool":
		return w.buildPrintBoolFunc(e)
	case "__print_f64":
		return w.buildPrintF64Func(e)
	}
	return nil
}

// EmitStartErrorPrint emits stderr writes via wasi:cli/stderr and wasi:io/streams.
// _start only allocates local 0 (main's result header pointer), so we stash
// local 0 at scratch address 52 before reusing local 0 for stderr's handle.
func (w *wasi2Syscalls) EmitStartErrorPrint(e *compiler.WasmEmitter, buf *bytes.Buffer) bool {
	// Save main result header pointer to scratch address 52.
	compiler.EmitI32Const(buf, 52)
	compiler.EmitLocalGet(buf, 0)
	compiler.EmitI32Store(buf, 0, 2)

	// local 0 = wasi:cli/stderr@0.2.0#get-stderr()
	compiler.EmitCallIdx(buf, w.importIndex("wasi:cli/stderr@0.2.0", "get-stderr"))
	compiler.EmitLocalSet(buf, 0)

	// write error string to stderr (ret_ptr at scratch 0..11)
	compiler.EmitLocalGet(buf, 0)
	compiler.EmitI32Const(buf, 52)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitI32Load(buf, 0, 2) // header.ptr
	compiler.EmitI32Const(buf, 52)
	compiler.EmitI32Load(buf, 0, 2)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitI32Load(buf, 4, 2) // header.len
	compiler.EmitI32Const(buf, 0)   // ret_ptr
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/streams@0.2.0", "[method]output-stream.blocking-write-and-flush"))

	// newline at scratch 60
	compiler.EmitI32Const(buf, 60)
	compiler.EmitI32Const(buf, 10)
	buf.WriteByte(0x3a) // i32.store8
	compiler.WriteUleb128(buf, 0)
	compiler.WriteUleb128(buf, 0)

	compiler.EmitLocalGet(buf, 0)
	compiler.EmitI32Const(buf, 60)
	compiler.EmitI32Const(buf, 1)
	compiler.EmitI32Const(buf, 0)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/streams@0.2.0", "[method]output-stream.blocking-write-and-flush"))

	compiler.EmitLocalGet(buf, 0)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/streams@0.2.0", "[resource-drop]output-stream"))

	compiler.EmitI32Const(buf, 1)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:cli/exit@0.2.0", "exit"))
	return true
}
