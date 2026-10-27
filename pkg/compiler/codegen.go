package compiler

import (
	"fmt"
	"strings"
)

type Generator struct {
	Program  *Program
	slices   map[string]string
	options  map[string]string
	results  map[string]string
	pools    map[string]bool
	includes []string
	buf      strings.Builder
	indent   int
	curFnRet UbeType
}

func NewGenerator(prog *Program) *Generator {
	return &Generator{
		Program: prog,
		slices:  make(map[string]string),
		options: make(map[string]string),
		results: make(map[string]string),
		pools:   make(map[string]bool),
	}
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
	case OptionalType:
		m := strings.ReplaceAll(v.Inner.String(), "*", "ptr_")
		g.options[m] = g.toCType(v.Inner)
		return fmt.Sprintf("UbeOption_%s", m)
	case ResultType:
		m := fmt.Sprintf("%s_%s", v.Ok, v.Err)
		g.results[m] = fmt.Sprintf("%s|%s", g.toCType(v.Ok), g.toCType(v.Err))
		return fmt.Sprintf("UbeResult_%s", m)
	case ChanType:
		return "UbeRawChan*"
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
	var body strings.Builder
	oldBuf := g.buf
	g.buf = body

	// Emit declarations
	for _, decl := range g.Program.Decls {
		g.genDecl(decl)
	}

	g.buf = oldBuf

	// Header and Preambles
	g.emit("#include \"ube_runtime.h\"")
	for _, inc := range g.includes {
		g.emit(fmt.Sprintf("#include \"%s\"", inc))
	}
	g.emit("")

	// Forward declare Structs
	for _, decl := range g.Program.Decls {
		if st, ok := decl.(*StructDecl); ok {
			g.emit(fmt.Sprintf("typedef struct %s %s;", st.Name, st.Name))
		}
	}
	g.emit("")

	// Define Structs
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

	// Define Enums
	for _, decl := range g.Program.Decls {
		if en, ok := decl.(*EnumDecl); ok {
			g.emit(fmt.Sprintf("typedef enum {"))
			for _, v := range en.Variants {
				g.emit(fmt.Sprintf("    %s_%s,", en.Name, v.Name))
			}
			g.emit(fmt.Sprintf("} %s_Tag;", en.Name))

			g.emit(fmt.Sprintf("typedef struct {"))
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

	// Slices
	for m, ct := range g.slices {
		g.emit(fmt.Sprintf("typedef struct { %s* data; size_t len; } UbeSlice_%s;", ct, m))
		g.emit(fmt.Sprintf("static inline UbeSlice_%s ube_slice_%s_new() { UbeSlice_%s s; s.data = (%s*)&current_tuber.buffer[current_tuber.top]; s.len = 0; return s; }", m, m, m, ct))
		g.emit(fmt.Sprintf("static inline void ube_slice_%s_push(UbeSlice_%s* s, %s val) { %s* slot = (%s*)ube_bump_alloc_unaligned(sizeof(%s)); if (s->len == 0) s->data = slot; *slot = val; s->len++; }", m, m, ct, ct, ct, ct))
	}

	// Options
	for m, ct := range g.options {
		g.emit(fmt.Sprintf("typedef struct { bool has_val; %s val; } UbeOption_%s;", ct, m))
	}

	// Results
	for m, pair := range g.results {
		parts := strings.Split(pair, "|")
		g.emit(fmt.Sprintf("typedef struct { bool ok; union { %s val; %s err; }; } UbeResult_%s;", parts[0], parts[1], m))
	}

	g.emit("")
	g.emit("static inline void ube_fmt_println_str_helper(string s) { printf(\"%.*s\\n\", (int)s.len, s.data); }")
	g.emit("static inline void ube_fmt_println_c_str_helper(const char* s) { printf(\"%s\\n\", s); }")
	g.emit("static inline void ube_fmt_println_ptr_helper(const void* p) { printf(\"%p\\n\", p); }")
	g.emit("")

	g.buf.WriteString(body.String())
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
			g.emit("size_t __watermark = ube_tuber_save();")
		} else {
			g.emit(fmt.Sprintf("%s %s(%s) {", g.toCType(d.RetType), fnName, strings.Join(params, ", ")))
			g.indent++
			g.emit("size_t __watermark = ube_tuber_save();")
		}

		for _, st := range d.Body.Stmts {
			g.genStmt(st)
		}

		if d.Name == "main" {
			g.emit("ube_tuber_restore(__watermark);")
			g.emit("return 0;")
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
			g.emit(fmt.Sprintf("UbeSlice_%s %s = ube_slice_%s_new();", m, s.Name, m))
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

	case *ReturnStmt:
		if s.Value != nil {
			g.emit("ube_tuber_restore(__watermark);")
			g.emit(fmt.Sprintf("return %s;", g.genExpr(s.Value)))
		} else {
			g.emit("ube_tuber_restore(__watermark);")
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
		g.emit(fmt.Sprintf("int32_t %s;", s.VarName))
		g.emit(fmt.Sprintf("if (!ube_raw_chan_recv(%s, &%s)) break;", g.genExpr(s.Call), s.VarName))
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

	case *TuberStmt:
		g.emit("{")
		g.indent++
		g.emit("size_t __watermark_scope = ube_tuber_save();")
		for _, sub := range s.Body.Stmts {
			g.genStmt(sub)
		}
		g.emit("ube_tuber_restore(__watermark_scope);")
		g.indent--
		g.emit("}")

	case *SpawnStmt:
		g.emit(fmt.Sprintf("ube_spawn((void*(*)(void*))%s, (void*)%s);", g.genExpr(s.Call.Callee), g.genExpr(s.Call.Args[0])))

	case *ExprStmt:
		g.emit(fmt.Sprintf("%s;", g.genExpr(s.Expr)))
	}
}

func (g *Generator) genExpr(expr Expr) string {
	switch e := expr.(type) {
	case *LiteralExpr:
		if e.Type.String() == "string" {
			return fmt.Sprintf("ube_str_lit(\"%s\")", e.Val)
		}
		return e.Val

	case *IdentExpr:
		return e.Name

	case *BinaryExpr:
		return fmt.Sprintf("(%s %s %s)", g.genExpr(e.Left), e.Op, g.genExpr(e.Right))

	case *UnaryExpr:
		return fmt.Sprintf("%s(%s)", e.Op, g.genExpr(e.Operand))

	case *CallExpr:
		if mem, ok := e.Callee.(*MemberExpr); ok {
			if mem.Member == "push" {
				return fmt.Sprintf("ube_slice_%s_push(&%s, %s)",
					strings.ReplaceAll(g.genExpr(mem.Target), "*", "ptr_"),
					g.genExpr(mem.Target),
					g.genExpr(e.Args[0]))
			}
			if mem.Member == "send" {
				return fmt.Sprintf("{ __auto_type __val = %s; ube_raw_chan_send(%s, &__val); }", g.genExpr(e.Args[0]), g.genExpr(mem.Target))
			}
			if mem.Member == "close" {
				return fmt.Sprintf("ube_raw_chan_close(%s)", g.genExpr(mem.Target))
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
		return fmt.Sprintf("(%s).data[%s]", g.genExpr(e.Target), g.genExpr(e.Index))

	case *StructInitExpr:
		var inits []string
		for _, f := range e.Fields {
			inits = append(inits, fmt.Sprintf(".%s = %s", f.Name, g.genExpr(f.Val)))
		}
		return fmt.Sprintf("(%s){ %s }", e.Name, strings.Join(inits, ", "))

	case *OrElseExpr:
		return fmt.Sprintf("({ __auto_type _t = %s; _t.ok ? _t.val : (%s); })", g.genExpr(e.Primary), g.genExpr(e.Fallback))
	}
	return ""
}
