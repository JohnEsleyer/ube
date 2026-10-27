package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

type Generator struct {
	Program    *Program
	slices     map[string]string
	options    map[string]string
	results    map[string]string
	pools      map[string]string
	sliceVars  map[string]string
	includes   []string
	buf        strings.Builder
	indent     int
	curFnRet   UbeType
	tmpCounter int
	deferCalls []string
}

func NewGenerator(prog *Program) *Generator {
	return &Generator{
		Program:   prog,
		slices:    make(map[string]string),
		options:   make(map[string]string),
		results:   make(map[string]string),
		pools:     make(map[string]string),
		sliceVars: make(map[string]string),
	}
}

func (g *Generator) newTmp() string {
	g.tmpCounter++
	return fmt.Sprintf("__ube_tmp_%d", g.tmpCounter)
}

func (g *Generator) toCType(t UbeType) string {
	switch v := t.(type) {
	case PrimitiveType:
		switch v.Name {
		case "i32":
			return "int32_t"
		case "u32":
			return "uint32_t"
		case "u8":
			return "uint8_t"
		case "i64":
			return "int64_t"
		case "u64":
			return "uint64_t"
		case "f32":
			return "float"
		case "f64":
			return "double"
		case "bool":
			return "bool"
		case "void":
			return "void"
		default:
			return v.Name
		}
	case StringType:
		return "string"
	case PointerType:
		return g.toCType(v.Base) + "*"
	case SliceType:
		m := strings.ReplaceAll(v.Elem.String(), "*", "ptr_")
		g.slices[m] = g.toCType(v.Elem)
		return fmt.Sprintf("UbeSlice_%s", m)
	case ArrayType:
		return fmt.Sprintf("struct { %s data[%d]; }", g.toCType(v.Elem), v.Size)
	case OptionalType:
		m := strings.ReplaceAll(v.Inner.String(), "*", "ptr_")
		g.options[m] = g.toCType(v.Inner)
		return fmt.Sprintf("UbeOption_%s", m)
	case ResultType:
		m := fmt.Sprintf("%s_%s", v.Ok, v.Err)
		m = strings.ReplaceAll(m, "*", "ptr_")
		g.results[m] = fmt.Sprintf("%s|%s", g.toCType(v.Ok), g.toCType(v.Err))
		return fmt.Sprintf("UbeResult_%s", m)
	case ChanType:
		return "UbeRawChan*"
	case PoolType:
		m := strings.ReplaceAll(v.Elem.String(), "*", "ptr_")
		g.pools[m] = g.toCType(v.Elem)
		return fmt.Sprintf("Pool_%s*", m)
	case CustomType:
		return v.Name
	default:
		return "void*"
	}
}

func (g *Generator) emit(s string) {
	g.buf.WriteString(strings.Repeat("    ", g.indent))
	g.buf.WriteString(s)
	g.buf.WriteString("\n")
}

func (g *Generator) Generate() string {
	oldBuf := g.buf
	g.buf = strings.Builder{}

	for _, decl := range g.Program.Decls {
		g.genDecl(decl)
	}

	body := g.buf.String()
	g.buf = oldBuf

	g.emit("#include \"ube_runtime.h\"")
	for _, inc := range g.includes {
		g.emit(fmt.Sprintf("#include \"%s\"", inc))
	}
	g.emit("")

	for _, decl := range g.Program.Decls {
		if st, ok := decl.(*StructDecl); ok {
			g.emit(fmt.Sprintf("typedef struct %s %s;", st.Name, st.Name))
		}
	}
	g.emit("")

	for _, decl := range g.Program.Decls {
		if st, ok := decl.(*StructDecl); ok {
			g.emit(fmt.Sprintf("struct %s {", st.Name))
			for _, f := range st.Fields {
				g.emit(fmt.Sprintf("    %s %s;", g.toCType(f.Type), f.Name))
			}
			g.emit("};")
			g.emit("")
		}
	}

	for _, decl := range g.Program.Decls {
		if en, ok := decl.(*EnumDecl); ok {
			g.emit("typedef enum {")
			for _, v := range en.Variants {
				g.emit(fmt.Sprintf("    %s_%s,", en.Name, v.Name))
			}
			g.emit(fmt.Sprintf("} %s_Tag;", en.Name))

			g.emit("typedef struct {")
			g.emit(fmt.Sprintf("    %s_Tag tag;", en.Name))
			g.emit("    union {")
			for _, v := range en.Variants {
				if len(v.Types) > 0 {
					var fields []string
					for i, t := range v.Types {
						fields = append(fields, fmt.Sprintf("%s _%d;", g.toCType(t), i))
					}
					g.emit(fmt.Sprintf("        struct { %s } %s;", strings.Join(fields, " "), v.Name))
				}
			}
			g.emit("    } as;")
			g.emit(fmt.Sprintf("} %s;", en.Name))
			g.emit("")
		}
	}

	for m, ct := range g.slices {
		g.emit(fmt.Sprintf("typedef struct { %s* data; size_t len; size_t cap; } UbeSlice_%s;", ct, m))
		g.emit(fmt.Sprintf("static inline UbeSlice_%s ube_slice_%s_new() { UbeSlice_%s s = {0}; return s; }", m, m, m))
		g.emit(fmt.Sprintf("static inline void ube_slice_%s_push(UbeSlice_%s* s, %s val) { if (s->len == s->cap) { size_t cap = s->cap ? s->cap * 2 : 4; %s* data = (%s*)ube_bump_alloc(cap * sizeof(%s)); if (s->len) memcpy(data, s->data, s->len * sizeof(%s)); s->data = data; s->cap = cap; } s->data[s->len++] = val; }", m, m, ct, ct, ct, ct, ct))
	}

	for m, ct := range g.options {
		g.emit(fmt.Sprintf("typedef struct { bool has_val; %s val; } UbeOption_%s;", ct, m))
	}

	for m, pair := range g.results {
		parts := strings.Split(pair, "|")
		g.emit(fmt.Sprintf("typedef struct { bool ok; union { %s val; %s err; }; } UbeResult_%s;", parts[0], parts[1], m))
	}

	for m := range g.pools {
		g.emit(fmt.Sprintf("DEFINE_UBE_POOL(%s)", m))
	}

	g.emit("")
	g.buf.WriteString(body)
	return g.buf.String()
}

func (g *Generator) genDecl(decl Decl) {
	switch d := decl.(type) {
	case *ExternFnDecl:
		var params []string
		for _, p := range d.Params {
			params = append(params, fmt.Sprintf("%s %s", g.toCType(p.Type), p.Name))
		}
		g.emit(fmt.Sprintf("extern %s %s(%s);", g.toCType(d.RetType), d.Name, strings.Join(params, ", ")))

	case *FnDecl:
		g.curFnRet = d.RetType
		var params []string
		fnName := d.Name
		if d.Receiver != nil {
			fnName = fmt.Sprintf("%s_%s", d.Receiver.Type, d.Name)
			params = append(params, fmt.Sprintf("%s %s", g.toCType(d.Receiver.Type), d.Receiver.Name))
		}
		for _, p := range d.Params {
			params = append(params, fmt.Sprintf("%s %s", g.toCType(p.Type), p.Name))
		}

		if d.Name == "main" {
			g.emit("int main(int argc, char** argv) {")
			g.indent++
			g.emit("(void)argc; (void)argv;")
			g.emit("ube_init();")
			g.emit("UbeWatermark __watermark = ube_scope_save();")
		} else {
			g.emit(fmt.Sprintf("%s %s(%s) {", g.toCType(d.RetType), fnName, strings.Join(params, ", ")))
			g.indent++
			g.emit("UbeWatermark __watermark = ube_scope_save();")
		}

		for _, st := range d.Body.Stmts {
			g.genStmt(st)
		}
		for i := len(g.deferCalls) - 1; i >= 0; i-- {
			g.emit(g.deferCalls[i] + ";")
		}
		g.deferCalls = nil

		if d.Name == "main" {
			g.emit("ube_scope_restore(__watermark);")
			g.emit("return 0;")
		} else {
			if d.RetType.String() == "void" {
				g.emit("ube_scope_restore(__watermark);")
			}
		}
		g.indent--
		g.emit("}")
		g.emit("")
	}
}

func (g *Generator) genStmt(st Stmt) {
	switch s := st.(type) {
	case *LetStmt:
		if sl, ok := s.Value.(*SliceInitExpr); ok {
			m := strings.ReplaceAll(sl.Elem.String(), "*", "ptr_")
			g.toCType(SliceType{Elem: sl.Elem})
			g.sliceVars[s.Name] = m
			if len(sl.Elements) == 0 {
				g.emit(fmt.Sprintf("UbeSlice_%s %s = ube_slice_%s_new();", m, s.Name, m))
			} else {
				g.emit(fmt.Sprintf("UbeSlice_%s %s = { .len=%d, .cap=%d, .data=(%s*)ube_bump_alloc(%d*sizeof(%s)) };", m, s.Name, len(sl.Elements), len(sl.Elements), g.toCType(sl.Elem), len(sl.Elements), g.toCType(sl.Elem)))
				for i, e := range sl.Elements {
					g.emit(fmt.Sprintf("%s.data[%d] = %s;", s.Name, i, g.genExpr(e)))
				}
			}
			return
		}
		if s.Type != nil {
			cType := g.toCType(s.Type)
			g.emit(fmt.Sprintf("%s %s = %s;", cType, s.Name, g.genExpr(s.Value)))
		} else {
			g.emit(fmt.Sprintf("__auto_type %s = %s;", s.Name, g.genExpr(s.Value)))
		}

	case *AssignStmt:
		g.emit(fmt.Sprintf("%s = %s;", g.genExpr(s.LHS), g.genExpr(s.RHS)))
	case *DeferStmt:
		g.deferCalls = append(g.deferCalls, g.genExpr(s.Call))

	case *ReturnStmt:
		for i := len(g.deferCalls) - 1; i >= 0; i-- {
			g.emit(g.deferCalls[i] + ";")
		}
		if s.Value != nil {
			// Caller-inherited dynamic slice bump preservation
			if _, isSlice := g.curFnRet.(SliceType); isSlice {
				g.emit(fmt.Sprintf("return %s;", g.genExpr(s.Value)))
				return
			}
			if resT, isResult := g.curFnRet.(ResultType); isResult {
				if errExpr, isErr := s.Value.(*ErrorExpr); isErr {
					resCType := g.toCType(resT)
					g.emit("ube_scope_restore(__watermark);")
					g.emit(fmt.Sprintf("return (%s){ .ok = false, .err = %s };", resCType, g.genExpr(errExpr.Arg)))
					return
				}
				resCType := g.toCType(resT)
				g.emit("ube_scope_restore(__watermark);")
				g.emit(fmt.Sprintf("return (%s){ .ok = true, .val = %s };", resCType, g.genExpr(s.Value)))
				return
			}
			g.emit("ube_scope_restore(__watermark);")
			g.emit(fmt.Sprintf("return %s;", g.genExpr(s.Value)))
		} else {
			g.emit("ube_scope_restore(__watermark);")
			g.emit("return;")
		}

	case *IfStmt:
		g.emit(fmt.Sprintf("if (%s) {", g.genExpr(s.Cond)))
		g.indent++
		for _, sub := range s.Then.Stmts {
			g.genStmt(sub)
		}
		g.indent--
		if s.Else != nil {
			g.emit("} else {")
			g.indent++
			if blk, ok := s.Else.(*BlockStmt); ok {
				for _, sub := range blk.Stmts {
					g.genStmt(sub)
				}
			} else {
				g.genStmt(s.Else)
			}
			g.indent--
		}
		g.emit("}")

	case *IfLetStmt:
		tmp := fmt.Sprintf("__opt_%s", s.VarName)
		g.emit(fmt.Sprintf("__auto_type %s = %s;", tmp, g.genExpr(s.Expr)))
		g.emit(fmt.Sprintf("if (%s.has_val) {", tmp))
		g.indent++
		g.emit(fmt.Sprintf("__auto_type %s = %s.val;", s.VarName, tmp))
		for _, sub := range s.Then.Stmts {
			g.genStmt(sub)
		}
		g.indent--
		if s.Else != nil {
			g.emit("} else {")
			g.indent++
			if blk, ok := s.Else.(*BlockStmt); ok {
				for _, sub := range blk.Stmts {
					g.genStmt(sub)
				}
			} else {
				g.genStmt(s.Else)
			}
			g.indent--
		}
		g.emit("}")

	case *WhileStmt:
		g.emit(fmt.Sprintf("while (%s) {", g.genExpr(s.Cond)))
		g.indent++
		for _, sub := range s.Body.Stmts {
			g.genStmt(sub)
		}
		g.indent--
		g.emit("}")

	case *WhileLetStmt:
		g.emit("while (1) {")
		g.indent++
		g.emit(fmt.Sprintf("__auto_type %s = %s;", s.VarName, g.genExpr(s.Call)))
		if mem, ok := s.Call.(*CallExpr); ok {
			if m, isM := mem.Callee.(*MemberExpr); isM && m.Member == "recv" {
				g.emit(fmt.Sprintf("if (!ube_raw_chan_recv(%s, &%s)) break;", g.genExpr(m.Target), s.VarName))
			}
		}
		for _, sub := range s.Body.Stmts {
			g.genStmt(sub)
		}
		g.indent--
		g.emit("}")

	case *ForStmt:
		iterExpr := g.genExpr(s.Iter)
		g.emit(fmt.Sprintf("for (size_t _i = 0; _i < (%s).len; _i++) {", iterExpr))
		g.indent++
		g.emit(fmt.Sprintf("__auto_type %s = (%s).data[_i];", s.ItemName, iterExpr))
		for _, sub := range s.Body.Stmts {
			g.genStmt(sub)
		}
		g.indent--
		g.emit("}")

	case *ScopeStmt:
		g.emit("{")
		g.indent++
		g.emit("UbeWatermark __watermark_scope = ube_scope_save();")
		for _, sub := range s.Body.Stmts {
			g.genStmt(sub)
		}
		g.emit("ube_scope_restore(__watermark_scope);")
		g.indent--
		g.emit("}")
	case *LoopControlStmt:
		if s.Continue {
			g.emit("continue;")
		} else {
			g.emit("break;")
		}

	case *SpawnStmt:
		var args []string
		for _, arg := range s.Call.Args {
			args = append(args, g.genExpr(arg))
		}
		argExpr := "NULL"
		if len(args) == 1 {
			argExpr = fmt.Sprintf("(void*)%s", args[0])
		} else if len(args) > 1 {
			argPack := g.newTmp()
			g.emit(fmt.Sprintf("void** %s = (void**)malloc(%d * sizeof(void*));", argPack, len(args)))
			for idx, a := range args {
				g.emit(fmt.Sprintf("%s[%d] = (void*)%s;", argPack, idx, a))
			}
			argExpr = fmt.Sprintf("(void*)%s", argPack)
		}
		g.emit(fmt.Sprintf("ube_spawn((void*(*)(void*))%s, %s);", g.genExpr(s.Call.Callee), argExpr))

	case *SelectStmt:
		g.emit("while (1) {")
		g.indent++
		for _, c := range s.Cases {
			g.emit("{")
			g.indent++
			g.emit(fmt.Sprintf("__auto_type __c = %s;", g.genExpr(c.Chan)))
			ct := c.ChanElem
			if ct == nil {
				ct = PrimitiveType{Name: "i32"}
			}
			g.emit(fmt.Sprintf("%s %s;", g.toCType(ct), c.VarName))
			g.emit(fmt.Sprintf("if (ube_raw_chan_try_recv(__c, &%s)) {", c.VarName))
			g.indent++
			for _, sub := range c.Body.Stmts {
				g.genStmt(sub)
			}
			g.emit("break;")
			g.indent--
			g.emit("}")
			g.indent--
			g.emit("}")
		}
		if s.Default != nil {
			for _, sub := range s.Default.Stmts {
				g.genStmt(sub)
			}
			g.emit("break;")
		} else {
			g.emit("struct timespec ts = {0, 1000000}; nanosleep(&ts, NULL);")
		}
		g.indent--
		g.emit("}")

	case *MatchStmt:
		tmp := g.newTmp()
		g.emit(fmt.Sprintf("__auto_type %s = %s;", tmp, g.genExpr(s.Expr)))
		g.emit(fmt.Sprintf("switch (%s.tag) {", tmp))
		for _, arm := range s.Arms {
			g.emit(fmt.Sprintf("case %s: {", arm.Variant))
			g.indent++
			for i, b := range arm.Bindings {
				g.emit(fmt.Sprintf("__auto_type %s = %s.as.%s._%d;", b, tmp, arm.Variant, i))
			}
			if blk, ok := arm.Body.(*BlockStmt); ok {
				for _, sub := range blk.Stmts {
					g.genStmt(sub)
				}
			} else {
				g.genStmt(arm.Body)
			}
			g.emit("break;")
			g.indent--
			g.emit("}")
		}
		g.emit("}")

	case *ExprStmt:
		g.emit(fmt.Sprintf("%s;", g.genExpr(s.Expr)))
	}
}

func (g *Generator) genExpr(expr Expr) string {
	switch e := expr.(type) {
	case *LiteralExpr:
		if e.Type.String() == "string" {
			return fmt.Sprintf("ube_str_lit(%s)", strconv.Quote(e.Val))
		}
		if e.Type.String() == "bool" {
			return fmt.Sprintf("((bool)%s)", e.Val)
		}
		return e.Val

	case *IdentExpr:
		return e.Name

	case *ErrorExpr:
		return g.genExpr(e.Arg)

	case *BinaryExpr:
		if e.Op == "&&" || e.Op == "||" || e.Op == "==" || e.Op == "!=" || e.Op == "<" || e.Op == "<=" || e.Op == ">" || e.Op == ">=" {
			return fmt.Sprintf("((bool)(%s %s %s))", g.genExpr(e.Left), e.Op, g.genExpr(e.Right))
		}
		return fmt.Sprintf("(%s %s %s)", g.genExpr(e.Left), e.Op, g.genExpr(e.Right))

	case *UnaryExpr:
		return fmt.Sprintf("%s(%s)", e.Op, g.genExpr(e.Operand))

	case *CallExpr:
		if id, ok := e.Callee.(*IdentExpr); ok && (id.Name == "println" || id.Name == "println_str") && len(e.Args) == 1 {
			return fmt.Sprintf("UBE_PRINTLN(%s)", g.genExpr(e.Args[0]))
		}
		if mem, ok := e.Callee.(*MemberExpr); ok {
			if gen, ok := mem.Target.(*GenericTypeExpr); ok && mem.Member == "new" && len(e.Args) == 1 {
				if gen.BaseType == "Chan" {
					return fmt.Sprintf("ube_raw_chan_new(%s, sizeof(%s))", g.genExpr(e.Args[0]), g.toCType(gen.ElemType))
				}
				m := strings.ReplaceAll(gen.ElemType.String(), "*", "ptr_")
				return fmt.Sprintf("Pool_%s_new(%s)", m, g.genExpr(e.Args[0]))
			}
			if mem.Member == "push" {
				sliceType := strings.ReplaceAll(g.genExpr(mem.Target), "*", "ptr_")
				if id, ok := mem.Target.(*IdentExpr); ok {
					if knownType, found := g.sliceVars[id.Name]; found {
						sliceType = knownType
					}
				}
				return fmt.Sprintf("ube_slice_%s_push(&%s, %s)",
					sliceType,
					g.genExpr(mem.Target),
					g.genExpr(e.Args[0]))
			}
			if mem.Member == "send" {
				return fmt.Sprintf("{ __auto_type __val = %s; ube_raw_chan_send(%s, &__val); }", g.genExpr(e.Args[0]), g.genExpr(mem.Target))
			}
			if mem.Member == "close" {
				return fmt.Sprintf("ube_raw_chan_close(%s)", g.genExpr(mem.Target))
			}
			if mem.Member == "spawn" {
				pName := strings.TrimPrefix(g.genExpr(mem.Target), "Pool_")
				return fmt.Sprintf("Pool_%s_spawn(%s, %s)", pName, g.genExpr(mem.Target), g.genExpr(e.Args[0]))
			}
			if mem.Member == "get" {
				pName := strings.TrimPrefix(g.genExpr(mem.Target), "Pool_")
				return fmt.Sprintf("Pool_%s_get(%s, %s)", pName, g.genExpr(mem.Target), g.genExpr(e.Args[0]))
			}
			if mem.Member == "despawn" {
				pName := strings.TrimPrefix(g.genExpr(mem.Target), "Pool_")
				return fmt.Sprintf("Pool_%s_despawn(%s, %s)", pName, g.genExpr(mem.Target), g.genExpr(e.Args[0]))
			}
		}
		var args []string
		for _, a := range e.Args {
			args = append(args, g.genExpr(a))
		}
		return fmt.Sprintf("%s(%s)", g.genExpr(e.Callee), strings.Join(args, ", "))

	case *MemberExpr:
		if e.Member == "ptr" {
			return fmt.Sprintf("(uint8_t*)((%s).data)", g.genExpr(e.Target))
		}
		return fmt.Sprintf("(%s).%s", g.genExpr(e.Target), e.Member)

	case *IndexExpr:
		if lit, ok := e.Target.(*LiteralExpr); ok && lit.Type.String() == "string" {
			return fmt.Sprintf("((uint8_t)ube_str_index(%s,%s))", g.genExpr(e.Target), g.genExpr(e.Index))
		}
		return fmt.Sprintf("(%s).data[%s]", g.genExpr(e.Target), g.genExpr(e.Index))
	case *SliceSubExpr:
		return fmt.Sprintf("ube_str_slice(%s, %s, %s)", g.genExpr(e.Target), g.genExpr(e.Start), g.genExpr(e.End))
	case *ArrayInitExpr:
		var vals []string
		for _, el := range e.Elements {
			vals = append(vals, g.genExpr(el))
		}
		return fmt.Sprintf("(%s){ .data = { %s } }", g.toCType(ArrayType{Elem: e.Elem, Size: e.Size}), strings.Join(vals, ", "))
	case *GenericTypeExpr:
		return e.BaseType

	case *StructInitExpr:
		var inits []string
		for _, f := range e.Fields {
			inits = append(inits, fmt.Sprintf(".%s = %s", f.Name, g.genExpr(f.Val)))
		}
		return fmt.Sprintf("(%s){ %s }", e.Name, strings.Join(inits, ", "))

	case *OrElseExpr:
		return fmt.Sprintf("({ __auto_type _t = %s; _t.ok ? _t.val : (%s); })", g.genExpr(e.Primary), g.genExpr(e.Fallback))

	case *TryExpr:
		resRet := g.toCType(g.curFnRet)
		return fmt.Sprintf("({ __auto_type _r = %s; if (!_r.ok) return (%s){ .ok = false, .err = _r.err }; _r.val; })", g.genExpr(e.Operand), resRet)
	}
	return ""
}
