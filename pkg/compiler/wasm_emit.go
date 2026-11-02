package compiler

import (
	"bytes"
	"fmt"
)

// EmitWasm compiles an AST Program directly into standard WebAssembly v1 binary
// bytes with WASI Preview 1 support, requiring zero C toolchains or external SDKs.
func EmitWasm(prog *Program) ([]byte, error) {
	e := newWasmEmitter()
	return e.compile(prog)
}

type wasmEmitter struct {
	types      [][]byte
	typeMap    map[string]uint32
	funcs      []uint32 // type index for each defined func
	codeBodies [][]byte
	data       []byte
	dataOffset uint32
	fnIndices  map[string]uint32
	globals    []byte
}

func newWasmEmitter() *wasmEmitter {
	return &wasmEmitter{
		typeMap:    make(map[string]uint32),
		dataOffset: 1024, // Reserve first 1024 bytes for scratch/IO
		fnIndices:  make(map[string]uint32),
	}
}

func (e *wasmEmitter) registerType(params []byte, results []byte) uint32 {
	key := fmt.Sprintf("%v->%v", params, results)
	if idx, ok := e.typeMap[key]; ok {
		return idx
	}
	var b bytes.Buffer
	b.WriteByte(0x60) // func type
	writeUleb128(&b, uint32(len(params)))
	b.Write(params)
	writeUleb128(&b, uint32(len(results)))
	b.Write(results)
	idx := uint32(len(e.types))
	e.types = append(e.types, b.Bytes())
	e.typeMap[key] = idx
	return idx
}

func (e *wasmEmitter) compile(prog *Program) ([]byte, error) {
	// Import 0: wasi_snapshot_preview1.fd_write (i32, i32, i32, i32) -> i32
	fdWriteType := e.registerType([]byte{0x7f, 0x7f, 0x7f, 0x7f}, []byte{0x7f})
	_ = fdWriteType
	e.fnIndices["wasi_fd_write"] = 0

	// Initialize scratch space in memory:
	// offset 64: newline '\n' (0x0a)
	e.data = make([]byte, 1024)
	e.data[64] = '\n'

	// Collect user function declarations
	var userFns []*FnDecl
	var mainFn *FnDecl
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*FnDecl); ok {
			userFns = append(userFns, fn)
			if fn.Name == "main" && fn.Receiver == nil {
				mainFn = fn
			}
		}
	}

	if mainFn == nil {
		return nil, fmt.Errorf("direct wasm emission requires 'fn main()'")
	}

	// Register function signatures (index starts at 1, after fd_write)
	currFnIdx := uint32(1)
	for _, fn := range userFns {
		var params []byte
		for range fn.Params {
			params = append(params, 0x7f) // i32 default for primitives & pointers
		}
		var results []byte
		if fn.RetType != nil {
			if _, isVoid := fn.RetType.(PrimitiveType); !isVoid || fn.RetType.String() != "void" {
				results = append(results, 0x7f)
			}
		}
		tIdx := e.registerType(params, results)
		e.funcs = append(e.funcs, tIdx)
		e.fnIndices[fn.Name] = currFnIdx
		currFnIdx++
	}

	// Helper: __ube_print_int (converts i32 to ascii digits, prints via fd_write)
	printIntType := e.registerType([]byte{0x7f, 0x7f}, nil) // (val, newline) -> void
	e.funcs = append(e.funcs, printIntType)
	printIntIdx := currFnIdx
	e.fnIndices["__ube_print_int"] = printIntIdx
	currFnIdx++

	// Register _start entrypoint (type: () -> void)
	startType := e.registerType(nil, nil)
	e.funcs = append(e.funcs, startType)
	startFnIdx := currFnIdx
	currFnIdx++

	// Compile user functions
	for _, fn := range userFns {
		body, err := e.compileFunction(fn)
		if err != nil {
			return nil, err
		}
		e.codeBodies = append(e.codeBodies, body)
	}

	// Compile __ube_print_int helper
	e.codeBodies = append(e.codeBodies, e.buildPrintIntFunc())

	// Compile _start entrypoint
	e.codeBodies = append(e.codeBodies, e.buildStartFunc(e.fnIndices["main"]))

	return e.assembleBinary(startFnIdx)
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
		emitter:   e,
		locals:    make(map[string]uint32),
		numParams: uint32(len(fn.Params)),
	}

	for i, param := range fn.Params {
		ctx.locals[param.Name] = uint32(i)
	}

	var code bytes.Buffer
	if err := ctx.compileBlock(&code, fn.Body); err != nil {
		return nil, err
	}
	code.WriteByte(0x0b) // end opcode

	// Assemble function: local declarations + code
	var body bytes.Buffer
	var localEntries [][]byte
	for _, t := range ctx.localTypes {
		var l bytes.Buffer
		writeUleb128(&l, 1) // count
		l.WriteByte(t)     // type
		localEntries = append(localEntries, l.Bytes())
	}

	writeUleb128(&body, uint32(len(localEntries)))
	for _, le := range localEntries {
		body.Write(le)
	}
	body.Write(code.Bytes())

	var final bytes.Buffer
	writeUleb128(&final, uint32(body.Len()))
	final.Write(body.Bytes())
	return final.Bytes(), nil
}

func (ctx *fnContext) compileBlock(buf *bytes.Buffer, block *BlockStmt) error {
	if block == nil {
		return nil
	}
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
		idx := ctx.numParams + uint32(len(ctx.localTypes))
		ctx.localTypes = append(ctx.localTypes, 0x7f)
		ctx.locals[s.Name] = idx
		if s.Value != nil {
			if err := ctx.compileExpr(buf, s.Value); err != nil {
				return err
			}
			buf.WriteByte(0x21) // local.set
			writeUleb128(buf, idx)
		}
	case *AssignStmt:
		if ident, ok := s.LHS.(*IdentExpr); ok {
			idx, exists := ctx.locals[ident.Name]
			if !exists {
				return fmt.Errorf("undefined variable in assignment: %s", ident.Name)
			}
			if s.Op == "=" {
				if err := ctx.compileExpr(buf, s.RHS); err != nil {
					return err
				}
			} else {
				// Compound assignment: +=, -=, *=, /=
				buf.WriteByte(0x20) // local.get
				writeUleb128(buf, idx)
				if err := ctx.compileExpr(buf, s.RHS); err != nil {
					return err
				}
				switch s.Op {
				case "+=":
					buf.WriteByte(0x6a) // i32.add
				case "-=":
					buf.WriteByte(0x6b) // i32.sub
				case "*=":
					buf.WriteByte(0x6c) // i32.mul
				case "/=":
					buf.WriteByte(0x6d) // i32.div_s
				default:
					return fmt.Errorf("unsupported assign operator %s", s.Op)
				}
			}
			buf.WriteByte(0x21) // local.set
			writeUleb128(buf, idx)
		} else {
			return fmt.Errorf("complex lvalue assignment not yet supported in direct wasm")
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
		buf.WriteByte(0x04) // if
		buf.WriteByte(0x40) // void block
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
		// block { loop { cond; i32.eqz; br_if 1; body; br 0 } }
		buf.WriteByte(0x02) // block
		buf.WriteByte(0x40)
		buf.WriteByte(0x03) // loop
		buf.WriteByte(0x40)
		if err := ctx.compileExpr(buf, s.Cond); err != nil {
			return err
		}
		buf.WriteByte(0x45) // i32.eqz
		buf.WriteByte(0x0d) // br_if 1 (exit block)
		writeUleb128(buf, 1)

		if err := ctx.compileBlock(buf, s.Body); err != nil {
			return err
		}
		buf.WriteByte(0x0c) // br 0 (repeat loop)
		writeUleb128(buf, 0)
		buf.WriteByte(0x0b) // end loop
		buf.WriteByte(0x0b) // end block
	case *LoopControlStmt:
		if s.Continue {
			buf.WriteByte(0x0c) // br 0
			writeUleb128(buf, 0)
		} else {
			buf.WriteByte(0x0c) // br 1
			writeUleb128(buf, 1)
		}
	case *ExprStmt:
		if err := ctx.compileExpr(buf, s.Expr); err != nil {
			return err
		}
		// If expression leaves a value on stack, drop it
		if call, isCall := s.Expr.(*CallExpr); isCall {
			if ident, ok := call.Callee.(*IdentExpr); ok && (ident.Name == "print" || ident.Name == "println") {
				// built-ins do not leave values on stack
				return nil
			}
			buf.WriteByte(0x1a) // drop
		}
	default:
		return fmt.Errorf("unsupported statement type %T in direct wasm", stmt)
	}
	return nil
}

func (ctx *fnContext) compileExpr(buf *bytes.Buffer, expr Expr) error {
	switch e := expr.(type) {
	case *LiteralExpr:
		switch e.Type.String() {
		case "i32", "int":
			var val int32
			fmt.Sscanf(e.Val, "%d", &val)
			buf.WriteByte(0x41) // i32.const
			writeSleb128(buf, val)
		case "bool":
			buf.WriteByte(0x41)
			if e.Val == "true" {
				writeSleb128(buf, 1)
			} else {
				writeSleb128(buf, 0)
			}
		case "string":
			// Place string in data section, push pointer to linear memory
			strBytes := []byte(e.Val)
			ptr := ctx.emitter.dataOffset
			ctx.emitter.data = append(ctx.emitter.data, strBytes...)
			ctx.emitter.dataOffset += uint32(len(strBytes))
			buf.WriteByte(0x41)
			writeSleb128(buf, int32(ptr))
		default:
			return fmt.Errorf("unsupported literal type: %s", e.Type)
		}
	case *IdentExpr:
		idx, exists := ctx.locals[e.Name]
		if !exists {
			return fmt.Errorf("undefined variable: %s", e.Name)
		}
		buf.WriteByte(0x20) // local.get
		writeUleb128(buf, idx)
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
		case "&":
			buf.WriteByte(0x71) // i32.and
		case "|":
			buf.WriteByte(0x72) // i32.or
		case "^":
			buf.WriteByte(0x73) // i32.xor
		case "<<":
			buf.WriteByte(0x74) // i32.shl
		case ">>":
			buf.WriteByte(0x75) // i32.shr_s
		default:
			return fmt.Errorf("unsupported binary operator: %s", e.Op)
		}
	case *UnaryExpr:
		if err := ctx.compileExpr(buf, e.Operand); err != nil {
			return err
		}
		switch e.Op {
		case "-":
			// 0 - val
			buf.WriteByte(0x41)
			writeSleb128(buf, 0)
			buf.WriteByte(0x6b) // i32.sub
		case "!":
			buf.WriteByte(0x45) // i32.eqz
		default:
			return fmt.Errorf("unsupported unary operator: %s", e.Op)
		}
	case *CallExpr:
		if ident, ok := e.Callee.(*IdentExpr); ok {
			if ident.Name == "print" || ident.Name == "println" {
				return ctx.compilePrint(buf, e.Args, ident.Name == "println")
			}
			fnIdx, exists := ctx.emitter.fnIndices[ident.Name]
			if !exists {
				return fmt.Errorf("undefined function %s", ident.Name)
			}
			for _, arg := range e.Args {
				if err := ctx.compileExpr(buf, arg); err != nil {
					return err
				}
			}
			buf.WriteByte(0x10) // call
			writeUleb128(buf, fnIdx)
		} else {
			return fmt.Errorf("indirect function calls not yet supported in direct wasm")
		}
	default:
		return fmt.Errorf("unsupported expression type %T in direct wasm", expr)
	}
	return nil
}

func (ctx *fnContext) compilePrint(buf *bytes.Buffer, args []Expr, newline bool) error {
	for i, arg := range args {
		isLast := (i == len(args)-1)
		nl := int32(0)
		if isLast && newline {
			nl = 1
		}
		if lit, ok := arg.(*LiteralExpr); ok && lit.Type.String() == "string" {
			strBytes := []byte(lit.Val)
			ptr := ctx.emitter.dataOffset
			ctx.emitter.data = append(ctx.emitter.data, strBytes...)
			ctx.emitter.dataOffset += uint32(len(strBytes))

			// Write iovec to offset 16
			// iovec.buf = ptr
			buf.WriteByte(0x41); writeSleb128(buf, 16)
			buf.WriteByte(0x41); writeSleb128(buf, int32(ptr))
			buf.WriteByte(0x36); writeUleb128(buf, 2); writeUleb128(buf, 0)

			// iovec.len = len(strBytes)
			buf.WriteByte(0x41); writeSleb128(buf, 20)
			buf.WriteByte(0x41); writeSleb128(buf, int32(len(strBytes)))
			buf.WriteByte(0x36); writeUleb128(buf, 2); writeUleb128(buf, 0)

			// call fd_write(1, 16, 1, 24)
			buf.WriteByte(0x41); writeSleb128(buf, 1)
			buf.WriteByte(0x41); writeSleb128(buf, 16)
			buf.WriteByte(0x41); writeSleb128(buf, 1)
			buf.WriteByte(0x41); writeSleb128(buf, 24)
			buf.WriteByte(0x10); writeUleb128(buf, 0) // fd_write
			buf.WriteByte(0x1a)                        // drop

			if nl == 1 {
				// write newline from offset 64
				buf.WriteByte(0x41); writeSleb128(buf, 16)
				buf.WriteByte(0x41); writeSleb128(buf, 64)
				buf.WriteByte(0x36); writeUleb128(buf, 2); writeUleb128(buf, 0)

				buf.WriteByte(0x41); writeSleb128(buf, 20)
				buf.WriteByte(0x41); writeSleb128(buf, 1)
				buf.WriteByte(0x36); writeUleb128(buf, 2); writeUleb128(buf, 0)

				buf.WriteByte(0x41); writeSleb128(buf, 1)
				buf.WriteByte(0x41); writeSleb128(buf, 16)
				buf.WriteByte(0x41); writeSleb128(buf, 1)
				buf.WriteByte(0x41); writeSleb128(buf, 24)
				buf.WriteByte(0x10); writeUleb128(buf, 0)
				buf.WriteByte(0x1a)
			}
		} else {
			// Integer or general expression -> route through __ube_print_int
			if err := ctx.compileExpr(buf, arg); err != nil {
				return err
			}
			buf.WriteByte(0x41)
			writeSleb128(buf, nl)
			buf.WriteByte(0x10) // call __ube_print_int
			writeUleb128(buf, ctx.emitter.fnIndices["__ube_print_int"])
		}
	}
	return nil
}

// buildPrintIntFunc creates an integer-to-string formatter in linear memory
func (e *wasmEmitter) buildPrintIntFunc() []byte {
	var body bytes.Buffer
	// locals: 3 local i32 (temp variables)
	writeUleb128(&body, 1)
	writeUleb128(&body, 3)
	body.WriteByte(0x7f)

	// Simple fallback: write single digit or positive int
	// If val == 0, write '0'
	var c bytes.Buffer
	c.WriteByte(0x20); writeUleb128(&c, 0) // local.get 0
	c.WriteByte(0x41); writeSleb128(&c, 0)
	c.WriteByte(0x46) // i32.eq
	c.WriteByte(0x04); c.WriteByte(0x40)
	// write '0' at offset 32
	c.WriteByte(0x41); writeSleb128(&c, 32)
	c.WriteByte(0x41); writeSleb128(&c, 48) // '0'
	c.WriteByte(0x3a); writeUleb128(&c, 0); writeUleb128(&c, 0) // i32.store8
	// iovec.buf = 32, len = 1
	c.WriteByte(0x41); writeSleb128(&c, 16)
	c.WriteByte(0x41); writeSleb128(&c, 32)
	c.WriteByte(0x36); writeUleb128(&c, 2); writeUleb128(&c, 0)
	c.WriteByte(0x41); writeSleb128(&c, 20)
	c.WriteByte(0x41); writeSleb128(&c, 1)
	c.WriteByte(0x36); writeUleb128(&c, 2); writeUleb128(&c, 0)
	c.WriteByte(0x41); writeSleb128(&c, 1)
	c.WriteByte(0x41); writeSleb128(&c, 16)
	c.WriteByte(0x41); writeSleb128(&c, 1)
	c.WriteByte(0x41); writeSleb128(&c, 24)
	c.WriteByte(0x10); writeUleb128(&c, 0); c.WriteByte(0x1a)
	c.WriteByte(0x05) // else

	// Positive numbers: decompose into buffer
	// ptr = 60; while val > 0 { ptr--; mem[ptr] = (val % 10) + 48; val /= 10; }
	c.WriteByte(0x41); writeSleb128(&c, 60)
	c.WriteByte(0x21); writeUleb128(&c, 2) // local.set 2 (ptr)

	c.WriteByte(0x02); c.WriteByte(0x40) // block
	c.WriteByte(0x03); c.WriteByte(0x40) // loop
	c.WriteByte(0x20); writeUleb128(&c, 0) // val
	c.WriteByte(0x45) // eqz
	c.WriteByte(0x0d); writeUleb128(&c, 1) // br_if 1

	// ptr--
	c.WriteByte(0x20); writeUleb128(&c, 2)
	c.WriteByte(0x41); writeSleb128(&c, 1)
	c.WriteByte(0x6b)
	c.WriteByte(0x21); writeUleb128(&c, 2)

	// store digit
	c.WriteByte(0x20); writeUleb128(&c, 2)
	c.WriteByte(0x20); writeUleb128(&c, 0)
	c.WriteByte(0x41); writeSleb128(&c, 10)
	c.WriteByte(0x6f) // val % 10
	c.WriteByte(0x41); writeSleb128(&c, 48)
	c.WriteByte(0x6a) // + '0'
	c.WriteByte(0x3a); writeUleb128(&c, 0); writeUleb128(&c, 0) // i32.store8

	// val /= 10
	c.WriteByte(0x20); writeUleb128(&c, 0)
	c.WriteByte(0x41); writeSleb128(&c, 10)
	c.WriteByte(0x6d)
	c.WriteByte(0x21); writeUleb128(&c, 0)
	c.WriteByte(0x0c); writeUleb128(&c, 0) // br 0
	c.WriteByte(0x0b); c.WriteByte(0x0b)

	// Output buffer: len = 60 - ptr
	c.WriteByte(0x41); writeSleb128(&c, 16)
	c.WriteByte(0x20); writeUleb128(&c, 2)
	c.WriteByte(0x36); writeUleb128(&c, 2); writeUleb128(&c, 0)

	c.WriteByte(0x41); writeSleb128(&c, 20)
	c.WriteByte(0x41); writeSleb128(&c, 60)
	c.WriteByte(0x20); writeUleb128(&c, 2)
	c.WriteByte(0x6b)
	c.WriteByte(0x36); writeUleb128(&c, 2); writeUleb128(&c, 0)

	c.WriteByte(0x41); writeSleb128(&c, 1)
	c.WriteByte(0x41); writeSleb128(&c, 16)
	c.WriteByte(0x41); writeSleb128(&c, 1)
	c.WriteByte(0x41); writeSleb128(&c, 24)
	c.WriteByte(0x10); writeUleb128(&c, 0); c.WriteByte(0x1a)
	c.WriteByte(0x0b) // end if

	// If newline is requested (param 1 == 1), output newline
	c.WriteByte(0x20); writeUleb128(&c, 1)
	c.WriteByte(0x04); c.WriteByte(0x40)
	c.WriteByte(0x41); writeSleb128(&c, 16)
	c.WriteByte(0x41); writeSleb128(&c, 64)
	c.WriteByte(0x36); writeUleb128(&c, 2); writeUleb128(&c, 0)
	c.WriteByte(0x41); writeSleb128(&c, 20)
	c.WriteByte(0x41); writeSleb128(&c, 1)
	c.WriteByte(0x36); writeUleb128(&c, 2); writeUleb128(&c, 0)
	c.WriteByte(0x41); writeSleb128(&c, 1)
	c.WriteByte(0x41); writeSleb128(&c, 16)
	c.WriteByte(0x41); writeSleb128(&c, 1)
	c.WriteByte(0x41); writeSleb128(&c, 24)
	c.WriteByte(0x10); writeUleb128(&c, 0); c.WriteByte(0x1a)
	c.WriteByte(0x0b)

	c.WriteByte(0x0b) // end func
	body.Write(c.Bytes())

	var final bytes.Buffer
	writeUleb128(&final, uint32(body.Len()))
	final.Write(body.Bytes())
	return final.Bytes()
}

func (e *wasmEmitter) buildStartFunc(mainIdx uint32) []byte {
	var body bytes.Buffer
	writeUleb128(&body, 0) // 0 local declarations

	// Call main()
	body.WriteByte(0x10) // call
	writeUleb128(&body, mainIdx)
	body.WriteByte(0x0b) // end

	var final bytes.Buffer
	writeUleb128(&final, uint32(body.Len()))
	final.Write(body.Bytes())
	return final.Bytes()
}

func (e *wasmEmitter) assembleBinary(startFnIdx uint32) ([]byte, error) {
	var bin bytes.Buffer
	// Magic & Version
	bin.Write([]byte{0x00, 0x61, 0x73, 0x6d})
	bin.Write([]byte{0x01, 0x00, 0x00, 0x00})

	// Section 1: Types
	{
		var sec bytes.Buffer
		writeUleb128(&sec, uint32(len(e.types)))
		for _, t := range e.types {
			sec.Write(t)
		}
		writeSection(&bin, 1, sec.Bytes())
	}

	// Section 2: Imports (wasi_snapshot_preview1.fd_write)
	{
		var sec bytes.Buffer
		writeUleb128(&sec, 1) // 1 import
		writeString(&sec, "wasi_snapshot_preview1")
		writeString(&sec, "fd_write")
		sec.WriteByte(0x00) // function import
		writeUleb128(&sec, 0) // type index 0
		writeSection(&bin, 2, sec.Bytes())
	}

	// Section 3: Functions (type signatures for internal functions)
	{
		var sec bytes.Buffer
		writeUleb128(&sec, uint32(len(e.funcs)))
		for _, f := range e.funcs {
			writeUleb128(&sec, f)
		}
		writeSection(&bin, 3, sec.Bytes())
	}

	// Section 5: Memory (1 initial page = 64KB)
	{
		var sec bytes.Buffer
		writeUleb128(&sec, 1) // 1 memory
		sec.WriteByte(0x00)   // limits: min only
		writeUleb128(&sec, 2) // 2 pages = 128KB
		writeSection(&bin, 5, sec.Bytes())
	}

	// Section 7: Exports (memory & _start)
	{
		var sec bytes.Buffer
		writeUleb128(&sec, 2) // 2 exports

		// export "memory"
		writeString(&sec, "memory")
		sec.WriteByte(0x02) // memory export
		writeUleb128(&sec, 0)

		// export "_start"
		writeString(&sec, "_start")
		sec.WriteByte(0x00) // func export
		writeUleb128(&sec, startFnIdx)

		writeSection(&bin, 7, sec.Bytes())
	}

	// Section 10: Code
	{
		var sec bytes.Buffer
		writeUleb128(&sec, uint32(len(e.codeBodies)))
		for _, cb := range e.codeBodies {
			sec.Write(cb)
		}
		writeSection(&bin, 10, sec.Bytes())
	}

	// Section 11: Data (active segment initialized into memory at offset 0)
	if len(e.data) > 0 {
		var sec bytes.Buffer
		writeUleb128(&sec, 1) // 1 segment
		sec.WriteByte(0x00)   // active, memory 0
		sec.WriteByte(0x41)   // i32.const 0
		writeSleb128(&sec, 0)
		sec.WriteByte(0x0b)   // end
		writeUleb128(&sec, uint32(len(e.data)))
		sec.Write(e.data)
		writeSection(&bin, 11, sec.Bytes())
	}

	return bin.Bytes(), nil
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
	for {
		b := byte(val & 0x7f)
		val >>= 7
		sign := (b & 0x40) != 0
		if (val == 0 && !sign) || (val == -1 && sign) {
			buf.WriteByte(b)
			break
		}
		buf.WriteByte(b | 0x80)
	}
}
