// File: pkg/compiler/wasm_emit.go
package compiler

import "github.com/JohnEsleyer/ube/pkg/wasm"

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// EmitWasm compiles an AST program directly into WebAssembly binary bytecode (.wasm)
// with zero external toolchains (no clang, no zig, no C headers).
func EmitWasm(prog *Program) (wasmBytes []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("internal compiler error (ICE): %v\nPlease report this issue", r)
		}
	}()
	emitter := newWasmEmitter()
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

	scratchCiovec = 0  // Offset 0..7: ciovec { ptr: i32, len: i32 }
	scratchNwrite = 8  // Offset 8..11: nwritten i32
	scratchItoa   = 16 // Offset 16..47: itoa string buffer
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
	wasiImports   map[string]uint32
	fdWriteIdx    uint32
	clockTimeIdx  uint32
	structs       map[string]structMeta
	enums         map[string][]VariantDecl
	traits        map[string]*TraitDecl
	typeIDs       map[string]int32
	moduleAliases map[string]string
	trueHeader    uint32
	falseHeader   uint32
}

func newWasmEmitter() *wasmEmitter {
	return &wasmEmitter{
		typeMap:       make(map[string]uint32),
		fnIndices:     make(map[string]uint32),
		methodIndices: make(map[string]uint32),
		fnReturns:     make(map[string]UbeType),
		wasiImports:   make(map[string]uint32),
		structs:       make(map[string]structMeta),
		enums:         make(map[string][]VariantDecl),
		traits:        make(map[string]*TraitDecl),
		typeIDs:       make(map[string]int32),
		moduleAliases: make(map[string]string),
		dataOffset:    staticDataOff,
	}
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

func (e *wasmEmitter) compile(prog *Program) ([]byte, error) {
	if prog != nil {
		for alias, prefix := range prog.ModuleAliases {
			e.moduleAliases[alias] = prefix
		}
	}

	// 1. Register WASI snapshot preview1 imports
	fdWriteType := e.registerType([]byte{valTypeI32, valTypeI32, valTypeI32, valTypeI32}, []byte{valTypeI32})
	e.fdWriteIdx = 0
	e.imports = append(e.imports, importMeta{
		module:  "wasi_snapshot_preview1",
		name:    "fd_write",
		typeIdx: fdWriteType,
	})

	clockTimeType := e.registerType([]byte{valTypeI32, valTypeI64, valTypeI32}, []byte{valTypeI32})
	e.clockTimeIdx = 1
	e.imports = append(e.imports, importMeta{
		module:  "wasi_snapshot_preview1",
		name:    "clock_time_get",
		typeIdx: clockTimeType,
	})
	e.wasiImports["fd_write"] = e.fdWriteIdx
	e.wasiImports["clock_time_get"] = e.clockTimeIdx

	// Additional WASI preview1 imports used by std/fs and std/io. Registering
	// them up-front keeps the emitter's raw syscall intrinsics simple.
	e.registerWasiImport("fd_read", []byte{valTypeI32, valTypeI32, valTypeI32, valTypeI32}, []byte{valTypeI32})
	e.registerWasiImport("fd_close", []byte{valTypeI32}, []byte{valTypeI32})
	e.registerWasiImport("fd_sync", []byte{valTypeI32}, []byte{valTypeI32})
	e.registerWasiImport("fd_seek", []byte{valTypeI32, valTypeI64, valTypeI32, valTypeI32}, []byte{valTypeI32})
	e.registerWasiImport("fd_filestat_get", []byte{valTypeI32, valTypeI32}, []byte{valTypeI32})
	e.registerWasiImport("path_open", []byte{valTypeI32, valTypeI32, valTypeI32, valTypeI32, valTypeI32, valTypeI64, valTypeI64, valTypeI32, valTypeI32}, []byte{valTypeI32})
	e.registerWasiImport("path_create_directory", []byte{valTypeI32, valTypeI32, valTypeI32}, []byte{valTypeI32})
	e.registerWasiImport("path_unlink_file", []byte{valTypeI32, valTypeI32, valTypeI32}, []byte{valTypeI32})
	e.registerWasiImport("path_remove_directory", []byte{valTypeI32, valTypeI32, valTypeI32}, []byte{valTypeI32})
	e.registerWasiImport("path_filestat_get", []byte{valTypeI32, valTypeI32, valTypeI32, valTypeI32, valTypeI32}, []byte{valTypeI32})
	e.registerWasiImport("proc_exit", []byte{valTypeI32}, []byte{})

	// Register user host imports (extern fn)
	for _, decl := range prog.Decls {
		if ext, ok := decl.(*ExternFnDecl); ok {
			modName := ext.Module
			if modName == "" {
				modName = "env"
			}
			if modName == "wasi_snapshot_preview1" {
				if idx, exists := e.wasiImports[ext.Name]; exists {
					e.fnIndices[ext.Name] = idx
					e.fnReturns[ext.Name] = ext.RetType
					continue
				}
			}
			var params []byte
			for _, p := range ext.Params {
				if p.Type != nil && p.Type.String() == "i64" {
					params = append(params, valTypeI64)
				} else {
					params = append(params, valTypeI32)
				}
			}
			var results []byte
			if ext.RetType != nil && ext.RetType.String() != "void" {
				switch ext.RetType.String() {
				case "f32":
					results = append(results, valTypeF32)
				case "f64":
					results = append(results, valTypeF64)
				case "i64":
					results = append(results, valTypeI64)
				default:
					results = append(results, valTypeI32)
				}
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
	// Pass 1: seed empty meta for every struct so recursive sizeof lookups
	// can terminate. Also register enums and traits.
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

	// sizeofStruct computes the total in-memory size of a struct, aligning
	// every field to its own Align() and advancing by its true ByteSize().
	// CustomType fields recurse (their ByteSize() reports the fat-pointer
	// size of 4, which is the wrong value for inline storage). The result
	// is memoized in meta.totalSize; the `seen` guard breaks any cycle.
	var sizeofStruct func(name string, seen map[string]bool) int
	sizeofStruct = func(name string, seen map[string]bool) int {
		meta, ok := e.structs[name]
		if !ok {
			return 4
		}
		if meta.totalSize > 0 {
			return meta.totalSize
		}
		if seen[name] {
			return 4
		}
		seen[name] = true
		sd, ok := structDecls[name]
		if !ok {
			return 4
		}
		offset := 0
		for _, f := range sd.Fields {
			sz := structFieldSize(f.Type, sizeofStruct, seen)
			al := f.Type.Align()
			if al < 1 {
				al = 1
			}
			offset = alignUpInt(offset, al)
			offset += sz
		}
		if offset == 0 {
			offset = 4
		}
		offset = alignUpInt(offset, 4)
		meta.totalSize = offset
		e.structs[name] = meta
		return offset
	}

	// Pass 2: fill in real fieldOffsets / fieldTypes using the recursive
	// size computation. Field start positions are aligned to the field
	// type's Align() so i64/f64 fields land on 8-byte boundaries.
	for _, name := range structOrder {
		sd := structDecls[name]
		meta := e.structs[name]
		offset := 0
		for _, f := range sd.Fields {
			al := f.Type.Align()
			if al < 1 {
				al = 1
			}
			offset = alignUpInt(offset, al)
			meta.fieldOffsets[f.Name] = offset
			meta.fieldTypes[f.Name] = f.Type
			offset += structFieldSize(f.Type, sizeofStruct, map[string]bool{})
		}
		if offset == 0 {
			offset = 4
		}
		offset = alignUpInt(offset, 4)
		meta.totalSize = offset
		e.structs[name] = meta
	}

	// After every direct struct is registered, walk each struct's anonymous
	// embedding chain and register promoted field offsets under dotted keys.
	// This makes `h.pos_x` resolve to the same offset as `h.Transform3D.pos_x`
	// when the typechecker has set MemberExpr.EmbedPath.
	for _, decl := range prog.Decls {
		sd, ok := decl.(*StructDecl)
		if !ok {
			continue
		}
		meta, ok := e.structs[sd.Name]
		if !ok {
			continue
		}
		e.registerPromotedFields(&meta, sd, nil, map[string]bool{})
		e.structs[sd.Name] = meta
	}

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

	// Register helper function indices
	printIntIdx := funcOffset + uint32(len(fnDecls))
	e.fnIndices["__print_i32"] = printIntIdx

	printStrIdx := printIntIdx + 1
	e.fnIndices["__print_str"] = printStrIdx

	printBoolIdx := printStrIdx + 1
	e.fnIndices["__print_bool"] = printBoolIdx

	strConcatIdx := printBoolIdx + 1
	e.fnIndices["__str_concat"] = strConcatIdx

	strEqIdx := strConcatIdx + 1
	e.fnIndices["__str_eq"] = strEqIdx

	intToStrIdx := strEqIdx + 1
	e.fnIndices["__i32_to_str"] = intToStrIdx

	startIdx := intToStrIdx + 1
	e.fnIndices["_start"] = startIdx

	// Register types for defined functions
	for _, fn := range fnDecls {
		var params []byte
		if fn.Receiver != nil {
			params = append(params, valTypeI32)
		}
		for _, p := range fn.Params {
			if p.Type != nil && p.Type.String() == "f32" {
				params = append(params, valTypeF32)
			} else {
				params = append(params, valTypeI32)
			}
		}
		var results []byte
		if fn.RetType != nil && fn.RetType.String() != "void" {
			if fn.RetType.String() == "f32" {
				results = append(results, valTypeF32)
			} else {
				results = append(results, valTypeI32)
			}
		}
		e.funcs = append(e.funcs, e.registerType(params, results))
	}

	// Helper types:
	e.funcs = append(e.funcs, e.registerType([]byte{valTypeI32, valTypeI32}, []byte{}))
	e.funcs = append(e.funcs, e.registerType([]byte{valTypeI32, valTypeI32, valTypeI32}, []byte{}))
	e.funcs = append(e.funcs, e.registerType([]byte{valTypeI32, valTypeI32}, []byte{}))
	e.funcs = append(e.funcs, e.registerType([]byte{valTypeI32, valTypeI32}, []byte{valTypeI32}))
	e.funcs = append(e.funcs, e.registerType([]byte{valTypeI32, valTypeI32}, []byte{valTypeI32}))
	e.funcs = append(e.funcs, e.registerType([]byte{valTypeI32}, []byte{valTypeI32}))
	e.funcs = append(e.funcs, e.registerType([]byte{}, []byte{}))

	// Compile user function bodies
	for _, fn := range fnDecls {
		body, err := e.compileFunction(fn)
		if err != nil {
			return nil, fmt.Errorf("function %s: %w", fn.Name, err)
		}
		e.codeBodies = append(e.codeBodies, body)
	}

	// Compile built-in helper bodies
	e.codeBodies = append(e.codeBodies, e.buildPrintIntFunc())
	e.codeBodies = append(e.codeBodies, e.buildPrintStrFunc())
	e.codeBodies = append(e.codeBodies, e.buildPrintBoolFunc())
	e.codeBodies = append(e.codeBodies, e.buildStrConcatFunc())
	e.codeBodies = append(e.codeBodies, e.buildStrEqFunc())
	e.codeBodies = append(e.codeBodies, e.buildIntToStrFunc())
	e.codeBodies = append(e.codeBodies, e.buildStartFunc(e.fnIndices["main"]))

	return e.assembleBinary()
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
	loopBodyDepths []int
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

	paramIdx := uint32(0)
	if fn.Receiver != nil {
		ctx.locals[fn.Receiver.Name] = paramIdx
		ctx.localTypes[fn.Receiver.Name] = fn.Receiver.Type
		paramIdx++
	}
	for _, param := range fn.Params {
		ctx.locals[param.Name] = paramIdx
		ctx.localTypes[param.Name] = param.Type
		paramIdx++
	}
	ctx.numParams = paramIdx

	var bodyBuf bytes.Buffer
	if err := ctx.compileBlock(&bodyBuf, fn.Body); err != nil {
		return nil, err
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
	codeBuf.Write(bodyBuf.Bytes())

	var fullBuf bytes.Buffer
	writeUleb128(&fullBuf, uint32(codeBuf.Len()))
	fullBuf.Write(codeBuf.Bytes())
	return fullBuf.Bytes(), nil
}

func (ctx *fnContext) allocLocal(t byte) uint32 {
	idx := ctx.numParams + uint32(len(ctx.allocTypes))
	ctx.allocTypes = append(ctx.allocTypes, t)
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

func (ctx *fnContext) compileStmt(buf *bytes.Buffer, stmt Stmt) error {
	switch s := stmt.(type) {
	case *LetStmt:
		localType := byte(valTypeI32)
		var t UbeType
		if s.Value != nil {
			if inferred := ctx.inferExprType(s.Value); inferred != "" {
				t = CustomType{Name: inferred}
			}
		}
		if t == nil && s.Type != nil {
			t = s.Type
		}
		if t == nil && s.ValueType != nil {
			t = s.ValueType
		}
		if t != nil {
			switch t.String() {
			case "f32":
				localType = valTypeF32
			case "f64":
				localType = valTypeF64
			case "i64":
				localType = valTypeI64
			}
		}
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
				if err := emitCompoundOp(buf, s.Op, ctx.inferExprType(s.RHS)); err != nil {
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
				if err := emitCompoundOp(buf, s.Op, ctx.inferExprType(s.RHS)); err != nil {
					return err
				}
			} else if isAddressMember(fieldType) {
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
			retValWasm := byte(valTypeI32)
			if ctx.fn.RetType != nil {
				switch ctx.fn.RetType.String() {
				case "f32":
					retValWasm = valTypeF32
				case "f64":
					retValWasm = valTypeF64
				case "i64":
					retValWasm = valTypeI64
				}
			}
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

			itemLocal := ctx.allocLocal(valTypeI32)
			ctx.locals[s.ItemName] = itemLocal

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
			writeSleb128(buf, 4)
			buf.WriteByte(0x6c)
			buf.WriteByte(0x6a)
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
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

		bindingType := unwrapBindingType(ctx.exprStaticType(s.Expr))
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

		varLocal := ctx.allocLocal(valTypeI32)
		ctx.locals[s.VarName] = varLocal
		ctx.localTypes[s.VarName] = unwrapBindingType(ctx.exprStaticType(s.Call))
		buf.WriteByte(0x20)
		writeUleb128(buf, optLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)
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
		buf.WriteByte(0x1a)

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

func (ctx *fnContext) compileExpr(buf *bytes.Buffer, expr Expr) error {
	switch e := expr.(type) {
	case *CastExpr:
		if err := ctx.compileExpr(buf, e.Expr); err != nil {
			return err
		}
		srcType := ctx.inferExprType(e.Expr)
		dstType := e.TargetType.String()
		emitCastOp(buf, srcType, dstType)
		return nil
	case *IfExpr:
		if err := ctx.compileExpr(buf, e.Cond); err != nil {
			return err
		}
		resultVal := byte(valTypeVoid)
		if e.Type != nil {
			switch e.Type.String() {
			case "f32":
				resultVal = valTypeF32
			case "f64":
				resultVal = valTypeF64
			case "i64":
				resultVal = valTypeI64
			case "void":
				resultVal = valTypeVoid
			default:
				resultVal = valTypeI32
			}
		} else {
			resultVal = valTypeI32
		}
		ctx.openLabel(buf, 0x04)
		buf.WriteByte(resultVal)
		if err := ctx.compileExprArm(buf, e.Then); err != nil {
			return err
		}
		buf.WriteByte(0x05)
		if err := ctx.compileExprArm(buf, e.Else); err != nil {
			return err
		}
		ctx.closeLabel(buf)
		return nil
	case *LiteralExpr:
		switch e.Type.String() {
		case "i32", "u8":
			var val int32
			fmt.Sscanf(e.Val, "%d", &val)
			buf.WriteByte(0x41)
			writeSleb128(buf, val)
		case "f32":
			var val float32
			fmt.Sscanf(e.Val, "%f", &val)
			buf.WriteByte(0x43)
			bits := math.Float32bits(val)
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], bits)
			buf.Write(b[:])
		case "bool":
			buf.WriteByte(0x41)
			if e.Val == "true" {
				writeSleb128(buf, 1)
			} else {
				writeSleb128(buf, 0)
			}
		case "none":
			hdr := ctx.allocLocal(valTypeI32)
			ctx.emitAlloc(buf, 8, hdr)
			buf.WriteByte(0x20)
			writeUleb128(buf, hdr)
			buf.WriteByte(0x41)
			writeSleb128(buf, 0)
			buf.WriteByte(0x36)
			writeUleb128(buf, 2)
			writeUleb128(buf, 0)
			buf.WriteByte(0x20)
			writeUleb128(buf, hdr)
		case "string":
			off := ctx.emitter.internString(e.Val)
			buf.WriteByte(0x41)
			writeSleb128(buf, int32(off))
		default:
			buf.WriteByte(0x41)
			writeSleb128(buf, 0)
		}

	case *UnaryExpr:
		if err := ctx.compileExpr(buf, e.Operand); err != nil {
			return err
		}
		switch e.Op {
		case "!":
			buf.WriteByte(0x45)
		case "-":
			switch ctx.inferExprType(e.Operand) {
			case "f32":
				buf.WriteByte(0x8c)
			case "f64":
				buf.WriteByte(0x9a)
			default:
				buf.WriteByte(0x41)
				writeSleb128(buf, -1)
				buf.WriteByte(0x6c)
			}
		case "~":
			buf.WriteByte(0x41)
			writeSleb128(buf, -1)
			buf.WriteByte(0x73)
		}

	case *IdentExpr:
		localIdx, exists := ctx.locals[e.Name]
		if !exists {
			return fmt.Errorf("unknown variable %s", e.Name)
		}
		buf.WriteByte(0x20)
		writeUleb128(buf, localIdx)

	case *BinaryExpr:
		if (e.Op == "==" || e.Op == "!=") && (e.Left != nil && (e.OperandType != nil && e.OperandType.String() == "string")) {
			if err := ctx.compileExpr(buf, e.Left); err != nil {
				return err
			}
			if err := ctx.compileExpr(buf, e.Right); err != nil {
				return err
			}
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__str_eq"])
			if e.Op == "!=" {
				buf.WriteByte(0x45)
			}
			return nil
		}

		if e.Op == "+" && (e.Type != nil && e.Type.String() == "string") {
			if err := ctx.compileExpr(buf, e.Left); err != nil {
				return err
			}
			if err := ctx.compileExpr(buf, e.Right); err != nil {
				return err
			}
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__str_concat"])
			return nil
		}

		if err := ctx.compileExpr(buf, e.Left); err != nil {
			return err
		}
		if err := ctx.compileExpr(buf, e.Right); err != nil {
			return err
		}

		isF32 := (e.OperandType != nil && e.OperandType.String() == "f32") || (e.Type != nil && e.Type.String() == "f32")
		if isF32 {
			switch e.Op {
			case "+":
				buf.WriteByte(0x92)
			case "-":
				buf.WriteByte(0x93)
			case "*":
				buf.WriteByte(0x94)
			case "/":
				buf.WriteByte(0x95)
			case "==":
				buf.WriteByte(0x5b)
			case "!=":
				buf.WriteByte(0x5c)
			case "<":
				buf.WriteByte(0x5d)
			case "<=":
				buf.WriteByte(0x5f)
			case ">":
				buf.WriteByte(0x5e)
			case ">=":
				buf.WriteByte(0x60)
			}
		} else {
			switch e.Op {
			case "+":
				buf.WriteByte(0x6a)
			case "-":
				buf.WriteByte(0x6b)
			case "*":
				buf.WriteByte(0x6c)
			case "/":
				buf.WriteByte(0x6d)
			case "%":
				buf.WriteByte(0x6f)
			case "==":
				buf.WriteByte(0x46)
			case "!=":
				buf.WriteByte(0x47)
			case "<":
				buf.WriteByte(0x48)
			case "<=":
				buf.WriteByte(0x4c)
			case ">":
				buf.WriteByte(0x4a)
			case ">=":
				buf.WriteByte(0x4e)
			case "&&", "&":
				buf.WriteByte(0x71)
			case "||", "|":
				buf.WriteByte(0x72)
			case "^":
				buf.WriteByte(0x73)
			case "<<":
				buf.WriteByte(0x74)
			case ">>":
				buf.WriteByte(0x75)
			}
		}

	case *IndexExpr:
		if err := ctx.compileExpr(buf, e.Target); err != nil {
			return err
		}
		if st, isSlice := e.TargetType.(SliceType); isSlice {
			// Deref the header pointer to get the data ptr.
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 0)
			width, loadOp, _, align := sliceElemInfo(st.Elem)
			if err := ctx.compileExpr(buf, e.Index); err != nil {
				return err
			}
			if width != 1 {
				buf.WriteByte(0x41)
				writeSleb128(buf, width)
				buf.WriteByte(0x6c) // i32.mul
			}
			buf.WriteByte(0x6a) // i32.add
			buf.WriteByte(loadOp)
			writeUleb128(buf, align)
			writeUleb128(buf, 0)
			return nil
		} else if _, isStr := e.TargetType.(StringType); isStr {
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 0)
			if err := ctx.compileExpr(buf, e.Index); err != nil {
				return err
			}
			buf.WriteByte(0x6a)
			buf.WriteByte(0x2d)
			writeUleb128(buf, 0)
			writeUleb128(buf, 0)
			return nil
		} else if at, isArr := e.TargetType.(ArrayType); isArr {
			width, loadOp, _, align := sliceElemInfo(at.Elem)
			if err := ctx.compileExpr(buf, e.Index); err != nil {
				return err
			}
			if width != 1 {
				buf.WriteByte(0x41)
				writeSleb128(buf, width)
				buf.WriteByte(0x6c) // i32.mul
			}
			buf.WriteByte(0x6a) // i32.add
			buf.WriteByte(loadOp)
			writeUleb128(buf, align)
			writeUleb128(buf, 0)
			return nil
		}
		if err := ctx.compileExpr(buf, e.Index); err != nil {
			return err
		}
		buf.WriteByte(0x41)
		writeSleb128(buf, 4)
		buf.WriteByte(0x6c)
		buf.WriteByte(0x6a)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

	case *MemberExpr:
		if e.Member == "len" {
			if arr, ok := e.TargetType.(ArrayType); ok {
				buf.WriteByte(0x41)
				writeSleb128(buf, int32(arr.Size))
				return nil
			}
			if err := ctx.compileExpr(buf, e.Target); err != nil {
				return err
			}
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 4)
			return nil
		}
		if e.Member == "cap" {
			if arr, ok := e.TargetType.(ArrayType); ok {
				buf.WriteByte(0x41)
				writeSleb128(buf, int32(arr.Size))
				return nil
			}
			if err := ctx.compileExpr(buf, e.Target); err != nil {
				return err
			}
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 8)
			return nil
		}

		if targetId, ok := e.Target.(*IdentExpr); ok {
			if variants, ok := ctx.emitter.enums[targetId.Name]; ok {
				for tagIdx, v := range variants {
					if v.Name == e.Member {
						enumHdr := ctx.allocLocal(valTypeI32)
						ctx.emitAlloc(buf, 4, enumHdr)
						buf.WriteByte(0x20)
						writeUleb128(buf, enumHdr)
						buf.WriteByte(0x41)
						writeSleb128(buf, int32(tagIdx))
						buf.WriteByte(0x36)
						writeUleb128(buf, 2)
						writeUleb128(buf, 0)
						buf.WriteByte(0x20)
						writeUleb128(buf, enumHdr)
						return nil
					}
				}
			}
		}

		structName := e.TargetType.String()
		meta, ok := ctx.emitter.structs[structName]
		if !ok {
			return fmt.Errorf("unknown struct type %s", structName)
		}
		fieldKey := memberFieldKey(e)
		offset, ok := meta.fieldOffsets[fieldKey]
		if !ok {
			return fmt.Errorf("unknown field %s on struct %s", fieldKey, structName)
		}
		fieldType := meta.fieldTypes[fieldKey]
		if isAddressMember(fieldType) {
			// Compound-valued field (nested struct or fixed array): return a
			// pointer into the owner's inline storage, not a scalar load.
			if err := ctx.compileExpr(buf, e.Target); err != nil {
				return err
			}
			if offset != 0 {
				buf.WriteByte(0x41)
				writeSleb128(buf, int32(offset))
				buf.WriteByte(0x6a)
			}
			return nil
		}
		if err := ctx.compileExpr(buf, e.Target); err != nil {
			return err
		}
		loadOp, _, loadAl := fieldLoadStoreInfo(fieldType)
		buf.WriteByte(loadOp)
		writeUleb128(buf, loadAl)
		writeUleb128(buf, uint32(offset))

	case *SliceSubExpr:
		// Determine element width and header size from the target type.
		// A string is {ptr, len} = 8 bytes with byte elements (width 1).
		// A []T is {ptr, len, cap} = 12 bytes with element width sizeof(T).
		// For []T we currently assume 4-byte elements; this matches the
		// ArrayInitExpr lowering and covers i32/f32/bool. Widen to 8 when
		// i64/f64 element slices are added.
		elemWidth := int32(1)
		hdrSize := int32(8)
		isSlice := false
		if e.TargetType != nil {
			switch tt := e.TargetType.(type) {
			case StringType:
				elemWidth = 1
				hdrSize = 8
				isSlice = false
			case SliceType:
				elemWidth = 4
				hdrSize = 12
				isSlice = true
				if tt.Elem != nil {
					switch tt.Elem.String() {
					case "i64", "u64", "f64":
						elemWidth = 8
					}
				}
			case ArrayType:
				elemWidth = 4
				hdrSize = 12
				isSlice = true
				if tt.Elem != nil {
					switch tt.Elem.String() {
					case "i64", "u64", "f64":
						elemWidth = 8
					}
				}
			}
		}

		targetLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, e.Target); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, targetLocal)

		startLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, e.Start); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, startLocal)

		endLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, e.End); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, endLocal)

		hdrLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, hdrSize, hdrLocal)

		// hdr[0] = target.ptr + start * elemWidth
		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)
		buf.WriteByte(0x20)
		writeUleb128(buf, targetLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)
		buf.WriteByte(0x20)
		writeUleb128(buf, startLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, elemWidth)
		buf.WriteByte(0x6c)
		buf.WriteByte(0x6a)
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

		// hdr[4] = end - start  (len)
		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)
		buf.WriteByte(0x20)
		writeUleb128(buf, endLocal)
		buf.WriteByte(0x20)
		writeUleb128(buf, startLocal)
		buf.WriteByte(0x6b)
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)

		// For []T only: hdr[8] = cap = end - start
		if isSlice {
			buf.WriteByte(0x20)
			writeUleb128(buf, hdrLocal)
			buf.WriteByte(0x20)
			writeUleb128(buf, endLocal)
			buf.WriteByte(0x20)
			writeUleb128(buf, startLocal)
			buf.WriteByte(0x6b)
			buf.WriteByte(0x36)
			writeUleb128(buf, 2)
			writeUleb128(buf, 8)
		}

		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)

	case *ArrayInitExpr:
		elemCount := len(e.Elements)
		elemSize := 4
		storeOp := byte(0x36)
		storeAlign := uint32(2)
		if e.Elem != nil {
			elemSize = e.Elem.ByteSize()
			if elemSize < 1 {
				elemSize = 4
			}
			_, storeOp, storeAlign = fieldLoadStoreInfo(e.Elem)
		}
		totalSize := elemCount * elemSize
		baseLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, int32(totalSize), baseLocal)

		for i, el := range e.Elements {
			buf.WriteByte(0x20)
			writeUleb128(buf, baseLocal)
			if err := ctx.compileExpr(buf, el); err != nil {
				return err
			}
			buf.WriteByte(storeOp)
			writeUleb128(buf, storeAlign)
			writeUleb128(buf, uint32(i*elemSize))
		}

		buf.WriteByte(0x20)
		writeUleb128(buf, baseLocal)

	case *SliceInitExpr:
		elemCount := len(e.Elements)
		capCount := elemCount
		if capCount < 4 {
			capCount = 4
		}
		dataLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, int32(capCount*4), dataLocal)

		for i, el := range e.Elements {
			buf.WriteByte(0x20)
			writeUleb128(buf, dataLocal)
			if err := ctx.compileExpr(buf, el); err != nil {
				return err
			}
			buf.WriteByte(0x36)
			writeUleb128(buf, 2)
			writeUleb128(buf, uint32(i*4))
		}

		headerLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, 12, headerLocal)

		buf.WriteByte(0x20)
		writeUleb128(buf, headerLocal)
		buf.WriteByte(0x20)
		writeUleb128(buf, dataLocal)
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

		buf.WriteByte(0x20)
		writeUleb128(buf, headerLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, int32(elemCount))
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)

		buf.WriteByte(0x20)
		writeUleb128(buf, headerLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, int32(capCount))
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 8)

		buf.WriteByte(0x20)
		writeUleb128(buf, headerLocal)

	case *ArrayToSliceExpr:
		arrLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, e.Value); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, arrLocal)

		hdrLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, 12, hdrLocal)

		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)
		buf.WriteByte(0x20)
		writeUleb128(buf, arrLocal)
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, int32(e.ArrayType.Size))
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)

		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, int32(e.ArrayType.Size))
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 8)

		buf.WriteByte(0x20)
		writeUleb128(buf, hdrLocal)

	case *StructInitExpr:
		meta, ok := ctx.emitter.structs[e.Name]
		if !ok {
			return fmt.Errorf("unknown struct %s in init", e.Name)
		}
		structBase := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, int32(meta.totalSize), structBase)

		for _, f := range e.Fields {
			offset := meta.fieldOffsets[f.Name]
			fieldType := meta.fieldTypes[f.Name]
			if isAddressMember(fieldType) {
				// mem.copy(structBase+offset, src_ptr, sizeofInline).
				// Stack order: [dst, src, len] then memory.copy.
				buf.WriteByte(0x20)
				writeUleb128(buf, structBase)
				if offset != 0 {
					buf.WriteByte(0x41)
					writeSleb128(buf, int32(offset))
					buf.WriteByte(0x6a)
				}
				if err := ctx.compileExpr(buf, f.Val); err != nil {
					return err
				}
				buf.WriteByte(0x41)
				writeSleb128(buf, int32(ctx.emitter.sizeofInline(fieldType)))
				ctx.emitMemoryCopy(buf)
				continue
			}
			buf.WriteByte(0x20)
			writeUleb128(buf, structBase)
			if err := ctx.compileExpr(buf, f.Val); err != nil {
				return err
			}
			_, storeOp, storeAl := fieldLoadStoreInfo(fieldType)
			buf.WriteByte(storeOp)
			writeUleb128(buf, storeAl)
			writeUleb128(buf, uint32(offset))
		}

		buf.WriteByte(0x20)
		writeUleb128(buf, structBase)

	case *TupleExpr:
		tupBase := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, int32(len(e.Elems)*4), tupBase)
		for i, el := range e.Elems {
			buf.WriteByte(0x20)
			writeUleb128(buf, tupBase)
			if err := ctx.compileExpr(buf, el); err != nil {
				return err
			}
			buf.WriteByte(0x36)
			writeUleb128(buf, 2)
			writeUleb128(buf, uint32(i*4))
		}
		buf.WriteByte(0x20)
		writeUleb128(buf, tupBase)

	case *PromoteExpr:
		if err := ctx.compileExpr(buf, e.Value); err != nil {
			return err
		}

	case *ArenaExpr:
		savedLocal := ctx.allocLocal(valTypeI32)
		buf.WriteByte(0x23)
		writeUleb128(buf, 0)
		buf.WriteByte(0x21)
		writeUleb128(buf, savedLocal)

		if err := ctx.compileBlock(buf, e.Body); err != nil {
			return err
		}

		buf.WriteByte(0x20)
		writeUleb128(buf, savedLocal)
		buf.WriteByte(0x24)
		writeUleb128(buf, 0)

	case *TraitCoerceExpr:
		fatLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, 8, fatLocal)

		buf.WriteByte(0x20)
		writeUleb128(buf, fatLocal)
		if err := ctx.compileExpr(buf, e.Value); err != nil {
			return err
		}
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

		typeID := ctx.emitter.typeIDs[e.Concrete.String()]
		buf.WriteByte(0x20)
		writeUleb128(buf, fatLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, typeID)
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)

		buf.WriteByte(0x20)
		writeUleb128(buf, fatLocal)

	case *TryExpr:
		resLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, e.Operand); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, resLocal)

		buf.WriteByte(0x20)
		writeUleb128(buf, resLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)
		buf.WriteByte(0x45)
		buf.WriteByte(0x04)
		buf.WriteByte(valTypeVoid)

		ctx.emitAllDefers(buf)

		buf.WriteByte(0x20)
		writeUleb128(buf, resLocal)
		buf.WriteByte(0x0f)
		buf.WriteByte(0x0b)

		buf.WriteByte(0x20)
		writeUleb128(buf, resLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)

	case *OrElseExpr:
		resWasm := byte(valTypeI32)
		switch ctx.inferExprType(e.Fallback) {
		case "f32":
			resWasm = valTypeF32
		case "f64":
			resWasm = valTypeF64
		case "i64":
			resWasm = valTypeI64
		}

		optLocal := ctx.allocLocal(valTypeI32)
		if err := ctx.compileExpr(buf, e.Primary); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, optLocal)

		resLocal := ctx.allocLocal(resWasm)

		buf.WriteByte(0x20)
		writeUleb128(buf, optLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)
		buf.WriteByte(0x04)
		buf.WriteByte(valTypeVoid)

		buf.WriteByte(0x20)
		writeUleb128(buf, optLocal)
		buf.WriteByte(0x28)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)
		switch resWasm {
		case valTypeF32:
			buf.WriteByte(0xbe)
		case valTypeF64:
			buf.WriteByte(0xad)
			buf.WriteByte(0xbf)
		case valTypeI64:
			buf.WriteByte(0xad)
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, resLocal)

		buf.WriteByte(0x05)

		if err := ctx.compileExpr(buf, e.Fallback); err != nil {
			return err
		}
		buf.WriteByte(0x21)
		writeUleb128(buf, resLocal)

		buf.WriteByte(0x0b)

		buf.WriteByte(0x20)
		writeUleb128(buf, resLocal)

	case *ErrorExpr:
		errLocal := ctx.allocLocal(valTypeI32)
		ctx.emitAlloc(buf, 8, errLocal)

		buf.WriteByte(0x20)
		writeUleb128(buf, errLocal)
		buf.WriteByte(0x41)
		writeSleb128(buf, 0)
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 0)

		buf.WriteByte(0x20)
		writeUleb128(buf, errLocal)
		if err := ctx.compileExpr(buf, e.Arg); err != nil {
			return err
		}
		buf.WriteByte(0x36)
		writeUleb128(buf, 2)
		writeUleb128(buf, 4)

		buf.WriteByte(0x20)
		writeUleb128(buf, errLocal)

	case *InterpolatedStringExpr:
		resHdr := ctx.allocLocal(valTypeI32)
		emptyOff := ctx.emitter.internString("")
		buf.WriteByte(0x41)
		writeSleb128(buf, int32(emptyOff))
		buf.WriteByte(0x21)
		writeUleb128(buf, resHdr)

		for _, p := range e.Parts {
			buf.WriteByte(0x20)
			writeUleb128(buf, resHdr)
			if err := ctx.compileStringPart(buf, p); err != nil {
				return err
			}
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__str_concat"])
			buf.WriteByte(0x21)
			writeUleb128(buf, resHdr)
		}
		buf.WriteByte(0x20)
		writeUleb128(buf, resHdr)

	case *CallExpr:
		if handled, err := ctx.compileSpecialCall(buf, e); handled {
			return err
		}
		if mem, ok := e.Callee.(*MemberExpr); ok {
			if targetId, ok := mem.Target.(*IdentExpr); ok {
				if variants, ok := ctx.emitter.enums[targetId.Name]; ok {
					for tagIdx, v := range variants {
						if v.Name == mem.Member {
							size := 4 + len(e.Args)*4
							enumHdr := ctx.allocLocal(valTypeI32)
							ctx.emitAlloc(buf, int32(size), enumHdr)

							buf.WriteByte(0x20)
							writeUleb128(buf, enumHdr)
							buf.WriteByte(0x41)
							writeSleb128(buf, int32(tagIdx))
							buf.WriteByte(0x36)
							writeUleb128(buf, 2)
							writeUleb128(buf, 0)

							for aIdx, arg := range e.Args {
								buf.WriteByte(0x20)
								writeUleb128(buf, enumHdr)
								if err := ctx.compileExpr(buf, arg); err != nil {
									return err
								}
								storeOp := byte(0x36)
								storeAl := uint32(2)
								if aIdx < len(v.Types) && v.Types[aIdx] != nil {
									switch v.Types[aIdx].String() {
									case "f32":
										storeOp = 0x38
									case "f64":
										storeOp = 0x39
										storeAl = 3
									case "i64":
										storeOp = 0x37
										storeAl = 3
									}
								}
								buf.WriteByte(storeOp)
								writeUleb128(buf, storeAl)
								writeUleb128(buf, uint32(4+aIdx*4))
							}

							buf.WriteByte(0x20)
							writeUleb128(buf, enumHdr)
							return nil
						}
					}
				}
			}
		}

		if mem, ok := e.Callee.(*MemberExpr); ok {
			if _, isSlice := mem.TargetType.(SliceType); isSlice {
				switch mem.Member {
				case "resize":
					if len(e.Args) != 1 {
						return nil
					}
					width := int32(4)
					if st, isSlice := mem.TargetType.(SliceType); isSlice {
						width, _, _, _ = sliceElemInfo(st.Elem)
					}

					slLocal := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, mem.Target); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, slLocal)

					newLenL := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, e.Args[0]); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, newLenL)

					oldLenL := ctx.allocLocal(valTypeI32)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					buf.WriteByte(0x21)
					writeUleb128(buf, oldLenL)

					// if newLen > cap { grow }
					newCapL := ctx.allocLocal(valTypeI32)
					newSizeL := ctx.allocLocal(valTypeI32)
					newBufL := ctx.allocLocal(valTypeI32)

					buf.WriteByte(0x20)
					writeUleb128(buf, newLenL)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)
					buf.WriteByte(0x4b) // i32.gt_u
					buf.WriteByte(0x04) // if
					buf.WriteByte(valTypeVoid)

					// newCap = select(newLen, cap*2+8, newLen > cap*2+8)
					// Wasm select returns the first value when the
					// condition is true, so the true value (newLen) is
					// pushed first and the false value (cap*2+8) second.
					buf.WriteByte(0x20)
					writeUleb128(buf, newLenL)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)
					buf.WriteByte(0x41)
					writeSleb128(buf, 2)
					buf.WriteByte(0x6c)
					buf.WriteByte(0x41)
					writeSleb128(buf, 8)
					buf.WriteByte(0x6a)
					//   condition: newLen > cap*2+8
					buf.WriteByte(0x20)
					writeUleb128(buf, newLenL)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)
					buf.WriteByte(0x41)
					writeSleb128(buf, 2)
					buf.WriteByte(0x6c)
					buf.WriteByte(0x41)
					writeSleb128(buf, 8)
					buf.WriteByte(0x6a)
					buf.WriteByte(0x4b) // i32.gt_u
					buf.WriteByte(0x1b) // select
					buf.WriteByte(0x21)
					writeUleb128(buf, newCapL)

					// newSize = newCap * width
					buf.WriteByte(0x20)
					writeUleb128(buf, newCapL)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, newSizeL)

					// newBuf = __arena_alloc_dyn(newSize)
					ctx.emitAllocDyn(buf, newSizeL, newBufL)

					// memory.copy(newBuf, slLocal.ptr, oldLen * width)
					buf.WriteByte(0x20)
					writeUleb128(buf, newBufL)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)
					buf.WriteByte(0x20)
					writeUleb128(buf, oldLenL)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					ctx.emitMemoryCopy(buf)

					// slLocal.ptr = newBuf
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, newBufL)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					// slLocal.cap = newCap
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, newCapL)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)

					buf.WriteByte(0x0b) // end if

					// if newLen > oldLen { memory.fill tail with zeros }
					fillDstL := ctx.allocLocal(valTypeI32)
					fillLenL := ctx.allocLocal(valTypeI32)

					buf.WriteByte(0x20)
					writeUleb128(buf, newLenL)
					buf.WriteByte(0x20)
					writeUleb128(buf, oldLenL)
					buf.WriteByte(0x4b) // i32.gt_u
					buf.WriteByte(0x04)
					buf.WriteByte(valTypeVoid)

					// dst = ptr + oldLen*width
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)
					buf.WriteByte(0x20)
					writeUleb128(buf, oldLenL)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					buf.WriteByte(0x6a)
					buf.WriteByte(0x21)
					writeUleb128(buf, fillDstL)

					// fillLen = (newLen - oldLen) * width
					buf.WriteByte(0x20)
					writeUleb128(buf, newLenL)
					buf.WriteByte(0x20)
					writeUleb128(buf, oldLenL)
					buf.WriteByte(0x6b) // i32.sub
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, fillLenL)

					// memory.fill(dst, 0, fillLen)
					buf.WriteByte(0x20)
					writeUleb128(buf, fillDstL)
					buf.WriteByte(0x41)
					writeSleb128(buf, 0)
					buf.WriteByte(0x20)
					writeUleb128(buf, fillLenL)
					buf.WriteByte(0xfc)
					writeUleb128(buf, 0x0b)
					writeUleb128(buf, 0x00)

					buf.WriteByte(0x0b) // end if

					// slLocal.len = newLen
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, newLenL)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					return nil

				case "push":
					width := int32(4)
					storeOp := byte(0x36)
					storeAlign := uint32(2)
					if st, isSlice := mem.TargetType.(SliceType); isSlice {
						width, _, storeOp, storeAlign = sliceElemInfo(st.Elem)
					}

					slLocal := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, mem.Target); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, slLocal)

					// if header.len >= header.cap {
					//     newCap = header.cap*2 + 8
					//     newSize = newCap * width
					//     newBuf = __arena_alloc_dyn(newSize)
					//     memory.copy(newBuf, header.ptr, header.len * width)
					//     header.ptr = newBuf
					//     header.cap = newCap
					// }
					newCapL := ctx.allocLocal(valTypeI32)
					newSizeL := ctx.allocLocal(valTypeI32)
					newBufL := ctx.allocLocal(valTypeI32)

					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28) // header.len
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28) // header.cap
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)
					buf.WriteByte(0x4f) // i32.ge_u
					buf.WriteByte(0x04) // if
					buf.WriteByte(valTypeVoid)

					// newCap = cap*2 + 8
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)
					buf.WriteByte(0x41)
					writeSleb128(buf, 2)
					buf.WriteByte(0x6c)
					buf.WriteByte(0x41)
					writeSleb128(buf, 8)
					buf.WriteByte(0x6a)
					buf.WriteByte(0x21)
					writeUleb128(buf, newCapL)

					// newSize = newCap * width
					buf.WriteByte(0x20)
					writeUleb128(buf, newCapL)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, newSizeL)

					// newBuf = __arena_alloc_dyn(newSize)
					ctx.emitAllocDyn(buf, newSizeL, newBufL)

					// memory.copy(newBuf, header.ptr, header.len * width)
					buf.WriteByte(0x20)
					writeUleb128(buf, newBufL)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					ctx.emitMemoryCopy(buf)

					// header.ptr = newBuf
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, newBufL)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					// header.cap = newCap
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, newCapL)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)

					buf.WriteByte(0x0b) // end if

					// header.ptr + header.len * width
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					buf.WriteByte(0x6a)

					if err := ctx.compileExpr(buf, e.Args[0]); err != nil {
						return err
					}
					buf.WriteByte(storeOp)
					writeUleb128(buf, storeAlign)
					writeUleb128(buf, 0)

					// header.len = header.len + 1
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x6a)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					return nil

				case "pop":
					width := int32(4)
					loadOp := byte(0x28)
					loadAlign := uint32(2)
					if st, isSlice := mem.TargetType.(SliceType); isSlice {
						width, loadOp, _, loadAlign = sliceElemInfo(st.Elem)
					}

					slLocal := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, mem.Target); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, slLocal)

					optHdr := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 8, optHdr)

					// header.len = header.len - 1
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x6b)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)

					// optHdr[0] = 1
					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					// optHdr[4] = header.ptr[header.len] (width-aware load)
					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)
					buf.WriteByte(0x20)
					writeUleb128(buf, slLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)
					if width != 1 {
						buf.WriteByte(0x41)
						writeSleb128(buf, width)
						buf.WriteByte(0x6c)
					}
					buf.WriteByte(0x6a)
					buf.WriteByte(loadOp)
					writeUleb128(buf, loadAlign)
					writeUleb128(buf, 0)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)

					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					return nil
				}
			}

			if _, isPool := mem.TargetType.(PoolType); isPool {
				switch mem.Member {
				case "new":
					poolHdr := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 16, poolHdr)

					capLocal := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, e.Args[0]); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, capLocal)

					slotsLocal := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 1024, slotsLocal)

					buf.WriteByte(0x20)
					writeUleb128(buf, poolHdr)
					buf.WriteByte(0x20)
					writeUleb128(buf, capLocal)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					buf.WriteByte(0x20)
					writeUleb128(buf, poolHdr)
					buf.WriteByte(0x20)
					writeUleb128(buf, slotsLocal)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)

					buf.WriteByte(0x20)
					writeUleb128(buf, poolHdr)
					return nil

				case "spawn":
					handleHdr := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 8, handleHdr)

					buf.WriteByte(0x20)
					writeUleb128(buf, handleHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					buf.WriteByte(0x20)
					writeUleb128(buf, handleHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)

					buf.WriteByte(0x20)
					writeUleb128(buf, handleHdr)
					return nil

				case "get":
					optHdr := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 8, optHdr)

					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 100)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)

					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					return nil

				case "despawn":
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					return nil
				}
			}

			if _, isChan := mem.TargetType.(ChanType); isChan {
				switch mem.Member {
				case "new":
					chanHdr := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 24, chanHdr)

					buf.WriteByte(0x20)
					writeUleb128(buf, chanHdr)
					if err := ctx.compileExpr(buf, e.Args[0]); err != nil {
						return err
					}
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					bufBuf := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 128, bufBuf)

					buf.WriteByte(0x20)
					writeUleb128(buf, chanHdr)
					buf.WriteByte(0x20)
					writeUleb128(buf, bufBuf)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 20)

					buf.WriteByte(0x20)
					writeUleb128(buf, chanHdr)
					return nil

				case "send":
					chanLocal := ctx.allocLocal(valTypeI32)
					if err := ctx.compileExpr(buf, mem.Target); err != nil {
						return err
					}
					buf.WriteByte(0x21)
					writeUleb128(buf, chanLocal)

					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 20)

					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 8)
					buf.WriteByte(0x41)
					writeSleb128(buf, 4)
					buf.WriteByte(0x6c)
					buf.WriteByte(0x6a)

					if err := ctx.compileExpr(buf, e.Args[0]); err != nil {
						return err
					}
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x20)
					writeUleb128(buf, chanLocal)
					buf.WriteByte(0x28)
					writeUleb128(buf, 2)
					writeUleb128(buf, 12)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x6a)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 12)
					return nil

				case "recv":
					optHdr := ctx.allocLocal(valTypeI32)
					ctx.emitAlloc(buf, 8, optHdr)

					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 1)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 0)

					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					buf.WriteByte(0x41)
					writeSleb128(buf, 100)
					buf.WriteByte(0x36)
					writeUleb128(buf, 2)
					writeUleb128(buf, 4)

					buf.WriteByte(0x20)
					writeUleb128(buf, optHdr)
					return nil

				case "close":
					return nil
				}
			}

			if _, isTrait := mem.TargetType.(TraitType); isTrait {
				fatLocal := ctx.allocLocal(valTypeI32)
				if err := ctx.compileExpr(buf, mem.Target); err != nil {
					return err
				}
				buf.WriteByte(0x21)
				writeUleb128(buf, fatLocal)

				buf.WriteByte(0x20)
				writeUleb128(buf, fatLocal)
				buf.WriteByte(0x28)
				writeUleb128(buf, 2)
				writeUleb128(buf, 0)

				for k, fnIdx := range ctx.emitter.methodIndices {
					if len(k) > len(mem.Member) && k[len(k)-len(mem.Member):] == mem.Member {
						buf.WriteByte(0x10)
						writeUleb128(buf, fnIdx)
						return nil
					}
				}
			}

			if target, isIdent := mem.Target.(*IdentExpr); isIdent {
				if canonical, isModule := ctx.emitter.moduleAliases[target.Name]; isModule {
					name := canonical + "." + mem.Member
					fnIdx, exists := ctx.emitter.fnIndices[name]
					if !exists {
						return fmt.Errorf("unknown function '%s' in module '%s' (expected '%s')", mem.Member, target.Name, name)
					}
					for _, arg := range e.Args {
						if err := ctx.compileExpr(buf, arg); err != nil {
							return err
						}
					}
					buf.WriteByte(0x10)
					writeUleb128(buf, fnIdx)
					return nil
				}
			}

			if mem.TargetType == nil {
				return fmt.Errorf("cannot resolve method call '%s' on untyped target", mem.Member)
			}
			methodKey := mem.TargetType.String() + "." + mem.Member
			if methodIdx, exists := ctx.emitter.methodIndices[methodKey]; exists {
				if err := ctx.compileExpr(buf, mem.Target); err != nil {
					return err
				}
				if len(mem.EmbedPath) > 0 {
					rootType := ctx.inferExprType(mem.Target)
					off, _, err := ctx.embedReceiverOffset(rootType, mem.EmbedPath)
					if err != nil {
						return err
					}
					if off != 0 {
						buf.WriteByte(0x41)
						writeSleb128(buf, int32(off))
						buf.WriteByte(0x6a)
					}
				}
				for _, arg := range e.Args {
					if err := ctx.compileExpr(buf, arg); err != nil {
						return err
					}
				}
				buf.WriteByte(0x10)
				writeUleb128(buf, methodIdx)
				return nil
			}
			{
				tt := mem.TargetType.String()
				short := tt
				if i := strings.LastIndexByte(tt, '.'); i >= 0 {
					short = tt[i+1:]
				} else if i := strings.LastIndex(tt, "__"); i >= 0 {
					short = tt[i+2:]
				}
				shortKey := short + "." + mem.Member
				if methodIdx, exists := ctx.emitter.methodIndices[shortKey]; exists {
					if err := ctx.compileExpr(buf, mem.Target); err != nil {
						return err
					}
					if len(mem.EmbedPath) > 0 {
						rootType := ctx.inferExprType(mem.Target)
						off, _, err := ctx.embedReceiverOffset(rootType, mem.EmbedPath)
						if err != nil {
							return err
						}
						if off != 0 {
							buf.WriteByte(0x41)
							writeSleb128(buf, int32(off))
							buf.WriteByte(0x6a)
						}
					}
					for _, arg := range e.Args {
						if err := ctx.compileExpr(buf, arg); err != nil {
							return err
						}
					}
					buf.WriteByte(0x10)
					writeUleb128(buf, methodIdx)
					return nil
				}
			}
		}

		name := ""
		if ident, ok := e.Callee.(*IdentExpr); ok {
			name = ident.Name
		}
		if name != "" {
			if name == "print" || name == "println" {
				return ctx.compilePrint(buf, e.Args, name == "println")
			}
			if name == "time_now_ms" {
				buf.WriteByte(0x41)
				writeSleb128(buf, 0)
				buf.WriteByte(0x42)
				writeSleb128(buf, 1000)
				buf.WriteByte(0x41)
				writeSleb128(buf, scratchNwrite)
				buf.WriteByte(0x10)
				writeUleb128(buf, ctx.emitter.clockTimeIdx)
				buf.WriteByte(0x1a)

				buf.WriteByte(0x41)
				writeSleb128(buf, 100)
				return nil
			}

			fnIdx, exists := ctx.emitter.fnIndices[name]
			if !exists {
				return fmt.Errorf("unknown function %s", name)
			}
			for _, arg := range e.Args {
				if err := ctx.compileExpr(buf, arg); err != nil {
					return err
				}
			}
			buf.WriteByte(0x10)
			writeUleb128(buf, fnIdx)
		}
	}
	return nil
}

func (ctx *fnContext) emitAlloc(buf *bytes.Buffer, size int32, resultLocal uint32) {
	// Keep all heap allocations 8-byte aligned. WASI writes u64 fields (for
	// example in filestat) and requires the destination pointer to be aligned.
	buf.WriteByte(0x23)
	writeUleb128(buf, 0)
	buf.WriteByte(0x41)
	writeSleb128(buf, 7)
	buf.WriteByte(0x6a)
	buf.WriteByte(0x41)
	writeSleb128(buf, -8)
	buf.WriteByte(0x71)
	buf.WriteByte(0x21)
	writeUleb128(buf, resultLocal)

	buf.WriteByte(0x20)
	writeUleb128(buf, resultLocal)
	buf.WriteByte(0x41)
	writeSleb128(buf, size)
	buf.WriteByte(0x6a)
	buf.WriteByte(0x24)
	writeUleb128(buf, 0)
}

// emitAllocDyn is emitAlloc with a runtime size. sizeLocal must hold a
// non-negative i32 byte count; resultLocal receives the 8-byte-aligned
// allocation address. Used by slice growth paths where the required size
// depends on the current length and capacity.
func (ctx *fnContext) emitAllocDyn(buf *bytes.Buffer, sizeLocal, resultLocal uint32) {
	buf.WriteByte(0x23) // global.get __arena_pointer
	writeUleb128(buf, 0)
	buf.WriteByte(0x41) // i32.const 7
	writeSleb128(buf, 7)
	buf.WriteByte(0x6a) // i32.add
	buf.WriteByte(0x41) // i32.const -8
	writeSleb128(buf, -8)
	buf.WriteByte(0x71) // i32.and
	buf.WriteByte(0x21) // local.set resultLocal
	writeUleb128(buf, resultLocal)

	buf.WriteByte(0x20) // local.get resultLocal
	writeUleb128(buf, resultLocal)
	buf.WriteByte(0x20) // local.get sizeLocal
	writeUleb128(buf, sizeLocal)
	buf.WriteByte(0x6a) // i32.add
	buf.WriteByte(0x24) // global.set __arena_pointer
	writeUleb128(buf, 0)
}

// emitMemoryCopy emits wasm memory.copy (bulk-memory, 0xfc 0x0a). Stack
// consumption: dst, src, len, in that push order. Wasmtime/Cranelift enable
// bulk-memory by default, so no module feature declaration is required.
func (ctx *fnContext) emitMemoryCopy(buf *bytes.Buffer) {
	buf.WriteByte(0xfc)
	writeUleb128(buf, 0x0a)
	writeUleb128(buf, 0x00) // dst memory index 0
	writeUleb128(buf, 0x00) // src memory index 0
}

// fieldLoadStoreInfo returns the correct load/store opcodes and alignment for
// a struct field's type. Fields are packed at their natural size, so a bool or
// u8 must not be accessed with a full 4-byte i32.load/i32.store.
func fieldLoadStoreInfo(t UbeType) (loadOp, storeOp byte, align uint32) {
	if t == nil {
		return 0x28, 0x36, 2 // i32.load, i32.store
	}
	switch t.String() {
	case "bool", "u8":
		return 0x2d, 0x3a, 0 // i32.load8_u, i32.store8
	case "i8":
		return 0x2c, 0x3a, 0 // i32.load8_s, i32.store8
	case "u16":
		return 0x2f, 0x3b, 1 // i32.load16_u, i32.store16
	case "i16":
		return 0x2e, 0x3b, 1 // i32.load16_s, i32.store16
	case "f32":
		return 0x2a, 0x38, 2 // f32.load, f32.store
	case "f64":
		return 0x2b, 0x39, 3 // f64.load, f64.store
	case "i64", "u64":
		return 0x29, 0x37, 3 // i64.load, i64.store
	default:
		return 0x28, 0x36, 2 // i32.load, i32.store
	}
}

// sliceElemInfo returns the element width and the correct load/store
// opcodes + alignment for a slice's element type.
func sliceElemInfo(elem UbeType) (width int32, loadOp, storeOp byte, align uint32) {
	if elem == nil {
		return 4, 0x28, 0x36, 2
	}
	switch elem.String() {
	case "i8", "u8", "bool":
		return 1, 0x2d, 0x3a, 0 // i32.load8_u, i32.store8
	case "i16", "u16":
		return 2, 0x2f, 0x3b, 1 // i32.load16_u, i32.store16
	case "i64", "u64":
		return 8, 0x29, 0x37, 3 // i64.load, i64.store
	case "f32":
		return 4, 0x2a, 0x38, 2 // f32.load, f32.store
	case "f64":
		return 8, 0x2b, 0x39, 3 // f64.load, f64.store
	default:
		// Aggregate element: derive the true element width so that
		// .resize growth, memory.copy, and capacity math are correct.
		// NOTE: the load/store opcodes below are still scalar (4-byte).
		// IndexExpr reads/writes of []Struct remain broken until a
		// separate address-based indexing path lands; see
		// _dev/problems/problems_list1.md item 5.
		w := int32(elem.ByteSize())
		if w <= 0 {
			w = 4
		}
		return w, 0x28, 0x36, 2 // i32.load, i32.store
	}
}

func (ctx *fnContext) compilePrint(buf *bytes.Buffer, args []Expr, newline bool) error {
	nlVal := int32(0)
	if newline {
		nlVal = 1
	}

	for i, arg := range args {
		isLast := i == len(args)-1
		currentNL := int32(0)
		if isLast {
			currentNL = nlVal
		}

		argT := ctx.inferExprType(arg)

		if argT == "string" {
			strLocal := ctx.allocLocal(valTypeI32)
			if err := ctx.compileExpr(buf, arg); err != nil {
				return err
			}
			buf.WriteByte(0x21)
			writeUleb128(buf, strLocal)

			buf.WriteByte(0x20)
			writeUleb128(buf, strLocal)
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 0)

			buf.WriteByte(0x20)
			writeUleb128(buf, strLocal)
			buf.WriteByte(0x28)
			writeUleb128(buf, 2)
			writeUleb128(buf, 4)

			buf.WriteByte(0x41)
			writeSleb128(buf, currentNL)
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__print_str"])
		} else if argT == "bool" {
			if err := ctx.compileExpr(buf, arg); err != nil {
				return err
			}
			buf.WriteByte(0x41)
			writeSleb128(buf, currentNL)
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__print_bool"])
		} else {
			if err := ctx.compileExpr(buf, arg); err != nil {
				return err
			}
			buf.WriteByte(0x41)
			writeSleb128(buf, currentNL)
			buf.WriteByte(0x10)
			writeUleb128(buf, ctx.emitter.fnIndices["__print_i32"])
		}
	}
	return nil
}

// isAddressMember returns true for field types whose runtime representation
// is a compound value (nested struct or fixed array) stored inline at the
// field offset. Reading such a field must return a pointer into the owner's
// storage, and writing one must copy the value into that storage; a scalar
// load/store would touch only the first word and corrupt memory.
func isAddressMember(t UbeType) bool {
	switch t.(type) {
	case CustomType, ArrayType:
		return true
	}
	return false
}

// embedReceiverOffset walks mem.EmbedPath from rootType and returns the
// composite byte offset to add to a pointer of type rootType in order to
// reach the embedded receiver, plus the innermost embedded type name.
// The walk relies on structMeta.fieldOffsets, which — thanks to patch 04 —
// reflect the actual inline layout of embedded structs.
func (ctx *fnContext) embedReceiverOffset(rootType string, path []string) (int, string, error) {
	cur := rootType
	total := 0
	for _, seg := range path {
		meta, ok := ctx.emitter.structs[cur]
		if !ok {
			return 0, "", fmt.Errorf("unknown struct %s while resolving embed path", cur)
		}
		off, ok := meta.fieldOffsets[seg]
		if !ok {
			return 0, "", fmt.Errorf("unknown embedded field %s on %s", seg, cur)
		}
		total += off
		ft, ok := meta.fieldTypes[seg]
		if !ok {
			return 0, "", fmt.Errorf("unknown embedded field type %s on %s", seg, cur)
		}
		cur = ft.String()
	}
	return total, cur, nil
}

// sizeofInline returns the byte size a value of type t occupies when stored
// inline as a struct field. CustomType uses the recursive struct size
// (CustomType.ByteSize() is the fat-pointer size of 4). ArrayType multiplies
// element count by inline element size. All other types use ByteSize().
func (e *wasmEmitter) sizeofInline(t UbeType) int {
	if ct, ok := t.(CustomType); ok {
		if meta, ok := e.structs[ct.Name]; ok && meta.totalSize > 0 {
			return meta.totalSize
		}
		return 4
	}
	if at, ok := t.(ArrayType); ok {
		if at.Elem != nil {
			return at.Size * e.sizeofInline(at.Elem)
		}
		return at.Size * 4
	}
	if sz := t.ByteSize(); sz > 0 {
		return sz
	}
	return 4
}

// alignUpInt rounds offset up to the next multiple of align. align must be
// positive; callers clamp to 1 before calling.
func alignUpInt(offset, align int) int {
	if align <= 1 {
		return offset
	}
	rem := offset % align
	if rem == 0 {
		return offset
	}
	return offset + (align - rem)
}

// structFieldSize returns the in-memory size of a field of type t when
// stored inline in a struct. Primitive, string, slice, and array sizes come
// from UbeType.ByteSize(). CustomType uses the recursive struct size (the
// built-in ByteSize() reports 4, which is the fat-pointer size and is not
// what an inline field occupies). Optional/result/tuple/chan/pool fall back
// to ByteSize(), which is defined for each by types.go.
func structFieldSize(t UbeType, sizeofStruct func(string, map[string]bool) int, seen map[string]bool) int {
	if ct, ok := t.(CustomType); ok {
		return sizeofStruct(ct.Name, seen)
	}
	sz := t.ByteSize()
	if sz < 1 {
		sz = 4
	}
	return sz
}

// registerPromotedFields descends into the anonymous embedded fields of sd
// and records each transitively accessible leaf field's composite offset in
// meta.fieldOffsets under a dotted key ("Transform3D.pos_x", "A.B.x", ...).
// It never overwrites an existing direct key, so `h.Transform3D` remains the
// embedded-struct offset itself. Cyclic embedding is guarded by `seen`.
func (e *wasmEmitter) registerPromotedFields(meta *structMeta, sd *StructDecl, path []string, seen map[string]bool) {
	if seen[sd.Name] {
		return
	}
	seen[sd.Name] = true
	for _, f := range sd.Fields {
		if !f.Anonymous {
			continue
		}
		ct, ok := f.Type.(CustomType)
		if !ok {
			continue
		}
		base, ok := meta.fieldOffsets[f.Name]
		if !ok {
			continue
		}
		inner, ok := e.structs[ct.Name]
		if !ok {
			continue
		}
		childPath := append(append([]string{}, path...), f.Name)
		// Promote every direct field of the embedded struct, plus any
		// fields already promoted into it (recursive descent).
		for leafName, leafOff := range inner.fieldOffsets {
			dotted := strings.Join(childPath, ".") + "." + leafName
			if _, exists := meta.fieldOffsets[dotted]; !exists {
				meta.fieldOffsets[dotted] = base + leafOff
			}
			if lt, ok := inner.fieldTypes[leafName]; ok {
				if _, exists := meta.fieldTypes[dotted]; !exists {
					meta.fieldTypes[dotted] = lt
				}
			}
		}
		// Recurse so that `A.B.x` is also registered on the outermost struct.
		e.registerPromotedFields(meta, &StructDecl{Name: ct.Name, Fields: innerFields(inner, e)}, childPath, seen)
	}
}

// innerFields reconstructs the fields of a struct from its structMeta so the
// recursion above can descend without a second AST walk. Only Anonymous
// fields are needed for further promotion, so any non-anonymous field is
// returned as-is (the recursion ignores them).
func innerFields(meta structMeta, e *wasmEmitter) []FieldDecl {
	var out []FieldDecl
	seen := map[string]bool{}
	for name, t := range meta.fieldTypes {
		if seen[name] {
			continue
		}
		seen[name] = true
		// Only fields whose type is a CustomType can carry further
		// anonymous embedding; keep them.
		if ct, ok := t.(CustomType); ok {
			out = append(out, FieldDecl{Name: name, Type: ct, Anonymous: true})
		}
	}
	return out
}

// memberFieldKey returns the key to use when looking up a MemberExpr in
// structMeta.fieldOffsets / fieldTypes. When the typechecker set EmbedPath
// (via findFieldMatches), the dotted form is used; otherwise the bare
// member name is returned unchanged.
func memberFieldKey(e *MemberExpr) string {
	if len(e.EmbedPath) == 0 {
		return e.Member
	}
	return strings.Join(e.EmbedPath, ".") + "." + e.Member
}

// compileExprArm emits a block whose value is the final ExprStmt. Earlier
// statements are emitted as normal, with any values they leave on the stack
// dropped at the ExprStmt site (see compileStmt's *ExprStmt case).
func (ctx *fnContext) compileExprArm(buf *bytes.Buffer, b *BlockStmt) error {
	if b == nil || len(b.Stmts) == 0 {
		return fmt.Errorf("%s: if-expression branch has no value", b.Loc())
	}
	for i, stmt := range b.Stmts {
		if i == len(b.Stmts)-1 {
			es, ok := stmt.(*ExprStmt)
			if !ok {
				return fmt.Errorf("%s: if-expression branch's last statement must be an expression", stmt.Loc())
			}
			return ctx.compileExpr(buf, es.Expr)
		}
		if err := ctx.compileStmt(buf, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (ctx *fnContext) inferExprType(expr Expr) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *CastExpr:
		if e.TargetType != nil {
			return e.TargetType.String()
		}
	case *IndexExpr:
		if e.TargetType != nil {
			if st, ok := e.TargetType.(SliceType); ok && st.Elem != nil {
				return st.Elem.String()
			}
			if at, ok := e.TargetType.(ArrayType); ok && at.Elem != nil {
				return at.Elem.String()
			}
			if _, ok := e.TargetType.(StringType); ok {
				return "u8"
			}
		}
	case *LiteralExpr:
		return e.Type.String()
	case *IdentExpr:
		if t, ok := ctx.localTypes[e.Name]; ok && t != nil {
			return t.String()
		}
	case *InterpolatedStringExpr:
		return "string"
	case *StructInitExpr:
		return e.Name
	case *MemberExpr:
		if e.TargetType != nil {
			if meta, ok := ctx.emitter.structs[e.TargetType.String()]; ok {
				if ft, ok := meta.fieldTypes[memberFieldKey(e)]; ok && ft != nil {
					return ft.String()
				}
			}
		}
	case *SliceSubExpr:
		if e.TargetType != nil {
			switch e.TargetType.(type) {
			case StringType:
				return "string"
			case SliceType:
				return e.TargetType.String()
			case ArrayType:
				// [N]T coerces to []T; the caller wraps in ArrayToSliceExpr,
				// so a bare ArrayType target here still means the result is
				// a slice of the same element.
				if at, ok := e.TargetType.(ArrayType); ok && at.Elem != nil {
					return SliceType{Elem: at.Elem}.String()
				}
			}
		}
	case *UnaryExpr:
		if t := ctx.inferExprType(e.Operand); t != "" {
			return t
		}
	case *BinaryExpr:
		if e.Type != nil {
			return e.Type.String()
		}
		if lt := ctx.inferExprType(e.Left); lt != "" {
			return lt
		}
		if rt := ctx.inferExprType(e.Right); rt != "" {
			return rt
		}
	case *IfExpr:
		if e.Type != nil {
			return e.Type.String()
		}
	case *CallExpr:
		if t := ctx.callReturnUbeType(e); t != nil {
			return t.String()
		}
	case *TryExpr:
		if call, ok := e.Operand.(*CallExpr); ok {
			if t := ctx.callReturnUbeType(call); t != nil {
				if res, ok := t.(ResultType); ok {
					return res.Ok.String()
				}
			}
		}
		if id, ok := e.Operand.(*IdentExpr); ok {
			if t, ok := ctx.localTypes[id.Name]; ok && t != nil {
				if res, ok := t.(ResultType); ok {
					return res.Ok.String()
				}
			}
		}
	case *OrElseExpr:
		if e.PrimaryType != nil {
			switch pt := e.PrimaryType.(type) {
			case ResultType:
				return pt.Ok.String()
			case OptionalType:
				return pt.Inner.String()
			}
		}
	}
	return ""
}

func (e *wasmEmitter) buildPrintStrFunc() []byte {
	var body bytes.Buffer

	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec)
	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x36)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec+4)
	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x36)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x41)
	writeSleb128(&body, scratchNwrite)
	body.WriteByte(0x10)
	writeUleb128(&body, e.fdWriteIdx)
	body.WriteByte(0x1a)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)

	// Write a newline byte into the scratch buffer.
	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec+48)
	body.WriteByte(0x41)
	writeSleb128(&body, 10)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec)
	body.WriteByte(0x41)
	writeSleb128(&body, 48)
	body.WriteByte(0x36)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec+4)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x36)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x41)
	writeSleb128(&body, scratchNwrite)
	body.WriteByte(0x10)
	writeUleb128(&body, e.fdWriteIdx)
	body.WriteByte(0x1a)

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	var full bytes.Buffer
	writeUleb128(&full, 0)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildPrintBoolFunc() []byte {
	var body bytes.Buffer

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x41)
	writeSleb128(&body, int32(e.trueHeader))
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)
	body.WriteByte(0x41)
	writeSleb128(&body, 4)
	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x10)
	writeUleb128(&body, e.fnIndices["__print_str"])

	body.WriteByte(0x05)

	body.WriteByte(0x41)
	writeSleb128(&body, int32(e.falseHeader))
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)
	body.WriteByte(0x41)
	writeSleb128(&body, 5)
	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x10)
	writeUleb128(&body, e.fnIndices["__print_str"])

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	var full bytes.Buffer
	writeUleb128(&full, 0)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

// buildIntToStrFunc converts an i32 into a freshly allocated string header.
// Locals: 1 = base, 2 = write position, 3 = negative flag.
func (e *wasmEmitter) buildIntToStrFunc() []byte {
	var body bytes.Buffer

	// base = align8(global0)
	body.WriteByte(0x23)
	writeUleb128(&body, 0)
	emitI32Const(&body, 7)
	body.WriteByte(0x6a)
	emitI32Const(&body, -8)
	body.WriteByte(0x71)
	emitLocalSet(&body, 1)

	// global0 = base + 40
	emitLocalGet(&body, 1)
	emitI32Const(&body, 40)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	writeUleb128(&body, 0)

	// pos = base + 40
	emitLocalGet(&body, 1)
	emitI32Const(&body, 40)
	body.WriteByte(0x6a)
	emitLocalSet(&body, 2)

	// neg = 0
	emitI32Const(&body, 0)
	emitLocalSet(&body, 3)

	emitLocalGet(&body, 0)
	emitI32Eqz(&body)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)

	// Zero: write '0'.
	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	emitLocalSet(&body, 2)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 48)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x05) // else

	// Negative?
	emitLocalGet(&body, 0)
	emitI32Const(&body, 0)
	body.WriteByte(0x48) // i32.lt_s
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitI32Const(&body, 1)
	emitLocalSet(&body, 3)
	emitI32Const(&body, 0)
	emitLocalGet(&body, 0)
	body.WriteByte(0x6b)
	emitLocalSet(&body, 0)
	body.WriteByte(0x0b)

	// loop: write digits backwards.
	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)

	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x22) // local.tee
	writeUleb128(&body, 2)
	emitLocalGet(&body, 0)
	emitI32Const(&body, 10)
	body.WriteByte(0x6f) // i32.rem_s
	emitI32Const(&body, 48)
	body.WriteByte(0x6a)
	body.WriteByte(0x3a) // i32.store8
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	emitLocalGet(&body, 0)
	emitI32Const(&body, 10)
	body.WriteByte(0x6d) // i32.div_s
	body.WriteByte(0x22) // local.tee
	writeUleb128(&body, 0)
	emitI32Eqz(&body)
	emitI32Eqz(&body)
	body.WriteByte(0x0d) // br_if
	writeUleb128(&body, 0)
	body.WriteByte(0x0b) // end loop

	// Sign.
	emitLocalGet(&body, 3)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x22) // local.tee
	writeUleb128(&body, 2)
	emitI32Const(&body, 45)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	body.WriteByte(0x0b)

	body.WriteByte(0x0b) // end zero if

	// header.ptr = pos
	emitLocalGet(&body, 1)
	emitLocalGet(&body, 2)
	emitI32Store(&body, 0, 2)
	// header.len = base + 40 - pos
	emitLocalGet(&body, 1)
	emitLocalGet(&body, 1)
	emitI32Const(&body, 40)
	body.WriteByte(0x6a)
	emitLocalGet(&body, 2)
	body.WriteByte(0x6b)
	emitI32Store(&body, 4, 2)
	emitLocalGet(&body, 1)

	var full bytes.Buffer
	writeUleb128(&full, 3) // three i32 locals
	for i := 0; i < 3; i++ {
		writeUleb128(&full, 1)
		full.WriteByte(valTypeI32)
	}
	full.Write(body.Bytes())
	full.WriteByte(0x0b)

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildPrintIntFunc() []byte {
	var body bytes.Buffer

	body.WriteByte(0x41)
	writeSleb128(&body, 47)
	body.WriteByte(0x21)
	writeUleb128(&body, 2)

	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 3)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x45)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 48)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x10)
	writeUleb128(&body, e.fnIndices["__print_str"])
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x48)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x6b)
	body.WriteByte(0x21)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x21)
	writeUleb128(&body, 3)

	body.WriteByte(0x0b)

	body.WriteByte(0x02)
	bufType := valTypeVoid
	body.WriteByte(bufType)
	body.WriteByte(0x03)
	body.WriteByte(bufType)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x45)
	body.WriteByte(0x0d)
	writeUleb128(&body, 1)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x41)
	writeSleb128(&body, 10)
	body.WriteByte(0x6f)
	body.WriteByte(0x41)
	writeSleb128(&body, 48)
	body.WriteByte(0x6a)
	body.WriteByte(0x21)
	writeUleb128(&body, 4)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x21)
	writeUleb128(&body, 2)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x41)
	writeSleb128(&body, 10)
	body.WriteByte(0x6d)
	body.WriteByte(0x21)
	writeUleb128(&body, 0)

	body.WriteByte(0x0c)
	writeUleb128(&body, 0)

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	writeUleb128(&body, 3)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 45)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x21)
	writeUleb128(&body, 2)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x6a)

	body.WriteByte(0x41)
	writeSleb128(&body, 47)
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x6b)

	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x10)
	writeUleb128(&body, e.fnIndices["__print_str"])

	body.WriteByte(0x0b)

	var full bytes.Buffer
	writeUleb128(&full, 1)
	writeUleb128(&full, 3)
	full.WriteByte(valTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildStrConcatFunc() []byte {
	var body bytes.Buffer

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 4)
	body.WriteByte(0x21)
	writeUleb128(&body, 2)

	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 4)
	body.WriteByte(0x21)
	writeUleb128(&body, 3)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x20)
	writeUleb128(&body, 3)
	body.WriteByte(0x6a)
	body.WriteByte(0x21)
	writeUleb128(&body, 4)

	body.WriteByte(0x23)
	writeUleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 5)

	body.WriteByte(0x20)
	writeUleb128(&body, 5)
	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	writeUleb128(&body, 0)

	body.WriteByte(0x23)
	writeUleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 6)

	body.WriteByte(0x20)
	writeUleb128(&body, 6)
	body.WriteByte(0x41)
	writeSleb128(&body, 8)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 7)

	body.WriteByte(0x02)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x4e)
	body.WriteByte(0x0d)
	writeUleb128(&body, 1)

	body.WriteByte(0x20)
	writeUleb128(&body, 5)
	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x6a)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)
	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x6a)
	body.WriteByte(0x2d)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x6a)
	body.WriteByte(0x21)
	writeUleb128(&body, 7)

	body.WriteByte(0x0c)
	writeUleb128(&body, 0)

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 7)

	body.WriteByte(0x02)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x20)
	writeUleb128(&body, 3)
	body.WriteByte(0x4e)
	body.WriteByte(0x0d)
	writeUleb128(&body, 1)

	body.WriteByte(0x20)
	writeUleb128(&body, 5)
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x6a)
	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x6a)

	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)
	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x6a)
	body.WriteByte(0x2d)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x6a)
	body.WriteByte(0x21)
	writeUleb128(&body, 7)

	body.WriteByte(0x0c)
	writeUleb128(&body, 0)

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	writeUleb128(&body, 6)
	body.WriteByte(0x20)
	writeUleb128(&body, 5)
	body.WriteByte(0x36)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	body.WriteByte(0x20)
	writeUleb128(&body, 6)
	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x36)
	writeUleb128(&body, 2)
	writeUleb128(&body, 4)

	body.WriteByte(0x20)
	writeUleb128(&body, 6)
	body.WriteByte(0x0b)

	var full bytes.Buffer
	writeUleb128(&full, 1)
	writeUleb128(&full, 6)
	full.WriteByte(valTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildStrEqFunc() []byte {
	var body bytes.Buffer

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 4)
	body.WriteByte(0x21)
	writeUleb128(&body, 2)

	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 4)
	body.WriteByte(0x21)
	writeUleb128(&body, 3)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x20)
	writeUleb128(&body, 3)
	body.WriteByte(0x47)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 5)

	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 6)

	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 4)

	body.WriteByte(0x02)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x4e)
	body.WriteByte(0x0d)
	writeUleb128(&body, 1)

	body.WriteByte(0x20)
	writeUleb128(&body, 5)
	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x6a)
	body.WriteByte(0x2d)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x20)
	writeUleb128(&body, 6)
	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x6a)
	body.WriteByte(0x2d)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x47)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x6a)
	body.WriteByte(0x21)
	writeUleb128(&body, 4)

	body.WriteByte(0x0c)
	writeUleb128(&body, 0)

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x0b)

	var full bytes.Buffer
	writeUleb128(&full, 1)
	writeUleb128(&full, 5)
	full.WriteByte(valTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildStartFunc(mainIdx uint32) []byte {
	var body bytes.Buffer

	body.WriteByte(0x41)
	writeSleb128(&body, 48)
	body.WriteByte(0x41)
	writeSleb128(&body, 10)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x10)
	writeUleb128(&body, mainIdx)
	// Drop main's return value if it's non-void. A ResultType/OptionalType
	// return compiles to a single i32 (pointer to a {tag, payload} header),
	// so dropping one i32 is enough. _start itself returns ().
	var mainRet UbeType
	if rt, ok := e.fnReturns["main"]; ok {
		mainRet = rt
	} else {
		for name, rt := range e.fnReturns {
			if strings.HasSuffix(name, ".main") {
				mainRet = rt
				break
			}
		}
	}
	// _start must consume main's return value (its own type is () -> ()).
	// A ResultType return is a single i32 pointer to a {tag, payload}
	// header. For main -> T ! string with tag == 0 (error), the payload at
	// offset 4 is an i32 pointer to the error's string header {ptr, len}.
	// Print it to fd 2 so a ? failure in main is no longer silent.
	if res, isRes := mainRet.(ResultType); isRes {
		if _, errIsStr := res.Err.(StringType); errIsStr {
			body.WriteByte(0x21)
			writeUleb128(&body, 0)

			body.WriteByte(0x20)
			writeUleb128(&body, 0)
			body.WriteByte(0x28)
			writeUleb128(&body, 2)
			writeUleb128(&body, 0)
			body.WriteByte(0x45)
			body.WriteByte(0x04)
			body.WriteByte(valTypeVoid)

			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec)
			body.WriteByte(0x20)
			writeUleb128(&body, 0)
			body.WriteByte(0x28)
			writeUleb128(&body, 2)
			writeUleb128(&body, 4)
			body.WriteByte(0x28)
			writeUleb128(&body, 2)
			writeUleb128(&body, 0)
			body.WriteByte(0x36)
			writeUleb128(&body, 2)
			writeUleb128(&body, 0)

			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec+4)
			body.WriteByte(0x20)
			writeUleb128(&body, 0)
			body.WriteByte(0x28)
			writeUleb128(&body, 2)
			writeUleb128(&body, 4)
			body.WriteByte(0x28)
			writeUleb128(&body, 2)
			writeUleb128(&body, 4)
			body.WriteByte(0x36)
			writeUleb128(&body, 2)
			writeUleb128(&body, 0)

			body.WriteByte(0x41)
			writeSleb128(&body, 2)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec)
			body.WriteByte(0x41)
			writeSleb128(&body, 1)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchNwrite)
			body.WriteByte(0x10)
			writeUleb128(&body, e.fdWriteIdx)
			body.WriteByte(0x1a)

			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec+48)
			body.WriteByte(0x41)
			writeSleb128(&body, 10)
			body.WriteByte(0x3a)
			writeUleb128(&body, 0)
			writeUleb128(&body, 0)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec+48)
			body.WriteByte(0x36)
			writeUleb128(&body, 2)
			writeUleb128(&body, 0)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec+4)
			body.WriteByte(0x41)
			writeSleb128(&body, 1)
			body.WriteByte(0x36)
			writeUleb128(&body, 2)
			writeUleb128(&body, 0)
			body.WriteByte(0x41)
			writeSleb128(&body, 2)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec)
			body.WriteByte(0x41)
			writeSleb128(&body, 1)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchNwrite)
			body.WriteByte(0x10)
			writeUleb128(&body, e.fdWriteIdx)
			body.WriteByte(0x1a)

			// proc_exit(1): the error message is already on fd 2; exit
			// non-zero so scripts that check $? observe the failure.
			// proc_exit is noreturn per WASI; the two 0x0b that follow
			// close the diagnostic `if` and the _start block.
			body.WriteByte(0x41) // i32.const
			writeSleb128(&body, 1)
			body.WriteByte(0x10) // call
			writeUleb128(&body, e.wasiImports["proc_exit"])

			body.WriteByte(0x0b)
			body.WriteByte(0x0b)
			return wrapStartWithLocals(body, 1)
		}
	}

	if mainRet != nil && mainRet.String() != "void" {
		body.WriteByte(0x1a) // drop
	}
	body.WriteByte(0x0b)
	return wrapStartWithLocals(body, 0)
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
	writeUleb128(&expSec, 2)

	writeString(&expSec, "memory")
	expSec.WriteByte(0x02)
	writeUleb128(&expSec, 0)

	writeString(&expSec, "_start")
	expSec.WriteByte(0x00)
	writeUleb128(&expSec, e.fnIndices["_start"])
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

// wrapStartWithLocals wraps a _start body with an i32 local declaration
// group of nLocals entries and the code-section length prefix.
func wrapStartWithLocals(body bytes.Buffer, nLocals uint32) []byte {
	var codeBuf bytes.Buffer
	if nLocals > 0 {
		writeUleb128(&codeBuf, 1)
		writeUleb128(&codeBuf, nLocals)
		codeBuf.WriteByte(valTypeI32)
	} else {
		writeUleb128(&codeBuf, 0)
	}
	codeBuf.Write(body.Bytes())

	var fullBuf bytes.Buffer
	writeUleb128(&fullBuf, uint32(codeBuf.Len()))
	fullBuf.Write(codeBuf.Bytes())
	return fullBuf.Bytes()
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
