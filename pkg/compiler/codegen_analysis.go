package compiler

import "fmt"

func callName(e *CallExpr) string {
	switch c := e.Callee.(type) {
	case *IdentExpr:
		return c.Name
	case *MemberExpr:
		if e.ReceiverType != nil {
			return fmt.Sprintf("%s_%s", e.ReceiverType.String(), c.Member)
		}
	}
	return ""
}

func (g *Generator) exprMutatesName(expr Expr, name string) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *CallExpr:
		if m, ok := e.Callee.(*MemberExpr); ok && (m.Member == "push" || m.Member == "pop" || m.Member == "clear") && assignmentRoot(m.Target) == name {
			return true
		}
		if callee := callName(e); callee != "" {
			for i, arg := range e.Args {
				if g.sliceMutParams[callee][i] && assignmentRoot(arg) == name {
					return true
				}
			}
		}
		if g.exprMutatesName(e.Callee, name) {
			return true
		}
		for _, arg := range e.Args {
			if g.exprMutatesName(arg, name) {
				return true
			}
		}
	case *BinaryExpr:
		return g.exprMutatesName(e.Left, name) || g.exprMutatesName(e.Right, name)
	case *UnaryExpr:
		return g.exprMutatesName(e.Operand, name)
	case *MemberExpr:
		return g.exprMutatesName(e.Target, name)
	case *IndexExpr:
		return g.exprMutatesName(e.Target, name) || g.exprMutatesName(e.Index, name)
	case *SliceSubExpr:
		return g.exprMutatesName(e.Target, name) || g.exprMutatesName(e.Start, name) || g.exprMutatesName(e.End, name)
	case *StructInitExpr:
		for _, f := range e.Fields {
			if g.exprMutatesName(f.Val, name) {
				return true
			}
		}
	case *SliceInitExpr:
		for _, el := range e.Elements {
			if g.exprMutatesName(el, name) {
				return true
			}
		}
	case *ArrayInitExpr:
		for _, el := range e.Elements {
			if g.exprMutatesName(el, name) {
				return true
			}
		}
	case *TryExpr:
		return g.exprMutatesName(e.Operand, name)
	case *OrElseExpr:
		return g.exprMutatesName(e.Primary, name) || g.exprMutatesName(e.Fallback, name)
	case *ErrorExpr:
		return g.exprMutatesName(e.Arg, name)
	case *UnsafeExpr:
		return g.exprMutatesName(e.Value, name)
	case *RangeExpr:
		return g.exprMutatesName(e.Start, name) || g.exprMutatesName(e.End, name)
	case *TraitCoerceExpr:
		return g.exprMutatesName(e.Value, name)
	case *ArrayToSliceExpr:
		return g.exprMutatesName(e.ArrayExpr, name)
	case *InterpolatedStringExpr:
		for _, part := range e.Parts {
			if g.exprMutatesName(part, name) {
				return true
			}
		}
	case *ArenaExpr:
		return g.stmtMutatesName(e.Body, name)
	}
	return false
}

func (g *Generator) stmtMutatesName(stmt Stmt, name string) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case *BlockStmt:
		for _, st := range s.Stmts {
			if g.stmtMutatesName(st, name) {
				return true
			}
		}
	case *LetStmt:
		return g.exprMutatesName(s.Value, name)
	case *AssignStmt:
		return g.exprMutatesName(s.LHS, name) || g.exprMutatesName(s.RHS, name)
	case *ReturnStmt:
		return g.exprMutatesName(s.Value, name)
	case *ExprStmt:
		return g.exprMutatesName(s.Expr, name)
	case *DeferStmt:
		return g.exprMutatesName(s.Call, name)
	case *IfStmt:
		return g.exprMutatesName(s.Cond, name) || g.stmtMutatesName(s.Then, name) || g.stmtMutatesName(s.Else, name)
	case *IfLetStmt:
		return g.exprMutatesName(s.Expr, name) || g.stmtMutatesName(s.Then, name) || g.stmtMutatesName(s.Else, name)
	case *WhileStmt:
		return g.exprMutatesName(s.Cond, name) || g.stmtMutatesName(s.Body, name)
	case *WhileLetStmt:
		return g.exprMutatesName(s.Call, name) || g.stmtMutatesName(s.Body, name)
	case *ForStmt:
		return g.exprMutatesName(s.Iter, name) || g.stmtMutatesName(s.Body, name)
	case *ArenaStmt:
		return g.stmtMutatesName(s.Body, name)
	case *UnsafeStmt:
		return g.stmtMutatesName(s.Body, name)
	case *SpawnStmt:
		return g.exprMutatesName(s.Call, name)
	case *SelectStmt:
		for _, c := range s.Cases {
			if g.exprMutatesName(c.Chan, name) || g.stmtMutatesName(c.Body, name) {
				return true
			}
		}
		return g.stmtMutatesName(s.Default, name)
	case *MatchStmt:
		if g.exprMutatesName(s.Expr, name) {
			return true
		}
		for _, a := range s.Arms {
			if g.stmtMutatesName(a.Body, name) {
				return true
			}
		}
	}
	return false
}

func (g *Generator) analyzeSliceMutations() {
	g.sliceMutParams = make(map[string]map[int]bool)
	for _, decl := range g.Program.Decls {
		if fn, ok := decl.(*FnDecl); ok {
			name := fn.Name
			if fn.Receiver != nil {
				name = fmt.Sprintf("%s_%s", fn.Receiver.Type, fn.Name)
			}
			g.sliceMutParams[name] = make(map[int]bool)
		}
	}

	for changed := true; changed; {
		changed = false
		for _, decl := range g.Program.Decls {
			fn, ok := decl.(*FnDecl)
			if !ok {
				continue
			}
			name := fn.Name
			if fn.Receiver != nil {
				name = fmt.Sprintf("%s_%s", fn.Receiver.Type, fn.Name)
			}
			for i, param := range fn.Params {
				if _, isSlice := param.Type.(SliceType); !isSlice || g.sliceMutParams[name][i] {
					continue
				}
				if g.stmtMutatesName(fn.Body, param.Name) {
					g.sliceMutParams[name][i] = true
					changed = true
				}
			}
		}
	}
}
