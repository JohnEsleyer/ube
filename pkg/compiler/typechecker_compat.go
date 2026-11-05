// File: pkg/compiler/typechecker_compat.go
package compiler

import "fmt"

func (tc *Typechecker) checkCallArgs(args []Expr, params []FieldDecl, escaping []bool, env *TypeEnvironment, loc SourceLocation) error {
	return tc.checkCallArgsWithReceiver(nil, false, args, params, escaping, env, loc)
}

// checkCallArgsWithReceiver verifies call arguments, enforces move semantics,
// and rejects aliasing between mutable loans in a single call.
func (tc *Typechecker) checkCallArgsWithReceiver(receiver Expr, receiverMut bool, args []Expr, params []FieldDecl, escaping []bool, env *TypeEnvironment, loc SourceLocation) error {
	if len(args) != len(params) {
		return fmt.Errorf("[%s] function expects %d arguments, got %d", loc, len(params), len(args))
	}

	mutRoots := make(map[string]bool)
	allRoots := make(map[string]bool)

	if receiver != nil {
		rRoot := assignmentRoot(receiver)
		if rRoot != "" {
			allRoots[rRoot] = true
			if receiverMut {
				if vi, found := env.LookupVar(rRoot); found && !vi.IsMut {
					return fmt.Errorf("[%s] cannot call mutating method on immutable value '%s'", loc, rRoot)
				}
				mutRoots[rRoot] = true
			}
		}
	}

	for i, arg := range args {
		_, explicitMut := mutArgOperand(arg)
		actual, err := tc.inferExpr(arg, env)
		if err != nil {
			return err
		}
		paramMut := params[i].IsMut
		if explicitMut && !paramMut {
			return fmt.Errorf("[%s] argument is marked 'mut' but parameter '%s' is not mutable", arg.Loc(), params[i].Name)
		}
		root := assignmentRoot(arg)
		if paramMut {
			if root != "" {
				if vi, found := env.LookupVar(root); found && !vi.IsMut {
					return fmt.Errorf("[%s] cannot pass immutable variable '%s' as mutable parameter '%s'", arg.Loc(), root, params[i].Name)
				}
				// A mutable loan must be disjoint from every other argument
				// and from a mutable receiver in the same call.
				if allRoots[root] {
					return fmt.Errorf("[%s] aliasing violation: variable '%s' cannot be passed as mutable argument while already referenced in the same call", arg.Loc(), root)
				}
				mutRoots[root] = true
				allRoots[root] = true
			}
		} else if root != "" {
			if mutRoots[root] {
				return fmt.Errorf("[%s] aliasing violation: variable '%s' cannot be passed as both mutable and immutable argument in the same call", arg.Loc(), root)
			}
			allRoots[root] = true
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
		if expSlice, isSlice := paramType.(SliceType); isSlice {
			if actArr, isArr := actual.(ArrayType); isArr {
				if tc.assertCompat(expSlice.Elem, actArr.Elem, arg.Loc()) == nil {
					args[i] = &ArrayToSliceExpr{At: arg.Loc(), Value: arg, ArrayType: actArr}
					actual = expSlice
				}
			}
		}
		if err := tc.assertCompat(paramType, actual, arg.Loc()); err != nil {
			return err
		}
		// Mutable parameters and non-escaping parameters are loans. Affine
		// values are only consumed when the callee can retain them.
		if !paramMut {
			escapes := i < len(escaping) && escaping[i]
			if escapes {
				if err := tc.markMovedIfAffine(arg, env); err != nil {
					return err
				}
			}
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
	if expSlice, ok := expected.(SliceType); ok {
		if actArr, ok := actual.(ArrayType); ok {
			if err := tc.assertCompat(expSlice.Elem, actArr.Elem, loc); err == nil {
				return nil
			}
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
	if expTuple, ok := expected.(TupleType); ok {
		if actTuple, ok := actual.(TupleType); ok && len(expTuple.Elems) == len(actTuple.Elems) {
			for i := range expTuple.Elems {
				if err := tc.assertCompat(expTuple.Elems[i], actTuple.Elems[i], loc); err != nil {
					return err
				}
			}
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
		if meta, ok := tc.Global.TraitMeta[v.Name]; ok {
			if tc.Global.CurModule != "" && meta.Package != "" && meta.Package != tc.Global.CurModule && !meta.IsPub {
				return fmt.Errorf("[%s] cannot access unexported trait '%s' from package '%s'", loc, v.Name, meta.Package)
			}
		}
		if _, ok := tc.Global.Traits[v.Name]; !ok {
			return fmt.Errorf("[%s] unknown trait '%s'", loc, v.Name)
		}
		return nil
	case TupleType:
		for _, e := range v.Elems {
			if err := tc.validateType(e, loc); err != nil {
				return err
			}
		}
		return nil
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
		if meta, ok := tc.Global.StructMeta[v.Name]; ok {
			if tc.Global.CurModule != "" && meta.Package != "" && meta.Package != tc.Global.CurModule && !meta.IsPub {
				return fmt.Errorf("[%s] cannot access unexported struct '%s' from package '%s'", loc, v.Name, meta.Package)
			}
		}
		if meta, ok := tc.Global.EnumMeta[v.Name]; ok {
			if tc.Global.CurModule != "" && meta.Package != "" && meta.Package != tc.Global.CurModule && !meta.IsPub {
				return fmt.Errorf("[%s] cannot access unexported enum '%s' from package '%s'", loc, v.Name, meta.Package)
			}
		}
		if meta, ok := tc.Global.TraitMeta[v.Name]; ok {
			if tc.Global.CurModule != "" && meta.Package != "" && meta.Package != tc.Global.CurModule && !meta.IsPub {
				return fmt.Errorf("[%s] cannot access unexported trait '%s' from package '%s'", loc, v.Name, meta.Package)
			}
		}
		return nil
	default:
		return nil
	}
}
