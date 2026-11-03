package compiler

import (
	"fmt"
	"strings"
)

func printableType(t UbeType) bool {
	switch v := t.(type) {
	case PrimitiveType:
		switch v.Name {
		case "i32", "u32", "u8", "i64", "u64", "f32", "f64", "bool":
			return true
		}
	case StringType, StringBuilderType:
		return true
	}
	return false
}

func integerType(t UbeType) bool {
	if p, ok := t.(PrimitiveType); ok {
		switch p.Name {
		case "i32", "u32", "u8", "i64", "u64":
			return true
		}
	}
	return false
}

// lookupModuleMember resolves `alias.member` to the internal flattened name
// used for an imported module. Unaliased imports map to the empty prefix, so
// `wasi.fd_write(...)` and `fd_write(...)` resolve to the same declaration.
func (tc *Typechecker) lookupModuleMember(alias, member string) (string, FuncSignature, bool) {
	prefix, isModule := tc.ModuleAliases[alias]
	if !isModule {
		return "", FuncSignature{}, false
	}
	key := prefix + member
	sig, ok := tc.Global.Funcs[key]
	return key, sig, ok
}

func (tc *Typechecker) inferExpr(expr Expr, env *TypeEnvironment) (UbeType, error) {
	switch e := expr.(type) {
	case *PromoteExpr:
		t, err := tc.inferExpr(e.Value, env)
		if err != nil {
			return nil, err
		}
		e.Type = t
		return t, nil

	case *ArenaExpr:
		scopeEnv := childTypeEnvironment(env)
		scopeEnv.ScopeDepth = env.ScopeDepth + 1
		for _, stmt := range e.Body.Stmts {
			if err := tc.checkStmt(stmt, scopeEnv); err != nil {
				return nil, err
			}
		}
		if len(e.Body.Stmts) == 0 {
			e.Type = PrimitiveType{Name: "void"}
			return e.Type, nil
		}
		lastStmt := e.Body.Stmts[len(e.Body.Stmts)-1]
		if es, ok := lastStmt.(*ExprStmt); ok {
			t, err := tc.inferExpr(es.Expr, scopeEnv)
			if err != nil {
				return nil, err
			}
			// Zero-leak arena sandbox invariant: arena-backed references may
			// only escape through an explicit promote(...) terminal value.
			if tc.hasLifetime(t) {
				if _, isPromote := es.Expr.(*PromoteExpr); !isPromote {
					return nil, fmt.Errorf("[%s] arena expression cannot return unpromoted value of type '%s'; use promote(...) to retain it", e.At, t)
				}
			}
			e.Type = t
			return t, nil
		}
		e.Type = PrimitiveType{Name: "void"}
		return e.Type, nil

	case *TraitCoerceExpr:
		_, err := tc.inferExpr(e.Value, env)
		if err != nil {
			return nil, err
		}
		return TraitType{Name: e.Trait}, nil

	case *ArrayToSliceExpr:
		_, err := tc.inferExpr(e.Value, env)
		if err != nil {
			return nil, err
		}
		return SliceType{Elem: e.ArrayType.Elem}, nil

	case *RangeExpr:
		startT, err := tc.inferExpr(e.Start, env)
		if err != nil {
			return nil, err
		}
		endT, err := tc.inferExpr(e.End, env)
		if err != nil {
			return nil, err
		}
		if !integerType(startT) {
			return nil, fmt.Errorf("[%s] range start must be an integer, got '%s'", e.At, startT)
		}
		if !integerType(endT) {
			return nil, fmt.Errorf("[%s] range end must be an integer, got '%s'", e.At, endT)
		}
		return RangeType{Elem: startT}, nil

	case *TupleExpr:
		elems := make([]UbeType, len(e.Elems))
		for i, elem := range e.Elems {
			t, err := tc.inferExpr(elem, env)
			if err != nil {
				return nil, err
			}
			elems[i] = t
		}
		e.Types = elems
		return TupleType{Elems: elems}, nil

	case *InterpolatedStringExpr:
		for _, part := range e.Parts {
			pType, err := tc.inferExpr(part, env)
			if err != nil {
				return nil, err
			}
			if !printableType(pType) && pType.String() != "string" {
				return nil, fmt.Errorf("[%s] cannot interpolate expression of type '%s'", part.Loc(), pType)
			}
		}
		return StringType{}, nil

	case *LiteralExpr:
		return e.Type, nil

	case *IdentExpr:
		if e.Name == "StringBuilder" {
			return StringBuilderType{}, nil
		}
		if v, ok := env.LookupVar(e.Name); ok {
			if v.Moved {
				return nil, movedValueError(e.At, e.Name)
			}
			return v.Type, nil
		}
		if _, ok := tc.Global.Enums[e.Name]; ok {
			return CustomType{Name: e.Name}, nil
		}
		if _, ok := tc.Global.Traits[e.Name]; ok {
			return TraitType{Name: e.Name}, nil
		}
		if strings.HasPrefix(e.Name, "Chan[") && strings.HasSuffix(e.Name, "]") {
			elemName := e.Name[5 : len(e.Name)-1]
			elemT := tc.typeFromName(elemName)
			chT := ChanType{Elem: elemT}
			if err := tc.validateType(chT, e.At); err != nil {
				return nil, err
			}
			return chT, nil
		}
		if strings.HasPrefix(e.Name, "Pool[") && strings.HasSuffix(e.Name, "]") {
			elemName := e.Name[5 : len(e.Name)-1]
			elemT := tc.typeFromName(elemName)
			pT := PoolType{Elem: elemT}
			return pT, nil
		}
		return nil, fmt.Errorf("[%s] Undefined identifier '%s'", e.At, e.Name)

	case *ErrorExpr:
		return tc.inferExpr(e.Arg, env)

	case *BinaryExpr:
		lT, err := tc.inferExpr(e.Left, env)
		if err != nil {
			return nil, err
		}
		rT, err := tc.inferExpr(e.Right, env)
		if err != nil {
			return nil, err
		}
		e.OperandType = lT
		switch e.Op {
		case "&&", "||":
			if err := tc.assertCompat(PrimitiveType{Name: "bool"}, lT, e.At); err != nil {
				return nil, err
			}
			if err := tc.assertCompat(PrimitiveType{Name: "bool"}, rT, e.At); err != nil {
				return nil, err
			}
			e.Type = PrimitiveType{Name: "bool"}
			return e.Type, nil
		case "==", "!=":
			if _, isCustom := lT.(CustomType); isCustom {
				return nil, fmt.Errorf("[%s] type '%s' does not support operator '%s'", e.At, lT, e.Op)
			}
			if _, isArr := lT.(ArrayType); isArr {
				return nil, fmt.Errorf("[%s] type '%s' does not support operator '%s'", e.At, lT, e.Op)
			}
			if _, isSlice := lT.(SliceType); isSlice {
				return nil, fmt.Errorf("[%s] type '%s' does not support operator '%s'", e.At, lT, e.Op)
			}
			e.OperandType = lT
			e.Type = PrimitiveType{Name: "bool"}
			return e.Type, tc.assertCompat(lT, rT, e.At)
		case "<", "<=", ">", ">=":
			if !integerType(lT) && lT.String() != "f32" && lT.String() != "f64" {
				return nil, fmt.Errorf("[%s] operator '%s' is only supported on numeric types, got '%s'", e.At, e.Op, lT)
			}
			e.OperandType = lT
			e.Type = PrimitiveType{Name: "bool"}
			return e.Type, tc.assertCompat(lT, rT, e.At)
		case "+":
			if lT.String() == "string" || rT.String() == "string" {
				e.Type = StringType{}
				return e.Type, tc.assertCompat(lT, rT, e.At)
			}
			if !integerType(lT) && lT.String() != "f32" && lT.String() != "f64" {
				return nil, fmt.Errorf("[%s] operator '+' is only supported on numeric types or string, got '%s'", e.At, lT)
			}
			e.Type = lT
			return lT, tc.assertCompat(lT, rT, e.At)
		case "&", "|", "^", "<<", ">>":
			if !integerType(lT) {
				return nil, fmt.Errorf("[%s] bitwise operator '%s' requires integer operand, got '%s'", e.At, e.Op, lT)
			}
			if !integerType(rT) {
				return nil, fmt.Errorf("[%s] bitwise operator '%s' requires integer operand, got '%s'", e.At, e.Op, rT)
			}
			e.Type = lT
			return lT, nil
		default: // -, *, /, %
			if !integerType(lT) && lT.String() != "f32" && lT.String() != "f64" {
				return nil, fmt.Errorf("[%s] operator '%s' is only supported on numeric types, got '%s'", e.At, e.Op, lT)
			}
			e.Type = lT
			return lT, tc.assertCompat(lT, rT, e.At)
		}

	case *UnaryExpr:
		if e.Op == "mut" {
			return tc.inferExpr(e.Operand, env)
		}
		opndT, err := tc.inferExpr(e.Operand, env)
		if err != nil {
			return nil, err
		}
		if e.Op == "!" {
			if err := tc.assertCompat(PrimitiveType{Name: "bool"}, opndT, e.At); err != nil {
				return nil, fmt.Errorf("[%s] operator '!' expects bool, got '%s'", e.At, opndT)
			}
			return PrimitiveType{Name: "bool"}, nil
		}
		if e.Op == "~" {
			if !integerType(opndT) {
				return nil, fmt.Errorf("[%s] operator '~' expects integer type, got '%s'", e.At, opndT)
			}
			return opndT, nil
		}
		if e.Op == "-" {
			if !integerType(opndT) && opndT.String() != "f32" && opndT.String() != "f64" {
				return nil, fmt.Errorf("[%s] operator '-' expects numeric type, got '%s'", e.At, opndT)
			}
			return opndT, nil
		}
		if e.Op == "&" {
			return nil, fmt.Errorf("[%s] address-of operator '&' is forbidden: Ube is 100%% sandboxed WebAssembly", e.At)
		}
		if e.Op == "*" {
			return nil, fmt.Errorf("[%s] pointer dereference '*' is forbidden: Ube is 100%% sandboxed WebAssembly", e.At)
		}
		return opndT, nil

	case *CallExpr:
		// Wasm host imports declared with 'extern fn' are safe, sandboxed calls.
		// No unsafe wrapper is required or permitted anywhere in the language.

		// Safe string construction from a byte slice:
		//   string.from_bytes(bytes) -> string ! string
		if mem, ok := e.Callee.(*MemberExpr); ok {
			if id, ok := mem.Target.(*IdentExpr); ok && id.Name == "string" &&
				(mem.Member == "from_bytes" || mem.Member == "from_utf8") {
				if len(e.Args) != 1 {
					return nil, fmt.Errorf("[%s] string.%s expects 1 argument (got %d)", e.At, mem.Member, len(e.Args))
				}
				argT, err := tc.inferExpr(e.Args[0], env)
				if err != nil {
					return nil, err
				}
				sl, isSlice := argT.(SliceType)
				if !isSlice || sl.Elem.String() != "u8" {
					return nil, fmt.Errorf("[%s] string.%s expects '[]u8', got '%s'", e.At, mem.Member, argT)
				}
				return ResultType{Ok: StringType{}, Err: StringType{}}, nil
			}

			// Qualified module function call: math.min(a, b).
			if id, ok := mem.Target.(*IdentExpr); ok {
				if _, isVar := env.LookupVar(id.Name); !isVar {
					if _, sig, found := tc.lookupModuleMember(id.Name, mem.Member); found {
						if err := tc.checkCallArgs(e.Args, sig.Params, sig.Escaping, env, e.At); err != nil {
							return nil, err
						}
						return sig.RetType, nil
					}
				}
			}
		}

		if id, ok := e.Callee.(*IdentExpr); ok && id.Name == "format" {
			if len(e.Args) == 0 {
				return nil, fmt.Errorf("[%s] format expects at least a format string", e.At)
			}
			fmtT, err := tc.inferExpr(e.Args[0], env)
			if err != nil {
				return nil, err
			}
			if err := tc.assertCompat(StringType{}, fmtT, e.Args[0].Loc()); err != nil {
				return nil, err
			}
			for _, arg := range e.Args[1:] {
				argT, err := tc.inferExpr(arg, env)
				if err != nil {
					return nil, err
				}
				if !printableType(argT) && argT.String() != "string" {
					return nil, fmt.Errorf("[%s] format does not support type '%s'", arg.Loc(), argT)
				}
			}
			return StringType{}, nil
		}
		if id, ok := e.Callee.(*IdentExpr); ok && (id.Name == "time_now_ms" || id.Name == "time_now_ns") {
			if len(e.Args) != 0 {
				return nil, fmt.Errorf("[%s] %s expects 0 arguments", e.At, id.Name)
			}
			return PrimitiveType{Name: "i64"}, nil
		}
		if id, ok := e.Callee.(*IdentExpr); ok && (id.Name == "println" || id.Name == "print") {
			for _, arg := range e.Args {
				argT, err := tc.inferExpr(arg, env)
				if err != nil {
					return nil, err
				}
				if opt, isOpt := argT.(OptionalType); isOpt {
					argName := assignmentRoot(arg)
					if argName == "" {
						argName = "expr"
					}
					return nil, fmt.Errorf("[%s] cannot print optional type '%s' directly (value may be null)\n  hint: unwrap it with 'or_else <default>' or an 'if let' block:\n        let val = %s or_else <default>\n        if let val = %s { %s(val) }", arg.Loc(), opt, argName, argName, id.Name)
				}
				if !printableType(argT) {
					return nil, fmt.Errorf("[%s] %s does not support type '%s'", e.At, id.Name, argT)
				}
			}
			return PrimitiveType{Name: "void"}, nil
		}
		if id, ok := e.Callee.(*IdentExpr); ok && id.Name == "int_to_str" && len(e.Args) == 1 {
			argType, err := tc.inferExpr(e.Args[0], env)
			if err != nil {
				return nil, err
			}
			if !integerType(argType) {
				return nil, fmt.Errorf("[%s] int_to_str expects an integer, got '%s'", e.At, argType)
			}
			return StringType{}, nil
		}
		if mem, ok := e.Callee.(*MemberExpr); ok {
			if id, ok := mem.Target.(*IdentExpr); ok && id.Name == "StringBuilder" && mem.Member == "new" {
				if len(e.Args) > 1 {
					return nil, fmt.Errorf("[%s] StringBuilder.new takes at most 1 capacity argument", e.At)
				}
				if len(e.Args) == 1 {
					argT, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if !integerType(argT) {
						return nil, fmt.Errorf("[%s] StringBuilder capacity must be an integer", e.At)
					}
				}
				return StringBuilderType{}, nil
			}

			if id, ok := mem.Target.(*IdentExpr); ok {
				if variants, ok := tc.Global.Enums[id.Name]; ok {
					if meta, hasMeta := tc.Global.EnumMeta[id.Name]; hasMeta {
						if env.CurFile != "" && meta.File != "" && env.CurFile != meta.File && !meta.IsPub {
							return nil, fmt.Errorf("[%s] cannot access private enum '%s'", e.At, id.Name)
						}
					}
					for _, variant := range variants {
						if variant.Name == mem.Member {
							if len(e.Args) != len(variant.Types) {
								return nil, fmt.Errorf("[%s] enum variant '%s' expects %d arguments, got %d", e.At, mem.Member, len(variant.Types), len(e.Args))
							}
							for i, arg := range e.Args {
								actual, err := tc.inferExpr(arg, env)
								if err != nil {
									return nil, err
								}
								if err := tc.assertCompat(variant.Types[i], actual, arg.Loc()); err != nil {
									return nil, err
								}
							}
							return CustomType{Name: id.Name}, nil
						}
					}
					return nil, fmt.Errorf("[%s] enum '%s' has no variant '%s'", e.At, id.Name, mem.Member)
				}
			}
			tgtT, err := tc.inferExpr(mem.Target, env)
			if err != nil {
				return nil, err
			}
			mem.TargetType = tgtT

			// String built-in instance methods
			if _, isStr := tgtT.(StringType); isStr {
				e.ReceiverType = tgtT
				switch mem.Member {
				case "bytes", "as_bytes":
					if len(e.Args) != 0 {
						return nil, fmt.Errorf("[%s] %s expects 0 arguments", e.At, mem.Member)
					}
					return SliceType{Elem: PrimitiveType{Name: "u8"}}, nil
				case "trim":
					if len(e.Args) != 0 {
						return nil, fmt.Errorf("[%s] trim expects 0 arguments", e.At)
					}
					return StringType{}, nil
				case "starts_with":
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] starts_with expects 1 argument", e.At)
					}
					aT, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if err := tc.assertCompat(StringType{}, aT, e.Args[0].Loc()); err != nil {
						return nil, err
					}
					return PrimitiveType{Name: "bool"}, nil
				case "ends_with":
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] ends_with expects 1 argument", e.At)
					}
					aT, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if err := tc.assertCompat(StringType{}, aT, e.Args[0].Loc()); err != nil {
						return nil, err
					}
					return PrimitiveType{Name: "bool"}, nil
				case "index_of":
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] index_of expects 1 argument", e.At)
					}
					aT, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if err := tc.assertCompat(StringType{}, aT, e.Args[0].Loc()); err != nil {
						return nil, err
					}
					return PrimitiveType{Name: "i32"}, nil
				case "split":
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] split expects 1 delimiter argument", e.At)
					}
					aT, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if err := tc.assertCompat(StringType{}, aT, e.Args[0].Loc()); err != nil {
						return nil, err
					}
					return SliceType{Elem: StringType{}}, nil
				}
			}

			// StringBuilder instance methods
			if _, isSB := tgtT.(StringBuilderType); isSB {
				e.ReceiverType = tgtT
				switch mem.Member {
				case "append", "append_str":
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] %s expects 1 string argument", e.At, mem.Member)
					}
					aT, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if err := tc.assertCompat(StringType{}, aT, e.At); err != nil {
						return nil, err
					}
					return PrimitiveType{Name: "void"}, nil
				case "append_int", "append_i64":
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] %s expects 1 integer argument", e.At, mem.Member)
					}
					aT, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if !integerType(aT) {
						return nil, fmt.Errorf("[%s] %s expects an integer", e.At, mem.Member)
					}
					return PrimitiveType{Name: "void"}, nil
				case "append_char":
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] append_char expects 1 integer argument", e.At)
					}
					aT, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if !integerType(aT) {
						return nil, fmt.Errorf("[%s] append_char expects an integer", e.At)
					}
					return PrimitiveType{Name: "void"}, nil
				case "append_bool":
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] append_bool expects 1 bool argument", e.At)
					}
					aT, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if err := tc.assertCompat(PrimitiveType{Name: "bool"}, aT, e.At); err != nil {
						return nil, err
					}
					return PrimitiveType{Name: "void"}, nil
				case "to_string":
					if len(e.Args) != 0 {
						return nil, fmt.Errorf("[%s] to_string expects 0 arguments", e.At)
					}
					return StringType{}, nil
				default:
					return nil, fmt.Errorf("[%s] unknown StringBuilder method '%s'", e.At, mem.Member)
				}
			}

			// Trait dynamic method calls
			if tr, isTrait := tgtT.(TraitType); isTrait {
				e.ReceiverType = tgtT
				if meta, hasMeta := tc.Global.TraitMeta[tr.Name]; hasMeta {
					if env.CurFile != "" && meta.File != "" && env.CurFile != meta.File && !meta.IsPub {
						return nil, fmt.Errorf("[%s] cannot access private trait '%s'", e.At, tr.Name)
					}
				}
				if tDecl, ok := tc.Global.Traits[tr.Name]; ok {
					for _, m := range tDecl.Methods {
						if m.Name == mem.Member {
							if err := tc.checkCallArgs(e.Args, m.Params, nil, env, e.At); err != nil {
								return nil, err
							}
							return m.RetType, nil
						}
					}
					return nil, fmt.Errorf("[%s] trait '%s' has no method '%s'", e.At, tr.Name, mem.Member)
				}
			}

			if sl, ok := tgtT.(SliceType); ok && mem.Member == "push" {
				if len(e.Args) != 1 {
					return nil, fmt.Errorf("[%s] push expects 1 argument", e.At)
				}
				if root := assignmentRoot(mem.Target); root != "" {
					if info, found := env.LookupVar(root); found {
						if !info.IsMut {
							return nil, fmt.Errorf("[%s] cannot call 'push' on immutable slice '%s'; declare it with 'let mut'", e.At, root)
						}
						if env.ScopeDepth > info.Depth {
							return nil, fmt.Errorf("[%s] Safety Error: Outer slice mutation attempted within inner arena at line %d; use promote(...) to export data", e.At, e.At.Line)
						}
					}
				}
				argT, err := tc.inferExpr(e.Args[0], env)
				if err != nil {
					return nil, err
				}
				return PrimitiveType{Name: "void"}, tc.assertCompat(sl.Elem, argT, e.At)
			}
			if sl, ok := tgtT.(SliceType); ok && mem.Member == "pop" {
				if len(e.Args) != 0 {
					return nil, fmt.Errorf("[%s] pop expects 0 arguments", e.At)
				}
				return OptionalType{Inner: sl.Elem}, nil
			}
			if _, ok := tgtT.(SliceType); ok && mem.Member == "clear" {
				if len(e.Args) != 0 {
					return nil, fmt.Errorf("[%s] clear expects 0 arguments", e.At)
				}
				if root := assignmentRoot(mem.Target); root != "" {
					if info, found := env.LookupVar(root); found && !info.IsMut {
						return nil, fmt.Errorf("[%s] Cannot call 'clear' on immutable slice '%s'", e.At, root)
					}
				}
				return PrimitiveType{Name: "void"}, nil
			}
			if sl, ok := tgtT.(SliceType); ok && mem.Member == "contains" {
				if len(e.Args) != 1 {
					return nil, fmt.Errorf("[%s] contains expects 1 argument", e.At)
				}
				argT, err := tc.inferExpr(e.Args[0], env)
				if err != nil {
					return nil, err
				}
				if err := tc.assertCompat(sl.Elem, argT, e.At); err != nil {
					return nil, err
				}
				return PrimitiveType{Name: "bool"}, nil
			}
			if ch, ok := tgtT.(ChanType); ok {
				if mem.Member == "new" {
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] Chan.new expects a capacity", e.At)
					}
					capacityType, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if !integerType(capacityType) {
						return nil, fmt.Errorf("[%s] channel capacity must be an integer, got '%s'", e.At, capacityType)
					}
					return ch, nil
				}
				if mem.Member == "recv" {
					if len(e.Args) != 0 {
						return nil, fmt.Errorf("[%s] Chan.recv expects no arguments", e.At)
					}
					e.PoolElem = ch.Elem
					return ch.Elem, nil
				}
				if mem.Member == "close" {
					if len(e.Args) != 0 {
						return nil, fmt.Errorf("[%s] Chan.close expects no arguments", e.At)
					}
					return PrimitiveType{Name: "void"}, nil
				}
				if mem.Member == "send" {
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] send expects 1 argument", e.At)
					}
					argT, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if !argT.IsFlat(tc.Global) {
						return nil, fmt.Errorf("[%s] cannot send non-flat type '%s' across a channel", e.At, argT)
					}
					if err := tc.assertCompat(ch.Elem, argT, e.At); err != nil {
						return nil, err
					}
					// Complete linear ownership handoff: the sending thread
					// can never touch the value again.
					if err := tc.markMovedIfAffine(e.Args[0], env); err != nil {
						return nil, err
					}
					return PrimitiveType{Name: "void"}, nil
				}
			}
			if p, ok := tgtT.(PoolType); ok {
				e.PoolElem = p.Elem
				if mem.Member == "new" {
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] Pool.new expects a capacity", e.At)
					}
					capacityType, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if !integerType(capacityType) {
						return nil, fmt.Errorf("[%s] pool capacity must be an integer, got '%s'", e.At, capacityType)
					}
					return p, nil
				}
				if mem.Member == "spawn" {
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] Pool.spawn expects one value", e.At)
					}
					valueType, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if err := tc.assertCompat(p.Elem, valueType, e.At); err != nil {
						return nil, err
					}
					if err := tc.markMovedIfAffine(e.Args[0], env); err != nil {
						return nil, err
					}
					return CustomType{Name: "UbeHandle"}, nil
				}
				if mem.Member == "get" {
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] Pool.get expects a handle", e.At)
					}
					handleType, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if err := tc.assertCompat(CustomType{Name: "UbeHandle"}, handleType, e.At); err != nil {
						return nil, err
					}
					return OptionalType{Inner: p.Elem}, nil
				}
				if mem.Member == "despawn" {
					if len(e.Args) != 1 {
						return nil, fmt.Errorf("[%s] Pool.despawn expects a handle", e.At)
					}
					handleType, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if err := tc.assertCompat(CustomType{Name: "UbeHandle"}, handleType, e.At); err != nil {
						return nil, err
					}
					if id, ok := e.Args[0].(*IdentExpr); ok {
						if err := tc.markMovedIfAffine(id, env); err != nil {
							return nil, err
						}
					}
					return PrimitiveType{Name: "bool"}, nil
				}
			}
			methodKey := fmt.Sprintf("%s.%s", tgtT, mem.Member)
			if sig, ok := tc.Global.Funcs[methodKey]; ok {
				if env.CurFile != "" && sig.File != "" && env.CurFile != sig.File && !sig.IsPub {
					return nil, fmt.Errorf("[%s] cannot access private method '%s' on '%s'", e.At, mem.Member, tgtT)
				}
				e.ReceiverType = tgtT
				if sig.ReceiverMut {
					if root := assignmentRoot(mem.Target); root != "" {
						if vInfo, found := env.LookupVar(root); found && !vInfo.IsMut {
							return nil, fmt.Errorf("[%s] cannot call mutating method '%s' on immutable value '%s'", e.At, mem.Member, root)
						}
					}
				}
				if err := tc.checkCallArgsWithReceiver(mem.Target, sig.ReceiverMut, e.Args, sig.Params, sig.Escaping, env, e.At); err != nil {
					return nil, err
				}
				return sig.RetType, nil
			}
		}
		if id, ok := e.Callee.(*IdentExpr); ok {
			if sig, ok := tc.Global.Funcs[id.Name]; ok {
				if env.CurFile != "" && sig.File != "" && env.CurFile != sig.File && !sig.IsPub {
					return nil, fmt.Errorf("[%s] cannot access private function '%s' from another file", e.At, id.Name)
				}
				if err := tc.checkCallArgs(e.Args, sig.Params, sig.Escaping, env, e.At); err != nil {
					return nil, err
				}
				return sig.RetType, nil
			}
			return nil, fmt.Errorf("[%s] Undefined function '%s'", e.At, id.Name)
		}
		return nil, fmt.Errorf("[%s] Unsupported call target", e.At)

	case *MemberExpr:
		if id, ok := e.Target.(*IdentExpr); ok {
			// Qualified zero-argument module member: math.PI.
			if _, isVar := env.LookupVar(id.Name); !isVar {
				if _, sig, found := tc.lookupModuleMember(id.Name, e.Member); found && len(sig.Params) == 0 {
					return sig.RetType, nil
				}
			}
			if variants, isEnum := tc.Global.Enums[id.Name]; isEnum {
				if meta, hasMeta := tc.Global.EnumMeta[id.Name]; hasMeta {
					if env.CurFile != "" && meta.File != "" && env.CurFile != meta.File && !meta.IsPub {
						return nil, fmt.Errorf("[%s] cannot access private enum '%s'", e.At, id.Name)
					}
				}
				for _, variant := range variants {
					if variant.Name == e.Member && len(variant.Types) == 0 {
						return CustomType{Name: id.Name}, nil
					}
				}
			}
		}
		if e.Member == "ptr" {
			return nil, fmt.Errorf("[%s] '.ptr' is forbidden: Ube is 100%% sandboxed WebAssembly without raw memory pointers", e.At)
		}
		tgtT, err := tc.inferExpr(e.Target, env)
		if err != nil {
			return nil, err
		}
		e.TargetType = tgtT
		if e.Member == "len" {
			switch tgtT.(type) {
			case StringType, SliceType, ArrayType, StringBuilderType:
				return PrimitiveType{Name: "i32"}, nil
			}
		}
		if e.Member == "cap" {
			switch tgtT.(type) {
			case SliceType, ArrayType, StringBuilderType:
				return PrimitiveType{Name: "i32"}, nil
			}
		}
		if e.Member == "len" || e.Member == "cap" {
			return nil, fmt.Errorf("[%s] '%s' is not available on '%s'", e.At, e.Member, tgtT)
		}
		if custom, ok := tgtT.(CustomType); ok {
			matches := tc.findFieldMatches(custom.Name, e.Member, nil, map[string]bool{})
			if len(matches) == 1 {
				match := matches[0]
				if meta, ok := tc.Global.StructMeta[match.Owner]; ok {
					if env.CurFile != "" && meta.File != "" && env.CurFile != meta.File && !match.Field.IsPub {
						return nil, fmt.Errorf("[%s] cannot access private field '%s' on struct '%s'", e.At, e.Member, match.Owner)
					}
				}
				e.EmbedPath = match.Path
				return match.Field.Type, nil
			}
			if len(matches) > 1 {
				var where []string
				for _, match := range matches {
					where = append(where, match.Owner)
				}
				return nil, fmt.Errorf("[%s] ambiguous embedded field '%s' on '%s' (found in %s); qualify the access explicitly", e.At, e.Member, custom.Name, strings.Join(where, ", "))
			}
		}
		return nil, fmt.Errorf("[%s] Cannot access '%s' on '%s'", e.At, e.Member, tgtT)

	case *IndexExpr:
		tgtT, err := tc.inferExpr(e.Target, env)
		if err != nil {
			return nil, err
		}
		idxT, err := tc.inferExpr(e.Index, env)
		if err != nil {
			return nil, err
		}
		if !integerType(idxT) {
			return nil, fmt.Errorf("[%s] index must be an integer, got '%s'", e.At, idxT)
		}
		if sl, ok := tgtT.(SliceType); ok {
			e.TargetType = tgtT
			return sl.Elem, nil
		}
		if arr, ok := tgtT.(ArrayType); ok {
			e.TargetType = tgtT
			return arr.Elem, nil
		}
		if _, ok := tgtT.(StringType); ok {
			e.TargetType = tgtT
			return PrimitiveType{Name: "u8"}, nil
		}
		return nil, fmt.Errorf("[%s] Type '%s' does not support indexing", e.At, tgtT)

	case *SliceSubExpr:
		t, err := tc.inferExpr(e.Target, env)
		if err != nil {
			return nil, err
		}
		startT, err := tc.inferExpr(e.Start, env)
		if err != nil {
			return nil, err
		}
		if !integerType(startT) {
			return nil, fmt.Errorf("[%s] slice start index must be an integer, got '%s'", e.At, startT)
		}
		endT, err := tc.inferExpr(e.End, env)
		if err != nil {
			return nil, err
		}
		if !integerType(endT) {
			return nil, fmt.Errorf("[%s] slice end index must be an integer, got '%s'", e.At, endT)
		}
		e.TargetType = t
		if _, ok := t.(StringType); ok {
			return StringType{}, nil
		}
		if sl, ok := t.(SliceType); ok {
			return sl, nil
		}
		if a, ok := t.(ArrayType); ok {
			return SliceType{Elem: a.Elem}, nil
		}
		return nil, fmt.Errorf("[%s] Type '%s' does not support slicing", e.At, t)

	case *SliceInitExpr:
		e.Elem = tc.resolveType(e.Elem)
		for i, el := range e.Elements {
			t, err := tc.inferExpr(el, env)
			if err != nil {
				return nil, err
			}
			t = tc.resolveType(t)
			if tr, isTrait := e.Elem.(TraitType); isTrait {
				if c, isCustom := t.(CustomType); isCustom && tc.structImplementsTrait(c.Name, tr.Name) {
					e.Elements[i] = &TraitCoerceExpr{At: el.Loc(), Value: el, Trait: tr.Name, Concrete: c}
					continue
				}
			}
			if err := tc.assertCompat(e.Elem, t, el.Loc()); err != nil {
				return nil, err
			}
		}
		return SliceType{Elem: e.Elem}, nil

	case *ArrayInitExpr:
		if len(e.Elements) > e.Size {
			return nil, fmt.Errorf("[%s] too many array initializer elements", e.At)
		}
		for _, el := range e.Elements {
			t, err := tc.inferExpr(el, env)
			if err != nil {
				return nil, err
			}
			if err := tc.assertCompat(e.Elem, t, el.Loc()); err != nil {
				return nil, err
			}
		}
		return ArrayType{Elem: e.Elem, Size: e.Size}, nil

	case *GenericTypeExpr:
		if e.BaseType == "Chan" {
			ch := ChanType{Elem: e.ElemType}
			return ch, tc.validateType(ch, e.At)
		}
		pool := PoolType{Elem: e.ElemType}
		return pool, tc.validateType(pool, e.At)

	case *StructInitExpr:
		meta, hasMeta := tc.Global.StructMeta[e.Name]
		if hasMeta {
			if env.CurFile != "" && meta.File != "" && env.CurFile != meta.File && !meta.IsPub {
				return nil, fmt.Errorf("[%s] cannot instantiate private struct '%s'", e.At, e.Name)
			}
		}
		fields, ok := tc.Global.Structs[e.Name]
		if !ok {
			return nil, fmt.Errorf("[%s] unknown struct '%s'", e.At, e.Name)
		}
		fieldMap := make(map[string]UbeType)
		fieldPubMap := make(map[string]bool)
		for _, f := range fields {
			fieldMap[f.Name] = f.Type
			fieldPubMap[f.Name] = f.IsPub
		}
		provided := make(map[string]bool)
		for i, init := range e.Fields {
			expectedType, exists := fieldMap[init.Name]
			if !exists {
				return nil, fmt.Errorf("[%s] struct '%s' has no field '%s'", init.Val.Loc(), e.Name, init.Name)
			}
			if provided[init.Name] {
				return nil, fmt.Errorf("[%s] duplicate field '%s' in struct '%s' initialization", init.Val.Loc(), init.Name, e.Name)
			}
			if hasMeta && env.CurFile != "" && meta.File != "" && env.CurFile != meta.File && !fieldPubMap[init.Name] {
				return nil, fmt.Errorf("[%s] cannot initialize private field '%s' on struct '%s'", init.Val.Loc(), init.Name, e.Name)
			}
			provided[init.Name] = true
			valT, err := tc.inferExpr(init.Val, env)
			if err != nil {
				return nil, err
			}
			if expSlice, isSlice := expectedType.(SliceType); isSlice {
				if actArr, isArr := valT.(ArrayType); isArr && tc.assertCompat(expSlice.Elem, actArr.Elem, init.Val.Loc()) == nil {
					e.Fields[i].Val = &ArrayToSliceExpr{At: init.Val.Loc(), Value: init.Val, ArrayType: actArr}
					valT = expSlice
				}
			}
			if err := tc.assertCompat(expectedType, valT, init.Val.Loc()); err != nil {
				return nil, fmt.Errorf("[%s] field '%s' type mismatch: expected '%s', got '%s'", init.Val.Loc(), init.Name, expectedType, valT)
			}
		}
		return CustomType{Name: e.Name}, nil

	case *TryExpr:
		opndT, err := tc.inferExpr(e.Operand, env)
		if err != nil {
			return nil, err
		}
		if res, ok := opndT.(ResultType); ok {
			if curRes, isResult := env.CurRet.(ResultType); isResult {
				if err := tc.assertCompat(curRes.Err, res.Err, e.At); err != nil {
					return nil, fmt.Errorf("[%s] cannot propagate error of type '%s' in function returning error of type '%s'", e.At, res.Err, curRes.Err)
				}
			}
			return res.Ok, nil
		}
		return nil, fmt.Errorf("[%s] '?' expects Result type, got '%s'", e.At, opndT)

	case *OrElseExpr:
		pT, err := tc.inferExpr(e.Primary, env)
		if err != nil {
			return nil, err
		}
		e.PrimaryType = pT
		fT, err := tc.inferExpr(e.Fallback, env)
		if err != nil {
			return nil, err
		}
		if res, ok := pT.(ResultType); ok {
			return res.Ok, tc.assertCompat(res.Ok, fT, e.At)
		}
		if opt, ok := pT.(OptionalType); ok {
			return opt.Inner, tc.assertCompat(opt.Inner, fT, e.At)
		}
		return nil, fmt.Errorf("[%s] or_else expects Result or Option", e.At)
	}
	return nil, fmt.Errorf("Cannot infer expression type")
}
