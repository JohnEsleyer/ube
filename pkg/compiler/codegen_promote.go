package compiler

import (
	"fmt"
	"strings"
)

func (g *Generator) findStruct(name string) *StructDecl {
	if g.Program == nil {
		return nil
	}
	for _, decl := range g.Program.Decls {
		if s, ok := decl.(*StructDecl); ok && s.Name == name {
			return s
		}
	}
	return nil
}

func (g *Generator) typeNeedsDeepPromoteSeen(t UbeType, seen map[string]bool) bool {
	if t == nil {
		return false
	}
	switch v := t.(type) {
	case SliceType, StringType:
		return true
	case TupleType:
		for _, elem := range v.Elems {
			if g.typeNeedsDeepPromoteSeen(elem, seen) {
				return true
			}
		}
		return false
	case OptionalType:
		return g.typeNeedsDeepPromoteSeen(v.Inner, seen)
	case ResultType:
		return g.typeNeedsDeepPromoteSeen(v.Ok, seen) || g.typeNeedsDeepPromoteSeen(v.Err, seen)
	case ArrayType:
		return g.typeNeedsDeepPromoteSeen(v.Elem, seen)
	case CustomType:
		if seen[v.Name] {
			return false
		}
		seen[v.Name] = true
		sDecl := g.findStruct(v.Name)
		if sDecl != nil {
			for _, f := range sDecl.Fields {
				if g.typeNeedsDeepPromoteSeen(f.Type, seen) {
					return true
				}
			}
		}
		return false
	default:
		return false
	}
}

func (g *Generator) typeNeedsDeepPromote(t UbeType) bool {
	return g.typeNeedsDeepPromoteSeen(t, make(map[string]bool))
}

func (g *Generator) genPromoteValue(t UbeType, valCode string) string {
	if t == nil {
		return valCode
	}
	switch v := t.(type) {
	case SliceType:
		m := g.sliceElemMangle(v.Elem)
		ct := g.toCType(v.Elem)
		sliceCT := fmt.Sprintf("UbeSlice_%s", m)
		sl := g.newTmp()
		nd := g.newTmp()
		if g.typeNeedsDeepPromote(v.Elem) {
			idx := g.newTmp()
			elemPromote := g.genPromoteValue(v.Elem, fmt.Sprintf("%s.data[%s]", sl, idx))
			return fmt.Sprintf("({ %s %s = %s; if (%s.data && %s.len > 0) { %s* %s = (%s*)ube_bump_alloc_parent(%s.len * sizeof(%s)); for (size_t %s = 0; %s < %s.len; %s++) { %s[%s] = %s; } %s.data = %s; %s.cap = %s.len; } %s; })",
				sliceCT, sl, valCode, sl, sl, ct, nd, ct, sl, ct, idx, idx, sl, idx, nd, idx, elemPromote, sl, nd, sl, sl, sl)
		}
		return fmt.Sprintf("({ %s %s = %s; if (%s.data && %s.len > 0) { %s* %s = (%s*)ube_bump_alloc_parent(%s.len * sizeof(%s)); memcpy(%s, %s.data, %s.len * sizeof(%s)); %s.data = %s; %s.cap = %s.len; } %s; })",
			sliceCT, sl, valCode, sl, sl, ct, nd, ct, sl, ct, nd, sl, sl, ct, sl, nd, sl, sl, sl)

	case StringType:
		return fmt.Sprintf("ube_str_promote(%s)", valCode)

	case CustomType:
		sDecl := g.findStruct(v.Name)
		if sDecl == nil {
			return valCode
		}
		st := g.newTmp()
		var fieldPromotes []string
		for _, f := range sDecl.Fields {
			if g.typeNeedsDeepPromote(f.Type) {
				fieldVal := fmt.Sprintf("%s.%s", st, f.Name)
				promoted := g.genPromoteValue(f.Type, fieldVal)
				fieldPromotes = append(fieldPromotes, fmt.Sprintf("%s.%s = %s;", st, f.Name, promoted))
			}
		}
		if len(fieldPromotes) == 0 {
			return valCode
		}
		return fmt.Sprintf("({ %s %s = %s; %s %s; })",
			v.Name, st, valCode, strings.Join(fieldPromotes, " "), st)

	case OptionalType:
		if g.typeNeedsDeepPromote(v.Inner) {
			optCT := g.toCType(v)
			opt := g.newTmp()
			promotedVal := g.genPromoteValue(v.Inner, fmt.Sprintf("%s.val", opt))
			return fmt.Sprintf("({ %s %s = %s; if (%s.has_val) { %s.val = %s; } %s; })",
				optCT, opt, valCode, opt, opt, promotedVal, opt)
		}
		return valCode

	case ResultType:
		if g.typeNeedsDeepPromote(v.Ok) || g.typeNeedsDeepPromote(v.Err) {
			resCT := g.toCType(v)
			res := g.newTmp()
			promotedOk := g.genPromoteValue(v.Ok, fmt.Sprintf("%s.val", res))
			promotedErr := g.genPromoteValue(v.Err, fmt.Sprintf("%s.err", res))
			return fmt.Sprintf("({ %s %s = %s; if (%s.ok) { %s.val = %s; } else { %s.err = %s; } %s; })",
				resCT, res, valCode, res, res, promotedOk, res, promotedErr, res)
		}
		return valCode

	default:
		return valCode
	}
}

func (g *Generator) genArenaExpr(e *ArenaExpr) string {
	frameTmp := g.newTmp()
	g.scopeMarks = append(g.scopeMarks, frameTmp)
	defer func() {
		g.scopeMarks = g.scopeMarks[:len(g.scopeMarks)-1]
	}()

	var sb strings.Builder
	sb.WriteString("({ ")
	sb.WriteString(fmt.Sprintf("UbeArenaLevel %s; ube_arena_enter(&%s); ", frameTmp, frameTmp))

	isVoid := false
	if p, ok := e.Type.(PrimitiveType); ok && p.Name == "void" {
		isVoid = true
	}
	if e.Type == nil {
		isVoid = true
	}

	var resTmp string
	if !isVoid {
		resTmp = g.newTmp()
		resCT := g.toCType(e.Type)
		sb.WriteString(fmt.Sprintf("%s %s; ", resCT, resTmp))
	}

	savedBuf := g.buf
	g.buf = strings.Builder{}

	stmts := e.Body.Stmts
	for i, st := range stmts {
		if i == len(stmts)-1 && !isVoid {
			if es, ok := st.(*ExprStmt); ok {
				valCode := g.genExpr(es.Expr)
				g.emit(fmt.Sprintf("%s = %s;", resTmp, valCode))
				continue
			}
		}
		g.genStmt(st)
	}

	innerCode := g.buf.String()
	g.buf = savedBuf

	sb.WriteString(innerCode)
	sb.WriteString(fmt.Sprintf("ube_arena_exit(&%s); ", frameTmp))
	if !isVoid {
		sb.WriteString(resTmp + "; ")
	}
	sb.WriteString("})")
	return sb.String()
}
