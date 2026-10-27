package compiler

import (
	"fmt"
	"strings"
)

type VarInfo struct {
	Type  UbeType
	IsMut bool
}

type FuncSignature struct {
	Params  []FieldDecl
	RetType UbeType
}

type TypeEnvironment struct {
	Parent  *TypeEnvironment
	Vars    map[string]VarInfo
	Structs map[string][]FieldDecl
	Enums   map[string][]VariantDecl
	Funcs   map[string]FuncSignature
	CurRet  UbeType
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

func (e *TypeEnvironment) LookupVar(name string) (VarInfo, bool) {
	if v, ok := e.Vars[name]; ok {
		return v, true
	}
	if e.Parent != nil {
		return e.Parent.LookupVar(name)
	}
	return VarInfo{}, false
}

type Typechecker struct {
	Global *TypeEnvironment
}

func NewTypechecker() *Typechecker {
	tc := &Typechecker{Global: NewTypeEnvironment(nil)}
	tc.Global.Funcs["println"] = FuncSignature{
		Params:  []FieldDecl{{Name: "v", Type: PrimitiveType{Name: "i32"}}},
		RetType: PrimitiveType{Name: "void"},
	}
	tc.Global.Funcs["println_str"] = FuncSignature{
		Params:  []FieldDecl{{Name: "s", Type: StringType{}}},
		RetType: PrimitiveType{Name: "void"},
	}
	tc.Global.Funcs["error"] = FuncSignature{
		Params:  []FieldDecl{{Name: "msg", Type: StringType{}}},
		RetType: PrimitiveType{Name: "void"},
	}
	return tc
}

func (tc *Typechecker) checkBlock(b *BlockStmt, env *TypeEnvironment) error {
	bEnv := NewTypeEnvironment(env)
	bEnv.CurRet = env.CurRet
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
		env.Vars[s.Name] = VarInfo{Type: valT, IsMut: s.Mut}

	case *AssignStmt:
		lhsT, err := tc.inferExpr(s.LHS, env)
		if err != nil {
			return err
		}
		rhsT, err := tc.inferExpr(s.RHS, env)
		if err != nil {
			return err
		}
		if id, ok := s.LHS.(*IdentExpr); ok {
			if vInfo, found := env.LookupVar(id.Name); found && !vInfo.IsMut {
				return fmt.Errorf("[%s] Cannot assign to immutable variable '%s'", s.At, id.Name)
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
			if resT, ok := expected.(ResultType); ok {
				if _, isErr := s.Value.(*CallExpr); !isErr {
					return tc.assertCompat(resT.Ok, valT, s.At)
				}
			} else if optT, ok := expected.(OptionalType); ok {
				if optT.Inner.String() != "void" {
					if _, isNull := s.Value.(*LiteralExpr); !isNull {
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
		thenEnv := NewTypeEnvironment(env)
		thenEnv.CurRet = env.CurRet
		thenEnv.Vars[s.VarName] = VarInfo{Type: oType.Inner, IsMut: false}
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
		return tc.checkBlock(s.Body, env)

	case *WhileLetStmt:
		cType, err := tc.inferExpr(s.Call, env)
		if err != nil {
			return err
		}
		loopEnv := NewTypeEnvironment(env)
		loopEnv.CurRet = env.CurRet
		loopEnv.Vars[s.VarName] = VarInfo{Type: cType, IsMut: false}
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
		loopEnv := NewTypeEnvironment(env)
		loopEnv.CurRet = env.CurRet
		loopEnv.Vars[s.ItemName] = VarInfo{Type: slT.Elem, IsMut: false}
		return tc.checkBlock(s.Body, loopEnv)

	case *TuberStmt:
		return tc.checkBlock(s.Body, env)

	case *SpawnStmt:
		_, err := tc.inferExpr(s.Call, env)
		if err != nil {
			return err
		}
		for _, arg := range s.Call.Args {
			argT, _ := tc.inferExpr(arg, env)
			if !argT.IsFlat(tc.Global) {
				return fmt.Errorf("[%s] cannot pass non-flat type '%s' across Vines", s.At, argT)
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
			armEnv := NewTypeEnvironment(env)
			armEnv.CurRet = env.CurRet
			for i, b := range arm.Bindings {
				armEnv.Vars[b] = VarInfo{Type: expected[i], IsMut: false}
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

func (tc *Typechecker) inferExpr(expr Expr, env *TypeEnvironment) (UbeType, error) {
	switch e := expr.(type) {
	case *LiteralExpr:
		return e.Type, nil

	case *IdentExpr:
		if v, ok := env.LookupVar(e.Name); ok {
			return v.Type, nil
		}
		if _, ok := tc.Global.Enums[e.Name]; ok {
			return CustomType{Name: e.Name}, nil
		}
		if strings.HasPrefix(e.Name, "Chan[") && strings.HasSuffix(e.Name, "]") {
			elemName := e.Name[5 : len(e.Name)-1]
			var elemT UbeType
			switch elemName {
			case "i32", "u32", "i64", "u64", "u8", "f32", "f64", "bool", "void":
				elemT = PrimitiveType{Name: elemName}
			case "string":
				elemT = StringType{}
			default:
				elemT = CustomType{Name: elemName}
			}
			chT := ChanType{Elem: elemT}
			if err := tc.validateType(chT, e.At); err != nil {
				return nil, err
			}
			return chT, nil
		}
		return nil, fmt.Errorf("[%s] Undefined identifier '%s'", e.At, e.Name)

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
			return PointerType{Base: opndT}, nil
		}
		return opndT, nil

	case *CallExpr:
		if mem, ok := e.Callee.(*MemberExpr); ok {
			if id, ok := mem.Target.(*IdentExpr); ok {
				if _, ok := tc.Global.Enums[id.Name]; ok {
					return CustomType{Name: id.Name}, nil
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
					return ch, nil
				}
				if mem.Member == "recv" {
					return ch.Elem, nil
				}
				if mem.Member == "send" || mem.Member == "close" {
					return PrimitiveType{Name: "void"}, nil
				}
			}
			// Receiver functions
			methodKey := fmt.Sprintf("%s.%s", tgtT, mem.Member)
			if sig, ok := tc.Global.Funcs[methodKey]; ok {
				return sig.RetType, nil
			}
		}
		if id, ok := e.Callee.(*IdentExpr); ok {
			if sig, ok := tc.Global.Funcs[id.Name]; ok {
				return sig.RetType, nil
			}
		}
		return PrimitiveType{Name: "void"}, nil

	case *MemberExpr:
		if e.Member == "ptr" {
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
			return sl.Elem, nil
		}
		return nil, fmt.Errorf("[%s] Type '%s' does not support indexing", e.At, tgtT)

	case *SliceInitExpr:
		return SliceType{Elem: e.Elem}, nil

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

func (tc *Typechecker) assertCompat(expected, actual UbeType, loc SourceLocation) error {
	if expected.String() == actual.String() {
		return nil
	}
	if opt, ok := expected.(OptionalType); ok {
		if actOpt, ok := actual.(OptionalType); ok && actOpt.Inner.String() == "void" {
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
		if !v.Elem.IsFlat(tc.Global) {
			return fmt.Errorf("[%s] Chan element type '%s' must be flat", loc, v.Elem)
		}
		return nil
	case CustomType:
		return nil
	default:
		return nil
	}
}

func (tc *Typechecker) Check(prog *Program) error {
	// Pass 1: Types & Declarations
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *StructDecl:
			tc.Global.Structs[d.Name] = d.Fields
		case *EnumDecl:
			tc.Global.Enums[d.Name] = d.Variants
		}
	}

	// Validate struct fields and enum variants
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

	// Pass 2: Function Signatures
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
		}
	}

	// Pass 3: Bodies
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*FnDecl); ok {
			env := NewTypeEnvironment(tc.Global)
			env.CurRet = fn.RetType
			if fn.Receiver != nil {
				env.Vars[fn.Receiver.Name] = VarInfo{Type: fn.Receiver.Type, IsMut: false}
			}
			for _, p := range fn.Params {
				env.Vars[p.Name] = VarInfo{Type: p.Type, IsMut: false}
			}
			if err := tc.checkBlock(fn.Body, env); err != nil {
				return err
			}
		}
	}
	return nil
}
