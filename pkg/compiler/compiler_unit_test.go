// File: pkg/compiler/compiler_unit_test.go

package compiler_test

import (
	"strings"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func TestLexer_BasicTokens(t *testing.T) {
	source := `let mut score: i32 = 42
let name = "Ube"
let hex = 0x2A
let bin = 0b1010
let f = f"score: {score}"
`
	lexer := compiler.NewLexer("test.ube", source)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("lexer error: %v", err)
	}

	expectedTypes := []compiler.TokenType{
		compiler.TokenLet,
		compiler.TokenMut,
		compiler.TokenIdent,
		compiler.TokenColon,
		compiler.TokenIdent,
		compiler.TokenAssign,
		compiler.TokenInt,
		compiler.TokenLet,
		compiler.TokenIdent,
		compiler.TokenAssign,
		compiler.TokenString,
		compiler.TokenLet,
		compiler.TokenIdent,
		compiler.TokenAssign,
		compiler.TokenInt,
		compiler.TokenLet,
		compiler.TokenIdent,
		compiler.TokenAssign,
		compiler.TokenInt,
		compiler.TokenLet,
		compiler.TokenIdent,
		compiler.TokenAssign,
		compiler.TokenFormatString,
		compiler.TokenEOF,
	}

	if len(tokens) != len(expectedTypes) {
		t.Fatalf("expected %d tokens, got %d", len(expectedTypes), len(tokens))
	}
	for i, exp := range expectedTypes {
		if tokens[i].Type != exp {
			t.Errorf("token %d: expected %s, got %s (%q)", i, exp, tokens[i].Type, tokens[i].Val)
		}
	}
}

func TestLexer_ForbiddenUnsafe(t *testing.T) {
	source := `let x = unsafe { 42 }`
	lexer := compiler.NewLexer("test.ube", source)
	_, err := lexer.Tokenize()
	if err == nil {
		t.Fatal("expected lexer error for 'unsafe', got nil")
	}
	if !strings.Contains(err.Error(), "'unsafe' is forbidden") {
		t.Errorf("expected forbidden message, got: %v", err)
	}
}

func TestParser_Declarations(t *testing.T) {
	source := `
package main

struct Point {
    x: i32,
    y: i32
}

enum State {
    Idle,
    Running(i32)
}

trait Speaker {
    fn speak() -> string
}

fn (p Point) sum() -> i32 {
    return p.x + p.y
}
`
	lexer := compiler.NewLexer("test.ube", source)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("lexer error: %v", err)
	}

	parser := compiler.NewParser(tokens)
	prog, err := parser.ParseProgram()
	if err != nil {
		t.Fatalf("parser error: %v", err)
	}

	if len(prog.Decls) != 5 {
		t.Fatalf("expected 5 declarations, got %d", len(prog.Decls))
	}

	pkg, ok := prog.Decls[0].(*compiler.PackageDecl)
	if !ok || pkg.Name != "main" || prog.Package != "main" {
		t.Fatalf("first decl should be 'package main', got: %T", prog.Decls[0])
	}

	st, ok := prog.Decls[1].(*compiler.StructDecl)
	if !ok || st.Name != "Point" || len(st.Fields) != 2 || st.Package != "main" {
		t.Errorf("second decl should be Point struct with 2 fields, got: %T", prog.Decls[1])
	}

	en, ok := prog.Decls[2].(*compiler.EnumDecl)
	if !ok || en.Name != "State" || len(en.Variants) != 2 || en.Package != "main" {
		t.Errorf("third decl should be State enum with 2 variants, got: %T", prog.Decls[2])
	}

	tr, ok := prog.Decls[3].(*compiler.TraitDecl)
	if !ok || tr.Name != "Speaker" || tr.Package != "main" {
		t.Errorf("fourth decl should be Speaker trait, got: %T", prog.Decls[3])
	}

	fn, ok := prog.Decls[4].(*compiler.FnDecl)
	if !ok || fn.Name != "sum" || fn.Receiver == nil || fn.Package != "main" {
		t.Errorf("fifth decl should be receiver method 'sum', got: %T", prog.Decls[4])
	}
}

func TestTypechecker_TypeMismatch(t *testing.T) {
	source := `
package main

fn main() {
    let x: i32 = "hello"
}
`
	lexer := compiler.NewLexer("test.ube", source)
	tokens, _ := lexer.Tokenize()
	prog, _ := compiler.NewParser(tokens).ParseProgram()

	tc := compiler.NewTypechecker()
	err := tc.Check(prog)
	if err == nil {
		t.Fatal("expected type mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "Type mismatch") {
		t.Errorf("expected 'Type mismatch' error, got: %v", err)
	}
}

func TestTypechecker_AffineMoveSemantics(t *testing.T) {
	source := `
package main

fn main() {
    let a = "resource"
    let b = move a
    let c = a
}
`
	lexer := compiler.NewLexer("test.ube", source)
	tokens, _ := lexer.Tokenize()
	prog, _ := compiler.NewParser(tokens).ParseProgram()

	tc := compiler.NewTypechecker()
	err := tc.Check(prog)
	if err == nil {
		t.Fatal("expected affine move error on 'a', got nil")
	}
	if !strings.Contains(err.Error(), "use of moved") {
		t.Errorf("expected 'use of moved' error, got: %v", err)
	}
}

func TestTypechecker_MoveKeywordExplicit(t *testing.T) {
	source := `
package main

fn main() {
    let a = "hello"
    let b = move a
    println(b)
}
`
	lexer := compiler.NewLexer("test.ube", source)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("lex error: %v", err)
	}
	prog, err := compiler.NewParser(tokens).ParseProgram()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("expected valid move, got: %v", err)
	}
}

func TestTypechecker_ImplicitMoveRejected(t *testing.T) {
	source := `
package main

fn main() {
    let a = "hello"
    let b = a
}
`
	lexer := compiler.NewLexer("test.ube", source)
	tokens, _ := lexer.Tokenize()
	prog, _ := compiler.NewParser(tokens).ParseProgram()

	tc := compiler.NewTypechecker()
	err := tc.Check(prog)
	if err == nil {
		t.Fatal("expected error for implicit move of 'a', got nil")
	}
	if !strings.Contains(err.Error(), "without explicit 'move'") {
		t.Errorf("expected explicit move error, got: %v", err)
	}
}

func TestTypechecker_MoveCopyTypeRejected(t *testing.T) {
	source := `
package main

fn main() {
    let x = 42
    let y = move x
}
`
	lexer := compiler.NewLexer("test.ube", source)
	tokens, _ := lexer.Tokenize()
	prog, _ := compiler.NewParser(tokens).ParseProgram()

	tc := compiler.NewTypechecker()
	err := tc.Check(prog)
	if err == nil {
		t.Fatal("expected error when moving copy type, got nil")
	}
	if !strings.Contains(err.Error(), "cannot move value of copy type") {
		t.Errorf("expected copy type move error, got: %v", err)
	}
}

func TestTypechecker_MoveLoopInvariant(t *testing.T) {
	source := `
package main

fn main() {
    let a = "outer"
    while true {
        let b = move a
    }
}
`
	lexer := compiler.NewLexer("test.ube", source)
	tokens, _ := lexer.Tokenize()
	prog, _ := compiler.NewParser(tokens).ParseProgram()

	tc := compiler.NewTypechecker()
	err := tc.Check(prog)
	if err == nil {
		t.Fatal("expected loop move error, got nil")
	}
	if !strings.Contains(err.Error(), "cannot move variable 'a' inside a loop") {
		t.Errorf("expected loop move error, got: %v", err)
	}
}

func TestTypechecker_ImmutableReassignment(t *testing.T) {
	source := `
package main

fn main() {
    let x = 10
    x = 20
}
`
	lexer := compiler.NewLexer("test.ube", source)
	tokens, _ := lexer.Tokenize()
	prog, _ := compiler.NewParser(tokens).ParseProgram()

	tc := compiler.NewTypechecker()
	err := tc.Check(prog)
	if err == nil {
		t.Fatal("expected immutability error on 'x', got nil")
	}
	if !strings.Contains(err.Error(), "immutable variable 'x'") {
		t.Errorf("expected immutable assignment error, got: %v", err)
	}
}

func TestTypechecker_ArenaUpwardInvariant(t *testing.T) {
	source := `
package main

fn main() {
    let mut outer = []i32{}
    arena {
        outer.push(10)
    }
}
`
	lexer := compiler.NewLexer("test.ube", source)
	tokens, _ := lexer.Tokenize()
	prog, _ := compiler.NewParser(tokens).ParseProgram()

	tc := compiler.NewTypechecker()
	err := tc.Check(prog)
	if err == nil {
		t.Fatal("expected arena upward reference violation, got nil")
	}
	if !strings.Contains(err.Error(), "Outer slice mutation attempted within inner arena") {
		t.Errorf("expected arena upward violation error, got: %v", err)
	}
}

func checkViewSource(t *testing.T, source string) error {
	t.Helper()
	lexer := compiler.NewLexer("test.ube", source)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("lex error: %v", err)
	}
	prog, err := compiler.NewParser(tokens).ParseProgram()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return compiler.NewTypechecker().Check(prog)
}

func TestTypechecker_ViewPreservesCallerOwnership(t *testing.T) {
	source := `
package main

fn inspect(view s: []string) -> i32 {
    return s.len
}

fn main() {
    let list = []string{"sys", "wasm"}
    let count = inspect(list)
    println(list[0])
    println(count)
}
`
	if err := checkViewSource(t, source); err != nil {
		t.Fatalf("expected view parameter to preserve caller ownership, got error: %v", err)
	}
}

func TestTypechecker_ViewRejectReturn(t *testing.T) {
	source := `
package main

fn escape(view tokens: []string) -> []string {
    return tokens
}

fn main() {}
`
	err := checkViewSource(t, source)
	if err == nil {
		t.Fatal("expected error returning view parameter, got nil")
	}
	if !strings.Contains(err.Error(), "cannot return view parameter 'tokens'") {
		t.Errorf("expected 'cannot return view parameter' error, got: %v", err)
	}
}

func TestTypechecker_ViewRejectStructField(t *testing.T) {
	source := `
package main

struct Container {
    items: []string
}

fn store_view(view tokens: []string) {
    let c = Container{ items: tokens }
}

fn main() {}
`
	err := checkViewSource(t, source)
	if err == nil {
		t.Fatal("expected error storing view into struct field, got nil")
	}
	if !strings.Contains(err.Error(), "cannot store view parameter 'tokens' into struct field 'items'") {
		t.Errorf("expected 'cannot store view parameter' error, got: %v", err)
	}
}

func TestTypechecker_ViewRejectReassignment(t *testing.T) {
	source := `
package main

fn reassign(view tokens: []string) {
    tokens = []string{}
}

fn main() {}
`
	err := checkViewSource(t, source)
	if err == nil {
		t.Fatal("expected error reassigning view parameter, got nil")
	}
	if !strings.Contains(err.Error(), "cannot assign to immutable view 'tokens'") {
		t.Errorf("expected 'cannot assign to immutable view' error, got: %v", err)
	}
}

func TestTypechecker_ViewRejectMutMethod(t *testing.T) {
	source := `
package main

fn mutate_view(view s: []i32) {
    s.push(10)
}

fn main() {}
`
	err := checkViewSource(t, source)
	if err == nil {
		t.Fatal("expected error calling push on view slice, got nil")
	}
	if !strings.Contains(err.Error(), "cannot call mutating method 'push' on view 's'") {
		t.Errorf("expected 'cannot call mutating method' error, got: %v", err)
	}
}

func TestTypechecker_ViewRejectMove(t *testing.T) {
	source := `
package main

fn move_view(view s: []string) {
    let x = move s
}

fn main() {}
`
	err := checkViewSource(t, source)
	if err == nil {
		t.Fatal("expected error moving view parameter, got nil")
	}
	if !strings.Contains(err.Error(), "cannot move from view parameter 's'") {
		t.Errorf("expected 'cannot move from view parameter' error, got: %v", err)
	}
}

func TestTypechecker_ViewRejectCallerMove(t *testing.T) {
	source := `
package main

fn inspect(view s: []string) -> i32 {
    return s.len
}

fn main() {
    let list = []string{"a"}
    inspect(move list)
}
`
	err := checkViewSource(t, source)
	if err == nil {
		t.Fatal("expected error passing move to view parameter, got nil")
	}
	if !strings.Contains(err.Error(), "cannot use 'move' when passing to view parameter 's'") {
		t.Errorf("expected 'cannot use move when passing to view' error, got: %v", err)
	}
}

func TestTypechecker_ViewRejectSpawn(t *testing.T) {
	source := `
package main

fn worker(data: []string) {}

fn spawn_view(view s: []string) {
    spawn worker(s)
}

fn main() {}
`
	err := checkViewSource(t, source)
	if err == nil {
		t.Fatal("expected error capturing view in spawn task, got nil")
	}
	if !strings.Contains(err.Error(), "cannot capture view parameter 's' in spawn task") {
		t.Errorf("expected 'cannot capture view parameter' error, got: %v", err)
	}
}

func TestTypechecker_ViewNestedPropagation(t *testing.T) {
	source := `
package main

fn helper(view data: []string) -> i32 {
    return data.len
}

fn forward(view tokens: []string) -> i32 {
    return helper(tokens)
}

fn main() {
    let list = []string{"a", "b"}
    println(forward(list))
    println(list[0])
}
`
	if err := checkViewSource(t, source); err != nil {
		t.Fatalf("expected view forwarding to succeed, got: %v", err)
	}
}

func TestTypechecker_ViewReceiver(t *testing.T) {
	source := `
package main

struct Rect {
    w: i32,
    h: i32
}

fn (view r Rect) area() -> i32 {
    return r.w * r.h
}

fn main() {
    let r = Rect{ w: 6, h: 7 }
    println(r.area())
    println(r.w)
}
`
	if err := checkViewSource(t, source); err != nil {
		t.Fatalf("expected view receiver to succeed, got: %v", err)
	}
}

func TestParser_ViewOnStructFieldRejected(t *testing.T) {
	source := `
package main

struct Bad {
    view item: string
}

fn main() {}
`
	lexer := compiler.NewLexer("test.ube", source)
	tokens, _ := lexer.Tokenize()
	_, err := compiler.NewParser(tokens).ParseProgram()
	if err == nil {
		t.Fatal("expected parse error for 'view' in struct field, got nil")
	}
	if !strings.Contains(err.Error(), "'view' modifier is not allowed on struct fields") {
		t.Errorf("expected struct field view rejection, got: %v", err)
	}
}

func TestParser_ViewMutMutuallyExclusive(t *testing.T) {
	source := `
package main

fn bad(view mut s: []string) {}

fn main() {}
`
	lexer := compiler.NewLexer("test.ube", source)
	tokens, _ := lexer.Tokenize()
	_, err := compiler.NewParser(tokens).ParseProgram()
	if err == nil {
		t.Fatal("expected parse error for 'view mut', got nil")
	}
	if !strings.Contains(err.Error(), "parameter cannot be both 'mut' and 'view'") {
		t.Errorf("expected mutual exclusion error, got: %v", err)
	}
}
