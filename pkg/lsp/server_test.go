package lsp_test

import (
	"testing"

	"github.com/JohnEsleyer/ube/pkg/lsp"
)

func TestLSPHover(t *testing.T) {
	src := `fn add(a: i32, b: i32) -> i32 { return a + b }`
	res := lsp.Hover("test.ube", src, struct {
		Line      uint32 `json:"line"`
		Character uint32 `json:"character"`
	}{Line: 0, Character: 4})
	if res == nil {
		t.Fatal("expected hover result")
	}
}

func TestLSPDocumentSymbols(t *testing.T) {
	src := `struct User { id: i32 }
fn run() {}
enum Mode { Fast, Safe }`
	symbols := lsp.DocumentSymbols("test.ube", src)
	if len(symbols) != 3 {
		t.Fatalf("expected 3 document symbols, got %d", len(symbols))
	}
}
