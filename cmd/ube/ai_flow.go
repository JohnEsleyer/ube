package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

// AST Flow and Signature Analysis

type flowVisitor struct {
	calls map[string]bool
	flow  map[string]bool
}

func analyzeFnFlow(fn *compiler.FnDecl) (calls []string, flow []string) {
	if fn.Body == nil {
		return nil, nil
	}
	v := &flowVisitor{
		calls: make(map[string]bool),
		flow:  make(map[string]bool),
	}
	v.walkStmt(fn.Body)

	for c := range v.calls {
		calls = append(calls, c)
	}
	sort.Strings(calls)

	orderedFlowTags := []string{
		"spawn", "select", "arena", "promote", "match",
		"if-let", "while-let", "for-in", "while", "try?", "or_else", "defer",
	}
	for _, tag := range orderedFlowTags {
		if v.flow[tag] {
			flow = append(flow, tag)
		}
	}
	return calls, flow
}

func (v *flowVisitor) walkStmt(st compiler.Stmt) {
	if st == nil {
		return
	}
	switch s := st.(type) {
	case *compiler.BlockStmt:
		for _, child := range s.Stmts {
			v.walkStmt(child)
		}
	case *compiler.LetStmt:
		v.walkExpr(s.Value)
	case *compiler.AssignStmt:
		v.walkExpr(s.LHS)
		v.walkExpr(s.RHS)
	case *compiler.ReturnStmt:
		v.walkExpr(s.Value)
	case *compiler.IfStmt:
		v.walkExpr(s.Cond)
		v.walkStmt(s.Then)
		v.walkStmt(s.Else)
	case *compiler.IfLetStmt:
		v.flow["if-let"] = true
		v.walkExpr(s.Expr)
		v.walkStmt(s.Then)
		v.walkStmt(s.Else)
	case *compiler.WhileStmt:
		v.flow["while"] = true
		v.walkExpr(s.Cond)
		v.walkStmt(s.Body)
	case *compiler.WhileLetStmt:
		v.flow["while-let"] = true
		v.walkExpr(s.Call)
		v.walkStmt(s.Body)
	case *compiler.ForStmt:
		v.flow["for-in"] = true
		v.walkExpr(s.Iter)
		v.walkStmt(s.Body)
	case *compiler.ArenaStmt:
		v.flow["arena"] = true
		v.walkStmt(s.Body)
	case *compiler.SpawnStmt:
		v.flow["spawn"] = true
		if s.Call != nil {
			v.walkExpr(s.Call)
		}
	case *compiler.MatchStmt:
		v.flow["match"] = true
		v.walkExpr(s.Expr)
		for _, arm := range s.Arms {
			v.walkStmt(arm.Body)
		}
	case *compiler.SelectStmt:
		v.flow["select"] = true
		for _, sc := range s.Cases {
			v.walkExpr(sc.Chan)
			v.walkStmt(sc.Body)
		}
		if s.Default != nil {
			v.walkStmt(s.Default)
		}
	case *compiler.ExprStmt:
		v.walkExpr(s.Expr)
	case *compiler.DeferStmt:
		v.flow["defer"] = true
		v.walkExpr(s.Call)
	}
}

func (v *flowVisitor) walkExpr(ex compiler.Expr) {
	if ex == nil {
		return
	}
	switch e := ex.(type) {
	case *compiler.BinaryExpr:
		v.walkExpr(e.Left)
		v.walkExpr(e.Right)
	case *compiler.UnaryExpr:
		v.walkExpr(e.Operand)
	case *compiler.CallExpr:
		cName := calleeName(e.Callee)
		if cName != "" {
			v.calls[cName] = true
		}
		v.walkExpr(e.Callee)
		for _, arg := range e.Args {
			v.walkExpr(arg)
		}
	case *compiler.MemberExpr:
		v.walkExpr(e.Target)
	case *compiler.IndexExpr:
		v.walkExpr(e.Target)
		v.walkExpr(e.Index)
	case *compiler.SliceSubExpr:
		v.walkExpr(e.Target)
		v.walkExpr(e.Start)
		v.walkExpr(e.End)
	case *compiler.StructInitExpr:
		for _, f := range e.Fields {
			v.walkExpr(f.Val)
		}
	case *compiler.SliceInitExpr:
		for _, el := range e.Elements {
			v.walkExpr(el)
		}
	case *compiler.ArrayInitExpr:
		for _, el := range e.Elements {
			v.walkExpr(el)
		}
	case *compiler.TryExpr:
		v.flow["try?"] = true
		v.walkExpr(e.Operand)
	case *compiler.OrElseExpr:
		v.flow["or_else"] = true
		v.walkExpr(e.Primary)
		v.walkExpr(e.Fallback)
	case *compiler.ErrorExpr:
		v.walkExpr(e.Arg)
	case *compiler.PromoteExpr:
		v.flow["promote"] = true
		v.walkExpr(e.Value)
	case *compiler.ArenaExpr:
		v.flow["arena"] = true
		v.walkStmt(e.Body)
	case *compiler.InterpolatedStringExpr:
		for _, p := range e.Parts {
			v.walkExpr(p)
		}
	case *compiler.RangeExpr:
		v.walkExpr(e.Start)
		v.walkExpr(e.End)
	case *compiler.TraitCoerceExpr:
		v.walkExpr(e.Value)
	case *compiler.TupleExpr:
		for _, elem := range e.Elems {
			v.walkExpr(elem)
		}
	}
}

// builtinMethods are language/runtime methods that are not project dependencies,
// so they are omitted from the call list to keep it focused on user code.
var builtinMethods = map[string]bool{
	"push": true, "pop": true, "clear": true, "contains": true,
	"len": true, "cap": true,
	"new": true, "send": true, "recv": true, "close": true,
	"spawn": true, "despawn": true, "get": true,
	"append": true, "append_str": true, "append_int": true,
	"append_i64": true, "append_char": true, "append_bool": true,
	"to_string": true,
}

func calleeName(e compiler.Expr) string {
	switch c := e.(type) {
	case *compiler.IdentExpr:
		return c.Name
	case *compiler.MemberExpr:
		if builtinMethods[c.Member] {
			return ""
		}
		if id, ok := c.Target.(*compiler.IdentExpr); ok {
			return id.Name + "." + c.Member
		}
		return c.Member
	default:
		return ""
	}
}

func formatTypeParams(params []compiler.TypeParam) string {
	if len(params) == 0 {
		return ""
	}
	var parts []string
	for _, tp := range params {
		if tp.Constraint != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", tp.Name, tp.Constraint))
		} else {
			parts = append(parts, tp.Name)
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func formatStructSign(s *compiler.StructDecl) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("struct %s%s {\n", s.Name, formatTypeParams(s.TypeParams)))
	for _, f := range s.Fields {
		mutPrefix := ""
		if f.IsMut {
			mutPrefix = "mut "
		}
		sb.WriteString(fmt.Sprintf("    %s%s: %s\n", mutPrefix, f.Name, f.Type.String()))
	}
	sb.WriteString("}\n")
	return sb.String()
}

func formatEnumSign(e *compiler.EnumDecl) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("enum %s {\n", e.Name))
	for _, v := range e.Variants {
		if len(v.Types) == 0 {
			sb.WriteString(fmt.Sprintf("    %s\n", v.Name))
		} else {
			var typeStrs []string
			for _, t := range v.Types {
				typeStrs = append(typeStrs, t.String())
			}
			sb.WriteString(fmt.Sprintf("    %s(%s)\n", v.Name, strings.Join(typeStrs, ", ")))
		}
	}
	sb.WriteString("}\n")
	return sb.String()
}

func formatTraitSign(t *compiler.TraitDecl) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("trait %s {\n", t.Name))
	for _, m := range t.Methods {
		var params []string
		for _, p := range m.Params {
			params = append(params, fmt.Sprintf("%s: %s", p.Name, p.Type.String()))
		}
		ret := ""
		if m.RetType != nil {
			ret = fmt.Sprintf(" -> %s", m.RetType.String())
		}
		sb.WriteString(fmt.Sprintf("    fn %s(%s)%s\n", m.Name, strings.Join(params, ", "), ret))
	}
	sb.WriteString("}\n")
	return sb.String()
}

func formatFnSign(fn *compiler.FnDecl) string {
	receiver := ""
	if fn.Receiver != nil {
		mutPrefix := ""
		if fn.Receiver.IsMut {
			mutPrefix = "mut "
		}
		receiver = fmt.Sprintf("(%s%s %s) ", mutPrefix, fn.Receiver.Name, fn.Receiver.Type.String())
	}

	var params []string
	for _, p := range fn.Params {
		mutPrefix := ""
		if p.IsMut {
			mutPrefix = "mut "
		}
		params = append(params, fmt.Sprintf("%s%s: %s", mutPrefix, p.Name, p.Type.String()))
	}

	ret := ""
	if fn.RetType != nil {
		ret = fmt.Sprintf(" -> %s", fn.RetType.String())
	}

	return fmt.Sprintf("fn %s%s%s(%s)%s", receiver, fn.Name, formatTypeParams(fn.TypeParams), strings.Join(params, ", "), ret)
}

func formatExternFnSign(fn *compiler.ExternFnDecl) string {
	var params []string
	for _, p := range fn.Params {
		params = append(params, fmt.Sprintf("%s: %s", p.Name, p.Type.String()))
	}
	ret := ""
	if fn.RetType != nil {
		ret = fmt.Sprintf(" -> %s", fn.RetType.String())
	}
	return fmt.Sprintf("extern fn %s(%s)%s", fn.Name, strings.Join(params, ", "), ret)
}
