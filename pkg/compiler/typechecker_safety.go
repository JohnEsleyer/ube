package compiler

import (
	"fmt"
	"strings"
)

func assignmentRoot(expr Expr) string {
	switch e := expr.(type) {
	case *IdentExpr:
		return e.Name
	case *MemberExpr:
		return assignmentRoot(e.Target)
	case *IndexExpr:
		return assignmentRoot(e.Target)
	case *UnaryExpr:
		if e.Op == "mut" {
			return assignmentRoot(e.Operand)
		}
	case *TraitCoerceExpr:
		return assignmentRoot(e.Value)
	case *ArrayToSliceExpr:
		return assignmentRoot(e.Value)
	}
	return ""
}

// mutArgOperand unwraps an explicit `mut` argument marker at a call site.
func mutArgOperand(expr Expr) (Expr, bool) {
	if u, ok := expr.(*UnaryExpr); ok && u.Op == "mut" {
		return u.Operand, true
	}
	return expr, false
}

func (tc *Typechecker) hasLifetime(t UbeType) bool {
	return tc.hasLifetimeSeen(t, map[string]bool{})
}

func (tc *Typechecker) hasLifetimeSeen(t UbeType, seen map[string]bool) bool {
	switch v := t.(type) {
	case SliceType, StringType, StringBuilderType, TraitType:
		return true
	case TupleType:
		for _, elem := range v.Elems {
			if tc.hasLifetimeSeen(elem, seen) {
				return true
			}
		}
		return false
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
	case *PromoteExpr:
		d := tc.exprDepth(e.Value, env)
		if d > 0 {
			return d - 1
		}
		return 0
	case *ArenaExpr:
		if len(e.Body.Stmts) > 0 {
			if lastExpr, ok := e.Body.Stmts[len(e.Body.Stmts)-1].(*ExprStmt); ok {
				scopeEnv := childTypeEnvironment(env)
				scopeEnv.ScopeDepth = env.ScopeDepth + 1
				return tc.exprDepth(lastExpr.Expr, scopeEnv)
			}
		}
		return env.ScopeDepth
	case *TraitCoerceExpr:
		return env.ScopeDepth
	case *ArrayToSliceExpr:
		return tc.exprDepth(e.Value, env)
	case *TupleExpr:
		depth := 0
		for _, elem := range e.Elems {
			if d := tc.exprDepth(elem, env); d > depth {
				depth = d
			}
		}
		return depth
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

// escapeTracker follows a function parameter through local aliases so that a
// plain `let p = path` or a short-lived wrapper struct such as
// `Path{ raw: path }` does not force the caller to give up ownership. A value
// only escapes when an alias is actually retained: returned, stored in an
// aggregate that outlives the call, pushed into a collection owned elsewhere,
// sent to a spawned task, or passed to another escaping parameter.
type escapeTracker struct {
	tc      *Typechecker
	aliases map[string]bool
}

func newEscapeTracker(tc *Typechecker, name string) *escapeTracker {
	return &escapeTracker{tc: tc, aliases: map[string]bool{name: true}}
}

func (t *escapeTracker) cloneAliases() map[string]bool {
	out := make(map[string]bool, len(t.aliases))
	for k, v := range t.aliases {
		out[k] = v
	}
	return out
}

// isAliasExpr reports whether expr is a direct view of a tracked value. It is
// intentionally structural: projections, aggregate literals, and trait/array
// coercions preserve the reference, while calls and interpolation produce a
// fresh value.
func (t *escapeTracker) isAliasExpr(expr Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *IdentExpr:
		return t.aliases[e.Name]
	case *MemberExpr:
		return t.isAliasExpr(e.Target)
	case *IndexExpr:
		return t.isAliasExpr(e.Target)
	case *SliceSubExpr:
		return t.isAliasExpr(e.Target)
	case *UnaryExpr:
		if e.Op == "&" || e.Op == "mut" {
			return t.isAliasExpr(e.Operand)
		}
	case *PromoteExpr:
		return t.isAliasExpr(e.Value)
	case *TraitCoerceExpr:
		return t.isAliasExpr(e.Value)
	case *ArrayToSliceExpr:
		return t.isAliasExpr(e.Value)
	case *StructInitExpr:
		for _, f := range e.Fields {
			if t.isAliasExpr(f.Val) {
				return true
			}
		}
	case *SliceInitExpr:
		for _, el := range e.Elements {
			if t.isAliasExpr(el) {
				return true
			}
		}
	case *ArrayInitExpr:
		for _, el := range e.Elements {
			if t.isAliasExpr(el) {
				return true
			}
		}
	case *TupleExpr:
		for _, el := range e.Elems {
			if t.isAliasExpr(el) {
				return true
			}
		}
	}
	return false
}

// exprContainsAlias is like isAliasExpr but also looks through call arguments,
// which is what a spawn or other unconditional hand-off needs to know.
func (t *escapeTracker) exprContainsAlias(expr Expr) bool {
	if t.isAliasExpr(expr) {
		return true
	}
	if call, ok := expr.(*CallExpr); ok {
		for _, arg := range call.Args {
			if t.exprContainsAlias(arg) {
				return true
			}
		}
		if mem, ok := call.Callee.(*MemberExpr); ok && t.exprContainsAlias(mem.Target) {
			return true
		}
	}
	if interp, ok := expr.(*InterpolatedStringExpr); ok {
		// Interpolation concatenates into a fresh string, so the parts do not
		// escape, but nested calls may still hand a value off.
		for _, part := range interp.Parts {
			if t.exprEscapes(part) {
				return true
			}
		}
	}
	return false
}

func (t *escapeTracker) exprEscapes(expr Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *CallExpr:
		if m, ok := e.Callee.(*MemberExpr); ok && m.Member == "push" && len(e.Args) == 1 {
			if !t.isAliasExpr(m.Target) && t.isAliasExpr(e.Args[0]) {
				return true
			}
		}
		for i, arg := range e.Args {
			if t.tc.callParamEscapes(e, i) && t.isAliasExpr(arg) {
				return true
			}
			if t.exprEscapes(arg) {
				return true
			}
		}
		return t.exprEscapes(e.Callee)
	case *StructInitExpr:
		for _, f := range e.Fields {
			if t.isAliasExpr(f.Val) || t.exprEscapes(f.Val) {
				return true
			}
		}
	case *SliceInitExpr:
		for _, el := range e.Elements {
			if t.isAliasExpr(el) || t.exprEscapes(el) {
				return true
			}
		}
	case *ArrayInitExpr:
		for _, el := range e.Elements {
			if t.isAliasExpr(el) || t.exprEscapes(el) {
				return true
			}
		}
	case *TupleExpr:
		for _, el := range e.Elems {
			if t.isAliasExpr(el) || t.exprEscapes(el) {
				return true
			}
		}
	case *BinaryExpr:
		return t.exprEscapes(e.Left) || t.exprEscapes(e.Right)
	case *UnaryExpr:
		return t.exprEscapes(e.Operand)
	case *MemberExpr:
		return t.exprEscapes(e.Target)
	case *IndexExpr:
		return t.exprEscapes(e.Target) || t.exprEscapes(e.Index)
	case *SliceSubExpr:
		return t.exprEscapes(e.Target) || t.exprEscapes(e.Start) || t.exprEscapes(e.End)
	case *TryExpr:
		return t.exprEscapes(e.Operand)
	case *OrElseExpr:
		return t.exprEscapes(e.Primary) || t.exprEscapes(e.Fallback)
	case *ErrorExpr:
		return t.isAliasExpr(e.Arg) || t.exprEscapes(e.Arg)
	case *RangeExpr:
		return t.exprEscapes(e.Start) || t.exprEscapes(e.End)
	case *PromoteExpr:
		return t.exprEscapes(e.Value)
	case *ArenaExpr:
		return t.stmtEscapes(e.Body)
	case *TraitCoerceExpr:
		return t.exprEscapes(e.Value)
	case *ArrayToSliceExpr:
		return t.exprEscapes(e.Value)
	case *InterpolatedStringExpr:
		for _, part := range e.Parts {
			if t.exprEscapes(part) {
				return true
			}
		}
	}
	return false
}

// exprLocalEscapes looks for escapes caused by the evaluation of expr while
// excluding the value's own aggregate literal from being treated as retained.
// It is used when the result is immediately bound to a local: the aggregate
// only escapes if a later statement actually lets it outlive the call.
func (t *escapeTracker) exprLocalEscapes(expr Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *CallExpr:
		if m, ok := e.Callee.(*MemberExpr); ok && m.Member == "push" && len(e.Args) == 1 {
			if !t.isAliasExpr(m.Target) && t.isAliasExpr(e.Args[0]) {
				return true
			}
		}
		for i, arg := range e.Args {
			if t.tc.callParamEscapes(e, i) && t.isAliasExpr(arg) {
				return true
			}
			if t.exprLocalEscapes(arg) {
				return true
			}
		}
		return t.exprLocalEscapes(e.Callee)
	case *StructInitExpr:
		for _, f := range e.Fields {
			if t.exprLocalEscapes(f.Val) {
				return true
			}
		}
	case *SliceInitExpr:
		for _, el := range e.Elements {
			if t.exprLocalEscapes(el) {
				return true
			}
		}
	case *ArrayInitExpr:
		for _, el := range e.Elements {
			if t.exprLocalEscapes(el) {
				return true
			}
		}
	case *TupleExpr:
		for _, el := range e.Elems {
			if t.exprLocalEscapes(el) {
				return true
			}
		}
	case *BinaryExpr:
		return t.exprLocalEscapes(e.Left) || t.exprLocalEscapes(e.Right)
	case *UnaryExpr:
		return t.exprLocalEscapes(e.Operand)
	case *MemberExpr:
		return t.exprLocalEscapes(e.Target)
	case *IndexExpr:
		return t.exprLocalEscapes(e.Target) || t.exprLocalEscapes(e.Index)
	case *SliceSubExpr:
		return t.exprLocalEscapes(e.Target) || t.exprLocalEscapes(e.Start) || t.exprLocalEscapes(e.End)
	case *TryExpr:
		return t.exprLocalEscapes(e.Operand)
	case *OrElseExpr:
		return t.exprLocalEscapes(e.Primary) || t.exprLocalEscapes(e.Fallback)
	case *ErrorExpr:
		return t.exprLocalEscapes(e.Arg)
	case *RangeExpr:
		return t.exprLocalEscapes(e.Start) || t.exprLocalEscapes(e.End)
	case *PromoteExpr:
		return t.exprLocalEscapes(e.Value)
	case *ArenaExpr:
		return t.stmtEscapes(e.Body)
	case *TraitCoerceExpr:
		return t.exprLocalEscapes(e.Value)
	case *ArrayToSliceExpr:
		return t.exprLocalEscapes(e.Value)
	case *InterpolatedStringExpr:
		for _, part := range e.Parts {
			if t.exprLocalEscapes(part) {
				return true
			}
		}
	}
	return false
}

func (t *escapeTracker) stmtEscapes(stmt Stmt) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case *BlockStmt:
		saved := t.cloneAliases()
		escaped := false
		for _, st := range s.Stmts {
			if t.stmtEscapes(st) {
				escaped = true
			}
		}
		t.aliases = saved
		return escaped
	case *LetStmt:
		escaped := t.exprLocalEscapes(s.Value)
		names := s.Names
		if len(names) == 0 {
			names = []string{s.Name}
		}
		alias := t.isAliasExpr(s.Value)
		for _, name := range names {
			if alias {
				t.aliases[name] = true
			} else {
				delete(t.aliases, name)
			}
		}
		return escaped
	case *AssignStmt:
		rhsAlias := t.isAliasExpr(s.RHS)
		root := assignmentRoot(s.LHS)
		if _, plainVar := s.LHS.(*IdentExpr); plainVar && root != "" {
			escaped := t.exprEscapes(s.LHS) || t.exprLocalEscapes(s.RHS)
			if rhsAlias {
				t.aliases[root] = true
			} else {
				delete(t.aliases, root)
			}
			return escaped
		}
		escaped := t.exprEscapes(s.LHS) || t.exprEscapes(s.RHS)
		if rhsAlias && root != "" {
			escaped = true
		}
		return escaped
	case *ReturnStmt:
		return t.isAliasExpr(s.Value) || t.exprEscapes(s.Value)
	case *ExprStmt:
		return t.exprEscapes(s.Expr)
	case *DeferStmt:
		return t.exprEscapes(s.Call)
	case *IfStmt:
		return t.exprEscapes(s.Cond) || t.stmtEscapes(s.Then) || t.stmtEscapes(s.Else)
	case *IfLetStmt:
		escaped := t.exprEscapes(s.Expr)
		delete(t.aliases, s.VarName)
		if t.stmtEscapes(s.Then) {
			escaped = true
		}
		if t.stmtEscapes(s.Else) {
			escaped = true
		}
		return escaped
	case *WhileStmt:
		return t.exprEscapes(s.Cond) || t.stmtEscapes(s.Body)
	case *WhileLetStmt:
		escaped := t.exprEscapes(s.Call)
		delete(t.aliases, s.VarName)
		if t.stmtEscapes(s.Body) {
			escaped = true
		}
		return escaped
	case *ForStmt:
		escaped := t.exprEscapes(s.Iter)
		delete(t.aliases, s.ItemName)
		if t.stmtEscapes(s.Body) {
			escaped = true
		}
		return escaped
	case *ArenaStmt:
		return t.stmtEscapes(s.Body)
	case *SpawnStmt:
		return t.exprContainsAlias(s.Call)
	case *SelectStmt:
		for _, c := range s.Cases {
			delete(t.aliases, c.VarName)
			if t.exprEscapes(c.Chan) || t.stmtEscapes(c.Body) {
				return true
			}
		}
		return t.stmtEscapes(s.Default)
	case *MatchStmt:
		if t.exprEscapes(s.Expr) {
			return true
		}
		for _, a := range s.Arms {
			for _, b := range a.Bindings {
				delete(t.aliases, b)
			}
			if t.stmtEscapes(a.Body) {
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
				tracker := newEscapeTracker(tc, fn.Params[i].Name)
				if tracker.stmtEscapes(fn.Body) {
					sig.Escaping[i] = true
					changed = true
				}
			}
			tc.Global.Funcs[key] = sig
		}
	}
}
