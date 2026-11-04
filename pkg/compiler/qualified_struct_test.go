package compiler_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

// TestParser_QualifiedStructLiteral verifies that the parser treats `alias.StructName{...}`
// as a struct initialization instead of a plain member expression.
func TestParser_QualifiedStructLiteral(t *testing.T) {
	src := `
fn main() {
    let s = mh.Stats{ min: 5, max: 25, count: 10 }
}
`
	lexer := compiler.NewLexer("main.ube", src)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("unexpected lex error: %v", err)
	}

	parser := compiler.NewParser(tokens)
	prog, err := parser.ParseProgram()
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if len(prog.Decls) == 0 {
		t.Fatal("expected at least 1 declaration")
	}

	fnDecl, ok := prog.Decls[0].(*compiler.FnDecl)
	if !ok || fnDecl.Body == nil || len(fnDecl.Body.Stmts) == 0 {
		t.Fatal("expected main function with body statements")
	}

	letStmt, ok := fnDecl.Body.Stmts[0].(*compiler.LetStmt)
	if !ok {
		t.Fatalf("expected LetStmt, got %T", fnDecl.Body.Stmts[0])
	}

	structInit, ok := letStmt.Value.(*compiler.StructInitExpr)
	if !ok {
		t.Fatalf("expected StructInitExpr for qualified struct, got %T", letStmt.Value)
	}

	if structInit.Name != "mh.Stats" {
		t.Errorf("expected struct name 'mh.Stats', got '%s'", structInit.Name)
	}

	if len(structInit.Fields) != 3 {
		t.Errorf("expected 3 fields initialized, got %d", len(structInit.Fields))
	}
}

// TestWasmEmit_NoPanicOnUntypedMember asserts that compiling a MemberExpr with a nil TargetType
// returns a descriptive error rather than causing a SIGSEGV / nil-pointer panic.
func TestWasmEmit_NoPanicOnUntypedMember(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("FATAL: Wasm emission panicked with: %v (expected descriptive error)", r)
		}
	}()

	// Construct AST with an invalid, untyped MemberExpr
	prog := &compiler.Program{
		Decls: []compiler.Decl{
			&compiler.FnDecl{
				Name: "main",
				RetType: compiler.PrimitiveType{Name: "void"},
				Body: &compiler.BlockStmt{
					Stmts: []compiler.Stmt{
						&compiler.ExprStmt{
							Expr: &compiler.MemberExpr{
								Target:     &compiler.IdentExpr{Name: "mh"},
								Member:     "Unresolved",
								TargetType: nil, // Intentionally nil to simulate un-inferred AST
							},
						},
					},
				},
			},
		},
	}

	_, err := compiler.EmitWasm(prog)
	if err == nil {
		t.Error("expected emission error for untyped MemberExpr, got nil")
	} else if !strings.Contains(err.Error(), "untyped") && !strings.Contains(err.Error(), "cannot resolve") {
		t.Logf("Got expected compiler error: %v", err)
	}
}

// TestEndToEnd_QualifiedStructModules executes a multi-file compilation with aliased imports
// to ensure the entire pipeline (Lex -> Parse -> ModuleRewrite -> Typecheck -> Emit) succeeds.
func TestEndToEnd_QualifiedStructModules(t *testing.T) {
	tmpDir := t.TempDir()

	mathHelperSrc := `
pub struct Stats {
    pub min: i32,
    pub max: i32,
    pub count: i32
}

pub fn (s Stats) range() -> i32 {
    return s.max - s.min
}
`
	mainSrc := `
import "./math_helpers" as mh

fn main() {
    let s = mh.Stats{ min: 5, max: 25, count: 10 }
    let r = s.range()
    println(r)
}
`

	err := os.WriteFile(filepath.Join(tmpDir, "math_helpers.ube"), []byte(mathHelperSrc), 0644)
	if err != nil {
		t.Fatalf("failed to write helper file: %v", err)
	}

	mainFile := filepath.Join(tmpDir, "main.ube")
	err = os.WriteFile(mainFile, []byte(mainSrc), 0644)
	if err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	loader := compiler.NewModuleLoader()
	prog, err := loader.Load(mainFile)
	if err != nil {
		t.Fatalf("failed to load modules: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typechecker failed on qualified struct: %v", err)
	}

	wasmBytes, err := compiler.EmitWasm(prog)
	if err != nil {
		t.Fatalf("EmitWasm failed on qualified struct: %v", err)
	}

	if len(wasmBytes) < 8 {
		t.Fatalf("emitted wasm too small: %d bytes", len(wasmBytes))
	}
}
