package compiler

import (
	"fmt"
	"strings"
)

type VarInfo struct {
	Type       UbeType
	IsMut      bool
	Depth      int
	ValueDepth int
}

type FuncSignature struct {
	Params  []FieldDecl
	RetType UbeType
}

type TypeEnvironment struct {
	Parent      *TypeEnvironment
	Vars        map[string]VarInfo
	Structs     map[string][]FieldDecl
	Enums       map[string][]VariantDecl
	Funcs       map[string]FuncSignature
	CurRet      UbeType
	LoopDepth   int
	ScopeDepth  int
	UnsafeDepth int
}

func NewTypeEnvironment(parent *TypeEnvironment) *TypeEnvironment {
	return &TypeEnvironment{
		Parent:  parent,
		Vars:    make(map[string]VarInfo),
		Structs: make(map[string][]FieldDecl),
		Enums:   make(map[string][]VariantDecl),
		Funcs:   make(map[string]FuncSignature),
	}
}

func childTypeEnvironment(parent *TypeEnvironment) *TypeEnvironment {
	env := NewTypeEnvironment(parent)
	if parent != nil {
		env.CurRet = parent.CurRet
		env.LoopDepth = parent.LoopDepth
		env.ScopeDepth = parent.ScopeDepth
		env.UnsafeDepth = parent.UnsafeDepth
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

func (tc *Typechecker) hasLifetime(t UbeType) bool {
	return tc.hasLifetimeSeen(t, map[string]bool{})
}

func (tc *Typechecker) hasLifetimeSeen(t UbeType, seen map[string]bool) bool {
	switch v := t.(type) {
	case PointerType, SliceType, StringType:
		return true
	case ArrayType:
		return tc.hasLifetimeSeen(v.Elem, seen)
	case OptionalType:
		return tc.hasLifetimeSeen(v.Inner, seen)
	case ResultType:
		return tc.hasLifetimeSeen(v.Ok, seen) || tc.hasLifetimeSeen(v.Err, seen)
	case CustomType:
		if seen[v.Name] {
			return false
		}
		seen[v.Name] = true
		for _, field := range tc.Global.Structs[v.Name] {
			if tc.hasLifetimeSeen(field.Type, seen) {
				return true
			}
		}
		for _, variant := range tc.Global.Enums[v.Name] {
			for _, fieldType := range variant.Types {
				if tc.hasLifetimeSeen(fieldType, seen) {
					return true
				}
			}
		}
	}
	return false
}

func (tc *Typechecker) exprDepth(expr Expr, env *TypeEnvironment) int {
	switch e := expr.(type) {
	case *IdentExpr:
		if info, ok := env.LookupVar(e.Name); ok {
			return info.ValueDepth
		}
		return 0
	case *LiteralExpr:
		return 0
	case *SliceInitExpr:
		return env.ScopeDepth
	case *ArrayInitExpr:
		depth := 0
		for _, item := range e.Elements {
			if d := tc.exprDepth(item, env); d > depth {
				depth = d
			}
		}
		return depth
	case *UnaryExpr:
		if e.Op == "&" {
			return env.ScopeDepth
		}
		return tc.exprDepth(e.Operand, env)
	case *MemberExpr:
		return tc.exprDepth(e.Target, env)
	case *IndexExpr:
		return tc.exprDepth(e.Target, env)
	case *SliceSubExpr:
		return tc.exprDepth(e.Target, env)
	case *StructInitExpr:
		depth := 0
		for _, field := range e.Fields {
			if d := tc.exprDepth(field.Val, env); d > depth {
				depth = d
			}
		}
		return depth
	case *BinaryExpr:
		if e.Op == "+" {
			if left, err := tc.inferExpr(e.Left, env); err == nil && left.String() == "string" {
				return env.ScopeDepth
			}
		}
		if tc.exprDepth(e.Left, env) > tc.exprDepth(e.Right, env) {
			return tc.exprDepth(e.Left, env)
		}
		return tc.exprDepth(e.Right, env)
	case *OrElseExpr:
		if tc.exprDepth(e.Primary, env) > tc.exprDepth(e.Fallback, env) {
			return tc.exprDepth(e.Primary, env)
		}
		return tc.exprDepth(e.Fallback, env)
	case *TryExpr:
		return tc.exprDepth(e.Operand, env)
	case *ErrorExpr:
		return tc.exprDepth(e.Arg, env)
	case *CallExpr:
		return env.ScopeDepth
	}
	return env.ScopeDepth
}

func NewTypechecker() *Typechecker {
	tc := &Typechecker{Global: NewTypeEnvironment(nil), Externs: make(map[string]bool)}
	tc.Global.Funcs["println"] = FuncSignature{RetType: PrimitiveType{Name: "void"}}
	tc.Global.Funcs["println_str"] = FuncSignature{
		Params:  []FieldDecl{{Name: "s", Type: StringType{}}},
		RetType: PrimitiveType{Name: "void"},
	}
	return tc
}

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

func requireUnsafePointerUse(t UbeType, env *TypeEnvironment, loc SourceLocation) error {
	if _, isPointer := t.(PointerType); isPointer && env.UnsafeDepth == 0 {
		return fmt.Errorf("[%s] Safety Error: using a raw pointer requires an unsafe block", loc)
	}
	return nil
}

func (tc *Typechecker) checkBlock(b *BlockStmt, env *TypeEnvironment) error {
	bEnv := childTypeEnvironment(env)
	bEnv.CurRet = env.CurRet
	bEnv.LoopDepth = env.LoopDepth
	bEnv.ScopeDepth = env.ScopeDepth + 1
	for _, stmt := range b.Stmts {
		if err := tc.checkStmt(stmt, bEnv); err != nil {
			return err
		}
	}
	return nil
}

func (tc *Typechecker) checkStmt(stmt Stmt, env *TypeEnvironment) error {
	switch s := stmt.(type) {
	case *LetStmt:
		valT, err := tc.inferExpr(s.Value, env)
		if err != nil {
			return err
		}
		if s.Type != nil {
			if err := tc.validateType(s.Type, s.At); err != nil {
				return err
			}
			if err := tc.assertCompat(s.Type, valT, s.At); err != nil {
				return err
			}
			valT = s.Type
		}
		env.Vars[s.Name] = VarInfo{Type: valT, IsMut: s.Mut, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Value, env)}
	case *DeferStmt:
		if _, ok := s.Call.(*CallExpr); !ok {
			return fmt.Errorf("[%s] defer requires a function call", s.At)
		}
		_, err := tc.inferExpr(s.Call, env)
		return err

	case *AssignStmt:
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
		if id := assignmentRoot(s.LHS); id != "" {
			if vInfo, found := env.LookupVar(id); found {
				if !vInfo.IsMut {
					return fmt.Errorf("[%s] Cannot assign through immutable variable '%s'", s.At, id)
				}
				if tc.hasLifetime(lhsT) && tc.exprDepth(s.RHS, env) > vInfo.Depth {
					return fmt.Errorf("[%s] Safety Error: value rooted at scope depth %d cannot escape into '%s' at depth %d", s.At, tc.exprDepth(s.RHS, env), id, vInfo.Depth)
				}
				env.updateVar(id, func(v *VarInfo) { v.ValueDepth = tc.exprDepth(s.RHS, env) })
			}
		}
		return tc.assertCompat(lhsT, rhsT, s.At)

	case *ReturnStmt:
		expected := env.CurRet
		if s.Value != nil {
			valT, err := tc.inferExpr(s.Value, env)
			if err != nil {
				return err
			}
			if tc.hasLifetime(valT) && tc.exprDepth(s.Value, env) > 0 {
				return fmt.Errorf("[%s] Safety Error: cannot return a reference rooted in this function's local scope", s.At)
			}
			if resT, ok := expected.(ResultType); ok {
				if _, isErr := s.Value.(*ErrorExpr); isErr {
					return tc.assertCompat(resT.Err, valT, s.At)
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
		loopEnv := childTypeEnvironment(env)
		loopEnv.LoopDepth = env.LoopDepth + 1
		loopEnv.Vars[s.VarName] = VarInfo{Type: cType, IsMut: false, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Call, env)}
		return tc.checkBlock(s.Body, loopEnv)

	case *ForStmt:
		iterT, err := tc.inferExpr(s.Iter, env)
		if err != nil {
			return err
		}
		slT, ok := iterT.(SliceType)
		if !ok {
			return fmt.Errorf("[%s] for-in requires slice type, got '%s'", s.At, iterT)
		}
		loopEnv := childTypeEnvironment(env)
		loopEnv.LoopDepth = env.LoopDepth + 1
		loopEnv.Vars[s.ItemName] = VarInfo{Type: slT.Elem, IsMut: false, Depth: env.ScopeDepth, ValueDepth: tc.exprDepth(s.Iter, env)}
		return tc.checkBlock(s.Body, loopEnv)

	case *ScopeStmt:
		return tc.checkBlock(s.Body, env)
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
		for _, arg := range s.Call.Args {
			argT, _ := tc.inferExpr(arg, env)
			if !argT.IsFlat(tc.Global) {
				return fmt.Errorf("[%s] cannot pass non-flat type '%s' across spawned Vines", s.At, argT)
			}
		}

	case *SelectStmt:
		for _, sc := range s.Cases {
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
		for _, arm := range s.Arms {
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

func assignmentRoot(expr Expr) string {
	switch e := expr.(type) {
	case *IdentExpr:
		return e.Name
	case *MemberExpr:
		return assignmentRoot(e.Target)
	case *IndexExpr:
		return assignmentRoot(e.Target)
	}
	return ""
}

func (tc *Typechecker) inferExpr(expr Expr, env *TypeEnvironment) (UbeType, error) {
	switch e := expr.(type) {
	case *UnsafeExpr:
		tc.HasUnsafe = true
		unsafeEnv := childTypeEnvironment(env)
		unsafeEnv.UnsafeDepth = env.UnsafeDepth + 1
		return tc.inferExpr(e.Value, unsafeEnv)
	case *LiteralExpr:
		return e.Type, nil

	case *IdentExpr:
		if v, ok := env.LookupVar(e.Name); ok {
			if err := requireUnsafePointerUse(v.Type, env, e.At); err != nil {
				return nil, err
			}
			return v.Type, nil
		}
		if _, ok := tc.Global.Enums[e.Name]; ok {
			return CustomType{Name: e.Name}, nil
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
		switch e.Op {
		case "&&", "||":
			if err := tc.assertCompat(PrimitiveType{Name: "bool"}, lT, e.At); err != nil {
				return nil, err
			}
			if err := tc.assertCompat(PrimitiveType{Name: "bool"}, rT, e.At); err != nil {
				return nil, err
			}
			return PrimitiveType{Name: "bool"}, nil
		case "==", "!=", "<", "<=", ">", ">=":
			return PrimitiveType{Name: "bool"}, tc.assertCompat(lT, rT, e.At)
		case "+":
			if lT.String() == "string" || rT.String() == "string" {
				return StringType{}, tc.assertCompat(lT, rT, e.At)
			}
			return lT, tc.assertCompat(lT, rT, e.At)
		default:
			return lT, tc.assertCompat(lT, rT, e.At)
		}

	case *UnaryExpr:
		opndT, err := tc.inferExpr(e.Operand, env)
		if err != nil {
			return nil, err
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
		if id, ok := e.Callee.(*IdentExpr); ok && id.Name == "println" {
			if len(e.Args) != 1 {
				return nil, fmt.Errorf("[%s] println expects one argument", e.At)
			}
			argT, err := tc.inferExpr(e.Args[0], env)
			if err != nil {
				return nil, err
			}
			if !printableType(argT) {
				return nil, fmt.Errorf("[%s] println does not support type '%s'", e.At, argT)
			}
			return PrimitiveType{Name: "void"}, err
		}
		if mem, ok := e.Callee.(*MemberExpr); ok {
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
			if sl, ok := tgtT.(SliceType); ok && mem.Member == "push" {
				if len(e.Args) != 1 {
					return nil, fmt.Errorf("[%s] push expects 1 argument", e.At)
				}
				argT, err := tc.inferExpr(e.Args[0], env)
				if err != nil {
					return nil, err
				}
				return PrimitiveType{Name: "void"}, tc.assertCompat(sl.Elem, argT, e.At)
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
				if err := tc.checkCallArgs(e.Args, sig.Params, env, e.At); err != nil {
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
				if err := tc.checkCallArgs(e.Args, sig.Params, env, e.At); err != nil {
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
		for _, el := range e.Elements {
			t, err := tc.inferExpr(el, env)
			if err != nil {
				return nil, err
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
		return CustomType{Name: e.Name}, nil

	case *TryExpr:
		opndT, err := tc.inferExpr(e.Operand, env)
		if err != nil {
			return nil, err
		}
		if res, ok := opndT.(ResultType); ok {
			return res.Ok, nil
		}
		return nil, fmt.Errorf("[%s] '?' expects Result type, got '%s'", e.At, opndT)

	case *OrElseExpr:
		pT, err := tc.inferExpr(e.Primary, env)
		if err != nil {
			return nil, err
		}
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

func (tc *Typechecker) checkCallArgs(args []Expr, params []FieldDecl, env *TypeEnvironment, loc SourceLocation) error {
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
		if err := tc.assertCompat(params[i].Type, actual, arg.Loc()); err != nil {
			return err
		}
	}
	return nil
}

func (tc *Typechecker) typeFromName(name string) UbeType {
	switch name {
	case "i32", "u32", "i64", "u64", "u8", "f32", "f64", "bool", "void":
		return PrimitiveType{Name: name}
	case "string":
		return StringType{}
	default:
		return CustomType{Name: name}
	}
}

func (tc *Typechecker) assertCompat(expected, actual UbeType, loc SourceLocation) error {
	if expected == nil || actual == nil {
		return nil
	}
	if expected.String() == actual.String() {
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
	case PrimitiveType, StringType:
		return nil
	case PointerType:
		return tc.validateType(v.Base, loc)
	case SliceType:
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

func (tc *Typechecker) Check(prog *Program) error {
	tc.Global.Structs["UbeHandle"] = []FieldDecl{
		{Name: "index", Type: PrimitiveType{Name: "u32"}},
		{Name: "gen", Type: PrimitiveType{Name: "u32"}},
	}

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *StructDecl:
			tc.Global.Structs[d.Name] = d.Fields
		case *EnumDecl:
			tc.Global.Enums[d.Name] = d.Variants
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
			tc.Global.Funcs[fnKey] = FuncSignature{Params: d.Params, RetType: d.RetType}
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

	for _, decl := range prog.Decls {
		if fn, ok := decl.(*FnDecl); ok {
			env := NewTypeEnvironment(tc.Global)
			env.CurRet = fn.RetType
			env.ScopeDepth = 0
			if fn.Receiver != nil {
				env.Vars[fn.Receiver.Name] = VarInfo{Type: fn.Receiver.Type, IsMut: false, Depth: 0, ValueDepth: 0}
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
