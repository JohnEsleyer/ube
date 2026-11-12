// File: pkg/compiler/wasm_emit.go
package compiler

import "github.com/JohnEsleyer/ube/pkg/wasm"

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"strings"
)

type EmitWasmConfig struct {
	MemTrace bool
}

type TraceScopeInfo struct {
	ID       int32
	Name     string
	File     string
	Line     int
	Kind     int32 // 0: func, 1: arena, 2: loop
	ParentID int32
}

// EmitWasm compiles an AST program directly into WebAssembly binary bytecode (.wasm)
// with zero external toolchains (no clang, no zig, no C headers).
func EmitWasm(prog *Program) (wasmBytes []byte, err error) {
	return EmitWasmWithConfig(prog, EmitWasmConfig{})
}

// EmitWasmWithConfig compiles an AST program with extra runtime configuration (e.g. --mem-trace).
func EmitWasmWithConfig(prog *Program, cfg EmitWasmConfig) (wasmBytes []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("internal compiler error (ICE): %v\nPlease report this issue", r)
		}
	}()
	emitter := newWasmEmitter()
	emitter.memTrace = cfg.MemTrace
	return emitter.compile(prog)
}

const (
	wasmMagic   = 0x6d736100
	wasmVersion = 0x00000001

	valTypeI32  = byte(0x7f)
	valTypeI64  = byte(0x7e)
	valTypeF32  = byte(0x7d)
	valTypeF64  = byte(0x7c)
	valTypeVoid = byte(0x40)

	secCustom   = byte(0)
	secType     = byte(1)
	secImport   = byte(2)
	secFunction = byte(3)
	secTable    = byte(4)
	secMemory   = byte(5)
	secGlobal   = byte(6)
	secExport   = byte(7)
	secStart    = byte(8)
	secElement  = byte(9)
	secCode     = byte(10)
	secData     = byte(11)

	staticDataOff = 64 // Offset 64..: static string literals and headers
)

type structMeta struct {
	fieldOffsets map[string]int
	fieldTypes   map[string]UbeType
	totalSize    int
}

type importMeta struct {
	module  string
	name    string
	typeIdx uint32
}

type wasmEmitter struct {
	types         [][]byte
	typeMap       map[string]uint32
	imports       []importMeta
	funcs         []uint32
	codeBodies    [][]byte
	data          []byte
	dataOffset    uint32
	fnIndices     map[string]uint32
	methodIndices map[string]uint32
	fnReturns     map[string]UbeType
	hostLayer     HostLayer
	abiLayer      AbiLayer
	structs       map[string]structMeta
	structFields  map[string][]FieldDecl
	enums         map[string][]VariantDecl
	traits        map[string]*TraitDecl
	typeIDs       map[string]int32
	moduleAliases map[string]string
	trueHeader    uint32
	falseHeader   uint32
	memTrace      bool
	traceImports  map[string]uint32
	nextScopeID   int32
	TraceScopes   []TraceScopeInfo
}

func newWasmEmitter() *wasmEmitter {
	return &wasmEmitter{
		typeMap:       make(map[string]uint32),
		fnIndices:     make(map[string]uint32),
		methodIndices: make(map[string]uint32),
		fnReturns:     make(map[string]UbeType),
		structs:       make(map[string]structMeta),
		structFields:  make(map[string][]FieldDecl),
		enums:         make(map[string][]VariantDecl),
		traits:        make(map[string]*TraitDecl),
		typeIDs:       make(map[string]int32),
		moduleAliases: make(map[string]string),
		traceImports:  make(map[string]uint32),
		dataOffset:    staticDataOff,
		nextScopeID:   1,
	}
}

func (e *wasmEmitter) allocScopeID(name, file string, line int, kind int32, parentID int32) int32 {
	id := e.nextScopeID
	e.nextScopeID++
	e.TraceScopes = append(e.TraceScopes, TraceScopeInfo{
		ID:       id,
		Name:     name,
		File:     file,
		Line:     line,
		Kind:     kind,
		ParentID: parentID,
	})
	return id
}

func (e *wasmEmitter) registerTraceImport(name string, params []byte, results []byte) uint32 {
	tIdx := e.registerType(params, results)
	idx := uint32(len(e.imports))
	e.imports = append(e.imports, importMeta{
		module:  "ube_trace",
		name:    name,
		typeIdx: tIdx,
	})
	e.traceImports[name] = idx
	return idx
}

func (e *wasmEmitter) registerType(params []byte, results []byte) uint32 {
	key := fmt.Sprintf("%v->%v", params, results)
	if idx, exists := e.typeMap[key]; exists {
		return idx
	}

	var buf bytes.Buffer
	buf.WriteByte(0x60)
	writeUleb128(&buf, uint32(len(params)))
	buf.Write(params)
	writeUleb128(&buf, uint32(len(results)))
	buf.Write(results)

	idx := uint32(len(e.types))
	e.types = append(e.types, buf.Bytes())
	e.typeMap[key] = idx
	return idx
}

// internString writes the raw UTF-8 string bytes and produces an 8-byte
// string header { ptr: i32, len: i32 }, returning the pointer to the header.
func (e *wasmEmitter) internString(s string) uint32 {
	strBytes := []byte(s)
	length := uint32(len(strBytes))

	bytesOffset := e.dataOffset
	e.data = append(e.data, strBytes...)
	e.dataOffset += length

	for e.dataOffset%4 != 0 {
		e.data = append(e.data, 0)
		e.dataOffset++
	}

	headerOffset := e.dataOffset
	var hdr [8]byte
	binary.LittleEndian.PutUint32(hdr[0:4], bytesOffset)
	binary.LittleEndian.PutUint32(hdr[4:8], length)
	e.data = append(e.data, hdr[:]...)
	e.dataOffset += 8

	return headerOffset
}

func typeToValType(t UbeType) wasm.ValType {
	if t == nil {
		return wasm.ValI32
	}
	switch t.String() {
	case "i64", "u64":
		return wasm.ValI64
	case "f32":
		return wasm.ValF32
	case "f64":
		return wasm.ValF64
	default:
		return wasm.ValI32
	}
}

func (e *wasmEmitter) compile(prog *Program) ([]byte, error) {
	if prog != nil {
		for alias, prefix := range prog.ModuleAliases {
			e.moduleAliases[alias] = prefix
		}
	}

	// 1. Register target imports via the registered host layer. The core
	// emitter is target-agnostic: WASI Preview 1 syscalls (and any other
	// host ABI) are contributed by pkg/targets/* layers.
	host := activeHostLayer()
	if host == nil {
		return nil, fmt.Errorf(
			"no host syscall layer registered for WebAssembly emission; " +
				"import a target backend such as _ \"github.com/JohnEsleyer/ube/pkg/targets/wasi1\"")
	}
	e.hostLayer = host
	host.RegisterImports(e)

	if e.memTrace {
		e.registerTraceImport("arena_enter", []byte{valTypeI32, valTypeI32}, []byte{})
		e.registerTraceImport("arena_exit", []byte{valTypeI32}, []byte{})
		e.registerTraceImport("arena_alloc", []byte{valTypeI32, valTypeI32}, []byte{})
		e.registerTraceImport("arena_promote", []byte{valTypeI32}, []byte{})
		e.registerTraceImport("slice_realloc", []byte{valTypeI32, valTypeI32, valTypeI32}, []byte{})
		e.registerTraceImport("loop_iter", []byte{valTypeI32}, []byte{})
		e.registerTraceImport("loop_enter", []byte{valTypeI32}, []byte{})
		e.registerTraceImport("loop_exit", []byte{valTypeI32}, []byte{})
	}

	// Register user host imports (extern fn)
	for _, decl := range prog.Decls {
		if ext, ok := decl.(*ExternFnDecl); ok {
			modName := ext.Module
			if modName == "" {
				modName = "env"
			}
			// Deduplicate against imports already registered by the active
			// target layers (e.g. wasi_snapshot_preview1 syscalls).
			if idx, exists := e.ImportIndexOf(modName, ext.Name); exists {
				e.fnIndices[ext.Name] = idx
				e.fnReturns[ext.Name] = ext.RetType
				continue
			}
			var params []byte
			for _, p := range ext.Params {
				params = append(params, wasmTypeOrDefault(p.Type))
			}
			var results []byte
			if ext.RetType != nil && ext.RetType.String() != "void" {
				results = append(results, wasmTypeOrDefault(ext.RetType))
			}
			tIdx := e.registerType(params, results)
			extIdx := uint32(len(e.imports))
			e.imports = append(e.imports, importMeta{
				module:  modName,
				name:    ext.Name,
				typeIdx: tIdx,
			})
			e.fnIndices[ext.Name] = extIdx
			e.fnReturns[ext.Name] = ext.RetType
		}
	}

	// Pre-scan structs, enums, and traits
	currentTypeID := int32(1)
	structDecls := make(map[string]*StructDecl)
	var structOrder []string
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *StructDecl:
			e.structs[d.Name] = structMeta{
				fieldOffsets: make(map[string]int),
				fieldTypes:   make(map[string]UbeType),
			}
			e.structFields[d.Name] = d.Fields
			e.typeIDs[d.Name] = currentTypeID
			currentTypeID++
			structDecls[d.Name] = d
			structOrder = append(structOrder, d.Name)
		case *EnumDecl:
			e.enums[d.Name] = d.Variants
		case *TraitDecl:
			e.traits[d.Name] = d
		}
	}

	// Compute memory layout, natural alignments, and promoted embedded fields
	e.computeStructLayouts(prog, structDecls, structOrder)

	// Intern static boolean headers
	e.trueHeader = e.internString("true")
	e.falseHeader = e.internString("false")

	// Index defined functions
	funcOffset := uint32(len(e.imports))
	var fnDecls []*FnDecl
	var mainFn *FnDecl

	for _, decl := range prog.Decls {
		if fn, ok := decl.(*FnDecl); ok {
			fnDecls = append(fnDecls, fn)
			fnIdx := funcOffset + uint32(len(fnDecls)-1)
			if fn.Receiver != nil {
				key := fn.Receiver.Type.String() + "." + fn.Name
				e.methodIndices[key] = fnIdx
				e.fnReturns[key] = fn.RetType

				// Also register the receiver's bare short name so calls
				// emitted with an unqualified receiver type resolve to the
				// same function. First registration wins, matching the
				// function-index fallback convention.
				if i := strings.LastIndexByte(fn.Receiver.Type.String(), '.'); i >= 0 {
					shortKey := fn.Receiver.Type.String()[i+1:] + "." + fn.Name
					if _, exists := e.methodIndices[shortKey]; !exists {
						e.methodIndices[shortKey] = fnIdx
						e.fnReturns[shortKey] = fn.RetType
					}
				}
			} else {
				e.fnIndices[fn.Name] = fnIdx
				e.fnReturns[fn.Name] = fn.RetType
				// Alias the bare short name so intra-module calls resolve.
				if i := strings.LastIndexByte(fn.Name, '.'); i >= 0 {
					bare := fn.Name[i+1:]
					if _, exists := e.fnIndices[bare]; !exists {
						e.fnIndices[bare] = fnIdx
						e.fnReturns[bare] = fn.RetType
					}
				}
				if fn.Name == "main" {
					mainFn = fn
				}
			}
		}
	}

	if mainFn == nil {
		return nil, fmt.Errorf("no 'main' function found in program")
	}

	if os.Getenv("UBE_DEBUG_FNS") != "" {
		type kv struct {
			name string
			idx  uint32
		}
		var list []kv
		for name, idx := range e.fnIndices {
			list = append(list, kv{name, idx})
		}
		for name, idx := range e.methodIndices {
			list = append(list, kv{name, idx})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].idx < list[j].idx })
		for _, it := range list {
			fmt.Fprintf(os.Stderr, "FN %d %s\n", it.idx, it.name)
		}
	}

	// Assemble the helper pipeline: shared target-agnostic helpers first,
	// then the target layers' contributions (print runtime, ABI helpers).
	layerHelpers := []HelperSpec{
		{Name: "__str_concat", Params: []byte{valTypeI32, valTypeI32}, Results: []byte{valTypeI32}},
		{Name: "__str_eq", Params: []byte{valTypeI32, valTypeI32}, Results: []byte{valTypeI32}},
		{Name: "__i32_to_str", Params: []byte{valTypeI32}, Results: []byte{valTypeI32}},
		{Name: "__i64_to_str", Params: []byte{valTypeI64}, Results: []byte{valTypeI32}},
		{Name: "__f64_to_str", Params: []byte{valTypeF64}, Results: []byte{valTypeI32}},
	}
	sharedBodies := map[string]func() []byte{
		"__str_concat": e.buildStrConcatFunc,
		"__str_eq":     e.buildStrEqFunc,
		"__i32_to_str": e.buildIntToStrFunc,
		"__i64_to_str": e.buildInt64ToStrFunc,
		"__f64_to_str": e.buildF64ToStrFunc,
	}
	// ABI helpers (Canonical ABI) then host helpers (print runtime).
	if abi := activeAbiLayer(); abi != nil {
		e.abiLayer = abi
		layerHelpers = append(layerHelpers, abi.Helpers()...)
	}
	layerHelpers = append(layerHelpers, host.Helpers()...)

	// Register helper indices in order. Function-section type entries are
	// appended below in the same order as the code bodies (user functions,
	// then helpers, then _start).
	helperIdx := funcOffset + uint32(len(fnDecls))
	for _, spec := range layerHelpers {
		e.fnIndices[spec.Name] = helperIdx
		helperIdx++
	}

	// _start runs last: the module entrypoint exporting the WASI command ABI.
	startIdx := helperIdx
	e.fnIndices["_start"] = startIdx

	// Validate the print helper ABI contract between the core emitter and
	// the host layer.
	for _, name := range RequiredPrintHelpers {
		if _, ok := e.fnIndices[name]; !ok {
			return nil, fmt.Errorf("host layer %q does not provide required helper %s", host.Name(), name)
		}
	}

	// Register types for defined functions
	for _, fn := range fnDecls {
		var params []byte
		if fn.Receiver != nil {
			params = append(params, valTypeI32)
		}
		for _, p := range fn.Params {
			params = append(params, wasmTypeOrDefault(p.Type))
		}
		var results []byte
		if fn.RetType != nil && fn.RetType.String() != "void" {
			results = append(results, wasmTypeOrDefault(fn.RetType))
		}
		e.funcs = append(e.funcs, e.registerType(params, results))
	}

	// Helper types, in code-body order.
	for _, spec := range layerHelpers {
		e.funcs = append(e.funcs, e.registerType(spec.Params, spec.Results))
	}
	e.funcs = append(e.funcs, e.registerType([]byte{}, []byte{})) // _start

	// Compile user function bodies
	for _, fn := range fnDecls {
		body, err := e.compileFunction(fn)
		if err != nil {
			return nil, fmt.Errorf("function %s: %w", fn.Name, err)
		}
		e.codeBodies = append(e.codeBodies, body)
	}

	// Compile helper bodies: shared helpers, then layer-contributed helpers
	// (cabi_realloc from the ABI layer, print runtime from the host layer).
	for _, spec := range layerHelpers {
		if build, ok := sharedBodies[spec.Name]; ok {
			e.codeBodies = append(e.codeBodies, build())
			continue
		}
		if e.abiLayer != nil {
			if body := e.abiLayer.BuildHelper(e, spec.Name); body != nil {
				e.codeBodies = append(e.codeBodies, body)
				continue
			}
		}
		if body := host.BuildHelper(e, spec.Name); body != nil {
			e.codeBodies = append(e.codeBodies, body)
			continue
		}
		return nil, fmt.Errorf("helper %s has no body from the shared emitter or the target layers", spec.Name)
	}
	e.codeBodies = append(e.codeBodies, e.buildStartFunc(e.fnIndices["main"]))

	coreBytes, err := e.assembleBinary()
	if err != nil {
		return nil, err
	}
	return coreBytes, nil
}

type fnContext struct {
	emitter    *wasmEmitter
	fn         *FnDecl
	locals     map[string]uint32
	localTypes map[string]UbeType
	numParams  uint32
	allocTypes []byte
	// deferScopes is a stack of defer lists, one per lexical block. A defer
	// runs when its own block exits (fall-through, break/continue out of a
	// loop body, `?`, or return), not merely when the function returns.
	deferScopes [][]Expr
	// loopDeferDepths is a stack of deferScopes indices for the innermost
	// loop body scopes. break/continue run every defer from the current
	// innermost scope down to (and including) the loop body's scope.
	loopDeferDepths []int
	// labelDepth is the number of wasm labels (block/loop/if) currently
	// open at this point in the emitted function body. It is incremented
	// on every 0x02/0x03/0x04 emission and decremented on every 0x0b,
	// so that break/continue in *LoopControlStmt can compute their
	// relative br depth instead of hardcoding 1/0 (which mis-targets
	// the enclosing block when break appears inside an if or match arm).
	labelDepth int
	// loopBodyDepths is a stack of labelDepth values captured right
	// after each enclosing loop's 0x03 (loop) opcode is emitted. The
	// top of the stack is the label depth at the innermost loop body
	// start, so break = (labelDepth - top) + 1 and
	// continue = (labelDepth - top).
	loopBreakDepths    []int
	loopContinueDepths []int
	loopArenaLocals    []uint32
	currentScopeID     int32
	ir                 *IRBuilder
	localPool          *LocalPool
}

func (ctx *fnContext) pushDeferScope() {
	ctx.deferScopes = append(ctx.deferScopes, nil)
}

func (ctx *fnContext) popDeferScope() []Expr {
	if len(ctx.deferScopes) == 0 {
		return nil
	}
	top := ctx.deferScopes[len(ctx.deferScopes)-1]
	ctx.deferScopes = ctx.deferScopes[:len(ctx.deferScopes)-1]
	return top
}

func (ctx *fnContext) addDefer(e Expr) {
	if len(ctx.deferScopes) == 0 {
		ctx.deferScopes = append(ctx.deferScopes, nil)
	}
	idx := len(ctx.deferScopes) - 1
	ctx.deferScopes[idx] = append(ctx.deferScopes[idx], e)
}

// emitAllDefers runs every active defer from innermost to outermost. It is
// used on paths that leave the function entirely (return and `?`).
func (ctx *fnContext) emitAllDefers(buf *bytes.Buffer) {
	for s := len(ctx.deferScopes) - 1; s >= 0; s-- {
		scope := ctx.deferScopes[s]
		for i := len(scope) - 1; i >= 0; i-- {
			_ = ctx.compileDeferredCall(buf, scope[i])
		}
	}
}

// emitCurrentScopeDefers runs only the innermost block's defers. It is used
// on normal fall-through out of a block.
func (ctx *fnContext) emitCurrentScopeDefers(buf *bytes.Buffer) {
	if len(ctx.deferScopes) == 0 {
		return
	}
	scope := ctx.deferScopes[len(ctx.deferScopes)-1]
	for i := len(scope) - 1; i >= 0; i-- {
		_ = ctx.compileDeferredCall(buf, scope[i])
	}
}

// emitLoopDefers runs the defers of every scope entered since the loop body
// began, including the loop body scope itself. It is used by break/continue,
// which leave the loop body but not the enclosing function.
func (ctx *fnContext) emitLoopDefers(buf *bytes.Buffer, loopScopeDepth int) {
	if loopScopeDepth < 0 {
		loopScopeDepth = 0
	}
	for s := len(ctx.deferScopes) - 1; s >= loopScopeDepth; s-- {
		scope := ctx.deferScopes[s]
		for i := len(scope) - 1; i >= 0; i-- {
			_ = ctx.compileDeferredCall(buf, scope[i])
		}
	}
}

// stmtTerminated reports whether a statement always transfers control away
// (return, break, continue) so the block-end defer flush must not run again on
// that path.
func stmtTerminated(s Stmt) bool {
	if s == nil {
		return false
	}
	switch st := s.(type) {
	case *ReturnStmt, *LoopControlStmt:
		return true
	case *BlockStmt:
		return blockTerminated(st)
	case *IfStmt:
		if st.Else == nil {
			return false
		}
		return blockTerminated(st.Then) && stmtTerminated(st.Else)
	}
	return false
}

func blockTerminated(b *BlockStmt) bool {
	if b == nil || len(b.Stmts) == 0 {
		return false
	}
	return stmtTerminated(b.Stmts[len(b.Stmts)-1])
}

func (e *wasmEmitter) compileFunction(fn *FnDecl) ([]byte, error) {
	ctx := &fnContext{
		emitter:    e,
		fn:         fn,
		locals:     make(map[string]uint32),
		localTypes: make(map[string]UbeType),
	}

	var paramTypes []wasm.ValType
	paramIdx := uint32(0)
	if fn.Receiver != nil {
		ctx.locals[fn.Receiver.Name] = paramIdx
		ctx.localTypes[fn.Receiver.Name] = fn.Receiver.Type
		paramTypes = append(paramTypes, typeToValType(fn.Receiver.Type))
		paramIdx++
	}
	for _, param := range fn.Params {
		ctx.locals[param.Name] = paramIdx
		ctx.localTypes[param.Name] = param.Type
		paramTypes = append(paramTypes, typeToValType(param.Type))
		paramIdx++
	}
	ctx.numParams = paramIdx

	ctx.localPool = NewLocalPool(paramTypes)
	irFn := &IRFunction{
		Name:    fn.Name,
		Params:  paramTypes,
		Results: nil,
	}
	ctx.ir = NewIRBuilder(irFn, ctx.localPool)

	var fnScopeID int32
	if e.memTrace {
		fnName := fn.Name
		if fn.Receiver != nil {
			fnName = fmt.Sprintf("(%s).%s", fn.Receiver.Type, fn.Name)
		}
		fnScopeID = e.allocScopeID(fnName, fn.At.File, fn.At.Line, 0, 0)
		ctx.currentScopeID = fnScopeID
	}

	var bodyBuf bytes.Buffer
	if e.memTrace {
		emitI32Const(&bodyBuf, fnScopeID)
		emitI32Const(&bodyBuf, 0)
		emitCallIdx(&bodyBuf, e.traceImports["arena_enter"])
	}

	if err := ctx.compileBlock(&bodyBuf, fn.Body); err != nil {
		return nil, err
	}

	if e.memTrace {
		emitI32Const(&bodyBuf, fnScopeID)
		emitCallIdx(&bodyBuf, e.traceImports["arena_exit"])
	}

	// compileBlock flushes the function body's defer scope on fall-through
	// (or the terminating statement already flushed every active scope).
	if fn.RetType != nil && fn.RetType.String() != "void" {
		bodyBuf.WriteByte(0x00)
	}
	bodyBuf.WriteByte(0x0b)

	var codeBuf bytes.Buffer
	writeUleb128(&codeBuf, uint32(len(ctx.allocTypes)))
	for _, lt := range ctx.allocTypes {
		writeUleb128(&codeBuf, 1)
		codeBuf.WriteByte(lt)
	}
	codeBuf.Write(peepholeOptimizeWasm(bodyBuf.Bytes()))

	var fullBuf bytes.Buffer
	writeUleb128(&fullBuf, uint32(codeBuf.Len()))
	fullBuf.Write(codeBuf.Bytes())
	return fullBuf.Bytes(), nil
}

func (ctx *fnContext) allocLocal(t byte) uint32 {
	idx := ctx.numParams + uint32(len(ctx.allocTypes))
	ctx.allocTypes = append(ctx.allocTypes, t)
	if ctx.localPool != nil {
		ctx.localPool.allLocals = append(ctx.localPool.allLocals, wasm.ValType(t))
	}
	return idx
}

// openLabel emits a wasm label-introducing opcode (0x02 block, 0x03 loop,
// 0x04 if) and tracks the increase in label depth.
func (ctx *fnContext) openLabel(buf *bytes.Buffer, op byte) {
	buf.WriteByte(op)
	ctx.labelDepth++
}

// closeLabel emits the 0x0b end opcode and tracks the decrease in label
// depth.
func (ctx *fnContext) closeLabel(buf *bytes.Buffer) {
	buf.WriteByte(0x0b)
	ctx.labelDepth--
}

func (ctx *fnContext) compileBlock(buf *bytes.Buffer, block *BlockStmt) error {
	if block == nil {
		return nil
	}

	// Every lexical block owns a defer scope. On normal fall-through its
	// defers run here; terminating statements (return, break, continue)
	// run the relevant scopes themselves and mark the block terminated so
	// we do not emit them a second time on the same control-flow path.
	ctx.pushDeferScope()
	terminated := false
	for _, stmt := range block.Stmts {
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
	return nil
}

func (e *wasmEmitter) assembleBinary() ([]byte, error) {
	var out bytes.Buffer

	out.Write([]byte{0x00, 0x61, 0x73, 0x6d})
	out.Write([]byte{0x01, 0x00, 0x00, 0x00})

	var typeSec bytes.Buffer
	writeUleb128(&typeSec, uint32(len(e.types)))
	for _, t := range e.types {
		typeSec.Write(t)
	}
	writeSection(&out, secType, typeSec.Bytes())

	if len(e.imports) > 0 {
		var importSec bytes.Buffer
		writeUleb128(&importSec, uint32(len(e.imports)))
		for _, imp := range e.imports {
			writeString(&importSec, imp.module)
			writeString(&importSec, imp.name)
			importSec.WriteByte(0x00)
			writeUleb128(&importSec, imp.typeIdx)
		}
		writeSection(&out, secImport, importSec.Bytes())
	}

	var funcSec bytes.Buffer
	writeUleb128(&funcSec, uint32(len(e.funcs)))
	for _, f := range e.funcs {
		writeUleb128(&funcSec, f)
	}
	writeSection(&out, secFunction, funcSec.Bytes())

	var memSec bytes.Buffer
	writeUleb128(&memSec, 1)
	memSec.WriteByte(0x01)
	writeUleb128(&memSec, 2)
	writeUleb128(&memSec, 256)
	writeSection(&out, secMemory, memSec.Bytes())

	heapStart := uint32(4096)
	if e.dataOffset >= heapStart {
		heapStart = ((e.dataOffset + 4095) / 4096) * 4096
	}
	var globSec bytes.Buffer
	writeUleb128(&globSec, 1)
	globSec.WriteByte(valTypeI32)
	globSec.WriteByte(0x01)
	globSec.WriteByte(0x41)
	writeSleb128(&globSec, int32(heapStart))
	globSec.WriteByte(0x0b)
	writeSection(&out, secGlobal, globSec.Bytes())

	var expSec bytes.Buffer
	// ABI layer exports (cabi_realloc etc.) come from the target.
	abiExportIdx := map[string]uint32{}
	if e.abiLayer != nil {
		for _, exp := range e.abiLayer.Exports() {
			if idx, ok := e.fnIndices[exp.Helper]; ok {
				abiExportIdx[exp.Export] = idx
			}
		}
	}
	totalExports := 2 + len(abiExportIdx) // memory, _start, abi exports
	writeUleb128(&expSec, uint32(totalExports))

	writeString(&expSec, "memory")
	expSec.WriteByte(0x02)
	writeUleb128(&expSec, 0)

	writeString(&expSec, "_start")
	expSec.WriteByte(0x00)
	writeUleb128(&expSec, e.fnIndices["_start"])

	// Deterministic order: sort ABI export names.
	abiNames := make([]string, 0, len(abiExportIdx))
	for name := range abiExportIdx {
		abiNames = append(abiNames, name)
	}
	sort.Strings(abiNames)
	for _, name := range abiNames {
		writeString(&expSec, name)
		expSec.WriteByte(0x00)
		writeUleb128(&expSec, abiExportIdx[name])
	}
	writeSection(&out, secExport, expSec.Bytes())

	var codeSec bytes.Buffer
	writeUleb128(&codeSec, uint32(len(e.codeBodies)))
	for _, body := range e.codeBodies {
		codeSec.Write(body)
	}
	writeSection(&out, secCode, codeSec.Bytes())

	if len(e.data) > 0 {
		var dataSec bytes.Buffer
		writeUleb128(&dataSec, 1)
		dataSec.WriteByte(0x00)
		dataSec.WriteByte(0x41)
		writeSleb128(&dataSec, staticDataOff)
		dataSec.WriteByte(0x0b)
		writeUleb128(&dataSec, uint32(len(e.data)))
		dataSec.Write(e.data)
		writeSection(&out, secData, dataSec.Bytes())
	}

	return out.Bytes(), nil
}

func writeSection(buf *bytes.Buffer, id byte, data []byte) {
	buf.WriteByte(id)
	writeUleb128(buf, uint32(len(data)))
	buf.Write(data)
}

func writeString(buf *bytes.Buffer, s string) {
	writeUleb128(buf, uint32(len(s)))
	buf.WriteString(s)
}

func writeUleb128(buf *bytes.Buffer, val uint32) {
	buf.Write(wasm.AppendUleb128(nil, val))
}

func writeSleb128(buf *bytes.Buffer, val int32) {
	buf.Write(wasm.AppendSleb128(nil, val))
}

func writeSleb128_64(buf *bytes.Buffer, val int64) {
	buf.Write(wasm.AppendSleb128_64(nil, val))
}

// wasmTypeOrDefault maps a Ube type to its Wasm value type, falling back to
// i32 for void/unknown types. Used for function signatures and locals so that
// i64, f32, and f64 are represented correctly rather than coerced to i32.
func wasmTypeOrDefault(t UbeType) byte {
	if t != nil {
		if wt := byte(t.WasmType()); wt != valTypeVoid {
			return wt
		}
	}
	return valTypeI32
}

// typeFromString reconstructs a primitive/string type from its name. Used
// when only an inferred type string is available (e.g. a let binding whose
// value type was inferred by the typechecker).
func typeFromString(s string) UbeType {
	switch s {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64", "bool", "void":
		return PrimitiveType{Name: s}
	case "string":
		return StringType{}
	case "StringBuilder":
		return StringBuilderType{}
	default:
		return CustomType{Name: s}
	}
}

func emitCastOp(buf *bytes.Buffer, srcType, dstType string) {
	if srcType == dstType {
		return
	}
	isSrcI32 := isI32Like(srcType)
	isDstI32 := isI32Like(dstType)

	if isSrcI32 && isDstI32 {
		if dstType == "u8" {
			buf.WriteByte(0x41) // i32.const
			writeSleb128(buf, 0xff)
			buf.WriteByte(0x71) // i32.and
		} else if dstType == "i8" {
			buf.WriteByte(0x41)
			writeSleb128(buf, 24)
			buf.WriteByte(0x74) // i32.shl
			buf.WriteByte(0x41)
			writeSleb128(buf, 24)
			buf.WriteByte(0x75) // i32.shr_s
		} else if dstType == "u16" {
			buf.WriteByte(0x41)
			writeSleb128(buf, 0xffff)
			buf.WriteByte(0x71)
		} else if dstType == "i16" {
			buf.WriteByte(0x41)
			writeSleb128(buf, 16)
			buf.WriteByte(0x74)
			buf.WriteByte(0x41)
			writeSleb128(buf, 16)
			buf.WriteByte(0x75)
		}
		return
	}

	if isSrcI32 {
		switch dstType {
		case "i64":
			if isUnsigned(srcType) {
				buf.WriteByte(0xad) // i64.extend_i32_u
			} else {
				buf.WriteByte(0xac) // i64.extend_i32_s
			}
		case "u64":
			buf.WriteByte(0xad) // i64.extend_i32_u
		case "f32":
			if isUnsigned(srcType) {
				buf.WriteByte(0xb3) // f32.convert_i32_u
			} else {
				buf.WriteByte(0xb2) // f32.convert_i32_s
			}
		case "f64":
			if isUnsigned(srcType) {
				buf.WriteByte(0xb8) // f64.convert_i32_u
			} else {
				buf.WriteByte(0xb7) // f64.convert_i32_s
			}
		}
		return
	}

	if srcType == "i64" || srcType == "u64" {
		switch {
		case isDstI32:
			buf.WriteByte(0xa7) // i32.wrap_i64
			if dstType == "u8" {
				buf.WriteByte(0x41)
				writeSleb128(buf, 0xff)
				buf.WriteByte(0x71)
			}
		case dstType == "f32":
			if srcType == "u64" {
				buf.WriteByte(0xb5) // f32.convert_i64_u
			} else {
				buf.WriteByte(0xb4) // f32.convert_i64_s
			}
		case dstType == "f64":
			if srcType == "u64" {
				buf.WriteByte(0xba) // f64.convert_i64_u
			} else {
				buf.WriteByte(0xb9) // f64.convert_i64_s
			}
		}
		return
	}

	if srcType == "f32" {
		switch {
		case isDstI32:
			if isUnsigned(dstType) {
				buf.WriteByte(0xa9) // i32.trunc_f32_u
			} else {
				buf.WriteByte(0xa8) // i32.trunc_f32_s
			}
		case dstType == "i64":
			buf.WriteByte(0xae) // i64.trunc_f32_s
		case dstType == "u64":
			buf.WriteByte(0xaf) // i64.trunc_f32_u
		case dstType == "f64":
			buf.WriteByte(0xbb) // f64.promote_f32
		}
		return
	}

	if srcType == "f64" {
		switch {
		case isDstI32:
			if isUnsigned(dstType) {
				buf.WriteByte(0xab) // i32.trunc_f64_u
			} else {
				buf.WriteByte(0xaa) // i32.trunc_f64_s
			}
		case dstType == "i64":
			buf.WriteByte(0xb0) // i64.trunc_f64_s
		case dstType == "u64":
			buf.WriteByte(0xb1) // i64.trunc_f64_u
		case dstType == "f32":
			buf.WriteByte(0xb6) // f32.demote_f64
		}
		return
	}
}

func isI32Like(t string) bool {
	switch t {
	case "i8", "u8", "i16", "u16", "i32", "u32", "bool", "":
		return true
	}
	return false
}

func isUnsigned(t string) bool {
	switch t {
	case "u8", "u16", "u32", "u64":
		return true
	}
	return false
}
