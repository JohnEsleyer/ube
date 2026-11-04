// File: pkg/compiler/module_rewrite.go
package compiler

// This file implements module namespace isolation for aliased imports.
//
// The module loader flattens declarations from every imported file into a
// single Program. Without qualification, two modules (or a module and the
// importing file) cannot define the same function or type. For an aliased
// import we therefore rewrite the imported module's top-level names with an
// internal `<alias>__` prefix before adding them to the flat program.
//
// References inside the imported module are rewritten as well, so a function
// such as `math.min` can still call its sibling `abs` even though both names
// have been prefixed.

type moduleRewriter struct {
	names map[string]string
}

func renameAliasedModule(prog *Program, prefix string) {
	if prog == nil || prefix == "" {
		return
	}

	names := make(map[string]string)
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *FnDecl:
			if d.Receiver == nil {
				names[d.Name] = prefix + d.Name
			}
		case *StructDecl:
			names[d.Name] = prefix + d.Name
		case *EnumDecl:
			names[d.Name] = prefix + d.Name
		case *TraitDecl:
			names[d.Name] = prefix + d.Name
		}
	}
	if len(names) == 0 {
		return
	}

	r := &moduleRewriter{names: names}
	for _, decl := range prog.Decls {
		r.decl(decl)
	}
}

func (r *moduleRewriter) typeName(name string) string {
	if renamed, ok := r.names[name]; ok {
		return renamed
	}
	return name
}

func (r *moduleRewriter) renameType(t UbeType) UbeType {
	switch v := t.(type) {
	case nil:
		return nil
	case CustomType:
		return CustomType{Name: r.typeName(v.Name), Reg: v.Reg}
	case TraitType:
		return TraitType{Name: r.typeName(v.Name), Reg: v.Reg}
	case GenericInstanceType:
		args := make([]UbeType, len(v.TypeArgs))
		for i, a := range v.TypeArgs {
			args[i] = r.renameType(a)
		}
		return GenericInstanceType{Base: r.typeName(v.Base), TypeArgs: args, Reg: v.Reg}
	case SliceType:
		return SliceType{Elem: r.renameType(v.Elem), Reg: v.Reg}
	case ArrayType:
		return ArrayType{Elem: r.renameType(v.Elem), Size: v.Size, Reg: v.Reg}
	case OptionalType:
		return OptionalType{Inner: r.renameType(v.Inner), Reg: v.Reg}
	case ResultType:
		return ResultType{Ok: r.renameType(v.Ok), Err: r.renameType(v.Err), Reg: v.Reg}
	case ChanType:
		return ChanType{Elem: r.renameType(v.Elem), Reg: v.Reg}
	case PoolType:
		return PoolType{Elem: r.renameType(v.Elem), Reg: v.Reg}
	case RangeType:
		return RangeType{Elem: r.renameType(v.Elem), Reg: v.Reg}
	case TupleType:
		elems := make([]UbeType, len(v.Elems))
		for i, e := range v.Elems {
			elems[i] = r.renameType(e)
		}
		return TupleType{Elems: elems, Reg: v.Reg}
	default:
		return t
	}
}

func (r *moduleRewriter) decl(decl Decl) {
	switch d := decl.(type) {
	case *StructDecl:
		d.Name = r.typeName(d.Name)
		for i := range d.Fields {
			d.Fields[i].Type = r.renameType(d.Fields[i].Type)
		}
	case *EnumDecl:
		d.Name = r.typeName(d.Name)
		for i := range d.Variants {
			for j, vt := range d.Variants[i].Types {
				d.Variants[i].Types[j] = r.renameType(vt)
			}
		}
	case *TraitDecl:
		d.Name = r.typeName(d.Name)
		for i, st := range d.Supertraits {
			d.Supertraits[i] = r.typeName(st)
		}
		for i := range d.Methods {
			for j := range d.Methods[i].Params {
				d.Methods[i].Params[j].Type = r.renameType(d.Methods[i].Params[j].Type)
			}
			d.Methods[i].RetType = r.renameType(d.Methods[i].RetType)
		}
	case *FnDecl:
		if d.Receiver == nil {
			d.Name = r.typeName(d.Name)
		} else {
			d.Receiver.Type = r.renameType(d.Receiver.Type)
		}
		for i := range d.Params {
			d.Params[i].Type = r.renameType(d.Params[i].Type)
		}
		d.RetType = r.renameType(d.RetType)
		locals := make(map[string]bool)
		if d.Receiver != nil {
			locals[d.Receiver.Name] = true
		}
		for _, p := range d.Params {
			locals[p.Name] = true
		}
		r.block(d.Body, locals)
	case *ExternFnDecl:
		// Extern declarations keep their host symbol name. Only their types
		// may refer to module-local structs/enums.
		for i := range d.Params {
			d.Params[i].Type = r.renameType(d.Params[i].Type)
		}
		d.RetType = r.renameType(d.RetType)
	}
}

func cloneLocals(locals map[string]bool) map[string]bool {
	out := make(map[string]bool, len(locals))
	for k, v := range locals {
		out[k] = v
	}
	return out
}

func (r *moduleRewriter) block(body *BlockStmt, locals map[string]bool) {
	if body == nil {
		return
	}
	scope := cloneLocals(locals)
	for _, st := range body.Stmts {
		r.stmt(st, scope)
	}
}

func (r *moduleRewriter) stmt(st Stmt, locals map[string]bool) {
	switch s := st.(type) {
	case *BlockStmt:
		r.block(s, locals)
	case *LetStmt:
		s.Type = r.renameType(s.Type)
		s.ValueType = r.renameType(s.ValueType)
		s.Value = r.expr(s.Value, locals)
		if s.Name != "" {
			locals[s.Name] = true
		}
		for _, n := range s.Names {
			locals[n] = true
		}
	case *AssignStmt:
		s.LHS = r.expr(s.LHS, locals)
		s.RHS = r.expr(s.RHS, locals)
	case *ReturnStmt:
		s.Value = r.expr(s.Value, locals)
	case *IfStmt:
		s.Cond = r.expr(s.Cond, locals)
		r.block(s.Then, locals)
		if s.Else != nil {
			r.stmt(s.Else, locals)
		}
	case *IfLetStmt:
		s.Expr = r.expr(s.Expr, locals)
		thenScope := cloneLocals(locals)
		thenScope[s.VarName] = true
		r.block(s.Then, thenScope)
		if s.Else != nil {
			r.stmt(s.Else, locals)
		}
	case *WhileStmt:
		s.Cond = r.expr(s.Cond, locals)
		r.block(s.Body, locals)
	case *WhileLetStmt:
		s.Call = r.expr(s.Call, locals)
		loopScope := cloneLocals(locals)
		loopScope[s.VarName] = true
		r.block(s.Body, loopScope)
	case *ForStmt:
		s.Iter = r.expr(s.Iter, locals)
		loopScope := cloneLocals(locals)
		loopScope[s.ItemName] = true
		r.block(s.Body, loopScope)
	case *ArenaStmt:
		r.block(s.Body, locals)
	case *SpawnStmt:
		if s.Call != nil {
			r.expr(s.Call, locals)
		}
	case *MatchStmt:
		s.Expr = r.expr(s.Expr, locals)
		for i := range s.Arms {
			armScope := cloneLocals(locals)
			for _, b := range s.Arms[i].Bindings {
				armScope[b] = true
			}
			r.stmt(s.Arms[i].Body, armScope)
		}
	case *SelectStmt:
		for i := range s.Cases {
			s.Cases[i].Chan = r.expr(s.Cases[i].Chan, locals)
			caseScope := cloneLocals(locals)
			caseScope[s.Cases[i].VarName] = true
			r.block(s.Cases[i].Body, caseScope)
		}
		if s.Default != nil {
			r.block(s.Default, locals)
		}
	case *ExprStmt:
		s.Expr = r.expr(s.Expr, locals)
	case *DeferStmt:
		s.Call = r.expr(s.Call, locals)
	}
}

func (r *moduleRewriter) expr(expr Expr, locals map[string]bool) Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *LiteralExpr:
		e.Type = r.renameType(e.Type)
	case *IdentExpr:
		if !locals[e.Name] {
			e.Name = r.typeName(e.Name)
		}
	case *BinaryExpr:
		e.Left = r.expr(e.Left, locals)
		e.Right = r.expr(e.Right, locals)
		e.Type = r.renameType(e.Type)
		e.OperandType = r.renameType(e.OperandType)
	case *UnaryExpr:
		e.Operand = r.expr(e.Operand, locals)
	case *CallExpr:
		e.Callee = r.expr(e.Callee, locals)
		for i := range e.Args {
			e.Args[i] = r.expr(e.Args[i], locals)
		}
		for i := range e.TypeArgs {
			e.TypeArgs[i] = r.renameType(e.TypeArgs[i])
		}
		e.PoolElem = r.renameType(e.PoolElem)
		e.ReceiverType = r.renameType(e.ReceiverType)
		for i := range e.ArgTypes {
			e.ArgTypes[i] = r.renameType(e.ArgTypes[i])
		}
	case *MemberExpr:
		e.Target = r.expr(e.Target, locals)
		e.TargetType = r.renameType(e.TargetType)
	case *IndexExpr:
		e.Target = r.expr(e.Target, locals)
		e.Index = r.expr(e.Index, locals)
		e.TargetType = r.renameType(e.TargetType)
	case *SliceSubExpr:
		e.Target = r.expr(e.Target, locals)
		e.Start = r.expr(e.Start, locals)
		e.End = r.expr(e.End, locals)
		e.TargetType = r.renameType(e.TargetType)
	case *StructInitExpr:
		e.Name = r.typeName(e.Name)
		for i := range e.Fields {
			e.Fields[i].Val = r.expr(e.Fields[i].Val, locals)
		}
		for i := range e.TypeArgs {
			e.TypeArgs[i] = r.renameType(e.TypeArgs[i])
		}
	case *SliceInitExpr:
		e.Elem = r.renameType(e.Elem)
		for i := range e.Elements {
			e.Elements[i] = r.expr(e.Elements[i], locals)
		}
	case *ArrayInitExpr:
		e.Elem = r.renameType(e.Elem)
		for i := range e.Elements {
			e.Elements[i] = r.expr(e.Elements[i], locals)
		}
	case *GenericTypeExpr:
		e.BaseType = r.typeName(e.BaseType)
		e.ElemType = r.renameType(e.ElemType)
	case *TryExpr:
		e.Operand = r.expr(e.Operand, locals)
	case *OrElseExpr:
		e.Primary = r.expr(e.Primary, locals)
		e.Fallback = r.expr(e.Fallback, locals)
		e.PrimaryType = r.renameType(e.PrimaryType)
	case *ErrorExpr:
		e.Arg = r.expr(e.Arg, locals)
	case *PromoteExpr:
		e.Value = r.expr(e.Value, locals)
		e.Type = r.renameType(e.Type)
	case *ArenaExpr:
		r.block(e.Body, locals)
		e.Type = r.renameType(e.Type)
	case *InterpolatedStringExpr:
		for i := range e.Parts {
			e.Parts[i] = r.expr(e.Parts[i], locals)
		}
	case *RangeExpr:
		e.Start = r.expr(e.Start, locals)
		e.End = r.expr(e.End, locals)
	case *TraitCoerceExpr:
		e.Value = r.expr(e.Value, locals)
		e.Trait = r.typeName(e.Trait)
		e.Concrete = r.renameType(e.Concrete)
	case *ArrayToSliceExpr:
		e.Value = r.expr(e.Value, locals)
		if arr, ok := r.renameType(e.ArrayType).(ArrayType); ok {
			e.ArrayType = arr
		}
	case *TupleExpr:
		for i := range e.Elems {
			e.Elems[i] = r.expr(e.Elems[i], locals)
		}
		for i := range e.Types {
			e.Types[i] = r.renameType(e.Types[i])
		}
	}
	return expr
}
