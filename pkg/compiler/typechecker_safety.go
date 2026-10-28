package compiler

import (
	"fmt"
	"strings"
)

func requireUnsafePointerUse(t UbeType, env *TypeEnvironment, loc SourceLocation) error {
	if _, isPointer := t.(PointerType); isPointer && env.UnsafeDepth == 0 {
		return fmt.Errorf("[%s] Safety Error: using a raw pointer requires an unsafe block", loc)
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

func (tc *Typechecker) hasLifetime(t UbeType) bool {
	return tc.hasLifetimeSeen(t, map[string]bool{})
}

func (tc *Typechecker) hasLifetimeSeen(t UbeType, seen map[string]bool) bool {
	switch v := t.(type) {
	case PointerType, SliceType, StringType, StringBuilderType, TraitType:
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
	case *InterpolatedStringExpr:
		return env.ScopeDepth
	case *TraitCoerceExpr:
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

func exprRefRootAt(expr Expr, name string) bool {
	switch e := expr.(type) {
	case *IdentExpr:
		return e.Name == name
	case *SliceSubExpr:
		return exprRefRootAt(e.Target, name)
	case *UnaryExpr:
		if e.Op == "&" {
			return exprRefRootAt(e.Operand, name)
		}
	}
	return false
}

func (tc *Typechecker) callParamEscapes(call *CallExpr, argIndex int) bool {
	switch callee := call.Callee.(type) {
	case *IdentExpr:
		if sig, ok := tc.Global.Funcs[callee.Name]; ok && argIndex < len(sig.Escaping) {
			return sig.Escaping[argIndex]
		}
	case *MemberExpr:
		if call.ReceiverType != nil {
			key := fmt.Sprintf("%s.%s", call.ReceiverType, callee.Member)
			if sig, ok := tc.Global.Funcs[key]; ok && argIndex < len(sig.Escaping) {
				return sig.Escaping[argIndex]
			}
		}
		suffix := "." + callee.Member
		for key, sig := range tc.Global.Funcs {
			if strings.HasSuffix(key, suffix) && argIndex < len(sig.Escaping) && sig.Escaping[argIndex] {
				return true
			}
		}
	}
	return false
}

func (tc *Typechecker) exprEscapesParam(expr Expr, name string) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *CallExpr:
		if m, ok := e.Callee.(*MemberExpr); ok && m.Member == "push" && len(e.Args) == 1 {
			if assignmentRoot(m.Target) != name && exprRefRootAt(e.Args[0], name) {
				return true
			}
		}
		for i, arg := range e.Args {
			if tc.callParamEscapes(e, i) && exprRefRootAt(arg, name) {
				return true
			}
			if tc.exprEscapesParam(arg, name) {
				return true
			}
		}
		return tc.exprEscapesParam(e.Callee, name)
	case *StructInitExpr:
		for _, f := range e.Fields {
			if exprRefRootAt(f.Val, name) || tc.exprEscapesParam(f.Val, name) {
				return true
			}
		}
	case *SliceInitExpr:
		for _, el := range e.Elements {
			if exprRefRootAt(el, name) || tc.exprEscapesParam(el, name) {
				return true
			}
		}
	case *ArrayInitExpr:
		for _, el := range e.Elements {
			if exprRefRootAt(el, name) || tc.exprEscapesParam(el, name) {
				return true
			}
		}
	case *BinaryExpr:
		return tc.exprEscapesParam(e.Left, name) || tc.exprEscapesParam(e.Right, name)
	case *UnaryExpr:
		return tc.exprEscapesParam(e.Operand, name)
	case *MemberExpr:
		return tc.exprEscapesParam(e.Target, name)
	case *IndexExpr:
		return tc.exprEscapesParam(e.Target, name) || tc.exprEscapesParam(e.Index, name)
	case *SliceSubExpr:
		return tc.exprEscapesParam(e.Target, name) || tc.exprEscapesParam(e.Start, name) || tc.exprEscapesParam(e.End, name)
	case *TryExpr:
		return tc.exprEscapesParam(e.Operand, name)
	case *OrElseExpr:
		return tc.exprEscapesParam(e.Primary, name) || tc.exprEscapesParam(e.Fallback, name)
	case *ErrorExpr:
		return tc.exprEscapesParam(e.Arg, name)
	case *UnsafeExpr:
		return tc.exprEscapesParam(e.Value, name)
	case *TraitCoerceExpr:
		return tc.exprEscapesParam(e.Value, name)
	case *InterpolatedStringExpr:
		for _, part := range e.Parts {
			if tc.exprEscapesParam(part, name) {
				return true
			}
		}
	}
	return false
}

func (tc *Typechecker) stmtEscapesParam(stmt Stmt, name string) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case *BlockStmt:
		for _, st := range s.Stmts {
			if tc.stmtEscapesParam(st, name) {
				return true
			}
		}
	case *LetStmt:
		return exprRefRootAt(s.Value, name) || tc.exprEscapesParam(s.Value, name)
	case *AssignStmt:
		if exprRefRootAt(s.RHS, name) && assignmentRoot(s.LHS) != name {
			return true
		}
		return tc.exprEscapesParam(s.LHS, name) || tc.exprEscapesParam(s.RHS, name)
	case *ReturnStmt:
		return exprRefRootAt(s.Value, name) || tc.exprEscapesParam(s.Value, name)
	case *ExprStmt:
		return tc.exprEscapesParam(s.Expr, name)
	case *DeferStmt:
		return tc.exprEscapesParam(s.Call, name)
	case *IfStmt:
		return tc.exprEscapesParam(s.Cond, name) || tc.stmtEscapesParam(s.Then, name) || tc.stmtEscapesParam(s.Else, name)
	case *IfLetStmt:
		return tc.exprEscapesParam(s.Expr, name) || tc.stmtEscapesParam(s.Then, name) || tc.stmtEscapesParam(s.Else, name)
	case *WhileStmt:
		return tc.exprEscapesParam(s.Cond, name) || tc.stmtEscapesParam(s.Body, name)
	case *WhileLetStmt:
		return tc.exprEscapesParam(s.Call, name) || tc.stmtEscapesParam(s.Body, name)
	case *ForStmt:
		return tc.exprEscapesParam(s.Iter, name) || tc.stmtEscapesParam(s.Body, name)
	case *ScopeStmt:
		return tc.stmtEscapesParam(s.Body, name)
	case *UnsafeStmt:
		return tc.stmtEscapesParam(s.Body, name)
	case *SpawnStmt:
		return tc.exprEscapesParam(s.Call, name)
	case *SelectStmt:
		for _, c := range s.Cases {
			if tc.exprEscapesParam(c.Chan, name) || tc.stmtEscapesParam(c.Body, name) {
				return true
			}
		}
		return tc.stmtEscapesParam(s.Default, name)
	case *MatchStmt:
		if tc.exprEscapesParam(s.Expr, name) {
			return true
		}
		for _, a := range s.Arms {
			if tc.stmtEscapesParam(a.Body, name) {
				return true
			}
		}
	}
	return false
}

func (tc *Typechecker) computeEscapingParams(prog *Program) {
	keys := make([]string, 0, len(tc.Global.Funcs))
	for key := range tc.Global.Funcs {
		keys = append(keys, key)
	}
	for _, key := range keys {
		sig := tc.Global.Funcs[key]
		sig.Escaping = make([]bool, len(sig.Params))
		tc.Global.Funcs[key] = sig
	}

	for changed := true; changed; {
		changed = false
		for _, decl := range prog.Decls {
			fn, ok := decl.(*FnDecl)
			if !ok {
				continue
			}
			key := fn.Name
			if fn.Receiver != nil {
				key = fmt.Sprintf("%s.%s", fn.Receiver.Type, fn.Name)
			}
			sig, ok := tc.Global.Funcs[key]
			if !ok {
				continue
			}
			for i := range fn.Params {
				if sig.Escaping[i] {
					continue
				}
				if tc.stmtEscapesParam(fn.Body, fn.Params[i].Name) {
					sig.Escaping[i] = true
					changed = true
				}
			}
			tc.Global.Funcs[key] = sig
		}
	}
}
