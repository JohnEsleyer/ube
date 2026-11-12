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

type wasi2Syscalls struct {
	imports map[string]uint32
}

func init() {
	// Register ABI layer
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

// RegisterImports registers lowered WIT component interface functions.
func (w *wasi2Syscalls) RegisterImports(e *compiler.WasmEmitter) {
	add := func(module, name string, params, results []byte) {
		w.imports[module+"#"+name] = e.AddImport(module, name, params, results)
	}

	i32 := compiler.ValTypeI32
	i64 := compiler.ValTypeI64

	// wasi:cli/stdout, stderr, stdin
	add("wasi:cli/stdout@0.2.0", "get-stdout", []byte{}, []byte{i32})
	add("wasi:cli/stderr@0.2.0", "get-stderr", []byte{}, []byte{i32})
	add("wasi:cli/stdin@0.2.0", "get-stdin", []byte{}, []byte{i32})

	// wasi:cli/exit
	add("wasi:cli/exit@0.2.0", "exit", []byte{i32}, []byte{})

	// wasi:io/streams
	// [method]output-stream.blocking-write-and-flush: func(self: borrow<output-stream>, contents: list<u8>) -> result<_, stream-error>
	// Lowered ABI: (stream_handle: i32, ptr: i32, len: i32, ret_ptr: i32)
	add("wasi:io/streams@0.2.0", "[method]output-stream.blocking-write-and-flush", []byte{i32, i32, i32, i32}, []byte{})

	// [method]input-stream.blocking-read: func(self: borrow<input-stream>, len: u64) -> result<list<u8>, stream-error>
	// Lowered ABI: (stream_handle: i32, max_len: i64, ret_ptr: i32)
	add("wasi:io/streams@0.2.0", "[method]input-stream.blocking-read", []byte{i32, i64, i32}, []byte{})

	// [resource-drop]output-stream
	add("wasi:io/streams@0.2.0", "[resource-drop]output-stream", []byte{i32}, []byte{})
	// [resource-drop]input-stream
	add("wasi:io/streams@0.2.0", "[resource-drop]input-stream", []byte{i32}, []byte{})

	// wasi:clocks/monotonic-clock: now() -> instant(u64)
	add("wasi:clocks/monotonic-clock@0.2.0", "now", []byte{}, []byte{i64})

	// wasi:clocks/wall-clock: now() -> datetime { seconds: u64, nanoseconds: u32 }
	// Lowered ABI: writes into ret_ptr i32
	add("wasi:clocks/wall-clock@0.2.0", "now", []byte{i32}, []byte{})

	// wasi:random/random: get-random-bytes(len: u64) -> list<u8>
	// Lowered ABI: (len: i64, ret_ptr: i32)
	add("wasi:random/random@0.2.0", "get-random-bytes", []byte{i64, i32}, []byte{})

	// wasi:filesystem/preopens: get-directories() -> list<tuple<descriptor, string>>
	// Lowered ABI: (ret_ptr: i32)
	add("wasi:filesystem/preopens@0.2.0", "get-directories", []byte{i32}, []byte{})

	// wasi:filesystem/types: [method]descriptor.read-via-stream(offset: u64) -> input-stream
	add("wasi:filesystem/types@0.2.0", "[method]descriptor.read-via-stream", []byte{i32, i64}, []byte{i32})

	// wasi:filesystem/types: [method]descriptor.write-via-stream(offset: u64) -> output-stream
	add("wasi:filesystem/types@0.2.0", "[method]descriptor.write-via-stream", []byte{i32, i64}, []byte{i32})
}

func (w *wasi2Syscalls) importIndex(module, name string) uint32 {
	return w.imports[module+"#"+name]
}

// CompileIntrinsic implements compiler.HostLayer.
func (w *wasi2Syscalls) CompileIntrinsic(ctx *compiler.FnContext, buf *bytes.Buffer, name string, args []compiler.Expr) (bool, error) {
	switch name {
	case "time_now_ns":
		compiler.EmitCallIdx(buf, w.importIndex("wasi:clocks/monotonic-clock@0.2.0", "now"))
		return true, nil
	case "time_now_ms":
		// wall-clock: now(ret_ptr) -> datetime { seconds: u64, nanoseconds: u32 }
		retLocal := ctx.AllocLocal(compiler.ValTypeI32)
		ctx.EmitAlloc(buf, 16, retLocal)
		compiler.EmitLocalGet(buf, retLocal)
		compiler.EmitCallIdx(buf, w.importIndex("wasi:clocks/wall-clock@0.2.0", "now"))
		// ms = seconds * 1000 + nanoseconds / 1_000_000
		compiler.EmitLocalGet(buf, retLocal)
		buf.WriteByte(0x29) // i64.load offset 0
		compiler.WriteUleb128(buf, 3)
		compiler.WriteUleb128(buf, 0)
		buf.WriteByte(0x42) // i64.const 1000
		compiler.WriteSleb128_64(buf, 1000)
		buf.WriteByte(0x7e) // i64.mul

		compiler.EmitLocalGet(buf, retLocal)
		buf.WriteByte(0x28) // i32.load offset 8
		compiler.WriteUleb128(buf, 2)
		compiler.WriteUleb128(buf, 8)
		buf.WriteByte(0xad) // i64.extend_i32_u
		buf.WriteByte(0x42) // i64.const 1_000_000
		compiler.WriteSleb128_64(buf, 1000000)
		buf.WriteByte(0x80) // i64.div_u
		buf.WriteByte(0x7c) // i64.add
		return true, nil
	case "exit_process":
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
		retLocal := ctx.AllocLocal(compiler.ValTypeI32)
		ctx.EmitAlloc(buf, 16, retLocal)
		buf.WriteByte(0x42) // i64.const 8
		compiler.WriteSleb128_64(buf, 8)
		compiler.EmitLocalGet(buf, retLocal)
		compiler.EmitCallIdx(buf, w.importIndex("wasi:random/random@0.2.0", "get-random-bytes"))
		// ret_ptr has { ptr: i32, len: i32 }
		compiler.EmitLocalGet(buf, retLocal)
		compiler.EmitI32Load(buf, 0, 2)
		buf.WriteByte(0x29) // i64.load
		compiler.WriteUleb128(buf, 3)
		compiler.WriteUleb128(buf, 0)
		return true, nil
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

	// ret_ptr = scratch (4 bytes for result)
	compiler.EmitI32Const(&body, 0)
	compiler.EmitLocalSet(&body, retLocal)

	// blocking-write-and-flush(stdout, ptr, len, retLocal)
	compiler.EmitLocalGet(&body, stdoutLocal)
	compiler.EmitLocalGet(&body, 0)
	compiler.EmitLocalGet(&body, 1)
	compiler.EmitLocalGet(&body, retLocal)
	compiler.EmitCallIdx(&body, w.importIndex("wasi:io/streams@0.2.0", "[method]output-stream.blocking-write-and-flush"))

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

	body.WriteByte(0x0b) // end if

	// [resource-drop]output-stream(stdout)
	compiler.EmitLocalGet(&body, stdoutLocal)
	compiler.EmitCallIdx(&body, w.importIndex("wasi:io/streams@0.2.0", "[resource-drop]output-stream"))

	body.WriteByte(0x0b) // end function

	var full bytes.Buffer
	compiler.WriteUleb128(&full, 1) // 1 local group
	compiler.WriteUleb128(&full, 2) // 2 locals (stdoutLocal, retLocal)
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
	compiler.EmitCallIdx(&body, e.FnIndex("__int_to_str"))
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
func (w *wasi2Syscalls) EmitStartErrorPrint(e *compiler.WasmEmitter, buf *bytes.Buffer) bool {
	// stderr = wasi:cli/stderr@0.2.0#get-stderr()
	compiler.EmitCallIdx(buf, w.importIndex("wasi:cli/stderr@0.2.0", "get-stderr"))
	// retLocal / stderrLocal
	stderrLocal := uint32(1)
	compiler.EmitLocalSet(buf, stderrLocal)

	// write error string to stderr
	compiler.EmitLocalGet(buf, stderrLocal)
	compiler.EmitLocalGet(buf, 0) // main result pointer
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitI32Load(buf, 0, 2) // header.ptr
	compiler.EmitLocalGet(buf, 0)
	compiler.EmitI32Load(buf, 4, 2)
	compiler.EmitI32Load(buf, 4, 2) // header.len
	compiler.EmitI32Const(buf, 0)   // ret_ptr
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/streams@0.2.0", "[method]output-stream.blocking-write-and-flush"))

	// newline
	compiler.EmitI32Const(buf, 60)
	compiler.EmitI32Const(buf, 10)
	buf.WriteByte(0x3a)
	compiler.WriteUleb128(buf, 0)
	compiler.WriteUleb128(buf, 0)

	compiler.EmitLocalGet(buf, stderrLocal)
	compiler.EmitI32Const(buf, 60)
	compiler.EmitI32Const(buf, 1)
	compiler.EmitI32Const(buf, 0)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/streams@0.2.0", "[method]output-stream.blocking-write-and-flush"))

	compiler.EmitLocalGet(buf, stderrLocal)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:io/streams@0.2.0", "[resource-drop]output-stream"))

	compiler.EmitI32Const(buf, 1)
	compiler.EmitCallIdx(buf, w.importIndex("wasi:cli/exit@0.2.0", "exit"))
	return true
}
