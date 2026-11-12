// File: pkg/compiler/emitter_host.go
//
// This file defines the extension contract between the target-agnostic core
// module emitter and the per-target layers that contribute host syscalls,
// runtime helpers, and ABI helpers.
//
// The frontend never references WASI directly: a target backend package
// (pkg/targets/wasi1, pkg/targets/wasi2) implements HostLayer or AbiLayer and
// registers itself via init(). The core emitter consults the registered
// layers at well-defined points:
//
//   - imports:      HostLayer.RegisterImports
//   - intrinsics:   HostLayer.CompileIntrinsic (wasi_fd_*_raw, time, random...)
//   - helpers:      HostLayer.Helpers/BuildHelper, AbiLayer.Helpers/BuildHelper
//   - _start error: HostLayer.EmitStartErrorPrint
//   - exports:      AbiLayer.Exports
//
// Helper names form the emitter's internal ABI: shared code (println, f-string
// interpolation, int_to_str) looks helpers up by name in the function index
// table, so every HostLayer must provide the print helpers listed in
// RequiredPrintHelpers.

package compiler

import "bytes"

// WasmEmitter is the exported view of the core-module emitter given to target
// layers. Layers use its exported facade; emitter internals stay private.
type WasmEmitter = wasmEmitter

// FnContext is the exported view of the per-function compilation context
// handed to HostLayer.CompileIntrinsic implementations.
type FnContext = fnContext

// ValType aliases for layers building signatures and instruction streams.
const (
	ValTypeI32  = valTypeI32
	ValTypeI64  = valTypeI64
	ValTypeF32  = valTypeF32
	ValTypeF64  = valTypeF64
	ValTypeVoid = valTypeVoid
)

// HelperSpec describes one helper function contributed by a target layer.
// Indices are assigned by the emitter in Helpers() order.
type HelperSpec struct {
	Name    string
	Params  []byte
	Results []byte
}

// ExportSpec declares a core-module export backed by a layer helper.
type ExportSpec struct {
	Export string // exported name, e.g. "cabi_realloc"
	Helper string // helper name providing the body
}

// HostLayer is the host-syscall surface a target contributes to core-module
// emission. Implemented by pkg/targets/wasi1 (WASI Preview 1 syscalls).
type HostLayer interface {
	// Name identifies the layer (used in diagnostics).
	Name() string

	// RegisterImports appends the layer's host imports to the module.
	RegisterImports(e *WasmEmitter)

	// CompileIntrinsic lowers a builtin call that maps to host syscalls.
	// It returns handled=false for call names outside the layer, letting
	// the core emitter apply its generic handling.
	CompileIntrinsic(ctx *FnContext, buf *bytes.Buffer, name string, args []Expr) (bool, error)

	// Helpers returns the runtime helpers the layer contributes, in index
	// order. The print helpers must be present (see RequiredPrintHelpers).
	Helpers() []HelperSpec

	// BuildHelper emits the body of one helper returned by Helpers().
	BuildHelper(e *WasmEmitter, name string) []byte

	// EmitStartErrorPrint emits the _start sequence that prints a
	// string-typed main error and exits non-zero. It returns false when the
	// layer does not support error printing, in which case the core emitter
	// drops the result instead.
	EmitStartErrorPrint(e *WasmEmitter, buf *bytes.Buffer) bool
}

// AbiLayer contributes ABI helpers and exports to the core module.
// Implemented by pkg/targets/wasi2 (Canonical ABI cabi_realloc).
type AbiLayer interface {
	// Name identifies the layer.
	Name() string

	// Helpers returns ABI helpers the layer contributes, in index order.
	Helpers() []HelperSpec

	// BuildHelper emits the body of one helper returned by Helpers().
	BuildHelper(e *WasmEmitter, name string) []byte

	// Exports returns the core-module exports the layer provides.
	Exports() []ExportSpec
}

// RequiredPrintHelpers lists the helper names the core emitter requires from
// the HostLayer. println, print, and string interpolation dispatch through
// these names.
var RequiredPrintHelpers = []string{
	"__print_i32",
	"__print_i64",
	"__print_str",
	"__print_bool",
	"__print_f64",
}

// Registered target layers. Backend packages install themselves via init():
//
//	func init() { compiler.RegisterHostLayer(syscalls{}) }
var (
	registeredHostLayer HostLayer
	registeredAbiLayer  AbiLayer
)

// RegisterHostLayer installs the host-syscall layer used during emission.
// The last registration wins; command builds register exactly one.
func RegisterHostLayer(layer HostLayer) {
	registeredHostLayer = layer
}

// RegisterAbiLayer installs the ABI layer used during emission.
func RegisterAbiLayer(layer AbiLayer) {
	registeredAbiLayer = layer
}

func activeHostLayer() HostLayer { return registeredHostLayer }
func activeAbiLayer() AbiLayer   { return registeredAbiLayer }

// -------------------------------------------------------------
// Exported emitter facade (used by target layers)
// -------------------------------------------------------------

// RegisterType registers a function signature and returns its type index.
func (e *wasmEmitter) RegisterType(params, results []byte) uint32 {
	return e.registerType(params, results)
}

// AddImport appends a host import (module.name) with the given signature and
// returns its function index.
func (e *wasmEmitter) AddImport(module, name string, params, results []byte) uint32 {
	typeIdx := e.registerType(params, results)
	idx := uint32(len(e.imports))
	e.imports = append(e.imports, importMeta{
		module:  module,
		name:    name,
		typeIdx: typeIdx,
	})
	return idx
}

// ImportIndexOf returns the function index of an already-registered import.
func (e *wasmEmitter) ImportIndexOf(module, name string) (uint32, bool) {
	for i, imp := range e.imports {
		if imp.module == module && imp.name == name {
			return uint32(i), true
		}
	}
	return 0, false
}

// FnIndex returns the function index registered for name (0 when absent).
func (e *wasmEmitter) FnIndex(name string) uint32 {
	return e.fnIndices[name]
}

// TrueHeader returns the linear-memory address of the interned "true" string.
func (e *wasmEmitter) TrueHeader() uint32 { return e.trueHeader }

// FalseHeader returns the linear-memory address of the interned "false" string.
func (e *wasmEmitter) FalseHeader() uint32 { return e.falseHeader }

// -------------------------------------------------------------
// Exported FnContext facade (used by HostLayer.CompileIntrinsic)
// -------------------------------------------------------------

// AllocLocal appends a local of the given value type and returns its index.
func (ctx *fnContext) AllocLocal(valType byte) uint32 {
	return ctx.allocLocal(valType)
}

// CompileExpr compiles one expression, leaving its value on the stack.
func (ctx *fnContext) CompileExpr(buf *bytes.Buffer, expr Expr) error {
	return ctx.compileExpr(buf, expr)
}

// EmitAlloc emits an 8-byte-aligned bump-arena allocation of static size into
// resultLocal.
func (ctx *fnContext) EmitAlloc(buf *bytes.Buffer, size int32, resultLocal uint32) {
	ctx.emitAlloc(buf, size, resultLocal)
}

// EmitAllocDyn is EmitAlloc with a runtime size held in sizeLocal.
func (ctx *fnContext) EmitAllocDyn(buf *bytes.Buffer, sizeLocal, resultLocal uint32) {
	ctx.emitAllocDyn(buf, sizeLocal, resultLocal)
}

// -------------------------------------------------------------
// Exported low-level opcode encoders (used by target layers)
// -------------------------------------------------------------

// WriteUleb128 encodes an unsigned 32-bit LEB128 value into buf.
func WriteUleb128(buf *bytes.Buffer, val uint32) { writeUleb128(buf, val) }

// WriteSleb128 encodes a signed 32-bit LEB128 value into buf.
func WriteSleb128(buf *bytes.Buffer, val int32) { writeSleb128(buf, val) }

// WriteSleb128_64 encodes a signed 64-bit LEB128 value into buf.
func WriteSleb128_64(buf *bytes.Buffer, val int64) { writeSleb128_64(buf, val) }

// EmitI32Const appends an i32.const instruction.
func EmitI32Const(buf *bytes.Buffer, v int32) { emitI32Const(buf, v) }

// EmitLocalGet appends a local.get instruction.
func EmitLocalGet(buf *bytes.Buffer, idx uint32) { emitLocalGet(buf, idx) }

// EmitLocalSet appends a local.set instruction.
func EmitLocalSet(buf *bytes.Buffer, idx uint32) { emitLocalSet(buf, idx) }

// EmitI32Load appends an i32.load instruction.
func EmitI32Load(buf *bytes.Buffer, offset, align uint32) { emitI32Load(buf, offset, align) }

// EmitI32Store appends an i32.store instruction.
func EmitI32Store(buf *bytes.Buffer, offset, align uint32) { emitI32Store(buf, offset, align) }

// EmitCallIdx appends a call instruction targeting idx.
func EmitCallIdx(buf *bytes.Buffer, idx uint32) { emitCallIdx(buf, idx) }

// EmitI32Eqz appends an i32.eqz instruction.
func EmitI32Eqz(buf *bytes.Buffer) { emitI32Eqz(buf) }
