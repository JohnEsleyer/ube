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
    let b = a
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
