// File: pkg/lsp/lsp_test.go
package lsp

import (
	"strings"
	"testing"
)

func TestLSP_CompletionKeywords(t *testing.T) {
	source := `package main

fn main() {
    l
}`
	items := CompletionItems(source, lspPosition{Line: 2, Character: 5})
	if len(items) == 0 {
		t.Fatal("expected completions, got empty list")
	}

	foundLet := false
	for _, item := range items {
		if label, ok := item["label"].(string); ok && label == "let" {
			foundLet = true
			break
		}
	}
	if !foundLet {
		t.Error("expected 'let' in keyword completions")
	}
}

func TestLSP_CompletionMemberDot(t *testing.T) {
	source := `package main

fn main() {
    let mut s = []i32{}
    s.
}`
	items := CompletionItems(source, lspPosition{Line: 4, Character: 6})
	foundPush := false
	foundPop := false
	for _, item := range items {
		if label, ok := item["label"].(string); ok {
			if label == "push" {
				foundPush = true
			}
			if label == "pop" {
				foundPop = true
			}
		}
	}
	if !foundPush || !foundPop {
		t.Errorf("expected 'push' and 'pop' completions after '.', got: %v", items)
	}
}

func TestLSP_HoverBuiltin(t *testing.T) {
	source := `package main

fn main() {
    println("hello")
}`
	res := Hover("file:///main.ube", source, lspPosition{Line: 3, Character: 6})
	if res == nil {
		t.Fatal("expected hover info for 'println', got nil")
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("unexpected hover result type: %T", res)
	}
	contents := m["contents"].(map[string]string)
	if !strings.Contains(contents["value"], "println") {
		t.Errorf("hover text missing 'println', got: %s", contents["value"])
	}
}

func TestLSP_DocumentSymbols(t *testing.T) {
	source := `package main

struct Point {
    x: i32,
    y: i32
}

fn add(a: i32, b: i32) -> i32 {
    return a + b
}
`
	syms := DocumentSymbols("file:///main.ube", source)
	if len(syms) < 2 {
		t.Fatalf("expected at least 2 symbols, got %d", len(syms))
	}

	names := map[string]bool{}
	for _, s := range syms {
		names[s.Name] = true
	}
	if !names["Point"] {
		t.Error("expected symbol 'Point'")
	}
	if !names["add"] {
		t.Error("expected symbol 'add'")
	}
}

func TestLSP_Definition(t *testing.T) {
	source := `package main

fn greet() {
    println("hi")
}

fn main() {
    greet()
}`
	def := Definition("file:///main.ube", source, lspPosition{Line: 7, Character: 6})
	if def == nil {
		t.Fatal("expected definition location for 'greet', got nil")
	}
	m, ok := def.(map[string]any)
	if !ok {
		t.Fatalf("unexpected definition result: %T", def)
	}
	rangeMap := m["range"].(map[string]any)
	start := rangeMap["start"].(map[string]int)
	if start["line"] != 2 {
		t.Errorf("expected definition line 2, got %d", start["line"])
	}
}

func TestLSP_Completion_PackageLineZero(t *testing.T) {
	source := `p`
	items := CompletionItems(source, lspPosition{Line: 0, Character: 1})
	found := false
	for _, item := range items {
		if label, ok := item["label"].(string); ok && label == "package main" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'package main' completion on line 0, got: %v", items)
	}
}

func TestLSP_Completion_ImportPath(t *testing.T) {
	source := "package main\n\nimport \""
	items := CompletionItems(source, lspPosition{Line: 2, Character: 8})
	foundMath, foundFS := false, false
	for _, item := range items {
		if label, ok := item["label"].(string); ok {
			if label == "std/math" {
				foundMath = true
			}
			if label == "std/fs" {
				foundFS = true
			}
		}
	}
	if !foundMath || !foundFS {
		t.Errorf("expected std/math and std/fs import completions, got: %v", items)
	}
}

func TestLSP_Hover_PackageKeyword(t *testing.T) {
	source := "package main\n\nfn main() {}"
	res := Hover("file:///main.ube", source, lspPosition{Line: 0, Character: 2})
	if res == nil {
		t.Fatal("expected hover for 'package'")
	}
	val := res.(map[string]any)["contents"].(map[string]string)["value"]
	if !strings.Contains(val, "package <name>") {
		t.Errorf("expected package keyword description, got: %s", val)
	}
}

func TestLSP_DocumentSymbols_PackageKind(t *testing.T) {
	source := "package main\n\nfn main() {}\n"
	syms := DocumentSymbols("file:///main.ube", source)
	foundPkg := false
	for _, s := range syms {
		if s.Kind == 4 && s.Name == "main" {
			foundPkg = true
		}
	}
	if !foundPkg {
		t.Errorf("expected a Package (kind 4) symbol named main, got: %v", syms)
	}
}

func TestLSP_SignatureHelp_QualifiedCall(t *testing.T) {
	source := "package main\n\nimport \"std/math\" as math\n\nfn main() {\n    math.clamp(10, \n}"
	sig := SignatureHelp(source, lspPosition{Line: 5, Character: 19})
	if sig == nil {
		t.Fatal("expected signature help for math.clamp, got nil")
	}
	sigs := sig.(map[string]any)["signatures"].([]signatureInformation)
	if len(sigs) == 0 || !strings.Contains(sigs[0].Label, "clamp") {
		t.Errorf("expected clamp signature, got %v", sig)
	}
}

func TestLSP_DocumentSymbols_Containment(t *testing.T) {
	source := `package main

import "./helpers" as geo
import "std/collections"

struct VeryLongStructNameForTestingContainment {
    very_long_field_name_one: i32,
    very_long_field_name_two: string,
}

enum ActionChoice {
    FirstVariant(i32),
    SecondVariantWithVeryLongName(f32, f32),
}

trait ActionPerformer {
    fn perform_very_long_action_name(x: i32) -> bool
}

fn test_qualified_struct_instantiation_with_very_long_function_name() {
    let x = 10
}
`
	syms := DocumentSymbols("file:///main.ube", source)
	if len(syms) == 0 {
		t.Fatal("expected document symbols, got none")
	}

	var validateSymbol func(s documentSymbol)
	validateSymbol = func(s documentSymbol) {
		rStart := s.Range["start"].(map[string]int)
		rEnd := s.Range["end"].(map[string]int)
		sStart := s.SelectionRange["start"].(map[string]int)
		sEnd := s.SelectionRange["end"].(map[string]int)

		// selectionRange must start at or after range start.
		if sStart["line"] < rStart["line"] || (sStart["line"] == rStart["line"] && sStart["character"] < rStart["character"]) {
			t.Fatalf("symbol %s: selectionRange start (%d:%d) precedes range start (%d:%d)",
				s.Name, sStart["line"], sStart["character"], rStart["line"], rStart["character"])
		}

		// selectionRange must end at or before range end.
		if sEnd["line"] > rEnd["line"] || (sEnd["line"] == rEnd["line"] && sEnd["character"] > rEnd["character"]) {
			t.Fatalf("symbol %s: selectionRange end (%d:%d) exceeds range end (%d:%d)",
				s.Name, sEnd["line"], sEnd["character"], rEnd["line"], rEnd["character"])
		}

		// Children must be contained in the parent range.
		for _, child := range s.Children {
			cStart := child.Range["start"].(map[string]int)
			cEnd := child.Range["end"].(map[string]int)

			if cStart["line"] < rStart["line"] || (cStart["line"] == rStart["line"] && cStart["character"] < rStart["character"]) {
				t.Fatalf("child %s of %s: range start (%d:%d) precedes parent start (%d:%d)",
					child.Name, s.Name, cStart["line"], cStart["character"], rStart["line"], rStart["character"])
			}
			if cEnd["line"] > rEnd["line"] || (cEnd["line"] == rEnd["line"] && cEnd["character"] > rEnd["character"]) {
				t.Fatalf("child %s of %s: range end (%d:%d) exceeds parent end (%d:%d)",
					child.Name, s.Name, cEnd["line"], cEnd["character"], rEnd["line"], rEnd["character"])
			}

			validateSymbol(child)
		}
	}

	for _, s := range syms {
		validateSymbol(s)
	}
}
