package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

func isRValue(expr Expr) bool {
	switch expr.(type) {
	case *CallExpr, *ArrayInitExpr, *SliceInitExpr, *StructInitExpr, *BinaryExpr, *UnaryExpr, *TryExpr, *OrElseExpr:
		return true
	}
	return false
}

func (g *Generator) genExpr(expr Expr) string {
	switch e := expr.(type) {
	case *UnsafeExpr:
		return fmt.Sprintf("({ __auto_type __ube_unsafe_value = %s; __ube_unsafe_value; })", g.genExpr(e.Value))

	case *TraitCoerceExpr:
		concrete := g.toCType(e.Concrete)
		vtable := g.registerTraitVtable(e.Trait, e.Concrete)
		tmp := g.newTmp()
		return fmt.Sprintf("({ %s* %s = (%s*)ube_bump_alloc(sizeof(%s)); *%s = %s; (%s){ .self = %s, .vtable = &%s }; })",
			concrete, tmp, concrete, concrete, tmp, g.genExpr(e.Value), e.Trait, tmp, vtable)

	case *LiteralExpr:
		if e.Type.String() == "string" {
			return fmt.Sprintf("ube_str_lit(%s)", strconv.Quote(e.Val))
		}
		if e.Type.String() == "bool" {
			return fmt.Sprintf("((bool)%s)", e.Val)
		}
		return e.Val

	case *InterpolatedStringExpr:
		sb := g.newTmp()
		calls := []string{fmt.Sprintf("UbeStringBuilder %s = ube_sb_new()", sb)}
		for _, part := range e.Parts {
			if lit, ok := part.(*LiteralExpr); ok && lit.Type.String() == "string" {
				calls = append(calls, fmt.Sprintf("ube_sb_append_cstr(&%s, %s)", sb, strconv.Quote(lit.Val)))
			} else {
				calls = append(calls, fmt.Sprintf("ube_sb_append_any(&%s, (%s))", sb, g.genExpr(part)))
			}
		}
		calls = append(calls, fmt.Sprintf("ube_sb_to_string(&%s)", sb))
		return fmt.Sprintf("({ %s; })", strings.Join(calls, "; "))

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
		if mem, ok := e.Callee.(*MemberExpr); ok && mem.Member == "recv" {
			elemType := e.PoolElem
			if elemType == nil {
				if ch, ok := mem.TargetType.(ChanType); ok {
					elemType = ch.Elem
				}
			}
			if elemType != nil {
				tmp := g.newTmp()
				ct := g.toCType(elemType)
				return fmt.Sprintf("({ %s %s = {0}; (void)ube_raw_chan_recv(%s, &%s); %s; })", ct, tmp, g.genExpr(mem.Target), tmp, tmp)
			}
		}
		if id, ok := e.Callee.(*IdentExpr); ok && (id.Name == "println" || id.Name == "print") {
			if len(e.Args) == 0 {
				if id.Name == "println" {
					return `puts("")`
				}
				return "((void)0)"
			}
			if len(e.Args) == 1 {
				if id.Name == "println" {
					return fmt.Sprintf("UBE_PRINTLN(%s)", g.genExpr(e.Args[0]))
				}
				return fmt.Sprintf("UBE_PRINT(%s)", g.genExpr(e.Args[0]))
			}
			calls := make([]string, 0, len(e.Args)*2)
			for i, arg := range e.Args {
				if i > 0 {
					calls = append(calls, `fputs(" ", stdout)`)
				}
				calls = append(calls, fmt.Sprintf("UBE_PRINT(%s)", g.genExpr(arg)))
			}
			if id.Name == "println" {
				calls = append(calls, `fputs("\n", stdout)`)
			}
			return fmt.Sprintf("({ %s; })", strings.Join(calls, "; "))
		}
		if id, ok := e.Callee.(*IdentExpr); ok && id.Name == "println_str" && len(e.Args) == 1 {
			return fmt.Sprintf("UBE_PRINTLN(%s)", g.genExpr(e.Args[0]))
		}
		if id, ok := e.Callee.(*IdentExpr); ok && id.Name == "format" && len(e.Args) >= 1 {
			if lit, isLit := e.Args[0].(*LiteralExpr); isLit && lit.Type.String() == "string" {
				sb := g.newTmp()
				calls := []string{fmt.Sprintf("UbeStringBuilder %s = ube_sb_new()", sb)}
				parts := strings.Split(lit.Val, "{}")
				for i, part := range parts {
					if len(part) > 0 {
						calls = append(calls, fmt.Sprintf("ube_sb_append_cstr(&%s, %s)", sb, strconv.Quote(part)))
					}
					if i < len(e.Args)-1 {
						calls = append(calls, fmt.Sprintf("ube_sb_append_any(&%s, (%s))", sb, g.genExpr(e.Args[i+1])))
					}
				}
				calls = append(calls, fmt.Sprintf("ube_sb_to_string(&%s)", sb))
				return fmt.Sprintf("({ %s; })", strings.Join(calls, "; "))
			}
		}
		if id, ok := e.Callee.(*IdentExpr); ok {
			builtins := map[string]string{
				"time_now_ms": "ube_time_now_ms", "sleep_ms": "ube_sleep_ms", "yield": "ube_yield",
				"exit_process": "ube_exit_process", "int_to_str": "ube_int_to_str", "str_to_int": "ube_str_to_int",
				"read_file": "ube_read_file", "write_file": "ube_write_file", "os_args": "ube_os_args",
				"str_contains": "ube_str_contains", "str_starts_with": "ube_str_starts_with", "str_ends_with": "ube_str_ends_with",
				"str_trim": "ube_str_trim", "str_index_of": "ube_str_index_of", "str_split": "ube_str_split",
				"env_get": "ube_env_get",
			}
			if fn, ok := builtins[id.Name]; ok {
				args := make([]string, len(e.Args))
				for i, arg := range e.Args {
					args[i] = g.genExpr(arg)
				}
				return fmt.Sprintf("%s(%s)", fn, strings.Join(args, ", "))
			}
		}
		if mem, ok := e.Callee.(*MemberExpr); ok {
			if e.TraitName != "" {
				tmp := g.newTmp()
				args := []string{tmp + ".self"}
				for _, arg := range e.Args {
					args = append(args, g.genExpr(arg))
				}
				call := fmt.Sprintf("%s.vtable->%s(%s)", tmp, mem.Member, strings.Join(args, ", "))
				return fmt.Sprintf("({ %s %s = %s; %s; })", e.TraitName, tmp, g.genExpr(mem.Target), call)
			}
			if id, ok := mem.Target.(*IdentExpr); ok && id.Name == "StringBuilder" && mem.Member == "new" {
				if len(e.Args) == 0 {
					return "ube_sb_new()"
				}
				return fmt.Sprintf("ube_sb_new_cap((size_t)(%s))", g.genExpr(e.Args[0]))
			}
			if _, isSB := mem.TargetType.(StringBuilderType); isSB {
				target := fmt.Sprintf("&(%s)", g.genExpr(mem.Target))
				switch mem.Member {
				case "append_str":
					return fmt.Sprintf("ube_sb_append_str(%s, %s)", target, g.genExpr(e.Args[0]))
				case "append_char":
					return fmt.Sprintf("ube_sb_append_char(%s, (char)(%s))", target, g.genExpr(e.Args[0]))
				case "append_i64":
					return fmt.Sprintf("ube_sb_append_i64(%s, (int64_t)(%s))", target, g.genExpr(e.Args[0]))
				case "append_f64":
					return fmt.Sprintf("ube_sb_append_f64(%s, (double)(%s))", target, g.genExpr(e.Args[0]))
				case "append_bool":
					return fmt.Sprintf("ube_sb_append_bool(%s, (bool)(%s))", target, g.genExpr(e.Args[0]))
				case "to_string":
					return fmt.Sprintf("ube_sb_to_string(%s)", target)
				case "len":
					return fmt.Sprintf("((int32_t)(%s)->len)", target)
				}
			}
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
				m := mangleTypeName(gen.ElemType.String())
				g.toCType(PoolType{Elem: gen.ElemType})
				return fmt.Sprintf("Pool_%s_new(%s)", m, g.genExpr(e.Args[0]))
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
			if mem.Member == "push" {
				sliceType := ""
				if sl, ok := mem.TargetType.(SliceType); ok {
					sliceType = g.sliceElemMangle(sl.Elem)
				} else if id, ok := mem.Target.(*IdentExpr); ok && g.sliceVars[id.Name] != "" {
					sliceType = g.sliceVars[id.Name]
				} else {
					sliceType = mangleTypeName(g.genExpr(mem.Target))
				}
				return fmt.Sprintf("ube_slice_%s_push(&%s, %s)",
					sliceType,
					g.genExpr(mem.Target),
					g.genExpr(e.Args[0]))
			}
			if mem.Member == "pop" {
				sliceType := ""
				if sl, ok := mem.TargetType.(SliceType); ok {
					sliceType = g.sliceElemMangle(sl.Elem)
					g.toCType(OptionalType{Inner: sl.Elem})
				} else if id, ok := mem.Target.(*IdentExpr); ok && g.sliceVars[id.Name] != "" {
					sliceType = g.sliceVars[id.Name]
				} else {
					sliceType = mangleTypeName(g.genExpr(mem.Target))
				}
				return fmt.Sprintf("ube_slice_%s_pop(&%s)", sliceType, g.genExpr(mem.Target))
			}
			if mem.Member == "send" {
				return fmt.Sprintf("({ __auto_type __val = %s; ube_raw_chan_send(%s, &__val); })", g.genExpr(e.Args[0]), g.genExpr(mem.Target))
			}
			if mem.Member == "close" {
				return fmt.Sprintf("ube_raw_chan_close(%s)", g.genExpr(mem.Target))
			}
			if mem.Member == "spawn" {
				pName := mangleTypeName(e.PoolElem.String())
				return fmt.Sprintf("Pool_%s_spawn(%s, %s)", pName, g.genExpr(mem.Target), g.genExpr(e.Args[0]))
			}
			if mem.Member == "get" {
				pName := mangleTypeName(e.PoolElem.String())
				return fmt.Sprintf("Pool_%s_get(%s, %s)", pName, g.genExpr(mem.Target), g.genExpr(e.Args[0]))
			}
			if mem.Member == "despawn" {
				pName := mangleTypeName(e.PoolElem.String())
				return fmt.Sprintf("Pool_%s_despawn(%s, %s)", pName, g.genExpr(mem.Target), g.genExpr(e.Args[0]))
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
		return fmt.Sprintf("(%s).%s", g.genExpr(e.Target), e.Member)

	case *IndexExpr:
		file := strconv.Quote(e.At.File)
		switch targetType := e.TargetType.(type) {
		case StringType:
			return fmt.Sprintf("UBE_STR_INDEX((%s), (%s), %s, %d)", g.genExpr(e.Target), g.genExpr(e.Index), file, e.At.Line)
		case ArrayType:
			targetExpr := g.genExpr(e.Target)
			if isRValue(e.Target) {
				return fmt.Sprintf("({ __auto_type _ube_arr = %s; (*UBE_ARRAY_PTR((_ube_arr), (%s), %d, %s, %d)); })", targetExpr, g.genExpr(e.Index), targetType.Size, file, e.At.Line)
			}
			return fmt.Sprintf("(*UBE_ARRAY_PTR((%s), (%s), %d, %s, %d))", targetExpr, g.genExpr(e.Index), targetType.Size, file, e.At.Line)
		default:
			return fmt.Sprintf("(*UBE_SLICE_PTR((%s), (%s), %s, %d))", g.genExpr(e.Target), g.genExpr(e.Index), file, e.At.Line)
		}

	case *SliceSubExpr:
		if _, isStr := e.TargetType.(StringType); isStr {
			return fmt.Sprintf("ube_str_slice(%s, %s, %s)", g.genExpr(e.Target), g.genExpr(e.Start), g.genExpr(e.End))
		}
		var elemType UbeType
		isArr := false
		arrLen := 0
		if sl, ok := e.TargetType.(SliceType); ok {
			elemType = sl.Elem
		} else if arr, ok := e.TargetType.(ArrayType); ok {
			elemType = arr.Elem
			isArr = true
			arrLen = arr.Size
		}
		if elemType != nil {
			m := g.sliceElemMangle(elemType)
			targetExpr := g.genExpr(e.Target)
			if isArr {
				if isRValue(e.Target) {
					return fmt.Sprintf("({ __auto_type _t_arr = %s; int32_t _s = %s; int32_t _e = %s; size_t _tlen = %d; size_t _tcap = %d; if (_s < 0) _s = 0; if ((size_t)_s > _tlen) _s = (int32_t)_tlen; if (_e < _s) _e = _s; if ((size_t)_e > _tlen) _e = (int32_t)_tlen; (UbeSlice_%s){ .data = _t_arr.data + _s, .len = (size_t)(_e - _s), .cap = (_tcap >= (size_t)_s) ? _tcap - (size_t)_s : 0 }; })",
						targetExpr, g.genExpr(e.Start), g.genExpr(e.End), arrLen, arrLen, m)
				}
				return fmt.Sprintf("({ __auto_type* _t = &(%s); int32_t _s = %s; int32_t _e = %s; size_t _tlen = %d; size_t _tcap = %d; if (_s < 0) _s = 0; if ((size_t)_s > _tlen) _s = (int32_t)_tlen; if (_e < _s) _e = _s; if ((size_t)_e > _tlen) _e = (int32_t)_tlen; (UbeSlice_%s){ .data = _t->data + _s, .len = (size_t)(_e - _s), .cap = (_tcap >= (size_t)_s) ? _tcap - (size_t)_s : 0 }; })",
					targetExpr, g.genExpr(e.Start), g.genExpr(e.End), arrLen, arrLen, m)
			}
			return fmt.Sprintf("({ __auto_type _t = %s; int32_t _s = %s; int32_t _e = %s; size_t _tlen = _t.len; size_t _tcap = _t.cap; if (_s < 0) _s = 0; if ((size_t)_s > _tlen) _s = (int32_t)_tlen; if (_e < _s) _e = _s; if ((size_t)_e > _tlen) _e = (int32_t)_tlen; (UbeSlice_%s){ .data = _t.data + _s, .len = (size_t)(_e - _s), .cap = (_tcap >= (size_t)_s) ? _tcap - (size_t)_s : 0 }; })",
				targetExpr, g.genExpr(e.Start), g.genExpr(e.End), m)
		}
		return fmt.Sprintf("ube_str_slice(%s, %s, %s)", g.genExpr(e.Target), g.genExpr(e.Start), g.genExpr(e.End))

	case *SliceInitExpr:
		m := g.sliceElemMangle(e.Elem)
		tmp := g.newTmp()
		calls := []string{fmt.Sprintf("UbeSlice_%s %s = ube_slice_%s_new()", m, tmp, m)}
		for _, el := range e.Elements {
			calls = append(calls, fmt.Sprintf("ube_slice_%s_push(&%s, %s)", m, tmp, g.genExpr(el)))
		}
		calls = append(calls, tmp)
		return fmt.Sprintf("({ %s; })", strings.Join(calls, "; "))

	case *ArrayInitExpr:
		var vals []string
		for _, el := range e.Elements {
			vals = append(vals, g.genExpr(el))
		}
		return fmt.Sprintf("(%s){ .data = { %s } }", g.toCType(ArrayType{Elem: e.Elem, Size: e.Size}), strings.Join(vals, ", "))

	case *GenericTypeExpr:
		return e.BaseType

	case *StructInitExpr:
		if len(e.Fields) == 0 {
			return fmt.Sprintf("(%s){0}", e.Name)
		}
		var inits []string
		for _, f := range e.Fields {
			inits = append(inits, fmt.Sprintf(".%s = %s", f.Name, g.genExpr(f.Val)))
		}
		return fmt.Sprintf("(%s){ %s }", e.Name, strings.Join(inits, ", "))

	case *OrElseExpr:
		if _, isOptional := e.PrimaryType.(OptionalType); isOptional {
			return fmt.Sprintf("({ __auto_type _t = %s; _t.has_val ? _t.val : (%s); })", g.genExpr(e.Primary), g.genExpr(e.Fallback))
		}
		return fmt.Sprintf("({ __auto_type _t = %s; _t.ok ? _t.val : (%s); })", g.genExpr(e.Primary), g.genExpr(e.Fallback))

	case *TryExpr:
		cleanup := g.returnCleanupCode()
		if resRet, ok := g.curFnRet.(ResultType); ok {
			return fmt.Sprintf("({ __auto_type _r = %s; if (!_r.ok) { %sreturn (%s){ .ok = false, .err = _r.err }; } _r.val; })", g.genExpr(e.Operand), cleanup, g.toCType(resRet))
		}
		return fmt.Sprintf("({ __auto_type _r = %s; if (!_r.ok) { %sfprintf(stderr, \"[Ube Panic]: Unhandled error at %s:%d\\n\"); exit(101); } _r.val; })", g.genExpr(e.Operand), cleanup, e.At.File, e.At.Line)
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
