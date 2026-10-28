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
	case StringType, PointerType:
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

func (tc *Typechecker) inferExpr(expr Expr, env *TypeEnvironment) (UbeType, error) {
	switch e := expr.(type) {
	case *TraitCoerceExpr:
		_, err := tc.inferExpr(e.Value, env)
		if err != nil {
			return nil, err
		}
		return TraitType{Name: e.Trait}, nil

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

	case *UnsafeExpr:
		tc.HasUnsafe = true
		unsafeEnv := childTypeEnvironment(env)
		unsafeEnv.UnsafeDepth = env.UnsafeDepth + 1
		return tc.inferExpr(e.Value, unsafeEnv)

	case *LiteralExpr:
		return e.Type, nil

	case *IdentExpr:
		if e.Name == "StringBuilder" {
			return StringBuilderType{}, nil
		}
		if v, ok := env.LookupVar(e.Name); ok {
			if err := requireUnsafePointerUse(v.Type, env, e.At); err != nil {
				return nil, err
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
			if env.UnsafeDepth == 0 {
				return nil, fmt.Errorf("[%s] Safety Error: taking a raw pointer requires an unsafe block", e.At)
			}
			return PointerType{Base: opndT}, nil
		}
		if e.Op == "*" {
			if env.UnsafeDepth == 0 {
				return nil, fmt.Errorf("[%s] Safety Error: dereferencing a raw pointer requires an unsafe block", e.At)
			}
			ptr, ok := opndT.(PointerType)
			if !ok {
				return nil, fmt.Errorf("[%s] cannot dereference non-pointer type '%s'", e.At, opndT)
			}
			return ptr.Base, nil
		}
		return opndT, nil

	case *CallExpr:
		if id, ok := e.Callee.(*IdentExpr); ok && tc.Externs[id.Name] && env.UnsafeDepth == 0 {
			return nil, fmt.Errorf("[%s] Safety Error: foreign function '%s' can only be called inside unsafe { ... }", e.At, id.Name)
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
		if id, ok := e.Callee.(*IdentExpr); ok && (id.Name == "println" || id.Name == "print") {
			for _, arg := range e.Args {
				argT, err := tc.inferExpr(arg, env)
				if err != nil {
					return nil, err
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
					if info, found := env.LookupVar(root); found && env.ScopeDepth > info.Depth {
						return nil, fmt.Errorf("[%s] Safety Error: cannot push to '%s' (declared at scope depth %d) from inside an inner scope (depth %d)", e.At, root, info.Depth, env.ScopeDepth)
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
					return PrimitiveType{Name: "bool"}, nil
				}
			}
			methodKey := fmt.Sprintf("%s.%s", tgtT, mem.Member)
			if sig, ok := tc.Global.Funcs[methodKey]; ok {
				e.ReceiverType = tgtT
				if err := tc.checkCallArgs(e.Args, sig.Params, sig.Escaping, env, e.At); err != nil {
					return nil, err
				}
				if err := requireUnsafePointerUse(sig.RetType, env, e.At); err != nil {
					return nil, err
				}
				return sig.RetType, nil
			}
		}
		if id, ok := e.Callee.(*IdentExpr); ok {
			if sig, ok := tc.Global.Funcs[id.Name]; ok {
				if err := tc.checkCallArgs(e.Args, sig.Params, sig.Escaping, env, e.At); err != nil {
					return nil, err
				}
				if err := requireUnsafePointerUse(sig.RetType, env, e.At); err != nil {
					return nil, err
				}
				return sig.RetType, nil
			}
			return nil, fmt.Errorf("[%s] Undefined function '%s'", e.At, id.Name)
		}
		return nil, fmt.Errorf("[%s] Unsupported call target", e.At)

	case *MemberExpr:
		if id, ok := e.Target.(*IdentExpr); ok {
			if variants, isEnum := tc.Global.Enums[id.Name]; isEnum {
				for _, variant := range variants {
					if variant.Name == e.Member && len(variant.Types) == 0 {
						return CustomType{Name: id.Name}, nil
					}
				}
			}
		}
		if e.Member == "ptr" {
			if env.UnsafeDepth == 0 {
				return nil, fmt.Errorf("[%s] Safety Error: raw pointer access requires an unsafe block", e.At)
			}
			return PointerType{Base: PrimitiveType{Name: "u8"}}, nil
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
			if fields, ok := tc.Global.Structs[custom.Name]; ok {
				for _, f := range fields {
					if f.Name == e.Member {
						if err := requireUnsafePointerUse(f.Type, env, e.At); err != nil {
							return nil, err
						}
						return f.Type, nil
					}
				}
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
			if err := requireUnsafePointerUse(sl.Elem, env, e.At); err != nil {
				return nil, err
			}
			return sl.Elem, nil
		}
		if arr, ok := tgtT.(ArrayType); ok {
			e.TargetType = tgtT
			if err := requireUnsafePointerUse(arr.Elem, env, e.At); err != nil {
				return nil, err
			}
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
		fields, ok := tc.Global.Structs[e.Name]
		if !ok {
			return nil, fmt.Errorf("[%s] unknown struct '%s'", e.At, e.Name)
		}
		fieldMap := make(map[string]UbeType)
		for _, f := range fields {
			fieldMap[f.Name] = f.Type
		}
		provided := make(map[string]bool)
		for _, init := range e.Fields {
			expectedType, exists := fieldMap[init.Name]
			if !exists {
				return nil, fmt.Errorf("[%s] struct '%s' has no field '%s'", init.Val.Loc(), e.Name, init.Name)
			}
			if provided[init.Name] {
				return nil, fmt.Errorf("[%s] duplicate field '%s' in struct '%s' initialization", init.Val.Loc(), init.Name, e.Name)
			}
			provided[init.Name] = true
			valT, err := tc.inferExpr(init.Val, env)
			if err != nil {
				return nil, err
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

func (tc *Typechecker) checkCallArgs(args []Expr, params []FieldDecl, escaping []bool, env *TypeEnvironment, loc SourceLocation) error {
	if len(args) != len(params) {
		return fmt.Errorf("[%s] function expects %d arguments, got %d", loc, len(params), len(args))
	}
	for i, arg := range args {
		actual, err := tc.inferExpr(arg, env)
		if err != nil {
			return err
		}
		if _, rawPointer := actual.(PointerType); rawPointer && env.UnsafeDepth == 0 {
			return fmt.Errorf("[%s] Safety Error: passing a raw pointer requires an unsafe block", arg.Loc())
		}
		if i < len(escaping) && escaping[i] && tc.hasLifetime(actual) {
			if depth := tc.exprDepth(arg, env); depth > 0 {
				return fmt.Errorf("[%s] Safety Error: argument rooted at scope depth %d may escape through a function parameter", arg.Loc(), depth)
			}
		}
		if params[i].Type == nil {
			continue
		}
		paramType := tc.resolveType(params[i].Type)
		actual = tc.resolveType(actual)
		if tr, isTrait := paramType.(TraitType); isTrait {
			if c, isCustom := actual.(CustomType); isCustom && tc.structImplementsTrait(c.Name, tr.Name) {
				args[i] = &TraitCoerceExpr{At: arg.Loc(), Value: arg, Trait: tr.Name, Concrete: c}
				actual = tr
			}
		}
		if err := tc.assertCompat(paramType, actual, arg.Loc()); err != nil {
			return err
		}
	}
	return nil
}

func (tc *Typechecker) structImplementsTrait(structName, traitName string) bool {
	tr, ok := tc.Global.Traits[traitName]
	if !ok {
		return false
	}
	for _, m := range tr.Methods {
		key := fmt.Sprintf("%s.%s", structName, m.Name)
		sig, ok := tc.Global.Funcs[key]
		if !ok {
			return false
		}
		if len(sig.Params) != len(m.Params) {
			return false
		}
		for i, p := range m.Params {
			if sig.Params[i].Type.String() != p.Type.String() {
				return false
			}
		}
		if sig.RetType.String() != m.RetType.String() {
			return false
		}
	}
	return true
}

func (tc *Typechecker) typeFromName(name string) UbeType {
	switch name {
	case "i32", "u32", "i64", "u64", "u8", "f32", "f64", "bool", "void":
		return PrimitiveType{Name: name}
	case "string":
		return StringType{}
	case "StringBuilder":
		return StringBuilderType{}
	default:
		if _, ok := tc.Global.Traits[name]; ok {
			return TraitType{Name: name}
		}
		return CustomType{Name: name}
	}
}

func (tc *Typechecker) assertCompat(expected, actual UbeType, loc SourceLocation) error {
	if expected == nil || actual == nil {
		return nil
	}
	expected = tc.resolveType(expected)
	actual = tc.resolveType(actual)
	if expected.String() == actual.String() {
		return nil
	}
	if tr, isTrait := expected.(TraitType); isTrait {
		if c, isCustom := actual.(CustomType); isCustom {
			if tc.structImplementsTrait(c.Name, tr.Name) {
				return nil
			}
			return fmt.Errorf("[%s] type '%s' does not implement trait '%s'", loc, c.Name, tr.Name)
		}
	}
	if integerType(expected) && integerType(actual) {
		return nil
	}
	if opt, ok := expected.(OptionalType); ok {
		if actOpt, ok := actual.(OptionalType); ok && actOpt.Inner.String() == "void" {
			return nil
		}
		if actOpt, ok := actual.(OptionalType); ok && opt.Inner.String() == actOpt.Inner.String() {
			return nil
		}
		if opt.Inner.String() == actual.String() {
			return nil
		}
		if integerType(opt.Inner) && integerType(actual) {
			return nil
		}
	}
	if res, ok := expected.(ResultType); ok {
		if actRes, ok := actual.(ResultType); ok {
			if err := tc.assertCompat(res.Ok, actRes.Ok, loc); err == nil {
				if err := tc.assertCompat(res.Err, actRes.Err, loc); err == nil {
					return nil
				}
			}
		}
		if err := tc.assertCompat(res.Ok, actual, loc); err == nil {
			return nil
		}
		if err := tc.assertCompat(res.Err, actual, loc); err == nil {
			return nil
		}
	}
	if expected.String() == "*void" {
		if _, ok := actual.(PointerType); ok {
			return nil
		}
	}
	return fmt.Errorf("[%s] Type mismatch: expected '%s', got '%s'", loc, expected, actual)
}

func (tc *Typechecker) validateType(t UbeType, loc SourceLocation) error {
	switch v := t.(type) {
	case PrimitiveType, StringType, StringBuilderType:
		return nil
	case TraitType:
		if _, ok := tc.Global.Traits[v.Name]; !ok {
			return fmt.Errorf("[%s] unknown trait '%s'", loc, v.Name)
		}
		return nil
	case PointerType:
		return tc.validateType(v.Base, loc)
	case SliceType:
		return tc.validateType(v.Elem, loc)
	case ArrayType:
		if v.Size <= 0 {
			return fmt.Errorf("[%s] array size must be greater than 0", loc)
		}
		return tc.validateType(v.Elem, loc)
	case OptionalType:
		return tc.validateType(v.Inner, loc)
	case ResultType:
		if err := tc.validateType(v.Ok, loc); err != nil {
			return err
		}
		return tc.validateType(v.Err, loc)
	case ChanType:
		if err := tc.validateType(v.Elem, loc); err != nil {
			return err
		}
		if p, ok := v.Elem.(PrimitiveType); ok && p.Name == "void" {
			return fmt.Errorf("[%s] Chan element type cannot be void", loc)
		}
		if !v.Elem.IsFlat(tc.Global) {
			return fmt.Errorf("[%s] Chan element type '%s' must be flat", loc, v.Elem)
		}
		return nil
	case PoolType:
		if err := tc.validateType(v.Elem, loc); err != nil {
			return err
		}
		if p, ok := v.Elem.(PrimitiveType); ok && p.Name == "void" {
			return fmt.Errorf("[%s] Pool element type cannot be void", loc)
		}
		if !v.Elem.IsFlat(tc.Global) {
			return fmt.Errorf("[%s] Pool element type '%s' must be flat", loc, v.Elem)
		}
		return nil
	case CustomType:
		return nil
	default:
		return nil
	}
}
