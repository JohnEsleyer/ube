package compiler

import "fmt"

type VarInfo struct {
	Type       UbeType
	IsMut      bool
	Depth      int
	ValueDepth int
	Moved      bool
}

type FuncSignature struct {
	Params      []FieldDecl
	RetType     UbeType
	Escaping    []bool
	ReceiverMut bool
}

type TypeEnvironment struct {
	Parent         *TypeEnvironment
	Vars           map[string]VarInfo
	Structs        map[string][]FieldDecl
	Enums          map[string][]VariantDecl
	Traits         map[string]*TraitDecl
	Funcs          map[string]FuncSignature
	GenericStructs map[string]*StructDecl
	GenericFuncs   map[string]*FnDecl
	CurRet         UbeType
	LoopDepth      int
	ScopeDepth     int
	UnsafeDepth    int
}

func NewTypeEnvironment(parent *TypeEnvironment) *TypeEnvironment {
	return &TypeEnvironment{
		Parent:         parent,
		Vars:           make(map[string]VarInfo),
		Structs:        make(map[string][]FieldDecl),
		Enums:          make(map[string][]VariantDecl),
		Traits:         make(map[string]*TraitDecl),
		Funcs:          make(map[string]FuncSignature),
		GenericStructs: make(map[string]*StructDecl),
		GenericFuncs:   make(map[string]*FnDecl),
	}
}

func childTypeEnvironment(parent *TypeEnvironment) *TypeEnvironment {
	env := NewTypeEnvironment(parent)
	if parent != nil {
		env.CurRet = parent.CurRet
		env.LoopDepth = parent.LoopDepth
		env.ScopeDepth = parent.ScopeDepth
		env.UnsafeDepth = parent.UnsafeDepth
		env.Traits = parent.Traits
		env.GenericStructs = parent.GenericStructs
		env.GenericFuncs = parent.GenericFuncs
	}
	return env
}

func (e *TypeEnvironment) LookupVar(name string) (VarInfo, bool) {
	if v, ok := e.Vars[name]; ok {
		return v, true
	}
	if e.Parent != nil {
		return e.Parent.LookupVar(name)
	}
	return VarInfo{}, false
}

func (e *TypeEnvironment) updateVar(name string, update func(*VarInfo)) bool {
	if _, ok := e.Vars[name]; ok {
		value := e.Vars[name]
		update(&value)
		e.Vars[name] = value
		return true
	}
	if e.Parent != nil {
		return e.Parent.updateVar(name, update)
	}
	return false
}

type Typechecker struct {
	Global    *TypeEnvironment
	Externs   map[string]bool
	HasUnsafe bool
}

func NewTypechecker() *Typechecker {
	tc := &Typechecker{Global: NewTypeEnvironment(nil), Externs: make(map[string]bool)}
	void := PrimitiveType{Name: "void"}
	i32 := PrimitiveType{Name: "i32"}
	i64 := PrimitiveType{Name: "i64"}
	boolType := PrimitiveType{Name: "bool"}
	str := StringType{}
	tc.Global.Funcs["println"] = FuncSignature{RetType: void}
	tc.Global.Funcs["println_str"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}}, RetType: void}
	tc.Global.Funcs["print"] = FuncSignature{RetType: void}
	tc.Global.Funcs["time_now_ms"] = FuncSignature{RetType: i64}
	tc.Global.Funcs["sleep_ms"] = FuncSignature{Params: []FieldDecl{{Name: "ms", Type: i32}}, RetType: void}
	tc.Global.Funcs["yield"] = FuncSignature{RetType: void}
	tc.Global.Funcs["exit_process"] = FuncSignature{Params: []FieldDecl{{Name: "code", Type: i32}}, RetType: void}
	tc.Global.Funcs["int_to_str"] = FuncSignature{Params: []FieldDecl{{Name: "val", Type: nil}}, RetType: str}
	tc.Global.Funcs["str_to_int"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}}, RetType: ResultType{Ok: i64, Err: str}}
	tc.Global.Funcs["read_file"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: ResultType{Ok: str, Err: str}}
	tc.Global.Funcs["write_file"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}, {Name: "content", Type: str}}, RetType: ResultType{Ok: boolType, Err: str}}
	tc.Global.Funcs["os_args"] = FuncSignature{RetType: SliceType{Elem: str}}
	tc.Global.Funcs["env_get"] = FuncSignature{Params: []FieldDecl{{Name: "key", Type: str}}, RetType: OptionalType{Inner: str}}
	tc.Global.Funcs["file_exists"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: boolType}
	tc.Global.Funcs["path_join"] = FuncSignature{Params: []FieldDecl{{Name: "a", Type: str}, {Name: "b", Type: str}}, RetType: str}
	tc.Global.Funcs["str_replace"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "old_s", Type: str}, {Name: "new_s", Type: str}}, RetType: str}
	tc.Global.Funcs["str_contains"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "substr", Type: str}}, RetType: boolType}
	tc.Global.Funcs["str_starts_with"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "prefix", Type: str}}, RetType: boolType}
	tc.Global.Funcs["str_ends_with"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "suffix", Type: str}}, RetType: boolType}
	tc.Global.Funcs["str_trim"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}}, RetType: str}
	tc.Global.Funcs["str_index_of"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "substr", Type: str}}, RetType: i32}
	tc.Global.Funcs["str_split"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "delim", Type: str}}, RetType: SliceType{Elem: str}}
	tc.Global.Funcs["dir_list"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: ResultType{Ok: SliceType{Elem: str}, Err: str}}
	tc.Global.Funcs["dir_create"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: ResultType{Ok: boolType, Err: str}}
	tc.Global.Funcs["file_remove"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: ResultType{Ok: boolType, Err: str}}
	tc.Global.Funcs["env_set"] = FuncSignature{Params: []FieldDecl{{Name: "key", Type: str}, {Name: "val", Type: str}}, RetType: boolType}
	tc.Global.Funcs["cmd_run"] = FuncSignature{Params: []FieldDecl{{Name: "cmd", Type: str}}, RetType: i32}
	tc.Global.Funcs["abs"] = FuncSignature{Params: []FieldDecl{{Name: "n", Type: i64}}, RetType: i64}
	tc.Global.Funcs["min"] = FuncSignature{Params: []FieldDecl{{Name: "a", Type: i64}, {Name: "b", Type: i64}}, RetType: i64}
	tc.Global.Funcs["max"] = FuncSignature{Params: []FieldDecl{{Name: "a", Type: i64}, {Name: "b", Type: i64}}, RetType: i64}
	tc.Global.Funcs["sqrt"] = FuncSignature{Params: []FieldDecl{{Name: "f", Type: PrimitiveType{Name: "f64"}}}, RetType: PrimitiveType{Name: "f64"}}
	tc.Global.Funcs["pow"] = FuncSignature{Params: []FieldDecl{{Name: "base", Type: PrimitiveType{Name: "f64"}}, {Name: "exp", Type: PrimitiveType{Name: "f64"}}}, RetType: PrimitiveType{Name: "f64"}}
	tc.Global.Funcs["floor"] = FuncSignature{Params: []FieldDecl{{Name: "f", Type: PrimitiveType{Name: "f64"}}}, RetType: PrimitiveType{Name: "f64"}}
	tc.Global.Funcs["ceil"] = FuncSignature{Params: []FieldDecl{{Name: "f", Type: PrimitiveType{Name: "f64"}}}, RetType: PrimitiveType{Name: "f64"}}

	// StringBuilder receiver methods
	tc.Global.Funcs["format"] = FuncSignature{RetType: str}
	tc.Global.Funcs["StringBuilder.append"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}}, RetType: void}
	tc.Global.Funcs["StringBuilder.append_str"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}}, RetType: void}
	tc.Global.Funcs["StringBuilder.append_int"] = FuncSignature{Params: []FieldDecl{{Name: "val", Type: i64}}, RetType: void}
	tc.Global.Funcs["StringBuilder.append_i64"] = FuncSignature{Params: []FieldDecl{{Name: "val", Type: i64}}, RetType: void}
	tc.Global.Funcs["StringBuilder.append_char"] = FuncSignature{Params: []FieldDecl{{Name: "val", Type: i32}}, RetType: void}
	tc.Global.Funcs["StringBuilder.append_bool"] = FuncSignature{Params: []FieldDecl{{Name: "val", Type: boolType}}, RetType: void}
	tc.Global.Funcs["StringBuilder.to_string"] = FuncSignature{RetType: str}

	return tc
}

func (tc *Typechecker) resolveType(t UbeType) UbeType {
	if t == nil {
		return nil
	}
	switch v := t.(type) {
	case CustomType:
		if _, isTrait := tc.Global.Traits[v.Name]; isTrait {
			return TraitType{Name: v.Name}
		}
		return v
	case SliceType:
		return SliceType{Elem: tc.resolveType(v.Elem)}
	case ArrayType:
		return ArrayType{Elem: tc.resolveType(v.Elem), Size: v.Size}
	case OptionalType:
		return OptionalType{Inner: tc.resolveType(v.Inner)}
	case ResultType:
		return ResultType{Ok: tc.resolveType(v.Ok), Err: tc.resolveType(v.Err)}
	case ChanType:
		return ChanType{Elem: tc.resolveType(v.Elem)}
	case PoolType:
		return PoolType{Elem: tc.resolveType(v.Elem)}
	case PointerType:
		return PointerType{Base: tc.resolveType(v.Base)}
	default:
		return t
	}
}

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
	switch t.(type) {
	case PrimitiveType, RangeType, ChanType, SliceType:
		return true
	default:
		return false
	}
}

func (tc *Typechecker) markMovedIfAffine(expr Expr, env *TypeEnvironment) {
	if id, ok := expr.(*IdentExpr); ok {
		if info, found := env.LookupVar(id.Name); found && !tc.isCopyType(info.Type) {
			env.updateVar(id.Name, func(v *VarInfo) {
				v.Moved = true
			})
		}
	}
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
		tc.markMovedIfAffine(s.Value, env)
		if tc.hasLifetime(valT) && tc.exprDepth(s.Value, env) > env.ScopeDepth {
			return fmt.Errorf("[%s] Safety Error: value rooted at scope depth %d cannot escape into '%s' at depth %d", s.At, tc.exprDepth(s.Value, env), s.Name, env.ScopeDepth)
		}
		env.Vars[s.Name] = VarInfo{Type: valT, IsMut: s.Mut, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Value, env), Moved: false}

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
		tc.markMovedIfAffine(s.RHS, env)
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
		if err := tc.checkBlock(s.Then, env); err != nil {
			return err
		}
		if s.Else != nil {
			if blk, ok := s.Else.(*BlockStmt); ok {
				return tc.checkBlock(blk, env)
			}
			return tc.checkStmt(s.Else, env)
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
		thenEnv.Vars[s.VarName] = VarInfo{Type: oType.Inner, IsMut: false, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Expr, env)}
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
		loopEnv.Vars[s.VarName] = VarInfo{Type: unwrappedType, IsMut: false, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Call, env)}
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
		loopEnv.Vars[s.ItemName] = VarInfo{Type: elemT, IsMut: false, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Iter, env)}
		return tc.checkBlock(s.Body, loopEnv)

	case *ScopeStmt:
		scopeEnv := childTypeEnvironment(env)
		scopeEnv.ScopeDepth = env.ScopeDepth + 1
		return tc.checkBlock(s.Body, scopeEnv)

	case *UnsafeStmt:
		tc.HasUnsafe = true
		unsafeEnv := childTypeEnvironment(env)
		unsafeEnv.UnsafeDepth = env.UnsafeDepth + 1
		return tc.checkBlock(s.Body, unsafeEnv)

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
			caseEnv.Vars[sc.VarName] = VarInfo{Type: chanType.Elem, IsMut: false, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(sc.Chan, env)}
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
				armEnv.Vars[b] = VarInfo{Type: expected[i], IsMut: false, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Expr, env)}
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

func (tc *Typechecker) Check(prog *Program) error {
	tc.Global.Structs["UbeHandle"] = []FieldDecl{
		{Name: "index", Type: PrimitiveType{Name: "u32"}},
		{Name: "gen", Type: PrimitiveType{Name: "u32"}},
	}

	// 1. Monomorphization Pass for Generics
	if err := tc.monomorphize(prog); err != nil {
		return err
	}

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *StructDecl:
			tc.Global.Structs[d.Name] = d.Fields
		case *EnumDecl:
			tc.Global.Enums[d.Name] = d.Variants
		case *TraitDecl:
			tc.Global.Traits[d.Name] = d
		}
	}

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *StructDecl:
			for _, f := range d.Fields {
				if err := tc.validateType(f.Type, d.At); err != nil {
					return err
				}
			}
		case *EnumDecl:
			for _, v := range d.Variants {
				for _, vt := range v.Types {
					if err := tc.validateType(vt, d.At); err != nil {
						return err
					}
				}
			}
		case *TraitDecl:
			for _, m := range d.Methods {
				for _, p := range m.Params {
					if err := tc.validateType(p.Type, m.At); err != nil {
						return err
					}
				}
				if err := tc.validateType(m.RetType, m.At); err != nil {
					return err
				}
			}
		}
	}

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *FnDecl:
			if d.Receiver != nil {
				if err := tc.validateType(d.Receiver.Type, d.At); err != nil {
					return err
				}
			}
			for _, p := range d.Params {
				if err := tc.validateType(p.Type, d.At); err != nil {
					return err
				}
			}
			if err := tc.validateType(d.RetType, d.At); err != nil {
				return err
			}

			fnKey := d.Name
			if d.Receiver != nil {
				fnKey = fmt.Sprintf("%s.%s", d.Receiver.Type, d.Name)
			}
			tc.Global.Funcs[fnKey] = FuncSignature{
				Params:      d.Params,
				RetType:     d.RetType,
				ReceiverMut: d.Receiver != nil && d.Receiver.IsMut,
			}
		case *ExternFnDecl:
			for _, p := range d.Params {
				if err := tc.validateType(p.Type, d.At); err != nil {
					return err
				}
			}
			if err := tc.validateType(d.RetType, d.At); err != nil {
				return err
			}
			tc.Global.Funcs[d.Name] = FuncSignature{Params: d.Params, RetType: d.RetType}
			tc.Externs[d.Name] = true
		}
	}

	tc.computeEscapingParams(prog)

	for _, decl := range prog.Decls {
		if fn, ok := decl.(*FnDecl); ok {
			env := NewTypeEnvironment(tc.Global)
			env.CurRet = fn.RetType
			env.ScopeDepth = 0
			if fn.Receiver != nil {
				env.Vars[fn.Receiver.Name] = VarInfo{Type: fn.Receiver.Type, IsMut: fn.Receiver.IsMut, Depth: 0, ValueDepth: 0}
			}
			for _, p := range fn.Params {
				env.Vars[p.Name] = VarInfo{Type: p.Type, IsMut: false, Depth: 0, ValueDepth: 0}
			}
			if err := tc.checkBlock(fn.Body, env); err != nil {
				return err
			}
		}
	}
	return nil
}
