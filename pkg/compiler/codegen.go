package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

type Generator struct {
	Program         *Program
	slices          map[string]string
	options         map[string]string
	results         map[string]string
	pools           map[string]string
	sliceVars       map[string]string
	includes        []string
	buf             strings.Builder
	indent          int
	curFnRet        UbeType
	tmpCounter      int
	deferScopes     [][]string
	scopeMarks      []string
	loopDeferDepth  []int
	loopScopeDepth  []int
	sliceParamVars  map[string]bool
	sliceMutParams  map[string]map[int]bool
	loopLabels      []loopLabel
	loopSelectDepth []int
	selectGroups    []string
	spawnWrappers   []string
}

type loopLabel struct{ breakTo, continueTo string }

func NewGenerator(prog *Program) *Generator {
	return &Generator{
		Program:        prog,
		slices:         make(map[string]string),
		options:        make(map[string]string),
		results:        make(map[string]string),
		pools:          make(map[string]string),
		sliceVars:      make(map[string]string),
		sliceParamVars: make(map[string]bool),
		sliceMutParams: make(map[string]map[int]bool),
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
		g.toCType(OptionalType{Inner: v.Elem})
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

func (g *Generator) pushDeferScope() { g.deferScopes = append(g.deferScopes, nil) }

func (g *Generator) emitDefersFrom(depth int) {
	for i := len(g.deferScopes) - 1; i >= depth; i-- {
		for j := len(g.deferScopes[i]) - 1; j >= 0; j-- {
			g.emit(g.deferScopes[i][j] + ";")
		}
	}
}

func (g *Generator) finishDeferScope() {
	if len(g.deferScopes) == 0 {
		return
	}
	last := len(g.deferScopes) - 1
	for i := len(g.deferScopes[last]) - 1; i >= 0; i-- {
		g.emit(g.deferScopes[last][i] + ";")
	}
	g.deferScopes = g.deferScopes[:last]
}

func (g *Generator) genBlock(body *BlockStmt) {
	g.pushDeferScope()
	for _, st := range body.Stmts {
		g.genStmt(st)
	}
	g.finishDeferScope()
}

func (g *Generator) emitReturnArenaCleanup() {
	for i := len(g.selectGroups) - 1; i >= 0; i-- {
		g.emit("ube_select_group_destroy(&" + g.selectGroups[i] + ");")
	}
	for i := len(g.scopeMarks) - 1; i >= 0; i-- {
		if g.scopeMarks[i] != "__watermark" {
			g.emit("ube_scope_restore(" + g.scopeMarks[i] + ");")
		}
	}
	if len(g.scopeMarks) > 0 && g.scopeMarks[0] == "__watermark" {
		g.emit("ube_scope_restore(__watermark);")
		g.emit("ube_scope_destroy();")
	}
}

func exprPushesName(expr Expr, name string) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *CallExpr:
		if m, ok := e.Callee.(*MemberExpr); ok && (m.Member == "push" || m.Member == "pop") && assignmentRoot(m.Target) == name {
			return true
		}
		if exprPushesName(e.Callee, name) {
			return true
		}
		for _, arg := range e.Args {
			if exprPushesName(arg, name) {
				return true
			}
		}
	case *BinaryExpr:
		return exprPushesName(e.Left, name) || exprPushesName(e.Right, name)
	case *UnaryExpr:
		return exprPushesName(e.Operand, name)
	case *MemberExpr:
		return exprPushesName(e.Target, name)
	case *IndexExpr:
		return exprPushesName(e.Target, name) || exprPushesName(e.Index, name)
	case *SliceSubExpr:
		return exprPushesName(e.Target, name) || exprPushesName(e.Start, name) || exprPushesName(e.End, name)
	case *StructInitExpr:
		for _, f := range e.Fields {
			if exprPushesName(f.Val, name) {
				return true
			}
		}
	case *SliceInitExpr:
		for _, el := range e.Elements {
			if exprPushesName(el, name) {
				return true
			}
		}
	case *ArrayInitExpr:
		for _, el := range e.Elements {
			if exprPushesName(el, name) {
				return true
			}
		}
	case *TryExpr:
		return exprPushesName(e.Operand, name)
	case *OrElseExpr:
		return exprPushesName(e.Primary, name) || exprPushesName(e.Fallback, name)
	case *ErrorExpr:
		return exprPushesName(e.Arg, name)
	case *UnsafeExpr:
		return exprPushesName(e.Value, name)
	}
	return false
}

func stmtPushesName(stmt Stmt, name string) bool {
	switch s := stmt.(type) {
	case *BlockStmt:
		for _, st := range s.Stmts {
			if stmtPushesName(st, name) {
				return true
			}
		}
	case *LetStmt:
		return exprPushesName(s.Value, name)
	case *AssignStmt:
		return exprPushesName(s.LHS, name) || exprPushesName(s.RHS, name)
	case *ReturnStmt:
		return exprPushesName(s.Value, name)
	case *ExprStmt:
		return exprPushesName(s.Expr, name)
	case *DeferStmt:
		return exprPushesName(s.Call, name)
	case *IfStmt:
		if exprPushesName(s.Cond, name) || stmtPushesName(s.Then, name) {
			return true
		}
		return stmtPushesName(s.Else, name)
	case *IfLetStmt:
		if exprPushesName(s.Expr, name) || stmtPushesName(s.Then, name) {
			return true
		}
		return stmtPushesName(s.Else, name)
	case *WhileStmt:
		return exprPushesName(s.Cond, name) || stmtPushesName(s.Body, name)
	case *WhileLetStmt:
		return exprPushesName(s.Call, name) || stmtPushesName(s.Body, name)
	case *ForStmt:
		return exprPushesName(s.Iter, name) || stmtPushesName(s.Body, name)
	case *ScopeStmt:
		return stmtPushesName(s.Body, name)
	case *UnsafeStmt:
		return stmtPushesName(s.Body, name)
	case *SpawnStmt:
		return exprPushesName(s.Call, name)
	case *SelectStmt:
		for _, c := range s.Cases {
			if exprPushesName(c.Chan, name) || stmtPushesName(c.Body, name) {
				return true
			}
		}
		return stmtPushesName(s.Default, name)
	case *MatchStmt:
		if exprPushesName(s.Expr, name) {
			return true
		}
		for _, a := range s.Arms {
			if stmtPushesName(a.Body, name) {
				return true
			}
		}
	}
	return false
}

func (g *Generator) Generate() string {
	oldBuf := g.buf
	g.buf = strings.Builder{}

	for _, decl := range g.Program.Decls {
		if fn, ok := decl.(*FnDecl); ok {
			name := fn.Name
			if fn.Receiver != nil {
				name = fmt.Sprintf("%s_%s", fn.Receiver.Type, fn.Name)
			}
			mutated := make(map[int]bool)
			for i, param := range fn.Params {
				if _, isSlice := param.Type.(SliceType); isSlice && stmtPushesName(fn.Body, param.Name) {
					mutated[i] = true
				}
			}
			g.sliceMutParams[name] = mutated
		}
	}

	for _, decl := range g.Program.Decls {
		g.genDecl(decl)
	}
	g.toCType(ResultType{Ok: StringType{}, Err: StringType{}})
	g.toCType(ResultType{Ok: PrimitiveType{Name: "bool"}, Err: StringType{}})
	g.toCType(ResultType{Ok: PrimitiveType{Name: "i64"}, Err: StringType{}})
	g.toCType(SliceType{Elem: StringType{}})

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
		g.options[m] = ct
		g.emit(fmt.Sprintf("#ifndef UbeSlice_%s_DEFINED\n#define UbeSlice_%s_DEFINED", m, m))
		g.emit(fmt.Sprintf("typedef struct { %s* data; size_t len; size_t cap; } UbeSlice_%s;", ct, m))
		g.emit("#endif")
	}
	for m, ct := range g.options {
		g.emit(fmt.Sprintf("#ifndef UbeOption_%s_DEFINED\n#define UbeOption_%s_DEFINED", m, m))
		g.emit(fmt.Sprintf("typedef struct { bool has_val; %s val; } UbeOption_%s;", ct, m))
		g.emit("#endif")
	}
	for m, pair := range g.results {
		parts := strings.Split(pair, "|")
		g.emit(fmt.Sprintf("#ifndef UbeResult_%s_DEFINED\n#define UbeResult_%s_DEFINED", m, m))
		g.emit(fmt.Sprintf("typedef struct { bool ok; union { %s val; %s err; }; } UbeResult_%s;", parts[0], parts[1], m))
		g.emit("#endif")
	}
	for m, ct := range g.pools {
		g.emit(fmt.Sprintf("DEFINE_UBE_POOL(%s, %s)", ct, m))
	}
	// Prototypes and spawn trampolines follow type declarations so their
	// argument structs can contain complete value types.
	for _, decl := range g.Program.Decls {
		if fn, ok := decl.(*FnDecl); ok {
			name := fn.Name
			params := make([]string, 0, len(fn.Params)+1)
			if fn.Receiver != nil {
				name = fmt.Sprintf("%s_%s", fn.Receiver.Type, fn.Name)
				params = append(params, g.toCType(fn.Receiver.Type))
			}
			mutName := name
			for i, p := range fn.Params {
				ct := g.toCType(p.Type)
				if g.sliceMutParams[mutName][i] {
					ct += "*"
				}
				params = append(params, ct)
			}
			if len(params) == 0 {
				params = append(params, "void")
			}
			if fn.Name != "main" {
				g.emit(fmt.Sprintf("%s %s(%s);", g.toCType(fn.RetType), name, strings.Join(params, ", ")))
			}
		}
	}
	for _, decl := range g.Program.Decls {
		if fn, ok := decl.(*ExternFnDecl); ok {
			params := make([]string, len(fn.Params))
			for i, param := range fn.Params {
				params[i] = g.toCType(param.Type)
			}
			if len(params) == 0 {
				params = append(params, "void")
			}
			g.emit(fmt.Sprintf("extern %s %s(%s);", g.toCType(fn.RetType), fn.Name, strings.Join(params, ", ")))
		}
	}
	for _, wrapper := range g.spawnWrappers {
		g.buf.WriteString(wrapper)
	}
	g.emit("")

	for m, ct := range g.slices {
		g.emit(fmt.Sprintf("static inline UbeSlice_%s ube_slice_%s_new() { UbeSlice_%s s = {0}; return s; }", m, m, m))
		g.emit(fmt.Sprintf("static inline void ube_slice_%s_push(UbeSlice_%s* s, %s val) { if (s->len == s->cap) { if (s->cap > SIZE_MAX / 2) ube_panic_capacity((int64_t)s->cap); size_t cap = s->cap ? s->cap * 2 : 4; if (cap > SIZE_MAX / sizeof(%s)) ube_panic_capacity((int64_t)cap); %s* data = (%s*)ube_bump_alloc(cap * sizeof(%s)); if (s->len) memcpy(data, s->data, s->len * sizeof(%s)); s->data = data; s->cap = cap; } s->data[s->len++] = val; }", m, m, ct, ct, ct, ct, ct, ct))
		g.emit(fmt.Sprintf("static inline UbeOption_%s ube_slice_%s_pop(UbeSlice_%s* s) { UbeOption_%s opt = { .has_val = false }; if (s->len) { opt.has_val = true; opt.val = s->data[--s->len]; } return opt; }", m, m, m, m))
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
		g.sliceVars = make(map[string]string)
		g.sliceParamVars = make(map[string]bool)
		mutName := d.Name
		if d.Receiver != nil {
			mutName = fmt.Sprintf("%s_%s", d.Receiver.Type, d.Name)
		}
		for i, p := range d.Params {
			ct := g.toCType(p.Type)
			if g.sliceMutParams[mutName][i] {
				ct += "*"
				if sl, ok := p.Type.(SliceType); ok {
					m := strings.ReplaceAll(sl.Elem.String(), "*", "ptr_")
					g.sliceVars[p.Name] = m
					g.sliceParamVars[p.Name] = true
				}
			}
			params = append(params, fmt.Sprintf("%s %s", ct, p.Name))
		}

		if d.Name == "main" {
			g.emit("int main(int argc, char** argv) {")
			g.indent++
			g.emit("ube_init_args(argc, argv);")
			g.emit("ube_init();")
			g.emit("UbeWatermark __watermark = ube_scope_save();")
			g.scopeMarks = []string{"__watermark"}
		} else {
			g.emit(fmt.Sprintf("%s %s(%s) {", g.toCType(d.RetType), fnName, strings.Join(params, ", ")))
			g.indent++
			g.scopeMarks = nil
		}
		g.deferScopes = nil
		g.loopDeferDepth = nil
		g.loopScopeDepth = nil
		g.loopLabels = nil
		g.loopSelectDepth = nil
		g.selectGroups = nil
		g.pushDeferScope()

		for _, st := range d.Body.Stmts {
			g.genStmt(st)
		}
		g.finishDeferScope()

		if d.Name == "main" {
			g.emit("ube_scope_restore(__watermark);")
			g.emit("ube_scope_destroy();")
			g.emit("return 0;")
		}
		g.indent--
		g.emit("}")
		g.deferScopes = nil
		g.scopeMarks = nil
		g.sliceParamVars = nil
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
		if len(g.deferScopes) == 0 {
			g.pushDeferScope()
		}
		last := len(g.deferScopes) - 1
		g.deferScopes[last] = append(g.deferScopes[last], g.genExpr(s.Call))

	case *ReturnStmt:
		if s.Value != nil {
			value := g.newTmp()
			g.emit(fmt.Sprintf("__auto_type %s = %s;", value, g.genExpr(s.Value)))
			g.emitDefersFrom(0)
			g.emitReturnArenaCleanup()
			if resT, isResult := g.curFnRet.(ResultType); isResult {
				resCType := g.toCType(resT)
				if _, isErr := s.Value.(*ErrorExpr); isErr {
					g.emit(fmt.Sprintf("return (%s){ .ok = false, .err = %s };", resCType, value))
				} else {
					g.emit(fmt.Sprintf("return (%s){ .ok = true, .val = %s };", resCType, value))
				}
				return
			}
			if optT, isOptional := g.curFnRet.(OptionalType); isOptional {
				optionType := g.toCType(optT)
				if lit, isNull := s.Value.(*LiteralExpr); isNull && lit.Val == "null" {
					g.emit(fmt.Sprintf("return (%s){ .has_val = false };", optionType))
				} else {
					g.emit(fmt.Sprintf("return (%s){ .has_val = true, .val = %s };", optionType, value))
				}
				return
			}
			g.emit(fmt.Sprintf("return %s;", value))
		} else {
			g.emitDefersFrom(0)
			g.emitReturnArenaCleanup()
			g.emit("return;")
		}

	case *IfStmt:
		g.emit(fmt.Sprintf("if (%s) {", g.genExpr(s.Cond)))
		g.indent++
		g.genBlock(s.Then)
		g.indent--
		if s.Else != nil {
			g.emit("} else {")
			g.indent++
			if blk, ok := s.Else.(*BlockStmt); ok {
				g.genBlock(blk)
			} else {
				g.pushDeferScope()
				g.genStmt(s.Else)
				g.finishDeferScope()
			}
			g.indent--
		}
		g.emit("}")

	case *IfLetStmt:
		tmp := g.newTmp()
		g.emit(fmt.Sprintf("__auto_type %s = %s;", tmp, g.genExpr(s.Expr)))
		g.emit(fmt.Sprintf("if (%s.has_val) {", tmp))
		g.indent++
		g.emit(fmt.Sprintf("__auto_type %s = %s.val;", s.VarName, tmp))
		g.genBlock(s.Then)
		g.indent--
		if s.Else != nil {
			g.emit("} else {")
			g.indent++
			if blk, ok := s.Else.(*BlockStmt); ok {
				g.genBlock(blk)
			} else {
				g.pushDeferScope()
				g.genStmt(s.Else)
				g.finishDeferScope()
			}
			g.indent--
		}
		g.emit("}")

	case *WhileStmt:
		labels := loopLabel{breakTo: g.newTmp() + "_break", continueTo: g.newTmp() + "_continue"}
		g.emit(fmt.Sprintf("while (%s) {", g.genExpr(s.Cond)))
		g.indent++
		g.loopLabels = append(g.loopLabels, labels)
		g.loopSelectDepth = append(g.loopSelectDepth, len(g.selectGroups))
		g.loopDeferDepth = append(g.loopDeferDepth, len(g.deferScopes))
		g.loopScopeDepth = append(g.loopScopeDepth, len(g.scopeMarks))
		g.genBlock(s.Body)
		g.loopDeferDepth = g.loopDeferDepth[:len(g.loopDeferDepth)-1]
		g.loopScopeDepth = g.loopScopeDepth[:len(g.loopScopeDepth)-1]
		g.emit(labels.continueTo + ":;")
		g.loopLabels = g.loopLabels[:len(g.loopLabels)-1]
		g.loopSelectDepth = g.loopSelectDepth[:len(g.loopSelectDepth)-1]
		g.indent--
		g.emit("}")
		g.emit(labels.breakTo + ":;")

	case *WhileLetStmt:
		labels := loopLabel{breakTo: g.newTmp() + "_break", continueTo: g.newTmp() + "_continue"}
		g.emit("while (1) {")
		g.indent++
		g.loopLabels = append(g.loopLabels, labels)
		g.loopSelectDepth = append(g.loopSelectDepth, len(g.selectGroups))
		if mem, ok := s.Call.(*CallExpr); ok {
			if m, isM := mem.Callee.(*MemberExpr); isM && m.Member == "recv" {
				g.emit(fmt.Sprintf("int32_t %s = {0};", s.VarName))
				g.emit(fmt.Sprintf("if (!ube_raw_chan_recv(%s, &%s)) goto %s;", g.genExpr(m.Target), s.VarName, labels.breakTo))
			} else {
				g.emit(fmt.Sprintf("__auto_type %s = %s;", s.VarName, g.genExpr(s.Call)))
			}
		} else {
			g.emit(fmt.Sprintf("__auto_type %s = %s;", s.VarName, g.genExpr(s.Call)))
		}
		g.loopDeferDepth = append(g.loopDeferDepth, len(g.deferScopes))
		g.loopScopeDepth = append(g.loopScopeDepth, len(g.scopeMarks))
		g.genBlock(s.Body)
		g.loopDeferDepth = g.loopDeferDepth[:len(g.loopDeferDepth)-1]
		g.loopScopeDepth = g.loopScopeDepth[:len(g.loopScopeDepth)-1]
		g.emit(labels.continueTo + ":;")
		g.loopLabels = g.loopLabels[:len(g.loopLabels)-1]
		g.loopSelectDepth = g.loopSelectDepth[:len(g.loopSelectDepth)-1]
		g.indent--
		g.emit("}")
		g.emit(labels.breakTo + ":;")

	case *ForStmt:
		labels := loopLabel{breakTo: g.newTmp() + "_break", continueTo: g.newTmp() + "_continue"}
		iterExpr := g.genExpr(s.Iter)
		g.emit(fmt.Sprintf("for (size_t _i = 0; _i < (%s).len; _i++) {", iterExpr))
		g.indent++
		g.loopLabels = append(g.loopLabels, labels)
		g.loopSelectDepth = append(g.loopSelectDepth, len(g.selectGroups))
		g.emit(fmt.Sprintf("__auto_type %s = (%s).data[_i];", s.ItemName, iterExpr))
		g.loopDeferDepth = append(g.loopDeferDepth, len(g.deferScopes))
		g.loopScopeDepth = append(g.loopScopeDepth, len(g.scopeMarks))
		g.genBlock(s.Body)
		g.loopDeferDepth = g.loopDeferDepth[:len(g.loopDeferDepth)-1]
		g.loopScopeDepth = g.loopScopeDepth[:len(g.loopScopeDepth)-1]
		g.emit(labels.continueTo + ":;")
		g.loopLabels = g.loopLabels[:len(g.loopLabels)-1]
		g.loopSelectDepth = g.loopSelectDepth[:len(g.loopSelectDepth)-1]
		g.indent--
		g.emit("}")
		g.emit(labels.breakTo + ":;")

	case *ScopeStmt:
		g.emit("{")
		g.indent++
		mark := g.newTmp()
		g.emit(fmt.Sprintf("UbeWatermark %s = ube_scope_save();", mark))
		g.scopeMarks = append(g.scopeMarks, mark)
		g.genBlock(s.Body)
		g.emit("ube_scope_restore(" + mark + ");")
		g.scopeMarks = g.scopeMarks[:len(g.scopeMarks)-1]
		g.indent--
		g.emit("}")
	case *UnsafeStmt:
		g.emit("{")
		g.indent++
		g.genBlock(s.Body)
		g.indent--
		g.emit("}")
	case *LoopControlStmt:
		if len(g.loopDeferDepth) > 0 {
			g.emitDefersFrom(g.loopDeferDepth[len(g.loopDeferDepth)-1])
		}
		if len(g.loopScopeDepth) > 0 {
			for i := len(g.scopeMarks) - 1; i >= g.loopScopeDepth[len(g.loopScopeDepth)-1]; i-- {
				g.emit("ube_scope_restore(" + g.scopeMarks[i] + ");")
			}
		}
		if len(g.loopSelectDepth) > 0 {
			depth := g.loopSelectDepth[len(g.loopSelectDepth)-1]
			for i := len(g.selectGroups) - 1; i >= depth; i-- {
				g.emit("ube_select_group_destroy(&" + g.selectGroups[i] + ");")
			}
		}
		if len(g.loopLabels) > 0 {
			labels := g.loopLabels[len(g.loopLabels)-1]
			if s.Continue {
				g.emit("goto " + labels.continueTo + ";")
			} else {
				g.emit("goto " + labels.breakTo + ";")
			}
		} else if s.Continue {
			g.emit("continue;")
		} else {
			g.emit("break;")
		}

	case *SpawnStmt:
		callee, ok := s.Call.Callee.(*IdentExpr)
		if !ok || len(s.Call.ArgTypes) != len(s.Call.Args) {
			g.emit("/* invalid spawn call omitted: type information unavailable */")
			break
		}
		wrapName := g.newTmp() + "_spawn"
		structName := wrapName + "_args"
		var fields, callArgs []string
		for i, t := range s.Call.ArgTypes {
			fields = append(fields, fmt.Sprintf("%s a%d", g.toCType(t), i))
			callArgs = append(callArgs, fmt.Sprintf("args->a%d", i))
		}
		if len(fields) == 0 {
			fields = append(fields, "uint8_t unused")
		}
		var wrapper strings.Builder
		fmt.Fprintf(&wrapper, "typedef struct { %s; } %s;\n", strings.Join(fields, "; "), structName)
		fmt.Fprintf(&wrapper, "static void* %s(void* raw) { %s* args = (%s*)raw; %s(%s); free(args); return NULL; }\n", wrapName, structName, structName, callee.Name, strings.Join(callArgs, ", "))
		g.spawnWrappers = append(g.spawnWrappers, wrapper.String())
		argNames := make([]string, len(s.Call.Args))
		for i, arg := range s.Call.Args {
			argNames[i] = g.newTmp()
			g.emit(fmt.Sprintf("__auto_type %s = %s;", argNames[i], g.genExpr(arg)))
		}
		argExpr := "NULL"
		if len(argNames) > 0 {
			values := make([]string, len(argNames))
			for i, name := range argNames {
				values[i] = fmt.Sprintf(".a%d = %s", i, name)
			}
			ptr := g.newTmp()
			argExpr = fmt.Sprintf("({ %s* %s = (%s*)malloc(sizeof(%s)); if (!%s) abort(); *%s = (%s){ %s }; %s; })", structName, ptr, structName, structName, ptr, ptr, structName, strings.Join(values, ", "), ptr)
		}
		g.emit(fmt.Sprintf("ube_spawn(%s, %s);", wrapName, argExpr))

	case *SelectStmt:
		label := g.newTmp() + "_select_done"
		channels := make([]string, len(s.Cases))
		for i, c := range s.Cases {
			channels[i] = g.newTmp()
			g.emit(fmt.Sprintf("__auto_type %s = %s;", channels[i], g.genExpr(c.Chan)))
		}
		if s.Default != nil {
			selected := g.newTmp()
			g.emit("do {")
			g.indent++
			g.emit(fmt.Sprintf("bool %s = false;", selected))
			for i, c := range s.Cases {
				g.emit("{")
				g.indent++
				ct := c.ChanElem
				if ct == nil {
					ct = PrimitiveType{Name: "i32"}
				}
				g.emit(fmt.Sprintf("%s %s;", g.toCType(ct), c.VarName))
				g.emit(fmt.Sprintf("if (ube_raw_chan_try_recv(%s, &%s)) {", channels[i], c.VarName))
				g.indent++
				g.genBlock(c.Body)
				g.emit(fmt.Sprintf("%s = true; break;", selected))
				g.indent--
				g.emit("}")
				g.indent--
				g.emit("}")
			}
			g.emit(fmt.Sprintf("if (!%s) {", selected))
			g.indent++
			g.genBlock(s.Default)
			g.indent--
			g.emit("}")
			g.indent--
			g.emit("} while (0);")
		} else {
			group := g.newTmp()
			g.selectGroups = append(g.selectGroups, group)
			g.emit(fmt.Sprintf("UbeSelectGroup %s;", group))
			g.emit(fmt.Sprintf("ube_select_group_init(&%s);", group))
			g.emit("while (1) {")
			g.indent++
			for i, c := range s.Cases {
				g.emit("{")
				g.indent++
				ct := c.ChanElem
				if ct == nil {
					ct = PrimitiveType{Name: "i32"}
				}
				g.emit(fmt.Sprintf("%s %s;", g.toCType(ct), c.VarName))
				g.emit(fmt.Sprintf("if (ube_raw_chan_try_recv(%s, &%s)) {", channels[i], c.VarName))
				g.indent++
				g.genBlock(c.Body)
				g.emit(fmt.Sprintf("goto %s;", label))
				g.indent--
				g.emit("}")
				g.indent--
				g.emit("}")
			}
			for _, ch := range channels {
				g.emit(fmt.Sprintf("if (ube_raw_chan_is_closed(%s)) goto %s;", ch, label))
			}
			waiters := make([]string, len(channels))
			for i, ch := range channels {
				waiters[i] = g.newTmp()
				g.emit(fmt.Sprintf("UbeSelectWaiter %s;", waiters[i]))
				g.emit(fmt.Sprintf("ube_select_register(%s, &%s, &%s);", ch, waiters[i], group))
			}
			ready := make([]string, len(channels))
			for i, ch := range channels {
				ready[i] = fmt.Sprintf("ube_raw_chan_has_data_or_closed(%s)", ch)
			}
			g.emit(fmt.Sprintf("if (%s) {", strings.Join(ready, " || ")))
			g.indent++
			for i, ch := range channels {
				g.emit(fmt.Sprintf("ube_select_unregister(%s, &%s);", ch, waiters[i]))
			}
			g.emit("continue;")
			g.indent--
			g.emit("}")
			g.emit(fmt.Sprintf("ube_select_wait(&%s);", group))
			for i, ch := range channels {
				g.emit(fmt.Sprintf("ube_select_unregister(%s, &%s);", ch, waiters[i]))
			}
			g.indent--
			g.emit("}")
			g.emit(label + ":;")
			g.emit(fmt.Sprintf("ube_select_group_destroy(&%s);", group))
			g.selectGroups = g.selectGroups[:len(g.selectGroups)-1]
		}

	case *MatchStmt:
		tmp := g.newTmp()
		g.emit(fmt.Sprintf("__auto_type %s = %s;", tmp, g.genExpr(s.Expr)))
		g.emit(fmt.Sprintf("switch (%s.tag) {", tmp))
		for _, arm := range s.Arms {
			g.emit(fmt.Sprintf("case %s_%s: {", s.EnumName, arm.Variant))
			g.indent++
			for i, b := range arm.Bindings {
				g.emit(fmt.Sprintf("__auto_type %s = %s.as.%s._%d;", b, tmp, arm.Variant, i))
			}
			if blk, ok := arm.Body.(*BlockStmt); ok {
				g.genBlock(blk)
			} else {
				g.pushDeferScope()
				g.genStmt(arm.Body)
				g.finishDeferScope()
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
	case *UnsafeExpr:
		return fmt.Sprintf("({ __auto_type __ube_unsafe_value = %s; __ube_unsafe_value; })", g.genExpr(e.Value))
	case *LiteralExpr:
		if e.Type.String() == "string" {
			return fmt.Sprintf("ube_str_lit(%s)", strconv.Quote(e.Val))
		}
		if e.Type.String() == "bool" {
			return fmt.Sprintf("((bool)%s)", e.Val)
		}
		return e.Val

	case *IdentExpr:
		if g.sliceParamVars[e.Name] {
			return "(*" + e.Name + ")"
		}
		return e.Name

	case *ErrorExpr:
		return g.genExpr(e.Arg)

	case *BinaryExpr:
		if e.Op == "+" && e.Type != nil && e.Type.String() == "string" {
			return fmt.Sprintf("ube_str_concat(%s, %s)", g.genExpr(e.Left), g.genExpr(e.Right))
		}
		if (e.Op == "==" || e.Op == "!=") && e.OperandType != nil && e.OperandType.String() == "string" {
			equality := fmt.Sprintf("ube_str_eq(%s, %s)", g.genExpr(e.Left), g.genExpr(e.Right))
			if e.Op == "!=" {
				return "((bool)!" + equality + ")"
			}
			return equality
		}
		if e.Op == "&&" || e.Op == "||" || e.Op == "==" || e.Op == "!=" || e.Op == "<" || e.Op == "<=" || e.Op == ">" || e.Op == ">=" {
			return fmt.Sprintf("((bool)(%s %s %s))", g.genExpr(e.Left), e.Op, g.genExpr(e.Right))
		}
		return fmt.Sprintf("(%s %s %s)", g.genExpr(e.Left), e.Op, g.genExpr(e.Right))

	case *UnaryExpr:
		return fmt.Sprintf("%s(%s)", e.Op, g.genExpr(e.Operand))

	case *CallExpr:
		if mem, ok := e.Callee.(*MemberExpr); ok && mem.Member == "recv" && e.PoolElem != nil {
			tmp := g.newTmp()
			ct := g.toCType(e.PoolElem)
			return fmt.Sprintf("({ %s %s = {0}; (void)ube_raw_chan_recv(%s, &%s); %s; })", ct, tmp, g.genExpr(mem.Target), tmp, tmp)
		}
		if id, ok := e.Callee.(*IdentExpr); ok && (id.Name == "println" || id.Name == "println_str") && len(e.Args) == 1 {
			return fmt.Sprintf("UBE_PRINTLN(%s)", g.genExpr(e.Args[0]))
		}
		if id, ok := e.Callee.(*IdentExpr); ok && id.Name == "print" && len(e.Args) == 1 {
			return fmt.Sprintf("UBE_PRINT(%s)", g.genExpr(e.Args[0]))
		}
		if id, ok := e.Callee.(*IdentExpr); ok {
			builtins := map[string]string{"time_now_ms": "ube_time_now_ms", "sleep_ms": "ube_sleep_ms", "yield": "ube_yield", "exit_process": "ube_exit_process", "int_to_str": "ube_int_to_str", "str_to_int": "ube_str_to_int", "read_file": "ube_read_file", "write_file": "ube_write_file", "os_args": "ube_os_args", "str_contains": "ube_str_contains", "str_starts_with": "ube_str_starts_with", "str_ends_with": "ube_str_ends_with"}
			if fn, ok := builtins[id.Name]; ok {
				args := make([]string, len(e.Args))
				for i, arg := range e.Args {
					args[i] = g.genExpr(arg)
				}
				return fmt.Sprintf("%s(%s)", fn, strings.Join(args, ", "))
			}
		}
		if mem, ok := e.Callee.(*MemberExpr); ok {
			if enum, ok := mem.Target.(*IdentExpr); ok {
				if types, found := g.enumVariantTypes(enum.Name, mem.Member); found {
					values := make([]string, 0, len(e.Args))
					for i, arg := range e.Args {
						values = append(values, fmt.Sprintf("._%d = %s", i, g.genExpr(arg)))
					}
					if len(types) == 0 {
						return fmt.Sprintf("(%s){ .tag = %s_%s }", enum.Name, enum.Name, mem.Member)
					}
					return fmt.Sprintf("(%s){ .tag = %s_%s, .as.%s = { %s } }", enum.Name, enum.Name, mem.Member, mem.Member, strings.Join(values, ", "))
				}
			}
			if gen, ok := mem.Target.(*GenericTypeExpr); ok && mem.Member == "new" && len(e.Args) == 1 {
				if gen.BaseType == "Chan" {
					return fmt.Sprintf("ube_raw_chan_new(%s, sizeof(%s))", g.genExpr(e.Args[0]), g.toCType(gen.ElemType))
				}
				m := strings.ReplaceAll(gen.ElemType.String(), "*", "ptr_")
				g.toCType(PoolType{Elem: gen.ElemType})
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
			if mem.Member == "pop" {
				sliceType := strings.ReplaceAll(g.genExpr(mem.Target), "*", "ptr_")
				if id, ok := mem.Target.(*IdentExpr); ok {
					if known, found := g.sliceVars[id.Name]; found {
						sliceType = known
					}
				}
				if sl, ok := mem.TargetType.(SliceType); ok {
					g.toCType(OptionalType{Inner: sl.Elem})
				}
				return fmt.Sprintf("ube_slice_%s_pop(&%s)", sliceType, g.genExpr(mem.Target))
			}
			if mem.Member == "send" {
				return fmt.Sprintf("{ __auto_type __val = %s; ube_raw_chan_send(%s, &__val); }", g.genExpr(e.Args[0]), g.genExpr(mem.Target))
			}
			if mem.Member == "close" {
				return fmt.Sprintf("ube_raw_chan_close(%s)", g.genExpr(mem.Target))
			}
			if mem.Member == "spawn" {
				pName := strings.ReplaceAll(e.PoolElem.String(), "*", "ptr_")
				return fmt.Sprintf("Pool_%s_spawn(%s, %s)", pName, g.genExpr(mem.Target), g.genExpr(e.Args[0]))
			}
			if mem.Member == "get" {
				pName := strings.ReplaceAll(e.PoolElem.String(), "*", "ptr_")
				return fmt.Sprintf("Pool_%s_get(%s, %s)", pName, g.genExpr(mem.Target), g.genExpr(e.Args[0]))
			}
			if mem.Member == "despawn" {
				pName := strings.ReplaceAll(e.PoolElem.String(), "*", "ptr_")
				return fmt.Sprintf("Pool_%s_despawn(%s, %s)", pName, g.genExpr(mem.Target), g.genExpr(e.Args[0]))
			}
			if e.ReceiverType != nil {
				fnName := fmt.Sprintf("%s_%s", e.ReceiverType.String(), mem.Member)
				args := []string{g.genExpr(mem.Target)}
				for i, arg := range e.Args {
					value := g.genExpr(arg)
					if g.sliceMutParams[fnName][i] {
						value = "&(" + value + ")"
					}
					args = append(args, value)
				}
				return fmt.Sprintf("%s(%s)", fnName, strings.Join(args, ", "))
			}
		}
		var args []string
		calleeName := ""
		if id, ok := e.Callee.(*IdentExpr); ok {
			calleeName = id.Name
		}
		for i, a := range e.Args {
			arg := g.genExpr(a)
			if g.sliceMutParams[calleeName][i] {
				arg = "&(" + arg + ")"
			}
			args = append(args, arg)
		}
		return fmt.Sprintf("%s(%s)", g.genExpr(e.Callee), strings.Join(args, ", "))

	case *MemberExpr:
		if id, ok := e.Target.(*IdentExpr); ok && g.isUnitEnumVariant(id.Name, e.Member) {
			return fmt.Sprintf("(%s){ .tag = %s_%s }", id.Name, id.Name, e.Member)
		}
		if e.Member == "ptr" {
			return fmt.Sprintf("(uint8_t*)((%s).data)", g.genExpr(e.Target))
		}
		if e.Member == "len" {
			if arr, ok := e.TargetType.(ArrayType); ok {
				return fmt.Sprintf("((int32_t)%d)", arr.Size)
			}
			return fmt.Sprintf("((int32_t)(%s).len)", g.genExpr(e.Target))
		}
		if e.Member == "cap" {
			if arr, ok := e.TargetType.(ArrayType); ok {
				return fmt.Sprintf("((int32_t)%d)", arr.Size)
			}
			return fmt.Sprintf("((int32_t)(%s).cap)", g.genExpr(e.Target))
		}
		if e.Member == "len" {
			if arr, ok := e.TargetType.(ArrayType); ok {
				return fmt.Sprintf("((int32_t)%d)", arr.Size)
			}
			if _, ok := e.TargetType.(StringType); ok {
				return fmt.Sprintf("((int32_t)(%s).len)", g.genExpr(e.Target))
			}
			return fmt.Sprintf("((int32_t)(%s).len)", g.genExpr(e.Target))
		}
		if e.Member == "cap" {
			if arr, ok := e.TargetType.(ArrayType); ok {
				return fmt.Sprintf("((int32_t)%d)", arr.Size)
			}
			return fmt.Sprintf("((int32_t)(%s).cap)", g.genExpr(e.Target))
		}
		return fmt.Sprintf("(%s).%s", g.genExpr(e.Target), e.Member)

	case *IndexExpr:
		file := strconv.Quote(e.At.File)
		switch targetType := e.TargetType.(type) {
		case ArrayType:
			return fmt.Sprintf("(*UBE_ARRAY_PTR((%s), (%s), %d, %s, %d))", g.genExpr(e.Target), g.genExpr(e.Index), targetType.Size, file, e.At.Line)
		default:
			return fmt.Sprintf("(*UBE_SLICE_PTR((%s), (%s), %s, %d))", g.genExpr(e.Target), g.genExpr(e.Index), file, e.At.Line)
		}
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
		if resRet, ok := g.curFnRet.(ResultType); ok {
			return fmt.Sprintf("({ __auto_type _r = %s; if (!_r.ok) return (%s){ .ok = false, .err = _r.err }; _r.val; })", g.genExpr(e.Operand), g.toCType(resRet))
		}
		return fmt.Sprintf("({ __auto_type _r = %s; if (!_r.ok) { fprintf(stderr, \"[Ube Panic]: Unhandled error at %s:%d\\n\"); exit(101); } _r.val; })", g.genExpr(e.Operand), e.At.File, e.At.Line)
	}
	return ""
}

func (g *Generator) isUnitEnumVariant(enumName, variantName string) bool {
	for _, decl := range g.Program.Decls {
		if enum, ok := decl.(*EnumDecl); ok && enum.Name == enumName {
			for _, variant := range enum.Variants {
				if variant.Name == variantName && len(variant.Types) == 0 {
					return true
				}
			}
		}
	}
	return false
}

func (g *Generator) enumVariantTypes(enumName, variantName string) ([]UbeType, bool) {
	for _, decl := range g.Program.Decls {
		if enum, ok := decl.(*EnumDecl); ok && enum.Name == enumName {
			for _, variant := range enum.Variants {
				if variant.Name == variantName {
					return variant.Types, true
				}
			}
		}
	}
	return nil, false
}
