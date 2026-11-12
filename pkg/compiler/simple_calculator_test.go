package compiler_test

import (
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
	// Register the WASI Preview 1/0.2 backend layers for emission.
	_ "github.com/JohnEsleyer/ube/pkg/targets/wasi1"
	_ "github.com/JohnEsleyer/ube/pkg/targets/wasi2"
)

func TestSimpleCalculatorCompiles(t *testing.T) {
	loader := compiler.NewModuleLoader()
	prog, err := loader.Load("testdata/simple_calculator/main.ube")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("typecheck: %v", err)
	}
	if _, err := compiler.EmitWasm(prog); err != nil {
		t.Fatalf("emit: %v", err)
	}
}
