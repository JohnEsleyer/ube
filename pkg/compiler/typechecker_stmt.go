package compiler

import "fmt"

func (tc *Typechecker) checkBlock(b *BlockStmt, env *TypeEnvironment) error {
	bEnv := childTypeEnvironment(env)
	bEnv.CurRet = env.CurRet
	bEnv.LoopDepth = env.LoopDepth
	bEnv.ScopeDepth = env.ScopeDepth
	for _, stmt := range b.Stmts {
		if err := tc.checkStmt(stmt, bEnv); err != nil {
			return err
		}
	}
	return nil
}

func (tc *Typechecker) isCopyType(t UbeType) bool {
	if t == nil {
		return true
	}
	switch typ := t.(type) {
	case PrimitiveType, RangeType, ChanType, SliceType:
		return true
	case ArrayType:
		return tc.isCopyType(typ.Elem)
	case OptionalType:
		return tc.isCopyType(typ.Inner)
	case ResultType:
		return tc.isCopyType(typ.Ok) && tc.isCopyType(typ.Err)
	default:
		return false
	}
}

// isMoveOnAssignmentType reports whether a value of this type is linearly
// consumed by `let b = a` / `b = a`. Slices and builders are owned resources
// even though they are passed to ordinary functions as borrowed views for
// backwards compatibility with Ube's existing slice helpers.
func (tc *Typechecker) isMoveOnAssignmentType(t UbeType) bool {
	if t == nil {
		return false
	}
	if !tc.isCopyType(t) {
		return true
	}
	switch t.(type) {
	case SliceType, StringBuilderType:
		return true
	default:
		return false
	}
}

func movedValueError(loc SourceLocation, name string) error {
	return fmt.Errorf("[%s] use of moved value '%s' (use of moved variable '%s')", loc, name, name)
}

// markMovedIfAffine marks an affine resource as consumed by a call, channel
// send, or pool free. It also enforces the loop boundary invariant: a value
// declared outside a loop cannot be moved on one iteration and then be
// missing on the next.
func (tc *Typechecker) markMovedIfAffine(expr Expr, env *TypeEnvironment) error {
	return tc.markMovedIfType(expr, env, func(t UbeType) bool {
		return !tc.isCopyType(t)
	})
}

// markMovedOnAssignment applies move-on-assignment semantics. Slices and
// StringBuilder values are additionally treated as owned resources here, so
// `let b = a` makes `a` statically dead.
func (tc *Typechecker) markMovedOnAssignment(expr Expr, env *TypeEnvironment) error {
	return tc.markMovedIfType(expr, env, tc.isMoveOnAssignmentType)
}

func (tc *Typechecker) markMovedIfType(expr Expr, env *TypeEnvironment, shouldMove func(UbeType) bool) error {
	root := assignmentRoot(expr)
	if root == "" {
		return nil
	}
	info, found := env.LookupVar(root)
	if !found || !shouldMove(info.Type) {
		return nil
	}
	if env.LoopDepth > 0 && info.LoopDepth < env.LoopDepth {
		return fmt.Errorf("[%s] cannot move variable '%s' inside a loop; it was declared outside the loop", expr.Loc(), root)
	}
	env.updateVar(root, func(v *VarInfo) {
		v.Moved = true
	})
	return nil
}

func (tc *Typechecker) checkStmt(stmt Stmt, env *TypeEnvironment) error {
	switch s := stmt.(type) {
	case *LetStmt:
		if _, exists := env.Vars[s.Name]; exists {
			return fmt.Errorf("[%s] variable '%s' already declared in this scope", s.At, s.Name)
		}
		valT, err := tc.inferExpr(s.Value, env)
		if err != nil {
			return err
		}
		s.ValueType = valT
		if s.Type != nil {
			s.Type = tc.resolveType(s.Type)
			if err := tc.validateType(s.Type, s.At); err != nil {
				return err
			}
			if tr, isTrait := s.Type.(TraitType); isTrait {
				if c, isCustom := valT.(CustomType); isCustom && tc.structImplementsTrait(c.Name, tr.Name) {
					s.Value = &TraitCoerceExpr{At: s.Value.Loc(), Value: s.Value, Trait: tr.Name, Concrete: c}
					valT = s.Type
				}
			}
			if targetSlice, isSlice := s.Type.(SliceType); isSlice {
				if arrT, isArr := valT.(ArrayType); isArr && arrT.Elem.String() == targetSlice.Elem.String() {
					s.Value = &ArrayToSliceExpr{At: s.Value.Loc(), ArrayExpr: s.Value, ElemType: arrT.Elem, Size: arrT.Size}
					valT = s.Type
				}
			}
			if err := tc.assertCompat(s.Type, valT, s.At); err != nil {
				return err
			}
			valT = s.Type
		} else {
			if opt, ok := valT.(OptionalType); ok && opt.Inner.String() == "void" {
				return fmt.Errorf("[%s] variable '%s' requires a type annotation when initialized with null", s.At, s.Name)
			}
		}
		valT = valT.WithRegion(env.ScopeDepth)
		if err := tc.markMovedOnAssignment(s.Value, env); err != nil {
			return err
		}
		if tc.hasLifetime(valT) && tc.exprDepth(s.Value, env) > env.ScopeDepth {
			return fmt.Errorf("[%s] Safety Error: value rooted at scope depth %d cannot escape into '%s' at depth %d", s.At, tc.exprDepth(s.Value, env), s.Name, env.ScopeDepth)
		}
		env.Vars[s.Name] = VarInfo{Type: valT, IsMut: s.Mut, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Value, env), LoopDepth: env.LoopDepth, Moved: false}

	case *DeferStmt:
		if _, ok := s.Call.(*CallExpr); !ok {
			return fmt.Errorf("[%s] defer requires a function call", s.At)
		}
		_, err := tc.inferExpr(s.Call, env)
		return err

	case *AssignStmt:
		// If LHS is a plain identifier being reassigned, allow it even if previously moved
		if id, ok := s.LHS.(*IdentExpr); ok {
			if vInfo, found := env.LookupVar(id.Name); found && vInfo.Moved {
				env.updateVar(id.Name, func(v *VarInfo) {
					v.Moved = false
				})
			}
		}
		lhsT, err := tc.inferExpr(s.LHS, env)
		if err != nil {
			return err
		}
		if indexed, ok := s.LHS.(*IndexExpr); ok {
			if _, isString := indexed.TargetType.(StringType); isString {
				return fmt.Errorf("[%s] cannot assign to a string index", s.At)
			}
		}
		rhsT, err := tc.inferExpr(s.RHS, env)
		if err != nil {
			return err
		}
		lhsT = tc.resolveType(lhsT)
		rhsT = tc.resolveType(rhsT)
		s.LHSType = lhsT
		s.RHSType = rhsT
		if s.Op == "" {
			s.Op = "="
		}
		if s.Op != "=" {
			if s.Op == "+=" && lhsT.String() == "string" && rhsT.String() == "string" {
				// string concatenation via +=
			} else if !integerType(lhsT) && lhsT.String() != "f32" && lhsT.String() != "f64" {
				return fmt.Errorf("[%s] compound assignment operator '%s' requires numeric type, got '%s'", s.At, s.Op, lhsT)
			} else {
				if err := tc.assertCompat(lhsT, rhsT, s.At); err != nil {
					return fmt.Errorf("[%s] type mismatch in compound assignment '%s': %w", s.At, s.Op, err)
				}
			}
		}
		if tr, isTrait := lhsT.(TraitType); isTrait {
			if c, isCustom := rhsT.(CustomType); isCustom && tc.structImplementsTrait(c.Name, tr.Name) {
				s.RHS = &TraitCoerceExpr{At: s.RHS.Loc(), Value: s.RHS, Trait: tr.Name, Concrete: c}
				rhsT = tr
			}
		}
		if sl, isSlice := lhsT.(SliceType); isSlice {
			if arrT, isArr := rhsT.(ArrayType); isArr && arrT.Elem.String() == sl.Elem.String() {
				s.RHS = &ArrayToSliceExpr{At: s.RHS.Loc(), ArrayExpr: s.RHS, ElemType: arrT.Elem, Size: arrT.Size}
				rhsT = sl
			}
		}
		if id := assignmentRoot(s.LHS); id != "" {
			if vInfo, found := env.LookupVar(id); found {
				if !vInfo.IsMut {
					return fmt.Errorf("[%s] Cannot assign through immutable variable '%s'", s.At, id)
				}
				if env.ScopeDepth > vInfo.Depth && !vInfo.Type.IsFlat(env) {
					if _, isPromote := s.RHS.(*PromoteExpr); !isPromote {
						return fmt.Errorf("[%s] Safety Error: Outer slice mutation attempted within inner arena at line %d; use promote(...) to export data", s.At, s.At.Line)
					}
				}
				if tc.hasLifetime(lhsT) && tc.exprDepth(s.RHS, env) > vInfo.Depth {
					return fmt.Errorf("[%s] Safety Error: value rooted at scope depth %d cannot escape into '%s' at depth %d", s.At, tc.exprDepth(s.RHS, env), id, vInfo.Depth)
				}
				env.updateVar(id, func(v *VarInfo) {
					v.ValueDepth = tc.exprDepth(s.RHS, env)
					v.Moved = false
				})
			}
		}
		if err := tc.markMovedOnAssignment(s.RHS, env); err != nil {
			return err
		}
		if opt, ok := lhsT.(OptionalType); ok {
			if lit, isNull := s.RHS.(*LiteralExpr); isNull && lit.Val == "null" {
				return nil
			}
			if err := tc.assertCompat(opt.Inner, rhsT, s.At); err == nil {
				return nil
			}
		}
		if res, ok := lhsT.(ResultType); ok {
			if _, isErr := s.RHS.(*ErrorExpr); isErr {
				return tc.assertCompat(res.Err, rhsT, s.At)
			}
			if _, alreadyResult := rhsT.(ResultType); alreadyResult {
				return tc.assertCompat(res, rhsT, s.At)
			}
			return tc.assertCompat(res.Ok, rhsT, s.At)
		}
		return tc.assertCompat(lhsT, rhsT, s.At)

	case *ReturnStmt:
		expected := env.CurRet
		if s.Value != nil {
			valT, err := tc.inferExpr(s.Value, env)
			if err != nil {
				return err
			}
			s.ValueType = valT
			if tc.hasLifetime(valT) && tc.exprDepth(s.Value, env) > 0 {
				return fmt.Errorf("[%s] Safety Error: cannot return a reference rooted in this function's local scope", s.At)
			}
			if targetSlice, isSlice := expected.(SliceType); isSlice {
				if arrT, isArr := valT.(ArrayType); isArr && arrT.Elem.String() == targetSlice.Elem.String() {
					s.Value = &ArrayToSliceExpr{At: s.Value.Loc(), ArrayExpr: s.Value, ElemType: arrT.Elem, Size: arrT.Size}
					valT = expected
				}
			}
			if resT, ok := expected.(ResultType); ok {
				if _, isErr := s.Value.(*ErrorExpr); isErr {
					return tc.assertCompat(resT.Err, valT, s.At)
				}
				if _, alreadyResult := valT.(ResultType); alreadyResult {
					return tc.assertCompat(expected, valT, s.At)
				}
				return tc.assertCompat(resT.Ok, valT, s.At)
			} else if optT, ok := expected.(OptionalType); ok {
				if optT.Inner.String() != "void" {
					if _, isNull := s.Value.(*LiteralExpr); !isNull {
						if _, alreadyOptional := valT.(OptionalType); alreadyOptional {
							return tc.assertCompat(expected, valT, s.At)
						}
						return tc.assertCompat(optT.Inner, valT, s.At)
					}
				}
			} else {
				return tc.assertCompat(expected, valT, s.At)
			}
		}

	case *IfStmt:
		condT, err := tc.inferExpr(s.Cond, env)
		if err != nil {
			return err
		}
		if err := tc.assertCompat(PrimitiveType{Name: "bool"}, condT, s.At); err != nil {
			return err
		}
		thenEnv := childTypeEnvironment(env)
		for k, v := range env.Vars {
			thenEnv.Vars[k] = v
		}
		if err := tc.checkBlock(s.Then, thenEnv); err != nil {
			return err
		}
		if s.Else != nil {
			elseEnv := childTypeEnvironment(env)
			for k, v := range env.Vars {
				elseEnv.Vars[k] = v
			}
			if blk, ok := s.Else.(*BlockStmt); ok {
				if err := tc.checkBlock(blk, elseEnv); err != nil {
					return err
				}
			} else {
				if err := tc.checkStmt(s.Else, elseEnv); err != nil {
					return err
				}
			}
			// If a variable was moved in either branch, reconcile in parent
			for name, tInfo := range thenEnv.Vars {
				if tInfo.Moved {
					env.updateVar(name, func(v *VarInfo) { v.Moved = true })
				}
			}
			for name, eInfo := range elseEnv.Vars {
				if eInfo.Moved {
					env.updateVar(name, func(v *VarInfo) { v.Moved = true })
				}
			}
		} else {
			for name, tInfo := range thenEnv.Vars {
				if tInfo.Moved {
					env.updateVar(name, func(v *VarInfo) { v.Moved = true })
				}
			}
		}

	case *IfLetStmt:
		optT, err := tc.inferExpr(s.Expr, env)
		if err != nil {
			return err
		}
		oType, ok := optT.(OptionalType)
		if !ok {
			return fmt.Errorf("[%s] if-let requires optional type, got '%s'", s.At, optT)
		}
		thenEnv := childTypeEnvironment(env)
		thenEnv.Vars[s.VarName] = VarInfo{Type: oType.Inner, IsMut: false, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Expr, env), LoopDepth: thenEnv.LoopDepth}
		if err := tc.checkBlock(s.Then, thenEnv); err != nil {
			return err
		}
		if s.Else != nil {
			if blk, ok := s.Else.(*BlockStmt); ok {
				return tc.checkBlock(blk, env)
			}
			return tc.checkStmt(s.Else, env)
		}

	case *WhileStmt:
		condT, err := tc.inferExpr(s.Cond, env)
		if err != nil {
			return err
		}
		if err := tc.assertCompat(PrimitiveType{Name: "bool"}, condT, s.At); err != nil {
			return err
		}
		loopEnv := childTypeEnvironment(env)
		loopEnv.LoopDepth = env.LoopDepth + 1
		return tc.checkBlock(s.Body, loopEnv)

	case *WhileLetStmt:
		cType, err := tc.inferExpr(s.Call, env)
		if err != nil {
			return err
		}
		var unwrappedType UbeType = cType
		isChanRecv := false
		if call, ok := s.Call.(*CallExpr); ok {
			if mem, isMem := call.Callee.(*MemberExpr); isMem && mem.Member == "recv" {
				if _, isChan := mem.TargetType.(ChanType); isChan {
					isChanRecv = true
				}
			}
		}
		if opt, ok := cType.(OptionalType); ok {
			unwrappedType = opt.Inner
		} else if !isChanRecv {
			return fmt.Errorf("[%s] while-let requires an optional type or channel recv(), got '%s'", s.At, cType)
		}
		loopEnv := childTypeEnvironment(env)
		loopEnv.LoopDepth = env.LoopDepth + 1
		loopEnv.Vars[s.VarName] = VarInfo{Type: unwrappedType, IsMut: false, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Call, env), LoopDepth: loopEnv.LoopDepth}
		return tc.checkBlock(s.Body, loopEnv)

	case *ForStmt:
		iterT, err := tc.inferExpr(s.Iter, env)
		if err != nil {
			return err
		}
		var elemT UbeType
		switch it := iterT.(type) {
		case SliceType:
			elemT = it.Elem
		case ArrayType:
			elemT = it.Elem
		case RangeType:
			elemT = it.Elem
		default:
			return fmt.Errorf("[%s] for-in requires slice, array, or range (start..end), got '%s'", s.At, iterT)
		}
		s.IterType = iterT
		loopEnv := childTypeEnvironment(env)
		loopEnv.LoopDepth = env.LoopDepth + 1
		loopEnv.Vars[s.ItemName] = VarInfo{Type: elemT, IsMut: false, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Iter, env), LoopDepth: loopEnv.LoopDepth}
		return tc.checkBlock(s.Body, loopEnv)

	case *ArenaStmt:
		scopeEnv := childTypeEnvironment(env)
		scopeEnv.ScopeDepth = env.ScopeDepth + 1
		return tc.checkBlock(s.Body, scopeEnv)

	case *UnsafeStmt:
		return fmt.Errorf("[%s] 'unsafe' is forbidden: Ube is 100%% sandboxed WebAssembly", s.At)

	case *LoopControlStmt:
		if env.LoopDepth == 0 {
			return fmt.Errorf("[%s] %s is only valid inside a loop", s.At, map[bool]string{true: "continue", false: "break"}[s.Continue])
		}
		return nil

	case *SpawnStmt:
		_, err := tc.inferExpr(s.Call, env)
		if err != nil {
			return err
		}
		if s.Call.ReceiverType != nil && !s.Call.ReceiverType.IsFlat(tc.Global) {
			return fmt.Errorf("[%s] cannot pass non-flat receiver type '%s' across spawned Vines", s.At, s.Call.ReceiverType)
		}
		s.Call.ArgTypes = nil
		for _, arg := range s.Call.Args {
			argT, err := tc.inferExpr(arg, env)
			if err != nil {
				return err
			}
			if !argT.IsFlat(tc.Global) {
				return fmt.Errorf("[%s] cannot pass non-flat type '%s' across spawned Vines", s.At, argT)
			}
			s.Call.ArgTypes = append(s.Call.ArgTypes, argT)
		}

	case *SelectStmt:
		if len(s.Cases) == 0 && s.Default == nil {
			return fmt.Errorf("[%s] select requires at least one case", s.At)
		}
		for i := range s.Cases {
			sc := &s.Cases[i]
			chT, err := tc.inferExpr(sc.Chan, env)
			if err != nil {
				return err
			}
			chanType, ok := chT.(ChanType)
			if !ok {
				return fmt.Errorf("[%s] select case must receive from Chan, got '%s'", sc.At, chT)
			}
			sc.ChanElem = chanType.Elem
			caseEnv := childTypeEnvironment(env)
			caseEnv.Vars[sc.VarName] = VarInfo{Type: chanType.Elem, IsMut: false, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(sc.Chan, env), LoopDepth: caseEnv.LoopDepth}
			if err := tc.checkBlock(sc.Body, caseEnv); err != nil {
				return err
			}
		}
		if s.Default != nil {
			if err := tc.checkBlock(s.Default, env); err != nil {
				return err
			}
		}

	case *MatchStmt:
		mType, err := tc.inferExpr(s.Expr, env)
		if err != nil {
			return err
		}
		custom, ok := mType.(CustomType)
		if !ok {
			return fmt.Errorf("[%s] match target must be enum, got '%s'", s.At, mType)
		}
		s.EnumName = custom.Name
		vars, ok := tc.Global.Enums[custom.Name]
		if !ok {
			return fmt.Errorf("[%s] unknown enum '%s'", s.At, custom.Name)
		}
		vMap := make(map[string][]UbeType)
		for _, v := range vars {
			vMap[v.Name] = v.Types
		}
		seenVariants := make(map[string]bool)
		for _, arm := range s.Arms {
			if seenVariants[arm.Variant] {
				return fmt.Errorf("[%s] duplicate match pattern '%s'", arm.At, arm.Variant)
			}
			seenVariants[arm.Variant] = true

			if arm.Variant == "_" {
				armEnv := childTypeEnvironment(env)
				if blk, ok := arm.Body.(*BlockStmt); ok {
					if err := tc.checkBlock(blk, armEnv); err != nil {
						return err
					}
				} else {
					if err := tc.checkStmt(arm.Body, armEnv); err != nil {
						return err
					}
				}
				continue
			}

			expected, exists := vMap[arm.Variant]
			if !exists {
				return fmt.Errorf("[%s] enum '%s' has no variant '%s'", arm.At, custom.Name, arm.Variant)
			}
			if len(expected) != len(arm.Bindings) {
				return fmt.Errorf("[%s] variant '%s' expects %d bindings, got %d", arm.At, arm.Variant, len(expected), len(arm.Bindings))
			}
			armEnv := childTypeEnvironment(env)
			for i, b := range arm.Bindings {
				armEnv.Vars[b] = VarInfo{Type: expected[i], IsMut: false, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Expr, env), LoopDepth: armEnv.LoopDepth}
			}
			if blk, ok := arm.Body.(*BlockStmt); ok {
				if err := tc.checkBlock(blk, armEnv); err != nil {
					return err
				}
			} else {
				if err := tc.checkStmt(arm.Body, armEnv); err != nil {
					return err
				}
			}
		}

	case *ExprStmt:
		_, err := tc.inferExpr(s.Expr, env)
		return err
	}
	return nil
}
