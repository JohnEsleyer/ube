// File: pkg/compiler/typechecker_expr.go
package compiler

import (
	"fmt"
	"strings"
)

func printableType(t UbeType) bool {
	switch v := t.(type) {
	case PrimitiveType:
		switch v.Name {
		case "i8", "i16", "i32", "u8", "u16", "u32", "i64", "u64", "f32", "f64", "bool":
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
		case "i8", "i16", "i32", "u8", "u16", "u32", "i64", "u64":
			return true
		}
	}
	return false
}

// lookupModuleMember resolves `alias.member` to the internal flattened name
// used for an imported module. Unaliased imports map to the empty prefix, so
// `wasi.fd_write(...)` and `fd_write(...)` resolve to the same declaration.
// inferIfArmValue typechecks every statement of an if-expression arm in the
// provided child scope, and returns the type of the final ExprStmt. Earlier
// statements are checked normally; only the final statement must be an
// expression (otherwise the arm has no value).
// findEmbeddedMethod walks the anonymous embedding chain of structName
// looking for a method called methodName. Returns the unique match; ambiguity
// is treated as "not found" so the caller can fall back to a diagnostic that
// explains how to qualify. `seen` guards against cyclic embedding.
func (tc *Typechecker) findEmbeddedMethod(structName, methodName string, path []string, seen map[string]bool) ([]string, string, FuncSignature, bool) {
	if seen[structName] {
		return nil, "", FuncSignature{}, false
	}
	seen[structName] = true
	meta, ok := tc.Global.StructMeta[structName]
	if !ok {
		return nil, "", FuncSignature{}, false
	}
	var matches [][]string
	var owners []string
	var sigs []FuncSignature
	for _, f := range meta.Fields {
		if !f.Anonymous {
			continue
		}
		ct, ok := f.Type.(CustomType)
		if !ok {
			continue
		}
		childPath := append(append([]string{}, path...), f.Name)
		if sig, has := tc.Global.Funcs[ct.Name+"."+methodName]; has {
			matches = append(matches, childPath)
			owners = append(owners, ct.Name)
			sigs = append(sigs, sig)
		}
		subPath, subOwner, subSig, subFound := tc.findEmbeddedMethod(ct.Name, methodName, childPath, seen)
		if subFound {
			matches = append(matches, subPath)
			owners = append(owners, subOwner)
			sigs = append(sigs, subSig)
		}
	}
	if len(matches) == 1 {
		return matches[0], owners[0], sigs[0], true
	}
	return nil, "", FuncSignature{}, false
}

// collectEmbeddedMethodHints appends "<Embed>[.<Embed>...].<method>" strings
// for every anonymous embedding path that exposes methodName. Used only to
// build the "did you mean ...?" part of the diagnostic.
func (tc *Typechecker) collectEmbeddedMethodHints(structName, methodName string, path []string, seen map[string]bool, out *[]string) {
	if seen[structName] {
		return
	}
	seen[structName] = true
	meta, ok := tc.Global.StructMeta[structName]
	if !ok {
		return
	}
	for _, f := range meta.Fields {
		if !f.Anonymous {
			continue
		}
		ct, ok := f.Type.(CustomType)
		if !ok {
			continue
		}
		childPath := append(append([]string{}, path...), f.Name)
		if _, has := tc.Global.Funcs[ct.Name+"."+methodName]; has {
			*out = append(*out, strings.Join(append(childPath, methodName), "."))
		}
		tc.collectEmbeddedMethodHints(ct.Name, methodName, childPath, seen, out)
	}
}

func (tc *Typechecker) inferIfArmValue(b *BlockStmt, env *TypeEnvironment) (UbeType, error) {
	if b == nil || len(b.Stmts) == 0 {
		return nil, fmt.Errorf("%s: if-expression branch has no value (empty block)", b.Loc())
	}
	for i, stmt := range b.Stmts {
		if i == len(b.Stmts)-1 {
			es, ok := stmt.(*ExprStmt)
			if !ok {
				return nil, fmt.Errorf("%s: if-expression branch's last statement must be an expression", stmt.Loc())
			}
			return tc.inferExpr(es.Expr, env)
		}
		if err := tc.checkStmt(stmt, env); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%s: if-expression branch has no value", b.Loc())
}

func (tc *Typechecker) lookupModuleMember(alias, member string) (string, FuncSignature, bool) {
	canonical, isModule := tc.ModuleAliases[alias]
	if !isModule {
		return "", FuncSignature{}, false
	}
	key := canonical + "." + member
	sig, ok := tc.Global.Funcs[key]
	return key, sig, ok
}

// isUntypedOperand reports whether expr is an integer/float literal (or an
// arithmetic expression composed entirely of untyped literals) whose type
// has not yet been pinned by an enclosing context.
func isUntypedOperand(expr Expr) bool {
	switch e := expr.(type) {
	case *LiteralExpr:
		switch e.Type.(type) {
		case UntypedIntType, UntypedFloatType:
			return true
		}
	case *UnaryExpr:
		if e.Op == "-" || e.Op == "+" || e.Op == "~" {
			return isUntypedOperand(e.Operand)
		}
	case *BinaryExpr:
		switch e.Op {
		case "+", "-", "*", "/", "%", "&", "|", "^", "<<", ">>":
			return isUntypedOperand(e.Left) && isUntypedOperand(e.Right)
		}
	}
	return false
}

// peekNumericTarget predicts the numeric type a non-literal operand will
// take, without defaulting any literal it may contain.
func (tc *Typechecker) peekNumericTarget(expr Expr, env *TypeEnvironment) UbeType {
	switch e := expr.(type) {
	case *IdentExpr:
		if v, ok := env.LookupVar(e.Name); ok {
			return tc.resolveType(v.Type)
		}
	case *UnaryExpr:
		if e.Op == "-" || e.Op == "+" || e.Op == "~" {
			return tc.peekNumericTarget(e.Operand, env)
		}
	case *CastExpr:
		return tc.resolveType(e.TargetType)
	case *BinaryExpr:
		if t := tc.peekNumericTarget(e.Left, env); t != nil {
			return t
		}
		return tc.peekNumericTarget(e.Right, env)
	}
	return nil
}

func (tc *Typechecker) inferExpr(expr Expr, env *TypeEnvironment) (UbeType, error) {
	switch e := expr.(type) {
	case *IfExpr:
		condT, err := tc.inferExpr(e.Cond, env)
		if err != nil {
			return nil, err
		}
		if condT.String() != "bool" {
			return nil, fmt.Errorf("[%s] if-expression condition must be bool, got '%s'", e.Cond.Loc(), condT)
		}
		thenEnv := childTypeEnvironment(env)
		for k, v := range env.Vars {
			thenEnv.Vars[k] = v
		}
		thenT, err := tc.inferIfArmValue(e.Then, thenEnv)
		if err != nil {
			return nil, err
		}
		elseEnv := childTypeEnvironment(env)
		for k, v := range env.Vars {
			elseEnv.Vars[k] = v
		}
		elseT, err := tc.inferIfArmValue(e.Else, elseEnv)
		if err != nil {
			return nil, err
		}
		thenT = tc.resolveType(thenT)
		elseT = tc.resolveType(elseT)
		if thenT.String() != elseT.String() {
			return nil, fmt.Errorf("[%s] if-expression branches have different types: then='%s' else='%s'", e.At, thenT, elseT)
		}
		for name, ti := range thenEnv.Vars {
			if ti.Moved {
				env.updateVar(name, func(v *VarInfo) { v.Moved = true })
			}
		}
		for name, ei := range elseEnv.Vars {
			if ei.Moved {
				env.updateVar(name, func(v *VarInfo) { v.Moved = true })
			}
		}
		e.Type = thenT
		return thenT, nil

	case *MoveExpr:
		valT, err := tc.inferExpr(e.Value, env)
		if err != nil {
			return nil, err
		}
		valT = tc.resolveType(valT)
		root := assignmentRoot(e.Value)
		if root != "" {
			if vInfo, found := env.LookupVar(root); found && vInfo.IsView {
				return nil, fmt.Errorf("[%s] cannot move from view parameter '%s'", e.At, root)
			}
		}
		if tc.isCopyType(valT) {
			return nil, fmt.Errorf("[%s] cannot move value of copy type '%s'", e.At, valT)
		}
		root = variableRoot(e.Value)
		if root == "" {
			return nil, fmt.Errorf("[%s] 'move' requires a variable to move from", e.At)
		}
		if vInfo, found := env.LookupVar(root); found {
			if vInfo.Moved {
				return nil, movedValueError(e.At, root)
			}
		}
		if err := tc.markMovedIfAffine(e.Value, env); err != nil {
			return nil, err
		}
		e.Type = valT
		return valT, nil

	case *PromoteExpr:
		if root := assignmentRoot(e.Value); root != "" {
			if vi, found := env.LookupVar(root); found && vi.IsView {
				return nil, fmt.Errorf("[%s] cannot promote view parameter '%s'", e.At, root)
			}
		}
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
			if root := assignmentRoot(elem); root != "" {
				if vi, found := env.LookupVar(root); found && vi.IsView {
					if !tc.isCopyType(t) || tc.hasLifetime(t) || variableRoot(elem) == root {
						return nil, fmt.Errorf("[%s] cannot store view parameter '%s' into tuple element (views are second-class)", elem.Loc(), root)
					}
				}
			}
			if _, isMove := elem.(*MoveExpr); !isMove {
				root := variableRoot(elem)
				if root != "" && !tc.isCopyType(t) {
					return nil, fmt.Errorf("[%s] cannot move non-copy variable '%s' into tuple element without explicit 'move'; use 'move %s'", elem.Loc(), root, root)
				}
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
		switch e.Type.(type) {
		case UntypedIntType:
			e.Type = PrimitiveType{Name: "i32"}
		case UntypedFloatType:
			e.Type = PrimitiveType{Name: "f32"}
		}
		return e.Type, nil

	case *CastExpr:
		targetT := tc.resolveType(e.TargetType)
		tc.pinLiteral(e.Expr, targetT)
		srcT, err := tc.inferExpr(e.Expr, env)
		if err != nil {
			return nil, err
		}
		srcT = tc.resolveType(srcT)
		if srcT.String() == targetT.String() {
			return targetT, nil
		}
		if isNumericType(targetT) && isNumericType(srcT) {
			return targetT, nil
		}
		return nil, fmt.Errorf("[%s] cannot convert '%s' to '%s'", e.At, srcT, targetT)

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
		canonical := tc.canonicalName(e.Name)
		if _, ok := tc.Global.Enums[canonical]; ok {
			e.Name = canonical
			return CustomType{Name: canonical}, nil
		}
		if _, ok := tc.Global.Traits[canonical]; ok {
			e.Name = canonical
			return TraitType{Name: canonical}, nil
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
		leftUntyped := isUntypedOperand(e.Left)
		rightUntyped := isUntypedOperand(e.Right)

		var lT, rT UbeType
		var err error

		if !leftUntyped && rightUntyped {
			lT, err = tc.inferExpr(e.Left, env)
			if err != nil {
				return nil, err
			}
			if isNumericType(lT) {
				tc.pinLiteral(e.Right, lT)
			}
			rT, err = tc.inferExpr(e.Right, env)
			if err != nil {
				return nil, err
			}
		} else if leftUntyped && !rightUntyped {
			rT, err = tc.inferExpr(e.Right, env)
			if err != nil {
				return nil, err
			}
			if isNumericType(rT) {
				tc.pinLiteral(e.Left, rT)
			}
			lT, err = tc.inferExpr(e.Left, env)
			if err != nil {
				return nil, err
			}
		} else {
			lT, err = tc.inferExpr(e.Left, env)
			if err != nil {
				return nil, err
			}
			if isNumericType(lT) && rightUntyped {
				tc.pinLiteral(e.Right, lT)
			}
			rT, err = tc.inferExpr(e.Right, env)
			if err != nil {
				return nil, err
			}
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

			if id, ok := mem.Target.(*IdentExpr); ok {
				if _, isVar := env.LookupVar(id.Name); !isVar {
					if _, isMod := tc.ModuleAliases[id.Name]; isMod {
						if _, sig, found := tc.lookupModuleMember(id.Name, mem.Member); found {
							if sig.Package != "" && env.CurModule != "" && sig.Package != env.CurModule && !sig.IsPub {
								return nil, fmt.Errorf("[%s] cannot access unexported function '%s' from package '%s'", e.At, mem.Member, sig.Package)
							}
							if err := tc.checkCallArgs(e.Args, sig.Params, sig.Escaping, env, e.At); err != nil {
								return nil, err
							}
							return sig.RetType, nil
						}
						return nil, fmt.Errorf("[%s] module '%s' has no function '%s'", e.At, id.Name, mem.Member)
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
				enumName := tc.canonicalName(id.Name)
				if variants, ok := tc.Global.Enums[enumName]; ok {
					id.Name = enumName
					if meta, hasMeta := tc.Global.EnumMeta[enumName]; hasMeta {
						if env.CurModule != "" && meta.Package != "" && meta.Package != env.CurModule && !meta.IsPub {
							return nil, fmt.Errorf("[%s] cannot access unexported enum '%s' from package '%s'", e.At, enumName, meta.Package)
						}
					}
					for _, variant := range variants {
						if variant.Name == mem.Member {
							if len(e.Args) != len(variant.Types) {
								return nil, fmt.Errorf("[%s] enum variant '%s' expects %d arguments, got %d", e.At, mem.Member, len(variant.Types), len(e.Args))
							}
							for i, arg := range e.Args {
								tc.pinLiteral(arg, variant.Types[i])
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

			if tr, isTrait := tgtT.(TraitType); isTrait {
				e.ReceiverType = tgtT
				if meta, hasMeta := tc.Global.TraitMeta[tr.Name]; hasMeta {
					if env.CurModule != "" && meta.Package != "" && meta.Package != env.CurModule && !meta.IsPub {
						return nil, fmt.Errorf("[%s] cannot access unexported trait '%s' from package '%s'", e.At, tr.Name, meta.Package)
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
						if info.IsView {
							return nil, fmt.Errorf("[%s] cannot call mutating method 'push' on view '%s'", e.At, root)
						}
						if !info.IsMut {
							return nil, fmt.Errorf("[%s] cannot call 'push' on immutable slice '%s'; declare it with 'let mut'", e.At, root)
						}
						if env.ScopeDepth > info.Depth {
							return nil, fmt.Errorf("[%s] Safety Error: Outer slice mutation attempted within inner arena at line %d; use promote(...) to export data", e.At, e.At.Line)
						}
					}
				}
				if root := assignmentRoot(e.Args[0]); root != "" {
					if vi, found := env.LookupVar(root); found && vi.IsView {
						return nil, fmt.Errorf("[%s] cannot push view parameter '%s' into slice (views are second-class)", e.Args[0].Loc(), root)
					}
				}
				tc.pinLiteral(e.Args[0], sl.Elem)
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
				if root := assignmentRoot(mem.Target); root != "" {
					if info, found := env.LookupVar(root); found && info.IsView {
						return nil, fmt.Errorf("[%s] cannot call mutating method 'pop' on view '%s'", e.At, root)
					}
				}
				return OptionalType{Inner: sl.Elem}, nil
			}
			if _, ok := tgtT.(SliceType); ok && mem.Member == "resize" {
				if len(e.Args) != 1 {
					return nil, fmt.Errorf("[%s] resize expects 1 argument", e.At)
				}
				if root := assignmentRoot(mem.Target); root != "" {
					if info, found := env.LookupVar(root); found {
						if info.IsView {
							return nil, fmt.Errorf("[%s] cannot call mutating method 'resize' on view '%s'", e.At, root)
						}
						if !info.IsMut {
							return nil, fmt.Errorf("[%s] cannot call 'resize' on immutable slice '%s'", e.At, root)
						}
					}
				}
				tc.pinLiteral(e.Args[0], PrimitiveType{Name: "i32"})
				argT, err := tc.inferExpr(e.Args[0], env)
				if err != nil {
					return nil, err
				}
				if err := tc.assertCompat(PrimitiveType{Name: "i32"}, argT, e.At); err != nil {
					return nil, err
				}
				return PrimitiveType{Name: "void"}, nil
			}
			if _, ok := tgtT.(SliceType); ok && mem.Member == "clear" {
				if len(e.Args) != 0 {
					return nil, fmt.Errorf("[%s] clear expects 0 arguments", e.At)
				}
				if root := assignmentRoot(mem.Target); root != "" {
					if info, found := env.LookupVar(root); found {
						if info.IsView {
							return nil, fmt.Errorf("[%s] cannot call mutating method 'clear' on view '%s'", e.At, root)
						}
						if !info.IsMut {
							return nil, fmt.Errorf("[%s] Cannot call 'clear' on immutable slice '%s'", e.At, root)
						}
					}
				}
				return PrimitiveType{Name: "void"}, nil
			}
			if sl, ok := tgtT.(SliceType); ok && mem.Member == "contains" {
				if len(e.Args) != 1 {
					return nil, fmt.Errorf("[%s] contains expects 1 argument", e.At)
				}
				tc.pinLiteral(e.Args[0], sl.Elem)
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
					tc.pinLiteral(e.Args[0], PrimitiveType{Name: "i32"})
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
					if root := assignmentRoot(e.Args[0]); root != "" {
						if vi, found := env.LookupVar(root); found && vi.IsView {
							return nil, fmt.Errorf("[%s] cannot send view parameter '%s' into channel", e.At, root)
						}
					}
					tc.pinLiteral(e.Args[0], ch.Elem)
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
					if _, isMove := e.Args[0].(*MoveExpr); !isMove {
						root := variableRoot(e.Args[0])
						if root != "" && !tc.isCopyType(argT) {
							return nil, fmt.Errorf("[%s] cannot move non-copy variable '%s' into channel send without explicit 'move'; use 'move %s'", e.Args[0].Loc(), root, root)
						}
					}
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
					tc.pinLiteral(e.Args[0], PrimitiveType{Name: "i32"})
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
					if root := assignmentRoot(e.Args[0]); root != "" {
						if vi, found := env.LookupVar(root); found && vi.IsView {
							return nil, fmt.Errorf("[%s] cannot store view parameter '%s' into pool", e.At, root)
						}
					}
					tc.pinLiteral(e.Args[0], p.Elem)
					valueType, err := tc.inferExpr(e.Args[0], env)
					if err != nil {
						return nil, err
					}
					if err := tc.assertCompat(p.Elem, valueType, e.At); err != nil {
						return nil, err
					}
					if _, isMove := e.Args[0].(*MoveExpr); !isMove {
						root := variableRoot(e.Args[0])
						if root != "" && !tc.isCopyType(valueType) {
							return nil, fmt.Errorf("[%s] cannot move non-copy variable '%s' into pool spawn without explicit 'move'; use 'move %s'", e.Args[0].Loc(), root, root)
						}
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
					despawnArg := e.Args[0]
					if me, isM := despawnArg.(*MoveExpr); isM {
						despawnArg = me.Value
					}
					if id, ok := despawnArg.(*IdentExpr); ok {
						if err := tc.markMovedIfAffine(id, env); err != nil {
							return nil, err
						}
					}
					return PrimitiveType{Name: "bool"}, nil
				}
			}
			methodKey := fmt.Sprintf("%s.%s", tgtT, mem.Member)
			if sig, ok := tc.Global.Funcs[methodKey]; ok {
				if env.CurModule != "" && sig.Package != "" && sig.Package != env.CurModule && !sig.IsPub {
					return nil, fmt.Errorf("[%s] cannot access unexported method '%s' on '%s' from package '%s'", e.At, mem.Member, tgtT, sig.Package)
				}
				e.ReceiverType = tgtT
				if sig.ReceiverMut {
					if root := assignmentRoot(mem.Target); root != "" {
						if vInfo, found := env.LookupVar(root); found {
							if vInfo.IsView {
								return nil, fmt.Errorf("[%s] cannot call mutating method '%s' on view '%s'", e.At, mem.Member, root)
							}
							if !vInfo.IsMut {
								return nil, fmt.Errorf("[%s] cannot call mutating method '%s' on immutable value '%s'", e.At, mem.Member, root)
							}
						}
					}
				}
				if err := tc.checkCallArgsWithReceiver(mem.Target, sig.ReceiverMut, e.Args, sig.Params, sig.Escaping, env, e.At); err != nil {
					return nil, err
				}
				return sig.RetType, nil
			}
			if ct, ok := tgtT.(CustomType); ok {
				if path, owner, sig, found := tc.findEmbeddedMethod(ct.Name, mem.Member, nil, map[string]bool{}); found && len(path) > 0 {
					mem.EmbedPath = path
					mem.TargetType = CustomType{Name: owner}
					e.ReceiverType = mem.TargetType
					if sig.ReceiverMut {
						if root := assignmentRoot(mem.Target); root != "" {
							if vInfo, found := env.LookupVar(root); found {
								if vInfo.IsView {
									return nil, fmt.Errorf("[%s] cannot call mutating method '%s' on view '%s'", e.At, mem.Member, root)
								}
								if !vInfo.IsMut {
									return nil, fmt.Errorf("[%s] cannot call mutating method '%s' on immutable value '%s'", e.At, mem.Member, root)
								}
							}
						}
					}
					if err := tc.checkCallArgsWithReceiver(mem.Target, sig.ReceiverMut, e.Args, sig.Params, sig.Escaping, env, e.At); err != nil {
						return nil, err
					}
					return sig.RetType, nil
				}
			}
		}
		if id, ok := e.Callee.(*IdentExpr); ok {
			canonical := tc.canonicalName(id.Name)
			if sig, ok := tc.Global.Funcs[canonical]; ok {
				if env.CurModule != "" && sig.Package != "" && sig.Package != env.CurModule && !sig.IsPub {
					return nil, fmt.Errorf("[%s] cannot access unexported function '%s' from package '%s'", e.At, id.Name, sig.Package)
				}
				id.Name = canonical
				if err := tc.checkCallArgs(e.Args, sig.Params, sig.Escaping, env, e.At); err != nil {
					return nil, err
				}
				return sig.RetType, nil
			}
			// Check if it's an unaliased function belonging to an imported package
			for _, pkgName := range tc.ModuleAliases {
				modKey := pkgName + "." + id.Name
				if sig, ok := tc.Global.Funcs[modKey]; ok {
					if sig.Package != "" && env.CurModule != "" && sig.Package != env.CurModule && !sig.IsPub {
						return nil, fmt.Errorf("[%s] cannot access unexported function '%s' from package '%s'", e.At, id.Name, sig.Package)
					}
					id.Name = modKey
					if err := tc.checkCallArgs(e.Args, sig.Params, sig.Escaping, env, e.At); err != nil {
						return nil, err
					}
					return sig.RetType, nil
				}
			}
			return nil, fmt.Errorf("[%s] Undefined function '%s'", e.At, id.Name)
		}
		if mem, ok := e.Callee.(*MemberExpr); ok {
			if tt, terr := tc.inferExpr(mem.Target, env); terr == nil {
				if ct, ok := tt.(CustomType); ok {
					var hints []string
					tc.collectEmbeddedMethodHints(ct.Name, mem.Member, nil, map[string]bool{}, &hints)
					if len(hints) > 0 {
						return nil, fmt.Errorf("[%s] type '%s' has no method '%s' (did you mean '%s'?)",
							e.At, ct.Name, mem.Member, hints[0])
					}
					return nil, fmt.Errorf("[%s] type '%s' has no method '%s'", e.At, ct.Name, mem.Member)
				}
			}
		}
		return nil, fmt.Errorf("[%s] Unsupported call target", e.At)

	case *MemberExpr:
		if id, ok := e.Target.(*IdentExpr); ok {
			if _, isVar := env.LookupVar(id.Name); !isVar {
				if _, isMod := tc.ModuleAliases[id.Name]; isMod {
					if _, sig, found := tc.lookupModuleMember(id.Name, e.Member); found && len(sig.Params) == 0 {
						if sig.Package != "" && env.CurModule != "" && sig.Package != env.CurModule && !sig.IsPub {
							return nil, fmt.Errorf("[%s] cannot access unexported member '%s' from package '%s'", e.At, e.Member, sig.Package)
						}
						return sig.RetType, nil
					}
					return nil, fmt.Errorf("[%s] module '%s' has no member '%s'", e.At, id.Name, e.Member)
				}
			}
			enumName := tc.canonicalName(id.Name)
			if variants, isEnum := tc.Global.Enums[enumName]; isEnum {
				id.Name = enumName
				if meta, hasMeta := tc.Global.EnumMeta[enumName]; hasMeta {
					if env.CurModule != "" && meta.Package != "" && meta.Package != env.CurModule && !meta.IsPub {
						return nil, fmt.Errorf("[%s] cannot access unexported enum '%s' from package '%s'", e.At, enumName, meta.Package)
					}
				}
				for _, variant := range variants {
					if variant.Name == e.Member && len(variant.Types) == 0 {
						return CustomType{Name: enumName}, nil
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
					if env.CurModule != "" && meta.Package != "" && meta.Package != env.CurModule && !match.Field.IsPub {
						return nil, fmt.Errorf("[%s] cannot access unexported field '%s' on struct '%s' from package '%s'", e.At, e.Member, match.Owner, meta.Package)
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
		tc.pinLiteral(e.Index, PrimitiveType{Name: "i32"})
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
		tc.pinLiteral(e.Start, PrimitiveType{Name: "i32"})
		startT, err := tc.inferExpr(e.Start, env)
		if err != nil {
			return nil, err
		}
		if !integerType(startT) {
			return nil, fmt.Errorf("[%s] slice start index must be an integer, got '%s'", e.At, startT)
		}
		tc.pinLiteral(e.End, PrimitiveType{Name: "i32"})
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
			tc.pinLiteral(el, e.Elem)
			t, err := tc.inferExpr(el, env)
			if err != nil {
				return nil, err
			}
			if root := assignmentRoot(el); root != "" {
				if vi, found := env.LookupVar(root); found && vi.IsView {
					if !tc.isCopyType(t) || tc.hasLifetime(t) || variableRoot(el) == root {
						return nil, fmt.Errorf("[%s] cannot store view parameter '%s' into slice element (views are second-class)", el.Loc(), root)
					}
				}
			}
			if _, isMove := el.(*MoveExpr); !isMove {
				root := variableRoot(el)
				if root != "" && !tc.isCopyType(t) {
					return nil, fmt.Errorf("[%s] cannot move non-copy variable '%s' into slice element without explicit 'move'; use 'move %s'", el.Loc(), root, root)
				}
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
			tc.pinLiteral(el, e.Elem)
			t, err := tc.inferExpr(el, env)
			if err != nil {
				return nil, err
			}
			if root := assignmentRoot(el); root != "" {
				if vi, found := env.LookupVar(root); found && vi.IsView {
					if !tc.isCopyType(t) || tc.hasLifetime(t) || variableRoot(el) == root {
						return nil, fmt.Errorf("[%s] cannot store view parameter '%s' into array element (views are second-class)", el.Loc(), root)
					}
				}
			}
			if _, isMove := el.(*MoveExpr); !isMove {
				root := variableRoot(el)
				if root != "" && !tc.isCopyType(t) {
					return nil, fmt.Errorf("[%s] cannot move non-copy variable '%s' into array element without explicit 'move'; use 'move %s'", el.Loc(), root, root)
				}
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
		e.Name = tc.canonicalName(e.Name)
		meta, hasMeta := tc.Global.StructMeta[e.Name]
		if hasMeta {
			if env.CurModule != "" && meta.Package != "" && meta.Package != env.CurModule && !meta.IsPub {
				return nil, fmt.Errorf("[%s] cannot instantiate unexported struct '%s' from package '%s'", e.At, e.Name, meta.Package)
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
			if hasMeta && env.CurModule != "" && meta.Package != "" && meta.Package != env.CurModule && !fieldPubMap[init.Name] {
				return nil, fmt.Errorf("[%s] cannot initialize unexported field '%s' on struct '%s' from package '%s'", init.Val.Loc(), init.Name, e.Name, meta.Package)
			}
			provided[init.Name] = true
			tc.pinLiteral(init.Val, expectedType)
			valT, err := tc.inferExpr(init.Val, env)
			if err != nil {
				return nil, err
			}
			if root := assignmentRoot(init.Val); root != "" {
				if vi, found := env.LookupVar(root); found && vi.IsView {
					if !tc.isCopyType(valT) || tc.hasLifetime(valT) || variableRoot(init.Val) == root {
						return nil, fmt.Errorf("[%s] cannot store view parameter '%s' into struct field '%s' (views are second-class)", init.Val.Loc(), root, init.Name)
					}
				}
			}
			if _, isMove := init.Val.(*MoveExpr); !isMove {
				root := variableRoot(init.Val)
				if root != "" && !tc.isCopyType(valT) {
					return nil, fmt.Errorf("[%s] cannot move non-copy variable '%s' into struct field '%s' without explicit 'move'; use 'move %s'", init.Val.Loc(), root, init.Name, root)
				}
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
