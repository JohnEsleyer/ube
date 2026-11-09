// File: pkg/compiler/typechecker_stmt.go

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
	return t.IsCopy()
}

func (tc *Typechecker) isSendableAcrossVines(t UbeType) bool {
	if t == nil {
		return true
	}
	if t.IsFlat(tc.Global) {
		return true
	}
	if ch, ok := t.(ChanType); ok {
		return ch.Elem.IsFlat(tc.Global)
	}
	return false
}

func movedValueError(loc SourceLocation, name string) error {
	return fmt.Errorf("[%s] use of moved value '%s' (use of moved variable '%s')", loc, name, name)
}

// markMovedIfAffine marks an affine resource as consumed by a call, assignment,
// channel send, or pool despawn. It also enforces the loop boundary invariant:
// a value declared outside a loop cannot be moved on one iteration and then be
// missing on the next.
func (tc *Typechecker) markMovedIfAffine(expr Expr, env *TypeEnvironment) error {
	return tc.markMovedIfType(expr, env, func(t UbeType) bool {
		return !tc.isCopyType(t)
	})
}

func (tc *Typechecker) markMovedIfType(expr Expr, env *TypeEnvironment, shouldMove func(UbeType) bool) error {
	// A projection such as `value.field` or `items[i]` reads a component; it
	// must not consume the whole aggregate. Only whole-value expressions
	// participate in move tracking.
	switch expr.(type) {
	case *MemberExpr, *IndexExpr, *SliceSubExpr:
		return nil
	}
	root := assignmentRoot(expr)
	if root == "" {
		return nil
	}
	info, found := env.LookupVar(root)
	if !found || !shouldMove(info.Type) {
		return nil
	}
	if info.IsView {
		return fmt.Errorf("[%s] cannot move from view parameter '%s'", expr.Loc(), root)
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
	case *BlockStmt:
		return tc.checkBlock(s, env)
	case *LetStmt:
		if s.Type != nil {
			s.Type = tc.resolveType(s.Type)
			tc.pinLiteral(s.Value, s.Type)
		}
		valT, err := tc.inferExpr(s.Value, env)
		if err != nil {
			return err
		}
		s.ValueType = valT

		if len(s.Names) > 0 {
			resolvedValT := tc.resolveType(valT)
			var elemTypes []UbeType
			if tupleT, ok := resolvedValT.(TupleType); ok {
				elemTypes = tupleT.Elems
			} else if customT, ok := resolvedValT.(CustomType); ok {
				if fields, ok := tc.Global.Structs[customT.Name]; ok {
					for _, f := range fields {
						elemTypes = append(elemTypes, f.Type)
					}
				} else {
					return fmt.Errorf("[%s] destructuring requires a tuple or struct, got '%s'", s.At, valT)
				}
			} else {
				return fmt.Errorf("[%s] destructuring requires a tuple or struct value, got '%s'", s.At, valT)
			}

			if len(elemTypes) != len(s.Names) {
				return fmt.Errorf("[%s] destructuring expects %d values, got %d", s.At, len(elemTypes), len(s.Names))
			}
			if s.Type != nil {
				if tupAnnot, ok := tc.resolveType(s.Type).(TupleType); ok {
					if len(tupAnnot.Elems) != len(s.Names) {
						return fmt.Errorf("[%s] destructuring type annotation expects %d types, got %d", s.At, len(s.Names), len(tupAnnot.Elems))
					}
					for i, expectedT := range tupAnnot.Elems {
						if err := tc.assertCompat(expectedT, elemTypes[i], s.At); err != nil {
							return fmt.Errorf("[%s] destructuring type mismatch at element %d: %w", s.At, i, err)
						}
					}
				} else {
					return fmt.Errorf("[%s] destructuring type annotation must be a tuple type", s.At)
				}
			}
			if _, isMove := s.Value.(*MoveExpr); !isMove {
				root := variableRoot(s.Value)
				if root != "" && !tc.isCopyType(valT) {
					if _, isCall := s.Value.(*CallExpr); !isCall {
						return fmt.Errorf("[%s] cannot move non-copy variable '%s' without explicit 'move'; use 'move %s'", s.At, root, root)
					}
				}
			}
			if err := tc.markMovedIfAffine(s.Value, env); err != nil {
				return err
			}
			for i, name := range s.Names {
				if _, exists := env.Vars[name]; exists {
					return fmt.Errorf("[%s] variable '%s' already declared in this scope", s.At, name)
				}
				elemT := elemTypes[i].WithRegion(env.ScopeDepth)
				if tc.hasLifetime(elemT) && tc.exprDepth(s.Value, env) > env.ScopeDepth {
					return fmt.Errorf("[%s] Safety Error: value rooted at scope depth %d cannot escape into '%s' at depth %d", s.At, tc.exprDepth(s.Value, env), name, env.ScopeDepth)
				}
				env.Vars[name] = VarInfo{Type: elemT, IsMut: s.Mut, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Value, env), LoopDepth: env.LoopDepth, Moved: false}
			}
			return nil
		}

		if _, exists := env.Vars[s.Name]; exists {
			return fmt.Errorf("[%s] variable '%s' already declared in this scope", s.At, s.Name)
		}
		if s.Type != nil {
			if err := tc.validateType(s.Type, s.At); err != nil {
				return err
			}
			if tr, isTrait := s.Type.(TraitType); isTrait {
				if c, isCustom := valT.(CustomType); isCustom && tc.structImplementsTrait(c.Name, tr.Name) {
					s.Value = &TraitCoerceExpr{At: s.Value.Loc(), Value: s.Value, Trait: tr.Name, Concrete: c}
					valT = s.Type
				}
			}
			if sl, isSlice := s.Type.(SliceType); isSlice {
				if arr, isArr := valT.(ArrayType); isArr && tc.assertCompat(sl.Elem, arr.Elem, s.Value.Loc()) == nil {
					s.Value = &ArrayToSliceExpr{At: s.Value.Loc(), Value: s.Value, ArrayType: arr}
					valT = s.Type
				}
			}
			if err := tc.assertCompat(s.Type, valT, s.At); err != nil {
				return err
			}
			valT = s.Type
		} else {
			if opt, ok := valT.(OptionalType); ok && opt.Inner.String() == "void" {
				return fmt.Errorf("[%s] variable '%s' requires a type annotation when initialized with none", s.At, s.Name)
			}
		}
		if _, isMove := s.Value.(*MoveExpr); !isMove {
			root := variableRoot(s.Value)
			if root != "" && !tc.isCopyType(valT) {
				if _, isCall := s.Value.(*CallExpr); !isCall {
					return fmt.Errorf("[%s] cannot move non-copy variable '%s' without explicit 'move'; use 'move %s'", s.At, root, root)
				}
			}
		}
		isViewVal := false
		if root := assignmentRoot(s.Value); root != "" {
			if vi, found := env.LookupVar(root); found && vi.IsView && (!tc.isCopyType(valT) || tc.hasLifetime(valT)) {
				isViewVal = true
			}
		}
		if isViewVal && s.Mut {
			return fmt.Errorf("[%s] cannot create mutable binding '%s' from view", s.At, s.Name)
		}
		valT = valT.WithRegion(env.ScopeDepth)
		if err := tc.markMovedIfAffine(s.Value, env); err != nil {
			return err
		}
		if tc.hasLifetime(valT) && tc.exprDepth(s.Value, env) > env.ScopeDepth {
			return fmt.Errorf("[%s] Safety Error: value rooted at scope depth %d cannot escape into '%s' at depth %d", s.At, tc.exprDepth(s.Value, env), s.Name, env.ScopeDepth)
		}
		env.Vars[s.Name] = VarInfo{Type: valT, IsMut: s.Mut, IsView: isViewVal, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Value, env), LoopDepth: env.LoopDepth, Moved: false}
	case *DeferStmt:
		if _, ok := s.Call.(*CallExpr); !ok {
			return fmt.Errorf("[%s] defer requires a function call", s.At)
		}
		_, err := tc.inferExpr(s.Call, env)
		return err

	case *AssignStmt:
		if id := assignmentRoot(s.LHS); id != "" {
			if vInfo, found := env.LookupVar(id); found && vInfo.IsView {
				return fmt.Errorf("[%s] cannot assign to immutable view '%s'", s.At, id)
			}
		}
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
		lhsT = tc.resolveType(lhsT)
		tc.pinLiteral(s.RHS, lhsT)
		rhsT, err := tc.inferExpr(s.RHS, env)
		if err != nil {
			return err
		}
		rhsT = tc.resolveType(rhsT)
		s.LHSType = lhsT
		s.RHSType = rhsT
		if s.Op == "" {
			s.Op = "="
		}
		if s.Op == "=" {
			if _, isMove := s.RHS.(*MoveExpr); !isMove {
				root := variableRoot(s.RHS)
				if root != "" && !tc.isCopyType(rhsT) {
					return fmt.Errorf("[%s] cannot move non-copy variable '%s' without explicit 'move'; use 'move %s'", s.At, root, root)
				}
			}
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
			if arr, isArr := rhsT.(ArrayType); isArr && tc.assertCompat(sl.Elem, arr.Elem, s.RHS.Loc()) == nil {
				s.RHS = &ArrayToSliceExpr{At: s.RHS.Loc(), Value: s.RHS, ArrayType: arr}
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
		if err := tc.markMovedIfAffine(s.RHS, env); err != nil {
			return err
		}
		if opt, ok := lhsT.(OptionalType); ok {
			if lit, isNone := s.RHS.(*LiteralExpr); isNone && lit.Val == "none" {
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
			if id, ok := s.Value.(*IdentExpr); ok {
				if vi, found := env.LookupVar(id.Name); found && vi.IsView {
					return fmt.Errorf("[%s] cannot return view parameter '%s' (views cannot escape the call frame)", s.At, id.Name)
				}
			}
			if expected != nil {
				tc.pinLiteral(s.Value, expected)
			}
			valT, err := tc.inferExpr(s.Value, env)
			if err != nil {
				return err
			}
			s.ValueType = valT
			if root := assignmentRoot(s.Value); root != "" {
				if vi, found := env.LookupVar(root); found && vi.IsView {
					if !tc.isCopyType(valT) || tc.hasLifetime(valT) {
						return fmt.Errorf("[%s] cannot return view parameter '%s' (views cannot escape the call frame)", s.At, root)
					}
				}
			}
			if tc.hasLifetime(valT) && tc.exprDepth(s.Value, env) > 0 {
				return fmt.Errorf("[%s] Safety Error: cannot return a reference rooted in this function's local scope", s.At)
			}
			if sl, isSlice := expected.(SliceType); isSlice {
				if arr, isArr := valT.(ArrayType); isArr && tc.assertCompat(sl.Elem, arr.Elem, s.Value.Loc()) == nil {
					s.Value = &ArrayToSliceExpr{At: s.Value.Loc(), Value: s.Value, ArrayType: arr}
					s.ValueType = expected
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
					if lit, isNone := s.Value.(*LiteralExpr); !isNone || lit.Val != "none" {
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
		var unwrapped UbeType
		switch t := optT.(type) {
		case OptionalType:
			unwrapped = t.Inner
		case ResultType:
			// `if let x = result` binds the success value; the else branch
			// is taken when the result carries an error.
			unwrapped = t.Ok
			s.IsResult = true
		default:
			return fmt.Errorf("[%s] if-let requires optional or result type, got '%s'", s.At, optT)
		}
		s.VarType = unwrapped
		thenEnv := childTypeEnvironment(env)
		thenEnv.Vars[s.VarName] = VarInfo{Type: unwrapped, IsMut: false, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Expr, env), LoopDepth: thenEnv.LoopDepth}
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
		if !s.IsArena && tc.canAutoArenaLoop(s.Body, env.LoopDepth+1, env) {
			s.IsArena = true
		}
		if s.IsArena && hasPromoteInBlock(s.Body) {
			return fmt.Errorf("[%s] cannot use 'promote' inside an arena loop (loop iterations are reclaimed each iteration)", s.At)
		}
		loopEnv := childTypeEnvironment(env)
		loopEnv.LoopDepth = env.LoopDepth + 1
		if s.IsArena {
			loopEnv.ScopeDepth = env.ScopeDepth + 1
		}
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
		s.VarType = unwrappedType
		if !s.IsArena && tc.canAutoArenaLoop(s.Body, env.LoopDepth+1, env) {
			s.IsArena = true
		}
		if s.IsArena && hasPromoteInBlock(s.Body) {
			return fmt.Errorf("[%s] cannot use 'promote' inside an arena loop (loop iterations are reclaimed each iteration)", s.At)
		}
		loopEnv := childTypeEnvironment(env)
		loopEnv.LoopDepth = env.LoopDepth + 1
		if s.IsArena {
			loopEnv.ScopeDepth = env.ScopeDepth + 1
		}
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
			// Custom iterator protocol:
			//   fn (mut self) next() -> T?
			typeName := iterT.String()
			sig, found := tc.Global.Funcs[typeName+".next"]
			if !found {
				base := typeBaseName(iterT)
				if base != "" && base != typeName {
					sig, found = tc.Global.Funcs[base+".next"]
					if found {
						typeName = base
					}
				}
			}
			if !found {
				return fmt.Errorf("[%s] cannot iterate over '%s': expected a slice, array, range, or a type implementing 'fn next(mut self) -> T?'", s.At, iterT)
			}
			if len(sig.Params) != 0 {
				return fmt.Errorf("[%s] iterator method '%s.next' must take no arguments", s.At, typeName)
			}
			opt, isOpt := sig.RetType.(OptionalType)
			if !isOpt {
				return fmt.Errorf("[%s] iterator method '%s.next' must return an optional type (got '%s')", s.At, typeName, sig.RetType)
			}
			elemT = opt.Inner
			s.IsCustomIterator = true
			s.NextMethod = typeName + "_next"
			s.ReceiverIsMut = sig.ReceiverMut
		}
		s.IterType = iterT
		s.ItemType = elemT
		if !s.IsArena && tc.canAutoArenaLoop(s.Body, env.LoopDepth+1, env) {
			s.IsArena = true
		}
		if s.IsArena && hasPromoteInBlock(s.Body) {
			return fmt.Errorf("[%s] cannot use 'promote' inside an arena loop (loop iterations are reclaimed each iteration)", s.At)
		}
		loopEnv := childTypeEnvironment(env)
		loopEnv.LoopDepth = env.LoopDepth + 1
		if s.IsArena {
			loopEnv.ScopeDepth = env.ScopeDepth + 1
		}
		isItemView := false
		if iterRoot := assignmentRoot(s.Iter); iterRoot != "" {
			if vi, found := env.LookupVar(iterRoot); found && vi.IsView && (!tc.isCopyType(elemT) || tc.hasLifetime(elemT)) {
				isItemView = true
			}
		}
		loopEnv.Vars[s.ItemName] = VarInfo{Type: elemT, IsMut: false, IsView: isItemView, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Iter, env), LoopDepth: loopEnv.LoopDepth}
		return tc.checkBlock(s.Body, loopEnv)

	case *ArenaStmt:
		scopeEnv := childTypeEnvironment(env)
		scopeEnv.ScopeDepth = env.ScopeDepth + 1
		return tc.checkBlock(s.Body, scopeEnv)

	case *LoopControlStmt:
		if env.LoopDepth == 0 {
			return fmt.Errorf("[%s] %s is only valid inside a loop", s.At, map[bool]string{true: "continue", false: "break"}[s.Continue])
		}
		return nil

	case *SpawnStmt:
		if s.Call != nil {
			if mem, ok := s.Call.Callee.(*MemberExpr); ok {
				if root := assignmentRoot(mem.Target); root != "" {
					if vi, found := env.LookupVar(root); found && vi.IsView {
						return fmt.Errorf("[%s] cannot capture view parameter '%s' in spawn task", s.At, root)
					}
				}
			}
			for _, arg := range s.Call.Args {
				if root := assignmentRoot(arg); root != "" {
					if vi, found := env.LookupVar(root); found && vi.IsView {
						return fmt.Errorf("[%s] cannot capture view parameter '%s' in spawn task", s.At, root)
					}
				}
			}
		}
		_, err := tc.inferExpr(s.Call, env)
		if err != nil {
			return err
		}
		if s.Call.ReceiverType != nil && !tc.isSendableAcrossVines(s.Call.ReceiverType) {
			return fmt.Errorf("[%s] cannot pass non-flat receiver type '%s' across spawned Vines", s.At, s.Call.ReceiverType)
		}
		s.Call.ArgTypes = nil
		for _, arg := range s.Call.Args {
			argT, err := tc.inferExpr(arg, env)
			if err != nil {
				return err
			}
			if !tc.isSendableAcrossVines(argT) {
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

func hasPromoteInExpr(e Expr) bool {
	if e == nil {
		return false
	}
	switch ex := e.(type) {
	case *PromoteExpr:
		return true
	case *BinaryExpr:
		return hasPromoteInExpr(ex.Left) || hasPromoteInExpr(ex.Right)
	case *UnaryExpr:
		return hasPromoteInExpr(ex.Operand)
	case *CallExpr:
		for _, a := range ex.Args {
			if hasPromoteInExpr(a) {
				return true
			}
		}
	case *MemberExpr:
		return hasPromoteInExpr(ex.Target)
	case *IndexExpr:
		return hasPromoteInExpr(ex.Target) || hasPromoteInExpr(ex.Index)
	case *SliceSubExpr:
		return hasPromoteInExpr(ex.Target) || hasPromoteInExpr(ex.Start) || hasPromoteInExpr(ex.End)
	case *StructInitExpr:
		for _, f := range ex.Fields {
			if hasPromoteInExpr(f.Val) {
				return true
			}
		}
	case *TupleExpr:
		for _, el := range ex.Elems {
			if hasPromoteInExpr(el) {
				return true
			}
		}
	case *ArrayInitExpr:
		for _, el := range ex.Elements {
			if hasPromoteInExpr(el) {
				return true
			}
		}
	case *SliceInitExpr:
		for _, el := range ex.Elements {
			if hasPromoteInExpr(el) {
				return true
			}
		}
	case *TryExpr:
		return hasPromoteInExpr(ex.Operand)
	case *OrElseExpr:
		return hasPromoteInExpr(ex.Primary) || hasPromoteInExpr(ex.Fallback)
	case *IfExpr:
		return hasPromoteInBlock(ex.Then) || hasPromoteInBlock(ex.Else)
	}
	return false
}

func hasPromoteInBlock(b *BlockStmt) bool {
	if b == nil {
		return false
	}
	for _, stmt := range b.Stmts {
		switch s := stmt.(type) {
		case *BlockStmt:
			if hasPromoteInBlock(s) {
				return true
			}
		case *LetStmt:
			if hasPromoteInExpr(s.Value) {
				return true
			}
		case *AssignStmt:
			if hasPromoteInExpr(s.RHS) {
				return true
			}
		case *ReturnStmt:
			if hasPromoteInExpr(s.Value) {
				return true
			}
		case *ExprStmt:
			if hasPromoteInExpr(s.Expr) {
				return true
			}
		case *IfStmt:
			if hasPromoteInExpr(s.Cond) || hasPromoteInBlock(s.Then) {
				return true
			}
			if s.Else != nil {
				if blk, ok := s.Else.(*BlockStmt); ok && hasPromoteInBlock(blk) {
					return true
				}
			}
		case *IfLetStmt:
			if hasPromoteInExpr(s.Expr) || hasPromoteInBlock(s.Then) {
				return true
			}
			if s.Else != nil {
				if blk, ok := s.Else.(*BlockStmt); ok && hasPromoteInBlock(blk) {
					return true
				}
			}
		}
	}
	return false
}

func exprHasTier2Alloc(expr Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *InterpolatedStringExpr, *SliceInitExpr, *SliceSubExpr:
		return true
	case *BinaryExpr:
		if e.Op == "+" && ((e.Type != nil && e.Type.String() == "string") || (e.OperandType != nil && e.OperandType.String() == "string")) {
			return true
		}
		return exprHasTier2Alloc(e.Left) || exprHasTier2Alloc(e.Right)
	case *UnaryExpr:
		return exprHasTier2Alloc(e.Operand)
	case *CallExpr:
		if id, ok := e.Callee.(*IdentExpr); ok {
			if id.Name == "int_to_str" || id.Name == "float_to_str" || id.Name == "f64_to_str" || id.Name == "f32_to_str" {
				return true
			}
		}
		if mem, ok := e.Callee.(*MemberExpr); ok {
			if mem.Member == "to_string" || mem.Member == "read_to_string" {
				return true
			}
		}
		for _, arg := range e.Args {
			if exprHasTier2Alloc(arg) {
				return true
			}
		}
	case *MemberExpr:
		return exprHasTier2Alloc(e.Target)
	case *IndexExpr:
		return exprHasTier2Alloc(e.Target) || exprHasTier2Alloc(e.Index)
	case *StructInitExpr:
		for _, f := range e.Fields {
			if exprHasTier2Alloc(f.Val) {
				return true
			}
		}
	case *TupleExpr:
		for _, el := range e.Elems {
			if exprHasTier2Alloc(el) {
				return true
			}
		}
	case *ArrayInitExpr:
		for _, el := range e.Elements {
			if exprHasTier2Alloc(el) {
				return true
			}
		}
	case *TryExpr:
		return exprHasTier2Alloc(e.Operand)
	case *OrElseExpr:
		return exprHasTier2Alloc(e.Primary) || exprHasTier2Alloc(e.Fallback)
	}
	return false
}

func stmtHasTier2Alloc(stmt Stmt) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case *BlockStmt:
		for _, st := range s.Stmts {
			if stmtHasTier2Alloc(st) {
				return true
			}
		}
	case *LetStmt:
		return exprHasTier2Alloc(s.Value)
	case *AssignStmt:
		return exprHasTier2Alloc(s.RHS)
	case *ExprStmt:
		return exprHasTier2Alloc(s.Expr)
	case *IfStmt:
		return exprHasTier2Alloc(s.Cond) || stmtHasTier2Alloc(s.Then) || stmtHasTier2Alloc(s.Else)
	case *IfLetStmt:
		return exprHasTier2Alloc(s.Expr) || stmtHasTier2Alloc(s.Then) || stmtHasTier2Alloc(s.Else)
	case *WhileStmt:
		return exprHasTier2Alloc(s.Cond) || stmtHasTier2Alloc(s.Body)
	case *WhileLetStmt:
		return exprHasTier2Alloc(s.Call) || stmtHasTier2Alloc(s.Body)
	case *ForStmt:
		return exprHasTier2Alloc(s.Iter) || stmtHasTier2Alloc(s.Body)
	case *MatchStmt:
		for _, arm := range s.Arms {
			if stmtHasTier2Alloc(arm.Body) {
				return true
			}
		}
	}
	return false
}

func (tc *Typechecker) canAutoArenaLoop(body *BlockStmt, loopDepth int, env *TypeEnvironment) bool {
	if body == nil || len(body.Stmts) == 0 {
		return false
	}
	if !stmtHasTier2Alloc(body) {
		return false
	}
	if tc.loopBodyEscapes(body, loopDepth, env) {
		return false
	}
	return true
}

func (tc *Typechecker) loopBodyEscapes(body *BlockStmt, loopDepth int, env *TypeEnvironment) bool {
	if body == nil {
		return false
	}
	for _, stmt := range body.Stmts {
		switch s := stmt.(type) {
		case *BlockStmt:
			if tc.loopBodyEscapes(s, loopDepth, env) {
				return true
			}
		case *AssignStmt:
			root := assignmentRoot(s.LHS)
			if root != "" {
				if vi, found := env.LookupVar(root); found {
					if vi.LoopDepth < loopDepth {
						return true
					}
				}
			}
		case *ExprStmt:
			if call, ok := s.Expr.(*CallExpr); ok {
				if mem, isMem := call.Callee.(*MemberExpr); isMem {
					root := assignmentRoot(mem.Target)
					if root != "" {
						if vi, found := env.LookupVar(root); found && vi.LoopDepth < loopDepth {
							return true
						}
					}
				}
				for _, arg := range call.Args {
					root := assignmentRoot(arg)
					if root != "" {
						if vi, found := env.LookupVar(root); found && vi.LoopDepth < loopDepth {
							return true
						}
					}
				}
			}
		case *ReturnStmt:
			if s.Value != nil {
				return true
			}
		case *SpawnStmt:
			return true
		case *ArenaStmt:
			return true
		case *IfStmt:
			if s.Then != nil && tc.loopBodyEscapes(s.Then, loopDepth, env) {
				return true
			}
			if s.Else != nil {
				if blk, ok := s.Else.(*BlockStmt); ok && tc.loopBodyEscapes(blk, loopDepth, env) {
					return true
				}
			}
		case *IfLetStmt:
			if s.Then != nil && tc.loopBodyEscapes(s.Then, loopDepth, env) {
				return true
			}
			if s.Else != nil {
				if blk, ok := s.Else.(*BlockStmt); ok && tc.loopBodyEscapes(blk, loopDepth, env) {
					return true
				}
			}
		case *ForStmt:
			if tc.loopBodyEscapes(s.Body, loopDepth, env) {
				return true
			}
		case *WhileStmt:
			if tc.loopBodyEscapes(s.Body, loopDepth, env) {
				return true
			}
		case *WhileLetStmt:
			if tc.loopBodyEscapes(s.Body, loopDepth, env) {
				return true
			}
		}
	}
	return false
}
