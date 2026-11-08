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
		if i < len(params) && params[i].Type != nil {
			tc.pinLiteral(arg, params[i].Type)
		}
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
				if _, isMove := arg.(*MoveExpr); !isMove {
					root := variableRoot(arg)
					if root != "" && !tc.isCopyType(actual) {
						return fmt.Errorf("[%s] cannot move non-copy variable '%s' into parameter '%s' without explicit 'move'; use 'move %s'", arg.Loc(), root, params[i].Name, root)
					}
				}
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
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64", "bool", "void":
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

// pinLiteral implements the literal pinning rules from typesafety.md
// section 2.1. A literal in source is untyped until a context pins it; a
// compatible numeric context (variable annotation, parameter, return,
// struct field, slice element, binary operand, or cast target) decides the
// literal's type in one local pass with no inference. An untyped integer
// literal may be pinned to any sized numeric type; an untyped float literal
// only to f32 or f64. It returns true when the literal (or one of its
// untyped sub-expressions) was pinned to target.
func (tc *Typechecker) pinLiteral(expr Expr, target UbeType) bool {
	if expr == nil || target == nil {
		return false
	}
	target = tc.resolveType(target)

	if opt, ok := target.(OptionalType); ok {
		target = opt.Inner
	} else if res, ok := target.(ResultType); ok {
		target = res.Ok
	}

	switch e := expr.(type) {
	case *LiteralExpr:
		switch e.Type.(type) {
		case UntypedIntType:
			if isNumericType(target) {
				e.Type = target
				return true
			}
		case UntypedFloatType:
			if target.String() == "f32" || target.String() == "f64" {
				e.Type = target
				return true
			}
		}
	case *UnaryExpr:
		if e.Op == "-" || e.Op == "+" || e.Op == "~" {
			return tc.pinLiteral(e.Operand, target)
		}
	case *BinaryExpr:
		pLeft := tc.pinLiteral(e.Left, target)
		pRight := tc.pinLiteral(e.Right, target)
		if pLeft || pRight {
			e.Type = target
			e.OperandType = target
			return true
		}
	case *MoveExpr:
		return tc.pinLiteral(e.Value, target)
	case *CastExpr:
		return tc.pinLiteral(e.Expr, e.TargetType)
	case *IfExpr:
		pThen := tc.pinBlockResult(e.Then, target)
		pElse := tc.pinBlockResult(e.Else, target)
		if pThen || pElse {
			e.Type = target
			return true
		}
	}
	return false
}

// pinBlockResult pins the terminal expression statement of an if-expression
// arm, if one exists.
func (tc *Typechecker) pinBlockResult(b *BlockStmt, target UbeType) bool {
	if b == nil || len(b.Stmts) == 0 {
		return false
	}
	last := b.Stmts[len(b.Stmts)-1]
	if es, ok := last.(*ExprStmt); ok {
		return tc.pinLiteral(es.Expr, target)
	}
	return false
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
	// Untyped literal allowances: an untyped int may flow into any numeric
	// expected type when it was not (or could not be) pinned ahead of time.
	// Runtime variables of different types are never compatible (the core
	// invariant of typesafety.md section 1; no implicit widening here).
	if _, ok := actual.(UntypedIntType); ok && isNumericType(expected) {
		return nil
	}
	if _, ok := actual.(UntypedFloatType); ok && (expected.String() == "f32" || expected.String() == "f64") {
		return nil
	}
	if _, ok := expected.(UntypedIntType); ok && isNumericType(actual) {
		return nil
	}
	if _, ok := expected.(UntypedFloatType); ok && (actual.String() == "f32" || actual.String() == "f64") {
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
