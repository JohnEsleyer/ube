package compiler_test

import (
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
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
