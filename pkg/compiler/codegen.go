package compiler

import (
	"fmt"
	"strings"
)

type Generator struct {
	Program         *Program
	slices          map[string]string
	options         map[string]string
	results         map[string]string
	pools           map[string]string
	arrays          map[string]string
	sliceKeys       []string
	optionKeys      []string
	resultKeys      []string
	poolKeys        []string
	arrayKeys       []string
	sliceVars       map[string]string
	includes        []string
	buf             strings.Builder
	indent          int
	curFnRet        UbeType
	curFnName       string
	tmpCounter      int
	deferScopes     [][]string
	scopeMarks      []string
	loopDeferDepth  []int
	loopScopeDepth  []int
	sliceParamVars  map[string]bool
	sliceMutParams    map[string]map[int]bool
	receiverMutParams map[string]bool
	loopLabels        []loopLabel
	loopSelectDepth []int
	selectGroups    []string
	spawnWrappers   []string
	traitDecls      map[string]*TraitDecl
	traitVtables    map[string]traitVtableInfo
	traitVtableKeys []string
}

type traitVtableInfo struct {
	Trait    string
	Concrete UbeType
}

type loopLabel struct{ breakTo, continueTo string }

func NewGenerator(prog *Program) *Generator {
	g := &Generator{
		Program:        prog,
		slices:         make(map[string]string),
		options:        make(map[string]string),
		results:        make(map[string]string),
		pools:          make(map[string]string),
		arrays:         make(map[string]string),
		sliceVars:      make(map[string]string),
		sliceParamVars: make(map[string]bool),
		sliceMutParams:    make(map[string]map[int]bool),
		receiverMutParams: make(map[string]bool),
		traitDecls:        make(map[string]*TraitDecl),
		traitVtables:   make(map[string]traitVtableInfo),
	}
	if prog != nil {
		for _, decl := range prog.Decls {
			if tr, ok := decl.(*TraitDecl); ok {
				g.traitDecls[tr.Name] = tr
			}
		}
	}
	return g
}

func (g *Generator) newTmp() string {
	g.tmpCounter++
	return fmt.Sprintf("__ube_tmp_%d", g.tmpCounter)
}

func (g *Generator) registerSlice(m, ct string) {
	if _, exists := g.slices[m]; !exists {
		g.sliceKeys = append(g.sliceKeys, m)
	}
	g.slices[m] = ct
}

func (g *Generator) registerOption(m, ct string) {
	if _, exists := g.options[m]; !exists {
		g.optionKeys = append(g.optionKeys, m)
	}
	g.options[m] = ct
}

func (g *Generator) registerResult(m, pair string) {
	if _, exists := g.results[m]; !exists {
		g.resultKeys = append(g.resultKeys, m)
	}
	g.results[m] = pair
}

func (g *Generator) registerPool(m, ct string) {
	if _, exists := g.pools[m]; !exists {
		g.poolKeys = append(g.poolKeys, m)
	}
	g.pools[m] = ct
}

func (g *Generator) registerArray(m, spec string) {
	if _, exists := g.arrays[m]; !exists {
		g.arrayKeys = append(g.arrayKeys, m)
	}
	g.arrays[m] = spec
}

func (g *Generator) registerTraitVtable(trait string, concrete UbeType) string {
	key := fmt.Sprintf("%s_%s", trait, mangleTypeName(concrete.String()))
	if _, exists := g.traitVtables[key]; !exists {
		g.traitVtables[key] = traitVtableInfo{Trait: trait, Concrete: concrete}
		g.traitVtableKeys = append(g.traitVtableKeys, key)
	}
	return key + "_vtable"
}

func mangleTypeName(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		r := s[i]
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteByte(r)
		case r == '*':
			b.WriteString("ptr_")
		case r == '[' && i+1 < len(s) && s[i+1] == ']':
			b.WriteString("slice_")
			i++
		case r == '?':
			b.WriteString("opt_")
		case r == '!':
			b.WriteString("res_")
		case r == ' ' || r == '\t':
			// skip whitespace
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
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
	case StringBuilderType:
		return "UbeStringBuilder"
	case TraitType:
		return v.Name
	case GenericInstanceType:
		return v.Base
	case PointerType:
		return g.toCType(v.Base) + "*"
	case SliceType:
		m := mangleTypeName(v.Elem.String())
		ct := g.toCType(v.Elem)
		g.registerSlice(m, ct)
		return fmt.Sprintf("UbeSlice_%s", m)
	case ArrayType:
		elemCT := g.toCType(v.Elem)
		m := fmt.Sprintf("%s_%d", mangleTypeName(v.Elem.String()), v.Size)
		g.registerArray(m, fmt.Sprintf("%s|%d", elemCT, v.Size))
		return fmt.Sprintf("UbeArray_%s", m)
	case OptionalType:
		m := mangleTypeName(v.Inner.String())
		ct := g.toCType(v.Inner)
		g.registerOption(m, ct)
		return fmt.Sprintf("UbeOption_%s", m)
	case ResultType:
		m := mangleTypeName(fmt.Sprintf("%s_%s", v.Ok, v.Err))
		g.registerResult(m, fmt.Sprintf("%s|%s", g.toCType(v.Ok), g.toCType(v.Err)))
		return fmt.Sprintf("UbeResult_%s", m)
	case ChanType:
		return "UbeRawChan*"
	case PoolType:
		m := mangleTypeName(v.Elem.String())
		ct := g.toCType(v.Elem)
		g.registerPool(m, ct)
		g.toCType(OptionalType{Inner: v.Elem})
		return fmt.Sprintf("Pool_%s*", m)
	case CustomType:
		return v.Name
	default:
		return "void*"
	}
}

func (g *Generator) sliceElemMangle(elem UbeType) string {
	m := mangleTypeName(elem.String())
	ct := g.toCType(elem)
	g.registerSlice(m, ct)
	g.registerOption(m, ct)
	return m
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

func (g *Generator) returnCleanupCode() string {
	var b strings.Builder
	for i := len(g.deferScopes) - 1; i >= 0; i-- {
		for j := len(g.deferScopes[i]) - 1; j >= 0; j-- {
			b.WriteString(g.deferScopes[i][j])
			b.WriteString("; ")
		}
	}
	for i := len(g.selectGroups) - 1; i >= 0; i-- {
		b.WriteString("ube_select_group_destroy(&" + g.selectGroups[i] + "); ")
	}
	for i := len(g.scopeMarks) - 1; i >= 0; i-- {
		if g.scopeMarks[i] != "__watermark" {
			b.WriteString("ube_arena_exit(&" + g.scopeMarks[i] + "); ")
		}
	}
	if len(g.scopeMarks) > 0 && g.scopeMarks[0] == "__watermark" {
		b.WriteString("ube_scope_restore(__watermark); ube_scope_destroy(); ")
	}
	return b.String()
}

func (g *Generator) emitReturnArenaCleanup() {
	for i := len(g.selectGroups) - 1; i >= 0; i-- {
		g.emit("ube_select_group_destroy(&" + g.selectGroups[i] + ");")
	}
	for i := len(g.scopeMarks) - 1; i >= 0; i-- {
		if g.scopeMarks[i] != "__watermark" {
			g.emit("ube_arena_exit(&" + g.scopeMarks[i] + ");")
		}
	}
	if len(g.scopeMarks) > 0 && g.scopeMarks[0] == "__watermark" {
		g.emit("ube_scope_restore(__watermark);")
		g.emit("ube_scope_destroy();")
	}
}

func (g *Generator) Generate() string {
	oldBuf := g.buf
	g.buf = strings.Builder{}

	g.analyzeSliceMutations()

	// Register mutable receivers before emitting function bodies so calls to
	// them know to pass an address.
	for _, decl := range g.Program.Decls {
		if fn, ok := decl.(*FnDecl); ok && fn.Receiver != nil && fn.Receiver.IsMut {
			g.receiverMutParams[fmt.Sprintf("%s_%s", fn.Receiver.Type, fn.Name)] = true
		}
	}

	for _, decl := range g.Program.Decls {
		g.genDecl(decl)
	}
	g.toCType(ResultType{Ok: StringType{}, Err: StringType{}})
	g.toCType(ResultType{Ok: PrimitiveType{Name: "bool"}, Err: StringType{}})
	g.toCType(ResultType{Ok: PrimitiveType{Name: "i64"}, Err: StringType{}})
	g.toCType(SliceType{Elem: StringType{}})
	g.toCType(ResultType{Ok: SliceType{Elem: StringType{}}, Err: StringType{}})

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

	// Pre-register aggregate types
	for _, decl := range g.Program.Decls {
		switch d := decl.(type) {
		case *StructDecl:
			for _, f := range d.Fields {
				g.toCType(f.Type)
			}
		case *EnumDecl:
			for _, v := range d.Variants {
				for _, t := range v.Types {
					g.toCType(t)
				}
			}
		case *TraitDecl:
			for _, method := range d.Methods {
				for _, p := range method.Params {
					g.toCType(p.Type)
				}
				g.toCType(method.RetType)
			}
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

	for _, decl := range g.Program.Decls {
		if tr, ok := decl.(*TraitDecl); ok {
			g.emit(fmt.Sprintf("typedef struct %s %s;", tr.Name, tr.Name))
			g.emit(fmt.Sprintf("typedef struct %s_VTable %s_VTable;", tr.Name, tr.Name))
			g.emit(fmt.Sprintf("struct %s { void* self; const %s_VTable* vtable; };", tr.Name, tr.Name))
			g.emit("")
		}
	}

	for _, m := range g.sliceKeys {
		ct := g.slices[m]
		g.registerOption(m, ct)
		g.emit(fmt.Sprintf("#ifndef UbeSlice_%s_DEFINED\n#define UbeSlice_%s_DEFINED", m, m))
		g.emit(fmt.Sprintf("typedef struct { %s* data; size_t len; size_t cap; } UbeSlice_%s;", ct, m))
		g.emit("#endif")
	}
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

	for _, m := range g.arrayKeys {
		spec := g.arrays[m]
		parts := strings.SplitN(spec, "|", 2)
		g.emit(fmt.Sprintf("#ifndef UbeArray_%s_DEFINED\n#define UbeArray_%s_DEFINED", m, m))
		g.emit(fmt.Sprintf("typedef struct { %s data[%s]; } UbeArray_%s;", parts[0], parts[1], m))
		g.emit("#endif")
	}
	for _, m := range g.optionKeys {
		ct := g.options[m]
		g.emit(fmt.Sprintf("#ifndef UbeOption_%s_DEFINED\n#define UbeOption_%s_DEFINED", m, m))
		g.emit(fmt.Sprintf("typedef struct { bool has_val; %s val; } UbeOption_%s;", ct, m))
		g.emit("#endif")
	}
	for _, m := range g.resultKeys {
		pair := g.results[m]
		parts := strings.Split(pair, "|")
		g.emit(fmt.Sprintf("#ifndef UbeResult_%s_DEFINED\n#define UbeResult_%s_DEFINED", m, m))
		g.emit(fmt.Sprintf("typedef struct { bool ok; union { %s val; %s err; }; } UbeResult_%s;", parts[0], parts[1], m))
		g.emit("#endif")
	}
	for _, m := range g.poolKeys {
		ct := g.pools[m]
		g.emit(fmt.Sprintf("DEFINE_UBE_POOL(%s, %s)", ct, m))
	}

	for _, decl := range g.Program.Decls {
		if tr, ok := decl.(*TraitDecl); ok {
			g.emit(fmt.Sprintf("struct %s_VTable {", tr.Name))
			for _, method := range tr.Methods {
				params := []string{"void* self"}
				for _, p := range method.Params {
					params = append(params, fmt.Sprintf("%s %s", g.toCType(p.Type), p.Name))
				}
				g.emit(fmt.Sprintf("    %s (*%s)(%s);", g.toCType(method.RetType), method.Name, strings.Join(params, ", ")))
			}
			g.emit("};")
			g.emit("")
		}
	}

	for _, decl := range g.Program.Decls {
		if fn, ok := decl.(*FnDecl); ok {
			name := fn.Name
			params := make([]string, 0, len(fn.Params)+1)
			if fn.Receiver != nil {
				name = fmt.Sprintf("%s_%s", fn.Receiver.Type, fn.Name)
				recCT := g.toCType(fn.Receiver.Type)
				if fn.Receiver.IsMut {
					recCT += "*"
					g.receiverMutParams[name] = true
				}
				params = append(params, recCT)
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
			if fn.Name == "puts" {
				continue
			}
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

	for _, key := range g.traitVtableKeys {
		info := g.traitVtables[key]
		trait := g.traitDecls[info.Trait]
		if trait == nil {
			continue
		}
		concreteCT := g.toCType(info.Concrete)
		concreteName := info.Concrete.String()
		var entries []string
		for _, method := range trait.Methods {
			wrapper := fmt.Sprintf("%s_%s", key, method.Name)
			params := []string{"void* self"}
			callArgs := []string{fmt.Sprintf("*(%s*)self", concreteCT)}
			for _, p := range method.Params {
				params = append(params, fmt.Sprintf("%s %s", g.toCType(p.Type), p.Name))
				callArgs = append(callArgs, p.Name)
			}
			call := fmt.Sprintf("%s_%s(%s)", concreteName, method.Name, strings.Join(callArgs, ", "))
			if g.toCType(method.RetType) == "void" {
				g.emit(fmt.Sprintf("static void %s(%s) { %s; }", wrapper, strings.Join(params, ", "), call))
			} else {
				g.emit(fmt.Sprintf("static %s %s(%s) { return %s; }", g.toCType(method.RetType), wrapper, strings.Join(params, ", "), call))
			}
			entries = append(entries, fmt.Sprintf(".%s = %s", method.Name, wrapper))
		}
		if len(entries) == 0 {
			entries = append(entries, "0")
		}
		g.emit(fmt.Sprintf("static const %s_VTable %s_vtable = { %s };", info.Trait, key, strings.Join(entries, ", ")))
	}

	for _, wrapper := range g.spawnWrappers {
		g.buf.WriteString(wrapper)
	}
	g.emit("")

	for _, m := range g.sliceKeys {
		ct := g.slices[m]
		g.emit(fmt.Sprintf("static inline UbeSlice_%s ube_slice_%s_new() { UbeSlice_%s s = {0}; return s; }", m, m, m))
		g.emit(fmt.Sprintf("static inline void ube_slice_%s_push(UbeSlice_%s* s, %s val) { if (s->len == s->cap) { if (s->cap > SIZE_MAX / 2) ube_panic_capacity((int64_t)s->cap); size_t cap = s->cap ? s->cap * 2 : 4; if (cap > SIZE_MAX / sizeof(%s)) ube_panic_capacity((int64_t)cap); %s* data = (%s*)ube_bump_alloc(cap * sizeof(%s)); if (s->len) memcpy(data, s->data, s->len * sizeof(%s)); s->data = data; s->cap = cap; } s->data[s->len++] = val; }", m, m, ct, ct, ct, ct, ct, ct))
		g.emit(fmt.Sprintf("static inline UbeOption_%s ube_slice_%s_pop(UbeSlice_%s* s) { UbeOption_%s opt = { .has_val = false }; if (s->len) { opt.has_val = true; opt.val = s->data[--s->len]; } return opt; }", m, m, m, m))
	}

	g.emit("")
	g.buf.WriteString(body)
	return g.buf.String()
}
