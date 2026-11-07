package compiler

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
	case TupleType:
		elems := make([]UbeType, len(v.Elems))
		for i, elem := range v.Elems {
			elems[i] = substituteType(elem, subMap)
		}
		return TupleType{Elems: elems}
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
			Names:     s.Names,
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
			At:       s.At,
			VarName:  s.VarName,
			VarType:  substituteType(s.VarType, subMap),
			Expr:     substituteExpr(s.Expr, subMap),
			IsResult: s.IsResult,
			Then:     substituteBlock(s.Then, subMap),
			Else:     substituteStmt(s.Else, subMap),
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
			VarType: substituteType(s.VarType, subMap),
			Call:    substituteExpr(s.Call, subMap),
			Body:    substituteBlock(s.Body, subMap),
		}
	case *ForStmt:
		return &ForStmt{
			At:               s.At,
			ItemName:         s.ItemName,
			ItemType:         substituteType(s.ItemType, subMap),
			Iter:             substituteExpr(s.Iter, subMap),
			IterType:         substituteType(s.IterType, subMap),
			IsCustomIterator: s.IsCustomIterator,
			NextMethod:       s.NextMethod,
			ReceiverIsMut:    s.ReceiverIsMut,
			Body:             substituteBlock(s.Body, subMap),
		}
	case *BlockStmt:
		return substituteBlock(s, subMap)
	case *ArenaStmt:
		return &ArenaStmt{At: s.At, Body: substituteBlock(s.Body, subMap)}
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
	case *CastExpr:
		return &CastExpr{
			At:         e.At,
			TargetType: substituteType(e.TargetType, subMap),
			Expr:       substituteExpr(e.Expr, subMap),
		}
	case *IfExpr:
		return &IfExpr{
			At:   e.At,
			Cond: substituteExpr(e.Cond, subMap),
			Then: substituteBlock(e.Then, subMap),
			Else: substituteBlock(e.Else, subMap),
			Type: substituteType(e.Type, subMap),
		}
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
		return &ArrayToSliceExpr{At: e.At, Value: substituteExpr(e.Value, subMap), ArrayType: substituteType(e.ArrayType, subMap).(ArrayType)}
	case *TupleExpr:
		out := &TupleExpr{At: e.At}
		for _, elem := range e.Elems {
			out.Elems = append(out.Elems, substituteExpr(elem, subMap))
		}
		for _, elemType := range e.Types {
			out.Types = append(out.Types, substituteType(elemType, subMap))
		}
		return out
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
			EmbedPath:  e.EmbedPath,
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
