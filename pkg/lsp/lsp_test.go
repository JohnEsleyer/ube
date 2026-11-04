// File: pkg/lsp/lsp_test.go
package lsp

import (
	"strings"
	"testing"
)

func TestLSP_CompletionKeywords(t *testing.T) {
	source := `fn main() {
    l
}`
	items := CompletionItems(source, lspPosition{Line: 1, Character: 5})
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
	source := `fn main() {
    let mut s = []i32{}
    s.
}`
	items := CompletionItems(source, lspPosition{Line: 2, Character: 6})
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
	source := `fn main() {
    println("hello")
}`
	res := Hover("file:///main.ube", source, lspPosition{Line: 1, Character: 6})
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
	source := `struct Point {
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
	source := `fn greet() {
    println("hi")
}

fn main() {
    greet()
}`
	def := Definition("file:///main.ube", source, lspPosition{Line: 5, Character: 6})
	if def == nil {
		t.Fatal("expected definition location for 'greet', got nil")
	}
	m, ok := def.(map[string]any)
	if !ok {
		t.Fatalf("unexpected definition result: %T", def)
	}
	rangeMap := m["range"].(map[string]any)
	start := rangeMap["start"].(map[string]int)
	if start["line"] != 0 {
		t.Errorf("expected definition line 0, got %d", start["line"])
	}
}
