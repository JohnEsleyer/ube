package compiler

import (
	"fmt"
	"strings"
)

// Monomorphizer turns user-defined generic declarations into concrete,
// specialized declarations before type checking and C code generation. This
// keeps the runtime free of boxing, dynamic type information, and garbage
// collection for static generics.
type Monomorphizer struct {
	genericStructs map[string]*StructDecl
	genericFuncs   map[string]*FnDecl
	genericMethods map[string][]*FnDecl
	traits         map[string]*TraitDecl
	concreteStruct map[string]*StructDecl
	concreteFunc   map[string]*FnDecl
	allMethods     map[string][]*FnDecl
	instantiated   map[string]bool
}

func NewMonomorphizer() *Monomorphizer {
	return &Monomorphizer{
		genericStructs: make(map[string]*StructDecl),
		genericFuncs:   make(map[string]*FnDecl),
		genericMethods: make(map[string][]*FnDecl),
		traits:         make(map[string]*TraitDecl),
		concreteStruct: make(map[string]*StructDecl),
		concreteFunc:   make(map[string]*FnDecl),
		allMethods:     make(map[string][]*FnDecl),
		instantiated:   make(map[string]bool),
	}
}

// Monomorphize is the entry point used by the driver and the LSP.
func Monomorphize(prog *Program) error {
	if prog == nil {
		return nil
	}
	return NewMonomorphizer().Process(prog)
}

func (m *Monomorphizer) Process(prog *Program) error {
	var concreteDecls []Decl

	// First pass: register every declaration kind before resolving receiver
	// methods, so declaration order does not affect generic detection.
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *TraitDecl:
			m.traits[d.Name] = d
		case *StructDecl:
			if len(d.TypeParams) > 0 {
				m.genericStructs[d.Name] = d
			} else {
				m.concreteStruct[d.Name] = d
			}
		case *FnDecl:
			if d.Receiver == nil {
				if len(d.TypeParams) > 0 {
					m.genericFuncs[d.Name] = d
				} else {
					m.concreteFunc[d.Name] = d
				}
			}
		}
	}
	for _, decl := range prog.Decls {
		if d, ok := decl.(*FnDecl); ok && d.Receiver != nil {
			base := typeBaseName(d.Receiver.Type)
			m.allMethods[base] = append(m.allMethods[base], d)
		}
	}

	// Second pass: split declarations into concrete declarations and templates.
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *TraitDecl:
			concreteDecls = append(concreteDecls, d)
		case *StructDecl:
			if len(d.TypeParams) == 0 {
				concreteDecls = append(concreteDecls, d)
			}
		case *FnDecl:
			if d.Receiver != nil {
				base := typeBaseName(d.Receiver.Type)
				if _, isGenericStruct := m.genericStructs[base]; isGenericStruct || len(d.TypeParams) > 0 {
					m.genericMethods[base] = append(m.genericMethods[base], d)
					continue
				}
			}
			if len(d.TypeParams) > 0 {
				continue
			}
			concreteDecls = append(concreteDecls, d)
		default:
			concreteDecls = append(concreteDecls, d)
		}
	}

	for {
		pending := make([]Decl, 0)
		before := len(m.instantiated)

		var scanType func(UbeType) (UbeType, error)
		var scanExpr func(Expr) error
		var scanStmt func(Stmt) error
		var scanDecls func([]Decl) error

		scanType = func(t UbeType) (UbeType, error) {
			if t == nil {
				return nil, nil
			}
			switch v := t.(type) {
			case CustomType:
				if _, isTrait := m.traits[v.Name]; isTrait {
					return TraitType{Name: v.Name}, nil
				}
				return v, nil
			case TraitType:
				return v, nil
			case GenericInstanceType:
				args := make([]UbeType, len(v.TypeArgs))
				for i, arg := range v.TypeArgs {
					ct, err := scanType(arg)
					if err != nil {
						return nil, err
					}
					args[i] = ct
				}
				if _, known := m.genericStructs[v.Base]; known {
					name, _, err := m.specializeStruct(v.Base, args, &pending)
					if err != nil {
						return nil, err
					}
					return CustomType{Name: name}, nil
				}
				return GenericInstanceType{Base: v.Base, TypeArgs: args}, nil
			case SliceType:
				el, err := scanType(v.Elem)
				if err != nil {
					return nil, err
				}
				return SliceType{Elem: el}, nil
			case ArrayType:
				el, err := scanType(v.Elem)
				if err != nil {
					return nil, err
				}
				return ArrayType{Elem: el, Size: v.Size}, nil
			case OptionalType:
				inner, err := scanType(v.Inner)
				if err != nil {
					return nil, err
				}
				return OptionalType{Inner: inner}, nil
			case ResultType:
				okT, err := scanType(v.Ok)
				if err != nil {
					return nil, err
				}
				errT, err := scanType(v.Err)
				if err != nil {
					return nil, err
				}
				return ResultType{Ok: okT, Err: errT}, nil
			case ChanType:
				el, err := scanType(v.Elem)
				if err != nil {
					return nil, err
				}
				return ChanType{Elem: el}, nil
			case PoolType:
				el, err := scanType(v.Elem)
				if err != nil {
					return nil, err
				}
				return PoolType{Elem: el}, nil
			case PointerType:
				base, err := scanType(v.Base)
				if err != nil {
					return nil, err
				}
				return PointerType{Base: base}, nil
			}
			return t, nil
		}

		scanExpr = func(expr Expr) error {
			if expr == nil {
				return nil
			}
			switch e := expr.(type) {
			case *StructInitExpr:
				if len(e.TypeArgs) > 0 {
					args := make([]UbeType, len(e.TypeArgs))
					for i, arg := range e.TypeArgs {
						ct, err := scanType(arg)
						if err != nil {
							return err
						}
						args[i] = ct
					}
					name, _, err := m.specializeStruct(e.Name, args, &pending)
					if err != nil {
						return err
					}
					e.Name = name
					e.TypeArgs = nil
				}
				for i := range e.Fields {
					if err := scanExpr(e.Fields[i].Val); err != nil {
						return err
					}
				}
			case *CallExpr:
				if len(e.TypeArgs) > 0 {
					if id, ok := e.Callee.(*IdentExpr); ok {
						if _, isGeneric := m.genericFuncs[id.Name]; isGeneric {
							args := make([]UbeType, len(e.TypeArgs))
							for i, arg := range e.TypeArgs {
								ct, err := scanType(arg)
								if err != nil {
									return err
								}
								args[i] = ct
							}
							name, _, err := m.specializeFunc(id.Name, args, &pending)
							if err != nil {
								return err
							}
							id.Name = name
							e.TypeArgs = nil
						}
					}
				}
				if err := scanExpr(e.Callee); err != nil {
					return err
				}
				for _, a := range e.Args {
					if err := scanExpr(a); err != nil {
						return err
					}
				}
			case *BinaryExpr:
				if err := scanExpr(e.Left); err != nil {
					return err
				}
				return scanExpr(e.Right)
			case *UnaryExpr:
				return scanExpr(e.Operand)
			case *MemberExpr:
				return scanExpr(e.Target)
			case *IndexExpr:
				if err := scanExpr(e.Target); err != nil {
					return err
				}
				return scanExpr(e.Index)
			case *SliceSubExpr:
				if err := scanExpr(e.Target); err != nil {
					return err
				}
				if err := scanExpr(e.Start); err != nil {
					return err
				}
				return scanExpr(e.End)
			case *ArrayInitExpr:
				for _, el := range e.Elements {
					if err := scanExpr(el); err != nil {
						return err
					}
				}
			case *SliceInitExpr:
				for _, el := range e.Elements {
					if err := scanExpr(el); err != nil {
						return err
					}
				}
			case *InterpolatedStringExpr:
				for _, part := range e.Parts {
					if err := scanExpr(part); err != nil {
						return err
					}
				}
			case *TraitCoerceExpr:
				ct, err := scanType(e.Concrete)
				if err != nil {
					return err
				}
				e.Concrete = ct
				return scanExpr(e.Value)
			case *ArrayToSliceExpr:
				ct, err := scanType(e.ElemType)
				if err != nil {
					return err
				}
				e.ElemType = ct
				return scanExpr(e.ArrayExpr)
			case *GenericTypeExpr:
				elem, err := scanType(e.ElemType)
				if err != nil {
					return err
				}
				e.ElemType = elem
			case *OrElseExpr:
				if err := scanExpr(e.Primary); err != nil {
					return err
				}
				return scanExpr(e.Fallback)
			case *TryExpr:
				return scanExpr(e.Operand)
			case *ErrorExpr:
				return scanExpr(e.Arg)
			case *UnsafeExpr:
				return scanExpr(e.Value)
			}
			return nil
		}

		scanStmt = func(stmt Stmt) error {
			if stmt == nil {
				return nil
			}
			switch s := stmt.(type) {
			case *BlockStmt:
				if s == nil {
					return nil
				}
				for _, st := range s.Stmts {
					if err := scanStmt(st); err != nil {
						return err
					}
				}
			case *LetStmt:
				if s.Type != nil {
					t, err := scanType(s.Type)
					if err != nil {
						return err
					}
					s.Type = t
				}
				return scanExpr(s.Value)
			case *AssignStmt:
				if err := scanExpr(s.LHS); err != nil {
					return err
				}
				return scanExpr(s.RHS)
			case *ReturnStmt:
				return scanExpr(s.Value)
			case *IfStmt:
				if err := scanExpr(s.Cond); err != nil {
					return err
				}
				if err := scanStmt(s.Then); err != nil {
					return err
				}
				return scanStmt(s.Else)
			case *IfLetStmt:
				if err := scanExpr(s.Expr); err != nil {
					return err
				}
				if err := scanStmt(s.Then); err != nil {
					return err
				}
				return scanStmt(s.Else)
			case *WhileStmt:
				if err := scanExpr(s.Cond); err != nil {
					return err
				}
				return scanStmt(s.Body)
			case *WhileLetStmt:
				if err := scanExpr(s.Call); err != nil {
					return err
				}
				return scanStmt(s.Body)
			case *ForStmt:
				if err := scanExpr(s.Iter); err != nil {
					return err
				}
				if s.IterType != nil {
					t, err := scanType(s.IterType)
					if err != nil {
						return err
					}
					s.IterType = t
				}
				return scanStmt(s.Body)
			case *ScopeStmt:
				return scanStmt(s.Body)
			case *UnsafeStmt:
				return scanStmt(s.Body)
			case *SpawnStmt:
				return scanExpr(s.Call)
			case *SelectStmt:
				for i := range s.Cases {
					if err := scanExpr(s.Cases[i].Chan); err != nil {
						return err
					}
					if s.Cases[i].ChanElem != nil {
						t, err := scanType(s.Cases[i].ChanElem)
						if err != nil {
							return err
						}
						s.Cases[i].ChanElem = t
					}
					if err := scanStmt(s.Cases[i].Body); err != nil {
						return err
					}
				}
				return scanStmt(s.Default)
			case *MatchStmt:
				if err := scanExpr(s.Expr); err != nil {
					return err
				}
				for _, a := range s.Arms {
					if err := scanStmt(a.Body); err != nil {
						return err
					}
				}
			case *ExprStmt:
				return scanExpr(s.Expr)
			case *DeferStmt:
				return scanExpr(s.Call)
			}
			return nil
		}

		scanDecls = func(decls []Decl) error {
			for _, d := range decls {
				switch decl := d.(type) {
				case *StructDecl:
					for i := range decl.Fields {
						t, err := scanType(decl.Fields[i].Type)
						if err != nil {
							return err
						}
						decl.Fields[i].Type = t
					}
				case *EnumDecl:
					for i := range decl.Variants {
						for j, vt := range decl.Variants[i].Types {
							t, err := scanType(vt)
							if err != nil {
								return err
							}
							decl.Variants[i].Types[j] = t
						}
					}
				case *TraitDecl:
					for i := range decl.Methods {
						for j := range decl.Methods[i].Params {
							t, err := scanType(decl.Methods[i].Params[j].Type)
							if err != nil {
								return err
							}
							decl.Methods[i].Params[j].Type = t
						}
						if decl.Methods[i].RetType != nil {
							t, err := scanType(decl.Methods[i].RetType)
							if err != nil {
								return err
							}
							decl.Methods[i].RetType = t
						}
					}
				case *FnDecl:
					if decl.Receiver != nil {
						t, err := scanType(decl.Receiver.Type)
						if err != nil {
							return err
						}
						decl.Receiver.Type = t
					}
					for i := range decl.Params {
						t, err := scanType(decl.Params[i].Type)
						if err != nil {
							return err
						}
						decl.Params[i].Type = t
					}
					if decl.RetType != nil {
						t, err := scanType(decl.RetType)
						if err != nil {
							return err
						}
						decl.RetType = t
					}
					if err := scanStmt(decl.Body); err != nil {
						return err
					}
				case *ExternFnDecl:
					for i := range decl.Params {
						t, err := scanType(decl.Params[i].Type)
						if err != nil {
							return err
						}
						decl.Params[i].Type = t
					}
					if decl.RetType != nil {
						t, err := scanType(decl.RetType)
						if err != nil {
							return err
						}
						decl.RetType = t
					}
				}
			}
			return nil
		}

		if err := scanDecls(concreteDecls); err != nil {
			return err
		}

		concreteDecls = append(concreteDecls, pending...)

		if len(m.instantiated) == before {
			break
		}
	}

	prog.Decls = concreteDecls
	return nil
}

// specializeStruct creates (once) a concrete struct declaration for the given
// type arguments, along with all of its receiver methods.
func (m *Monomorphizer) specializeStruct(base string, args []UbeType, pending *[]Decl) (string, bool, error) {
	tmpl, exists := m.genericStructs[base]
	if !exists {
		return "", false, fmt.Errorf("unknown generic struct '%s'", base)
	}
	if len(tmpl.TypeParams) != len(args) {
		return "", false, fmt.Errorf("generic struct '%s' expects %d type arguments, got %d", base, len(tmpl.TypeParams), len(args))
	}

	specName := specializeName(base, args)
	if m.instantiated[specName] {
		return specName, false, nil
	}
	m.instantiated[specName] = true

	subMap := make(map[string]UbeType, len(tmpl.TypeParams))
	for i, param := range tmpl.TypeParams {
		subMap[param.Name] = args[i]
	}

	fields := make([]FieldDecl, 0, len(tmpl.Fields))
	for _, f := range tmpl.Fields {
		fields = append(fields, FieldDecl{
			Name:  f.Name,
			Type:  substituteType(f.Type, subMap),
			IsMut: f.IsMut,
			IsPub: f.IsPub,
		})
	}

	specDecl := &StructDecl{
		At:     tmpl.At,
		Name:   specName,
		IsPub:  tmpl.IsPub,
		Fields: fields,
	}
	m.concreteStruct[specName] = specDecl
	*pending = append(*pending, specDecl)

	for _, method := range m.genericMethods[base] {
		specMethod := m.specializeMethod(method, specName, subMap)
		*pending = append(*pending, specMethod)
	}

	return specName, true, nil
}

func (m *Monomorphizer) specializeFunc(name string, args []UbeType, pending *[]Decl) (string, bool, error) {
	tmpl, exists := m.genericFuncs[name]
	if !exists {
		return "", false, fmt.Errorf("unknown generic function '%s'", name)
	}
	if len(tmpl.TypeParams) != len(args) {
		return "", false, fmt.Errorf("generic function '%s' expects %d type arguments, got %d", name, len(tmpl.TypeParams), len(args))
	}

	for i, p := range tmpl.TypeParams {
		if p.Constraint != "" {
			if err := m.checkTraitConstraint(args[i], p.Constraint, tmpl.At); err != nil {
				return "", false, err
			}
		}
	}

	specName := specializeName(name, args)
	if m.instantiated[specName] {
		return specName, false, nil
	}
	m.instantiated[specName] = true

	subMap := make(map[string]UbeType, len(tmpl.TypeParams))
	for i, p := range tmpl.TypeParams {
		subMap[p.Name] = args[i]
	}

	params := make([]FieldDecl, 0, len(tmpl.Params))
	for _, p := range tmpl.Params {
		params = append(params, FieldDecl{
			Name: p.Name,
			Type: substituteType(p.Type, subMap),
		})
	}

	specDecl := &FnDecl{
		At:      tmpl.At,
		Name:    specName,
		IsPub:   tmpl.IsPub,
		Params:  params,
		RetType: substituteType(tmpl.RetType, subMap),
		Body:    substituteBlock(tmpl.Body, subMap),
	}
	m.concreteFunc[specName] = specDecl
	*pending = append(*pending, specDecl)
	return specName, true, nil
}

func (m *Monomorphizer) specializeMethod(tmpl *FnDecl, specializedStruct string, subMap map[string]UbeType) *FnDecl {
	params := make([]FieldDecl, 0, len(tmpl.Params))
	for _, p := range tmpl.Params {
		params = append(params, FieldDecl{
			Name: p.Name,
			Type: substituteType(p.Type, subMap),
		})
	}
	receiver := &FieldDecl{
		Name:  tmpl.Receiver.Name,
		Type:  CustomType{Name: specializedStruct},
		IsMut: tmpl.Receiver.IsMut,
	}
	return &FnDecl{
		At:       tmpl.At,
		Name:     tmpl.Name,
		IsPub:    tmpl.IsPub,
		Receiver: receiver,
		Params:   params,
		RetType:  substituteType(tmpl.RetType, subMap),
		Body:     substituteBlock(tmpl.Body, subMap),
	}
}

func (m *Monomorphizer) checkTraitConstraint(t UbeType, traitName string, loc SourceLocation) error {
	trait, exists := m.traits[traitName]
	if !exists {
		return fmt.Errorf("[%s] unknown trait '%s'", loc, traitName)
	}
	if loc.File != "" && trait.At.File != "" && loc.File != trait.At.File && !trait.IsPub {
		return fmt.Errorf("[%s] cannot access private trait '%s'", loc, traitName)
	}
	base := typeBaseName(t)
	methods := m.allMethods[base]

	for _, required := range trait.Methods {
		found := false
		for _, actual := range methods {
			if actual.Name == required.Name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("[%s] type '%s' does not satisfy trait '%s': missing method '%s'", loc, t, traitName, required.Name)
		}
	}
	return nil
}

func specializeName(base string, args []UbeType) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = mangleTypeName(a.String())
	}
	return fmt.Sprintf("%s_%s", base, strings.Join(parts, "_"))
}

func typeBaseName(t UbeType) string {
	if t == nil {
		return ""
	}
	switch v := t.(type) {
	case CustomType:
		return v.Name
	case GenericInstanceType:
		return v.Base
	case TraitType:
		return v.Name
	default:
		return t.String()
	}
}

func substituteType(t UbeType, subMap map[string]UbeType) UbeType {
	if t == nil {
		return nil
	}
	switch v := t.(type) {
	case CustomType:
		if repl, ok := subMap[v.Name]; ok {
			return repl
		}
		return v
	case GenericInstanceType:
		newArgs := make([]UbeType, len(v.TypeArgs))
		for i, arg := range v.TypeArgs {
			newArgs[i] = substituteType(arg, subMap)
		}
		return GenericInstanceType{Base: v.Base, TypeArgs: newArgs}
	case SliceType:
		return SliceType{Elem: substituteType(v.Elem, subMap)}
	case ArrayType:
		return ArrayType{Elem: substituteType(v.Elem, subMap), Size: v.Size}
	case OptionalType:
		return OptionalType{Inner: substituteType(v.Inner, subMap)}
	case ResultType:
		return ResultType{Ok: substituteType(v.Ok, subMap), Err: substituteType(v.Err, subMap)}
	case ChanType:
		return ChanType{Elem: substituteType(v.Elem, subMap)}
	case PoolType:
		return PoolType{Elem: substituteType(v.Elem, subMap)}
	case PointerType:
		return PointerType{Base: substituteType(v.Base, subMap)}
	default:
		return t
	}
}

func substituteBlock(b *BlockStmt, subMap map[string]UbeType) *BlockStmt {
	if b == nil {
		return nil
	}
	stmts := make([]Stmt, 0, len(b.Stmts))
	for _, s := range b.Stmts {
		stmts = append(stmts, substituteStmt(s, subMap))
	}
	return &BlockStmt{At: b.At, Stmts: stmts}
}

func substituteStmt(stmt Stmt, subMap map[string]UbeType) Stmt {
	if stmt == nil {
		return nil
	}
	switch s := stmt.(type) {
	case *LetStmt:
		return &LetStmt{
			At:        s.At,
			Name:      s.Name,
			Mut:       s.Mut,
			Type:      substituteType(s.Type, subMap),
			Value:     substituteExpr(s.Value, subMap),
			ValueType: substituteType(s.ValueType, subMap),
		}
	case *AssignStmt:
		return &AssignStmt{
			At:      s.At,
			LHS:     substituteExpr(s.LHS, subMap),
			Op:      s.Op,
			RHS:     substituteExpr(s.RHS, subMap),
			LHSType: substituteType(s.LHSType, subMap),
			RHSType: substituteType(s.RHSType, subMap),
		}
	case *ReturnStmt:
		return &ReturnStmt{
			At:        s.At,
			Value:     substituteExpr(s.Value, subMap),
			ValueType: substituteType(s.ValueType, subMap),
		}
	case *IfStmt:
		return &IfStmt{
			At:   s.At,
			Cond: substituteExpr(s.Cond, subMap),
			Then: substituteBlock(s.Then, subMap),
			Else: substituteStmt(s.Else, subMap),
		}
	case *IfLetStmt:
		return &IfLetStmt{
			At:      s.At,
			VarName: s.VarName,
			Expr:    substituteExpr(s.Expr, subMap),
			Then:    substituteBlock(s.Then, subMap),
			Else:    substituteStmt(s.Else, subMap),
		}
	case *WhileStmt:
		return &WhileStmt{
			At:   s.At,
			Cond: substituteExpr(s.Cond, subMap),
			Body: substituteBlock(s.Body, subMap),
		}
	case *WhileLetStmt:
		return &WhileLetStmt{
			At:      s.At,
			VarName: s.VarName,
			Call:    substituteExpr(s.Call, subMap),
			Body:    substituteBlock(s.Body, subMap),
		}
	case *ForStmt:
		return &ForStmt{
			At:       s.At,
			ItemName: s.ItemName,
			Iter:     substituteExpr(s.Iter, subMap),
			IterType: substituteType(s.IterType, subMap),
			Body:     substituteBlock(s.Body, subMap),
		}
	case *BlockStmt:
		return substituteBlock(s, subMap)
	case *ScopeStmt:
		return &ScopeStmt{At: s.At, Body: substituteBlock(s.Body, subMap)}
	case *UnsafeStmt:
		return &UnsafeStmt{At: s.At, Body: substituteBlock(s.Body, subMap)}
	case *LoopControlStmt:
		return &LoopControlStmt{At: s.At, Continue: s.Continue}
	case *SpawnStmt:
		call, _ := substituteExpr(s.Call, subMap).(*CallExpr)
		return &SpawnStmt{At: s.At, Call: call}
	case *SelectStmt:
		out := &SelectStmt{At: s.At}
		for _, c := range s.Cases {
			out.Cases = append(out.Cases, SelectCase{
				At:       c.At,
				VarName:  c.VarName,
				Chan:     substituteExpr(c.Chan, subMap),
				ChanElem: substituteType(c.ChanElem, subMap),
				Body:     substituteBlock(c.Body, subMap),
			})
		}
		out.Default = substituteBlock(s.Default, subMap)
		return out
	case *MatchStmt:
		out := &MatchStmt{At: s.At, Expr: substituteExpr(s.Expr, subMap), EnumName: s.EnumName}
		for _, a := range s.Arms {
			out.Arms = append(out.Arms, MatchArm{
				At:       a.At,
				Variant:  a.Variant,
				Bindings: a.Bindings,
				Body:     substituteStmt(a.Body, subMap),
			})
		}
		return out
	case *ExprStmt:
		return &ExprStmt{At: s.At, Expr: substituteExpr(s.Expr, subMap)}
	case *DeferStmt:
		return &DeferStmt{At: s.At, Call: substituteExpr(s.Call, subMap)}
	default:
		return stmt
	}
}

func substituteExpr(expr Expr, subMap map[string]UbeType) Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *LiteralExpr:
		return &LiteralExpr{At: e.At, Val: e.Val, Type: substituteType(e.Type, subMap)}
	case *InterpolatedStringExpr:
		parts := make([]Expr, 0, len(e.Parts))
		for _, p := range e.Parts {
			parts = append(parts, substituteExpr(p, subMap))
		}
		return &InterpolatedStringExpr{At: e.At, Parts: parts}
	case *RangeExpr:
		return &RangeExpr{At: e.At, Start: substituteExpr(e.Start, subMap), End: substituteExpr(e.End, subMap)}
	case *TraitCoerceExpr:
		return &TraitCoerceExpr{At: e.At, Value: substituteExpr(e.Value, subMap), Trait: e.Trait, Concrete: substituteType(e.Concrete, subMap)}
	case *ArrayToSliceExpr:
		return &ArrayToSliceExpr{At: e.At, ArrayExpr: substituteExpr(e.ArrayExpr, subMap), ElemType: substituteType(e.ElemType, subMap), Size: e.Size}
	case *IdentExpr:
		return &IdentExpr{At: e.At, Name: e.Name}
	case *StructInitExpr:
		typeArgs := make([]UbeType, len(e.TypeArgs))
		for i, arg := range e.TypeArgs {
			typeArgs[i] = substituteType(arg, subMap)
		}
		fields := make([]FieldInit, 0, len(e.Fields))
		for _, f := range e.Fields {
			fields = append(fields, FieldInit{Name: f.Name, Val: substituteExpr(f.Val, subMap)})
		}
		return &StructInitExpr{At: e.At, Name: e.Name, TypeArgs: typeArgs, Fields: fields}
	case *CallExpr:
		typeArgs := make([]UbeType, len(e.TypeArgs))
		for i, arg := range e.TypeArgs {
			typeArgs[i] = substituteType(arg, subMap)
		}
		args := make([]Expr, 0, len(e.Args))
		for _, a := range e.Args {
			args = append(args, substituteExpr(a, subMap))
		}
		return &CallExpr{
			At:           e.At,
			Callee:       substituteExpr(e.Callee, subMap),
			TypeArgs:     typeArgs,
			Args:         args,
			PoolElem:     substituteType(e.PoolElem, subMap),
			ReceiverType: substituteType(e.ReceiverType, subMap),
			TraitName:    e.TraitName,
		}
	case *BinaryExpr:
		return &BinaryExpr{
			At:          e.At,
			Left:        substituteExpr(e.Left, subMap),
			Op:          e.Op,
			Right:       substituteExpr(e.Right, subMap),
			Type:        substituteType(e.Type, subMap),
			OperandType: substituteType(e.OperandType, subMap),
		}
	case *UnaryExpr:
		return &UnaryExpr{At: e.At, Op: e.Op, Operand: substituteExpr(e.Operand, subMap)}
	case *MemberExpr:
		return &MemberExpr{
			At:         e.At,
			Target:     substituteExpr(e.Target, subMap),
			Member:     e.Member,
			TargetType: substituteType(e.TargetType, subMap),
		}
	case *IndexExpr:
		return &IndexExpr{
			At:         e.At,
			Target:     substituteExpr(e.Target, subMap),
			Index:      substituteExpr(e.Index, subMap),
			TargetType: substituteType(e.TargetType, subMap),
		}
	case *SliceSubExpr:
		return &SliceSubExpr{
			At:         e.At,
			Target:     substituteExpr(e.Target, subMap),
			Start:      substituteExpr(e.Start, subMap),
			End:        substituteExpr(e.End, subMap),
			TargetType: substituteType(e.TargetType, subMap),
		}
	case *SliceInitExpr:
		elements := make([]Expr, 0, len(e.Elements))
		for _, el := range e.Elements {
			elements = append(elements, substituteExpr(el, subMap))
		}
		return &SliceInitExpr{At: e.At, Elem: substituteType(e.Elem, subMap), Elements: elements}
	case *ArrayInitExpr:
		elements := make([]Expr, 0, len(e.Elements))
		for _, el := range e.Elements {
			elements = append(elements, substituteExpr(el, subMap))
		}
		return &ArrayInitExpr{At: e.At, Elem: substituteType(e.Elem, subMap), Size: e.Size, Elements: elements}
	case *GenericTypeExpr:
		return &GenericTypeExpr{At: e.At, BaseType: e.BaseType, ElemType: substituteType(e.ElemType, subMap)}
	case *OrElseExpr:
		return &OrElseExpr{
			At:          e.At,
			Primary:     substituteExpr(e.Primary, subMap),
			Fallback:    substituteExpr(e.Fallback, subMap),
			PrimaryType: substituteType(e.PrimaryType, subMap),
		}
	case *TryExpr:
		return &TryExpr{At: e.At, Operand: substituteExpr(e.Operand, subMap)}
	case *ErrorExpr:
		return &ErrorExpr{At: e.At, Arg: substituteExpr(e.Arg, subMap)}
	case *UnsafeExpr:
		return &UnsafeExpr{At: e.At, Value: substituteExpr(e.Value, subMap)}
	case *PromoteExpr:
		return &PromoteExpr{
			At:    e.At,
			Value: substituteExpr(e.Value, subMap),
			Type:  substituteType(e.Type, subMap),
		}
	case *ArenaExpr:
		return &ArenaExpr{
			At:   e.At,
			Body: substituteBlock(e.Body, subMap),
			Type: substituteType(e.Type, subMap),
		}
	default:
		return expr
	}
}
