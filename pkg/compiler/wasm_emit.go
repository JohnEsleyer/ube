package compiler

import (
	"bytes"
	"fmt"
)

// EmitWasm compiles an AST program directly into WebAssembly binary bytecode (.wasm)
// without requiring Clang, Zig, or any intermediate C compiler.
func EmitWasm(prog *Program) ([]byte, error) {
	emitter := newWasmEmitter()
	return emitter.compile(prog)
}

// Bytecode constants for WebAssembly binary encoding
const (
	wasmMagic   = 0x6d736100
	wasmVersion = 0x00000001

	// Type opcodes
	valTypeI32  = byte(0x7f)
	valTypeI64  = byte(0x7e)
	valTypeF32  = byte(0x7d)
	valTypeF64  = byte(0x7c)
	valTypeVoid = byte(0x40)

	// Section IDs
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

	// WASI Scratch and Memory Layout
	scratchCiovec = 0    // Offset 0..7: ciovec { ptr: i32, len: i32 }
	scratchNwrite = 8    // Offset 8..11: nwritten i32
	scratchItoa   = 16   // Offset 16..47: itoa string buffer
	staticDataOff = 64   // Offset 64..: static string literals
	initialHeap   = 4096 // Offset 4096..: dynamic arena bump allocations
)

type structMeta struct {
	fieldOffsets map[string]int
	totalSize    int
}

type wasmEmitter struct {
	types         [][]byte
	typeMap       map[string]uint32
	funcs         []uint32 // type index for each defined func
	codeBodies    [][]byte
	data          []byte
	dataOffset    uint32
	fnIndices     map[string]uint32
	fdWriteIdx    uint32
	hasFdWrite    bool
	structs       map[string]structMeta
	moduleAliases map[string]string
}

func newWasmEmitter() *wasmEmitter {
	return &wasmEmitter{
		typeMap:    make(map[string]uint32),
		fnIndices:  make(map[string]uint32),
		structs:    make(map[string]structMeta),
		dataOffset: staticDataOff,
	}
}

func (e *wasmEmitter) registerType(params []byte, results []byte) uint32 {
	key := fmt.Sprintf("%v->%v", params, results)
	if idx, exists := e.typeMap[key]; exists {
		return idx
	}

	var buf bytes.Buffer
	buf.WriteByte(0x60) // func type
	writeUleb128(&buf, uint32(len(params)))
	buf.Write(params)
	writeUleb128(&buf, uint32(len(results)))
	buf.Write(results)

	idx := uint32(len(e.types))
	e.types = append(e.types, buf.Bytes())
	e.typeMap[key] = idx
	return idx
}

func (e *wasmEmitter) internString(s string) (uint32, uint32) {
	strBytes := []byte(s)
	length := uint32(len(strBytes))
	offset := e.dataOffset
	e.data = append(e.data, strBytes...)
	e.dataOffset += length
	return offset, length
}

func (e *wasmEmitter) compile(prog *Program) ([]byte, error) {
	e.moduleAliases = map[string]string{}
	if prog != nil {
		for alias, prefix := range prog.ModuleAliases {
			e.moduleAliases[alias] = prefix
		}
	}
	// Register WASI fd_write import type: (i32, i32, i32, i32) -> i32
	e.fdWriteIdx = 0
	e.hasFdWrite = true
	e.registerType([]byte{valTypeI32, valTypeI32, valTypeI32, valTypeI32}, []byte{valTypeI32})

	// Pre-scan structs to build memory field layouts
	for _, decl := range prog.Decls {
		if s, ok := decl.(*StructDecl); ok {
			meta := structMeta{
				fieldOffsets: make(map[string]int),
			}
			offset := 0
			for _, f := range s.Fields {
				meta.fieldOffsets[f.Name] = offset
				offset += 4 // standard 32-bit word alignment
			}
			meta.totalSize = offset
			if meta.totalSize == 0 {
				meta.totalSize = 4
			}
			e.structs[s.Name] = meta
		}
	}

	// First pass: register function signatures and assign function indices
	// Index 0 is reserved for WASI fd_write
	funcOffset := uint32(1)

	var fnDecls []*FnDecl
	var mainFn *FnDecl

	for _, decl := range prog.Decls {
		if fn, ok := decl.(*FnDecl); ok {
			fnDecls = append(fnDecls, fn)
			e.fnIndices[fn.Name] = funcOffset + uint32(len(fnDecls)-1)
			if fn.Name == "main" {
				mainFn = fn
			}
		}
	}

	if mainFn == nil {
		return nil, fmt.Errorf("no 'main' function found in program")
	}

	// Register helper function indices
	printIntIdx := funcOffset + uint32(len(fnDecls))
	e.fnIndices["__print_i32"] = printIntIdx

	printStrIdx := printIntIdx + 1
	e.fnIndices["__print_str"] = printStrIdx

	startIdx := printStrIdx + 1
	e.fnIndices["_start"] = startIdx

	// Register types for defined functions
	for _, fn := range fnDecls {
		var params []byte
		for range fn.Params {
			params = append(params, valTypeI32)
		}
		var results []byte
		if fn.RetType != nil {
			if _, isVoid := fn.RetType.(PrimitiveType); !isVoid || fn.RetType.String() != "void" {
				if fn.RetType.String() == "f32" {
					results = append(results, valTypeF32)
				} else {
					results = append(results, valTypeI32)
				}
			}
		}
		typeIdx := e.registerType(params, results)
		e.funcs = append(e.funcs, typeIdx)
	}

	// Register types for helpers:
	// __print_i32: (i32, i32) -> void  (val, newline)
	e.funcs = append(e.funcs, e.registerType([]byte{valTypeI32, valTypeI32}, []byte{}))
	// __print_str: (i32, i32, i32) -> void (ptr, len, newline)
	e.funcs = append(e.funcs, e.registerType([]byte{valTypeI32, valTypeI32, valTypeI32}, []byte{}))
	// _start: () -> void
	e.funcs = append(e.funcs, e.registerType([]byte{}, []byte{}))

	// Second pass: compile function bodies
	for _, fn := range fnDecls {
		body, err := e.compileFunction(fn)
		if err != nil {
			return nil, fmt.Errorf("function %s: %w", fn.Name, err)
		}
		e.codeBodies = append(e.codeBodies, body)
	}

	// Compile built-in helper functions
	e.codeBodies = append(e.codeBodies, e.buildPrintIntFunc())
	e.codeBodies = append(e.codeBodies, e.buildPrintStrFunc())
	e.codeBodies = append(e.codeBodies, e.buildStartFunc(e.fnIndices["main"]))

	return e.assembleBinary()
}

type fnContext struct {
	emitter    *wasmEmitter
	locals     map[string]uint32
	numParams  uint32
	localTypes []byte
	loopLabels []int
}

func (e *wasmEmitter) compileFunction(fn *FnDecl) ([]byte, error) {
	ctx := &fnContext{
		emitter: e,
		locals:  make(map[string]uint32),
	}

	for i, param := range fn.Params {
		ctx.locals[param.Name] = uint32(i)
	}
	ctx.numParams = uint32(len(fn.Params))

	var bodyBuf bytes.Buffer
	if err := ctx.compileBlock(&bodyBuf, fn.Body); err != nil {
		return nil, err
	}
	bodyBuf.WriteByte(0x0b) // end opcode

	var codeBuf bytes.Buffer
	writeUleb128(&codeBuf, uint32(len(ctx.localTypes)))
	for _, lt := range ctx.localTypes {
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
	idx := ctx.numParams + uint32(len(ctx.localTypes))
	ctx.localTypes = append(ctx.localTypes, t)
	return idx
}

func (ctx *fnContext) compileBlock(buf *bytes.Buffer, block *BlockStmt) error {
	for _, stmt := range block.Stmts {
		if err := ctx.compileStmt(buf, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (ctx *fnContext) compileStmt(buf *bytes.Buffer, stmt Stmt) error {
	switch s := stmt.(type) {
	case *LetStmt:
		valIdx := ctx.allocLocal(valTypeI32)
		ctx.locals[s.Name] = valIdx
		if s.Value != nil {
			if err := ctx.compileExpr(buf, s.Value); err != nil {
				return err
			}
			buf.WriteByte(0x21) // local.set
			writeUleb128(buf, valIdx)
		}

	case *AssignStmt:
		if ident, ok := s.LHS.(*IdentExpr); ok {
			localIdx, exists := ctx.locals[ident.Name]
			if !exists {
				return fmt.Errorf("unknown variable %s in assignment", ident.Name)
			}
			if err := ctx.compileExpr(buf, s.RHS); err != nil {
				return err
			}
			buf.WriteByte(0x21) // local.set
			writeUleb128(buf, localIdx)
		} else if mem, ok := s.LHS.(*MemberExpr); ok {
			// Struct field assignment: target.member = RHS
			structName := mem.TargetType.String()
			meta, ok := ctx.emitter.structs[structName]
			if !ok {
				return fmt.Errorf("unknown struct type %s", structName)
			}
			offset, ok := meta.fieldOffsets[mem.Member]
			if !ok {
				return fmt.Errorf("unknown field %s on struct %s", mem.Member, structName)
			}
			// Push target address
			if err := ctx.compileExpr(buf, mem.Target); err != nil {
				return err
			}
			// Push value to store
			if err := ctx.compileExpr(buf, s.RHS); err != nil {
				return err
			}
			buf.WriteByte(0x36) // i32.store
			writeUleb128(buf, 2)
			writeUleb128(buf, uint32(offset))
		}

	case *ExprStmt:
		if err := ctx.compileExpr(buf, s.Expr); err != nil {
			return err
		}
		if call, ok := s.Expr.(*CallExpr); ok {
			if ident, ok := call.Callee.(*IdentExpr); ok && (ident.Name == "print" || ident.Name == "println") {
				return nil
			}
		}
		if _, isCall := s.Expr.(*CallExpr); isCall {
			buf.WriteByte(0x1a) // drop
		}

	case *ReturnStmt:
		if s.Value != nil {
			if err := ctx.compileExpr(buf, s.Value); err != nil {
				return err
			}
		}
		buf.WriteByte(0x0f) // return

	case *IfStmt:
		if err := ctx.compileExpr(buf, s.Cond); err != nil {
			return err
		}
		buf.WriteByte(0x04)        // if
		buf.WriteByte(valTypeVoid) // blocktype: void
		if err := ctx.compileBlock(buf, s.Then); err != nil {
			return err
		}
		if s.Else != nil {
			buf.WriteByte(0x05) // else
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
		buf.WriteByte(0x0b) // end

	case *WhileStmt:
		buf.WriteByte(0x02)        // block
		buf.WriteByte(valTypeVoid) // void
		buf.WriteByte(0x03)        // loop
		buf.WriteByte(valTypeVoid) // void

		ctx.loopLabels = append(ctx.loopLabels, 1)

		if err := ctx.compileExpr(buf, s.Cond); err != nil {
			return err
		}
		buf.WriteByte(0x45) // i32.eqz
		buf.WriteByte(0x0d) // br_if break out
		writeUleb128(buf, 1)

		if err := ctx.compileBlock(buf, s.Body); err != nil {
			return err
		}

		buf.WriteByte(0x0c) // br loop again
		writeUleb128(buf, 0)

		ctx.loopLabels = ctx.loopLabels[:len(ctx.loopLabels)-1]

		buf.WriteByte(0x0b) // end loop
		buf.WriteByte(0x0b) // end block

	case *ArenaStmt:
		// arena { ... }
		// 1. Save global $arena_ptr into a local
		// 2. Execute block
		// 3. Rewind global $arena_ptr back to saved value
		savedLocal := ctx.allocLocal(valTypeI32)
		buf.WriteByte(0x23) // global.get $arena_ptr (global 0)
		writeUleb128(buf, 0)
		buf.WriteByte(0x21) // local.set savedLocal
		writeUleb128(buf, savedLocal)

		if err := ctx.compileBlock(buf, s.Body); err != nil {
			return err
		}

		buf.WriteByte(0x20) // local.get savedLocal
		writeUleb128(buf, savedLocal)
		buf.WriteByte(0x24) // global.set $arena_ptr (global 0)
		writeUleb128(buf, 0)

	case *LoopControlStmt:
		if s.Continue {
			buf.WriteByte(0x0c) // br (to loop start)
			writeUleb128(buf, 0)
		} else {
			buf.WriteByte(0x0c) // br (to block end)
			writeUleb128(buf, 1)
		}
	}
	return nil
}

func (ctx *fnContext) compileExpr(buf *bytes.Buffer, expr Expr) error {
	switch e := expr.(type) {
	case *LiteralExpr:
		switch e.Type.String() {
		case "i32":
			var val int32
			fmt.Sscanf(e.Val, "%d", &val)
			buf.WriteByte(0x41) // i32.const
			writeSleb128(buf, val)
		case "bool":
			buf.WriteByte(0x41) // i32.const
			if e.Val == "true" {
				writeSleb128(buf, 1)
			} else {
				writeSleb128(buf, 0)
			}
		case "string":
			// Intern string literal in linear data section and return pointer
			off, _ := ctx.emitter.internString(e.Val)
			buf.WriteByte(0x41) // i32.const (memory pointer)
			writeSleb128(buf, int32(off))
		default:
			buf.WriteByte(0x41)
			writeSleb128(buf, 0)
		}

	case *IdentExpr:
		localIdx, exists := ctx.locals[e.Name]
		if !exists {
			return fmt.Errorf("unknown variable %s", e.Name)
		}
		buf.WriteByte(0x20) // local.get
		writeUleb128(buf, localIdx)

	case *BinaryExpr:
		if err := ctx.compileExpr(buf, e.Left); err != nil {
			return err
		}
		if err := ctx.compileExpr(buf, e.Right); err != nil {
			return err
		}
		switch e.Op {
		case "+":
			buf.WriteByte(0x6a) // i32.add
		case "-":
			buf.WriteByte(0x6b) // i32.sub
		case "*":
			buf.WriteByte(0x6c) // i32.mul
		case "/":
			buf.WriteByte(0x6d) // i32.div_s
		case "%":
			buf.WriteByte(0x6f) // i32.rem_s
		case "==":
			buf.WriteByte(0x46) // i32.eq
		case "!=":
			buf.WriteByte(0x47) // i32.ne
		case "<":
			buf.WriteByte(0x48) // i32.lt_s
		case "<=":
			buf.WriteByte(0x4c) // i32.le_s
		case ">":
			buf.WriteByte(0x4a) // i32.gt_s
		case ">=":
			buf.WriteByte(0x4e) // i32.ge_s
		case "&&":
			buf.WriteByte(0x71) // i32.and
		case "||":
			buf.WriteByte(0x72) // i32.or
		default:
			return fmt.Errorf("unsupported operator %s in wasm emission", e.Op)
		}

	case *MemberExpr:
		// Qualified zero-argument module member: math.PI.
		if target, ok := e.Target.(*IdentExpr); ok {
			if prefix, isModule := ctx.emitter.moduleAliases[target.Name]; isModule {
				name := prefix + e.Member
				if fnIdx, exists := ctx.emitter.fnIndices[name]; exists {
					buf.WriteByte(0x10) // call
					writeUleb128(buf, fnIdx)
					return nil
				}
			}
		}
		// Struct field load: target.member
		structName := e.TargetType.String()
		meta, ok := ctx.emitter.structs[structName]
		if !ok {
			return fmt.Errorf("unknown struct type %s", structName)
		}
		offset, ok := meta.fieldOffsets[e.Member]
		if !ok {
			return fmt.Errorf("unknown field %s on struct %s", e.Member, structName)
		}
		if err := ctx.compileExpr(buf, e.Target); err != nil {
			return err
		}
		buf.WriteByte(0x28) // i32.load
		writeUleb128(buf, 2)
		writeUleb128(buf, uint32(offset))

	case *StructInitExpr:
		// Allocates struct on the arena bump pointer and populates initialized fields
		meta, ok := ctx.emitter.structs[e.Name]
		if !ok {
			return fmt.Errorf("unknown struct %s in init", e.Name)
		}
		structBase := ctx.allocLocal(valTypeI32)

		// 1. structBase = $arena_ptr
		buf.WriteByte(0x23) // global.get 0
		writeUleb128(buf, 0)
		buf.WriteByte(0x21) // local.set structBase
		writeUleb128(buf, structBase)

		// 2. $arena_ptr += totalSize
		buf.WriteByte(0x20) // local.get structBase
		writeUleb128(buf, structBase)
		buf.WriteByte(0x41) // i32.const
		writeSleb128(buf, int32(meta.totalSize))
		buf.WriteByte(0x6a) // i32.add
		buf.WriteByte(0x24) // global.set 0
		writeUleb128(buf, 0)

		// 3. Store field values
		for _, f := range e.Fields {
			offset := meta.fieldOffsets[f.Name]
			buf.WriteByte(0x20) // local.get structBase
			writeUleb128(buf, structBase)
			if err := ctx.compileExpr(buf, f.Val); err != nil {
				return err
			}
			buf.WriteByte(0x36) // i32.store
			writeUleb128(buf, 2)
			writeUleb128(buf, uint32(offset))
		}

		// Return structBase address
		buf.WriteByte(0x20) // local.get structBase
		writeUleb128(buf, structBase)

	case *CallExpr:
		name := ""
		switch callee := e.Callee.(type) {
		case *IdentExpr:
			name = callee.Name
		case *MemberExpr:
			if target, ok := callee.Target.(*IdentExpr); ok {
				if prefix, isModule := ctx.emitter.moduleAliases[target.Name]; isModule {
					name = prefix + callee.Member
				}
			}
		}
		if name != "" {
			if name == "print" || name == "println" {
				return ctx.compilePrint(buf, e.Args, name == "println")
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
			buf.WriteByte(0x10) // call
			writeUleb128(buf, fnIdx)
		}
	}
	return nil
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

		if lit, ok := arg.(*LiteralExpr); ok && lit.Type.String() == "string" {
			off, length := ctx.emitter.internString(lit.Val)
			buf.WriteByte(0x41) // i32.const ptr
			writeSleb128(buf, int32(off))
			buf.WriteByte(0x41) // i32.const len
			writeSleb128(buf, int32(length))
			buf.WriteByte(0x41) // i32.const newline
			writeSleb128(buf, currentNL)
			buf.WriteByte(0x10) // call __print_str
			writeUleb128(buf, ctx.emitter.fnIndices["__print_str"])
		} else {
			if err := ctx.compileExpr(buf, arg); err != nil {
				return err
			}
			buf.WriteByte(0x41) // i32.const newline
			writeSleb128(buf, currentNL)
			buf.WriteByte(0x10) // call __print_i32
			writeUleb128(buf, ctx.emitter.fnIndices["__print_i32"])
		}
	}
	return nil
}

// buildPrintStrFunc emits WASI fd_write invocation: (ptr: i32, len: i32, nl: i32) -> void
func (e *wasmEmitter) buildPrintStrFunc() []byte {
	var body bytes.Buffer

	// ciovec.buf = ptr (store at scratchCiovec)
	body.WriteByte(0x41) // i32.const 0
	writeSleb128(&body, scratchCiovec)
	body.WriteByte(0x20) // local.get 0 (ptr)
	writeUleb128(&body, 0)
	body.WriteByte(0x36) // i32.store
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	// ciovec.len = len (store at scratchCiovec + 4)
	body.WriteByte(0x41) // i32.const 4
	writeSleb128(&body, scratchCiovec+4)
	body.WriteByte(0x20) // local.get 1 (len)
	writeUleb128(&body, 1)
	body.WriteByte(0x36) // i32.store
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	// call fd_write(1, scratchCiovec, 1, scratchNwrite)
	body.WriteByte(0x41) // i32.const 1 (stdout)
	writeSleb128(&body, 1)
	body.WriteByte(0x41) // i32.const ciovec_ptr
	writeSleb128(&body, scratchCiovec)
	body.WriteByte(0x41) // i32.const 1 (ciovec count)
	writeSleb128(&body, 1)
	body.WriteByte(0x41) // i32.const nwritten_ptr
	writeSleb128(&body, scratchNwrite)
	body.WriteByte(0x10) // call fd_write
	writeUleb128(&body, e.fdWriteIdx)
	body.WriteByte(0x1a) // drop result code

	// If nl == 1, print newline byte
	body.WriteByte(0x20) // local.get 2 (nl)
	writeUleb128(&body, 2)
	body.WriteByte(0x04)        // if
	body.WriteByte(valTypeVoid) // void

	// Setup ciovec for "\n" at staticDataOff - 1
	body.WriteByte(0x41) // i32.const 0
	writeSleb128(&body, scratchCiovec)
	body.WriteByte(0x41) // i32.const newline_ptr (offset 48)
	writeSleb128(&body, 48)
	body.WriteByte(0x36) // i32.store
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	body.WriteByte(0x41) // i32.const 4
	writeSleb128(&body, scratchCiovec+4)
	body.WriteByte(0x41) // i32.const 1
	writeSleb128(&body, 1)
	body.WriteByte(0x36) // i32.store
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	body.WriteByte(0x41) // i32.const 1 (stdout)
	writeSleb128(&body, 1)
	body.WriteByte(0x41) // ciovec ptr
	writeSleb128(&body, scratchCiovec)
	body.WriteByte(0x41) // count 1
	writeSleb128(&body, 1)
	body.WriteByte(0x41) // nwritten
	writeSleb128(&body, scratchNwrite)
	body.WriteByte(0x10) // call fd_write
	writeUleb128(&body, e.fdWriteIdx)
	body.WriteByte(0x1a) // drop

	body.WriteByte(0x0b) // end if
	body.WriteByte(0x0b) // end func

	var full bytes.Buffer
	writeUleb128(&full, 0) // no extra locals
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

// buildPrintIntFunc formats an i32 to ASCII and prints it via __print_str
func (e *wasmEmitter) buildPrintIntFunc() []byte {
	// (val: i32, newline: i32) -> void
	// Locals: 2: ptr, 3: is_neg, 4: digit
	var body bytes.Buffer

	// ptr = 47 (end of scratchItoa buffer)
	body.WriteByte(0x41)
	writeSleb128(&body, 47)
	body.WriteByte(0x21)
	writeUleb128(&body, 2)

	// is_neg = 0
	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 3)

	// if val == 0 -> write '0'
	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x45) // eqz
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x20) // local.get ptr
	writeUleb128(&body, 2)
	body.WriteByte(0x41) // '0' = 48
	writeSleb128(&body, 48)
	body.WriteByte(0x3a) // i32.store8
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x20) // ptr
	writeUleb128(&body, 2)
	body.WriteByte(0x41) // len 1
	writeSleb128(&body, 1)
	body.WriteByte(0x20) // newline
	writeUleb128(&body, 1)
	body.WriteByte(0x10) // call __print_str
	writeUleb128(&body, e.fnIndices["__print_str"])
	body.WriteByte(0x0f) // return
	body.WriteByte(0x0b) // end if

	// if val < 0 -> val = -val, is_neg = 1
	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x48) // lt_s
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x6b) // sub (0 - val)
	body.WriteByte(0x21)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x21)
	writeUleb128(&body, 3)

	body.WriteByte(0x0b) // end if

	// Loop: extract digits
	body.WriteByte(0x02) // block
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x03) // loop
	body.WriteByte(valTypeVoid)

	// val == 0 -> break
	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x45) // eqz
	body.WriteByte(0x0d) // br_if
	writeUleb128(&body, 1)

	// digit = (val % 10) + 48
	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x41)
	writeSleb128(&body, 10)
	body.WriteByte(0x6f) // rem_s
	body.WriteByte(0x41)
	writeSleb128(&body, 48)
	body.WriteByte(0x6a) // add
	body.WriteByte(0x21)
	writeUleb128(&body, 4)

	// store at ptr
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x3a) // i32.store8
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	// ptr--
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x21)
	writeUleb128(&body, 2)

	// val /= 10
	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x41)
	writeSleb128(&body, 10)
	body.WriteByte(0x6d) // div_s
	body.WriteByte(0x21)
	writeUleb128(&body, 0)

	body.WriteByte(0x0c) // br 0
	writeUleb128(&body, 0)

	body.WriteByte(0x0b) // end loop
	body.WriteByte(0x0b) // end block

	// If negative, prepend '-'
	body.WriteByte(0x20)
	writeUleb128(&body, 3)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 45) // '-'
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

	// Call __print_str(ptr + 1, 47 - ptr, newline)
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

	body.WriteByte(0x0b) // end

	var full bytes.Buffer
	writeUleb128(&full, 1) // 1 local entry
	writeUleb128(&full, 3) // 3 locals of type i32
	full.WriteByte(valTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildStartFunc(mainIdx uint32) []byte {
	var body bytes.Buffer

	// Store newline character at address 48
	body.WriteByte(0x41) // i32.const 48
	writeSleb128(&body, 48)
	body.WriteByte(0x41) // i32.const 10 ('\n')
	writeSleb128(&body, 10)
	body.WriteByte(0x3a) // i32.store8
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x10) // call main
	writeUleb128(&body, mainIdx)
	body.WriteByte(0x0b) // end

	var codeBuf bytes.Buffer
	writeUleb128(&codeBuf, 0) // 0 locals
	codeBuf.Write(body.Bytes())

	var fullBuf bytes.Buffer
	writeUleb128(&fullBuf, uint32(codeBuf.Len()))
	fullBuf.Write(codeBuf.Bytes())
	return fullBuf.Bytes()
}

func (e *wasmEmitter) assembleBinary() ([]byte, error) {
	var out bytes.Buffer

	// 1. Magic + Version
	out.Write([]byte{0x00, 0x61, 0x73, 0x6d})
	out.Write([]byte{0x01, 0x00, 0x00, 0x00})

	// 2. Type section
	var typeSec bytes.Buffer
	writeUleb128(&typeSec, uint32(len(e.types)))
	for _, t := range e.types {
		typeSec.Write(t)
	}
	writeSection(&out, secType, typeSec.Bytes())

	// 3. Import section (WASI Preview 1 fd_write)
	var importSec bytes.Buffer
	writeUleb128(&importSec, 1) // 1 import
	writeString(&importSec, "wasi_snapshot_preview1")
	writeString(&importSec, "fd_write")
	importSec.WriteByte(0x00)   // import kind: func
	writeUleb128(&importSec, 0) // type index 0
	writeSection(&out, secImport, importSec.Bytes())

	// 4. Function section
	var funcSec bytes.Buffer
	writeUleb128(&funcSec, uint32(len(e.funcs)))
	for _, f := range e.funcs {
		writeUleb128(&funcSec, f)
	}
	writeSection(&out, secFunction, funcSec.Bytes())

	// 5. Memory section (1 page minimum, 256 pages maximum)
	var memSec bytes.Buffer
	writeUleb128(&memSec, 1) // 1 memory declaration
	memSec.WriteByte(0x01)   // flags: has maximum
	writeUleb128(&memSec, 2) // min: 2 pages (128 KB)
	writeUleb128(&memSec, 256)
	writeSection(&out, secMemory, memSec.Bytes())

	// 6. Global section ($arena_ptr at global 0, initialized to initialHeap)
	var globSec bytes.Buffer
	writeUleb128(&globSec, 1) // 1 global
	globSec.WriteByte(valTypeI32)
	globSec.WriteByte(0x01) // mutability: mutable (1)
	globSec.WriteByte(0x41) // i32.const
	writeSleb128(&globSec, initialHeap)
	globSec.WriteByte(0x0b) // end
	writeSection(&out, secGlobal, globSec.Bytes())

	// 7. Export section (export "memory" and "_start")
	var expSec bytes.Buffer
	writeUleb128(&expSec, 2)

	writeString(&expSec, "memory")
	expSec.WriteByte(0x02) // memory export
	writeUleb128(&expSec, 0)

	writeString(&expSec, "_start")
	expSec.WriteByte(0x00) // func export
	writeUleb128(&expSec, e.fnIndices["_start"])
	writeSection(&out, secExport, expSec.Bytes())

	// 8. Code section
	var codeSec bytes.Buffer
	writeUleb128(&codeSec, uint32(len(e.codeBodies)))
	for _, body := range e.codeBodies {
		codeSec.Write(body)
	}
	writeSection(&out, secCode, codeSec.Bytes())

	// 9. Data section (static strings)
	if len(e.data) > 0 {
		var dataSec bytes.Buffer
		writeUleb128(&dataSec, 1) // 1 data segment
		dataSec.WriteByte(0x00)   // memory index 0
		dataSec.WriteByte(0x41)   // i32.const
		writeSleb128(&dataSec, staticDataOff)
		dataSec.WriteByte(0x0b) // end
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
	for {
		b := byte(val & 0x7f)
		val >>= 7
		if val != 0 {
			b |= 0x80
		}
		buf.WriteByte(b)
		if val == 0 {
			break
		}
	}
}

func writeSleb128(buf *bytes.Buffer, val int32) {
	more := true
	for more {
		b := byte(val & 0x7f)
		val >>= 7
		sign := (b & 0x40) != 0
		if (val == 0 && !sign) || (val == -1 && sign) {
			more = false
		} else {
			b |= 0x80
		}
		buf.WriteByte(b)
	}
}
