package compiler_test

import (
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func TestLexer(t *testing.T) {
	code := "fn let mut struct enum match if while worker scope Chan 42 \"hello\" true null"
	lexer := compiler.NewLexer("test.ube", code)
	toks, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Lexer error: %v", err)
	}
	expected := []compiler.TokenType{
		compiler.TokenFn, compiler.TokenLet, compiler.TokenMut,
		compiler.TokenStruct, compiler.TokenEnum, compiler.TokenMatch,
		compiler.TokenIf, compiler.TokenWhile, compiler.TokenWorker,
		compiler.TokenScope, compiler.TokenIdent, compiler.TokenInt,
		compiler.TokenString, compiler.TokenBool, compiler.TokenNull,
		compiler.TokenEOF,
	}

	for i, exp := range expected {
		if toks[i].Type != exp {
			t.Errorf("Index %d: expected token %s, got %s", i, exp, toks[i].Type)
		}
	}
}

func TestParserAndTypechecker(t *testing.T) {
	code := `
	struct Particle {
		x: f32,
		y: f32,
		active: bool
	}

	fn make_particle() -> Particle {
		let p = Particle{ x: 10.5, y: 20.0, active: true }
		return p
	}

	fn main() {
		let mut pt = make_particle()
		pt.x = 99.0
	}
	`
	lexer := compiler.NewLexer("test.ube", code)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Lexer failed: %v", err)
	}

	parser := compiler.NewParser(tokens)
	ast, err := parser.ParseProgram()
	if err != nil {
		t.Fatalf("Parser failed: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(ast); err != nil {
		t.Fatalf("Typechecker failed: %v", err)
	}
}

func TestTypecheckerRejectsImmutableAssignment(t *testing.T) {
	badCode := `
	fn main() {
		let val = 10
		val = 20
	}
	`
	tokens, _ := compiler.NewLexer("test.ube", badCode).Tokenize()
	ast, _ := compiler.NewParser(tokens).ParseProgram()
	tc := compiler.NewTypechecker()

	if err := tc.Check(ast); err == nil {
		t.Errorf("Expected immutable assignment error, but typecheck passed")
	}
}

func TestTypecheckerRejectsNonFlatChannel(t *testing.T) {
	badCode := `
	struct User {
		name: string
	}

	fn handle_user(ch: Chan[User]) {
	}
	`
	tokens, _ := compiler.NewLexer("test.ube", badCode).Tokenize()
	ast, _ := compiler.NewParser(tokens).ParseProgram()
	tc := compiler.NewTypechecker()

	if err := tc.Check(ast); err == nil {
		t.Errorf("Expected rejection of non-flat Chan[User], but typecheck passed")
	}
}
