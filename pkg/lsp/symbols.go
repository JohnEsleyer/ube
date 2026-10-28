package lsp

import (
	"github.com/JohnEsleyer/ube/pkg/compiler"
)

type documentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          map[string]any   `json:"range"`
	SelectionRange map[string]any   `json:"selectionRange"`
	Children       []documentSymbol `json:"children,omitempty"`
}

func Definition(uri, source string, p lspPosition) any {
	lines := stringsSplit(source, "\n")
	if int(p.Line) >= len(lines) {
		return nil
	}
	line := lines[p.Line]
	c := int(p.Character)
	if c > len(line) {
		c = len(line)
	}
	a, b := c, c
	for a > 0 && isWord(line[a-1]) {
		a--
	}
	for b < len(line) && isWord(line[b]) {
		b++
	}
	if a == b {
		return nil
	}
	word := line[a:b]

	tokens, err := compiler.NewLexer(uri, source).Tokenize()
	if err != nil {
		return nil
	}
	prog, err := compiler.NewParser(tokens).ParseProgram()
	if err != nil {
		return nil
	}
	for _, decl := range prog.Decls {
		loc := decl.Loc()
		name := ""
		switch d := decl.(type) {
		case *compiler.FnDecl:
			name = d.Name
		case *compiler.StructDecl:
			name = d.Name
		case *compiler.EnumDecl:
			name = d.Name
		case *compiler.ExternFnDecl:
			name = d.Name
		}
		if name == word {
			return map[string]any{
				"uri": uri,
				"range": map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len(word)},
				},
			}
		}
	}
	return nil
}

func DocumentSymbols(uri, source string) []documentSymbol {
	symbols := []documentSymbol{}
	tokens, err := compiler.NewLexer(uri, source).Tokenize()
	if err != nil {
		return symbols
	}
	prog, err := compiler.NewParser(tokens).ParseProgram()
	if err != nil {
		return symbols
	}
	for _, decl := range prog.Decls {
		loc := decl.Loc()
		switch d := decl.(type) {
		case *compiler.FnDecl:
			symbols = append(symbols, documentSymbol{
				Name:   d.Name,
				Detail: "fn",
				Kind:   12, // Function
				Range: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": 0},
					"end":   map[string]int{"line": loc.Line - 1, "character": 20},
				},
				SelectionRange: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len(d.Name)},
				},
			})
		case *compiler.StructDecl:
			fields := []documentSymbol{}
			for _, f := range d.Fields {
				fields = append(fields, documentSymbol{
					Name:   f.Name,
					Detail: f.Type.String(),
					Kind:   8, // Field
					Range: map[string]any{
						"start": map[string]int{"line": loc.Line - 1, "character": 0},
						"end":   map[string]int{"line": loc.Line - 1, "character": 20},
					},
					SelectionRange: map[string]any{
						"start": map[string]int{"line": loc.Line - 1, "character": 0},
						"end":   map[string]int{"line": loc.Line - 1, "character": len(f.Name)},
					},
				})
			}
			symbols = append(symbols, documentSymbol{
				Name:     d.Name,
				Detail:   "struct",
				Kind:     23, // Struct
				Children: fields,
				Range: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": 0},
					"end":   map[string]int{"line": loc.Line - 1, "character": 20},
				},
				SelectionRange: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len(d.Name)},
				},
			})
		case *compiler.EnumDecl:
			symbols = append(symbols, documentSymbol{
				Name:   d.Name,
				Detail: "enum",
				Kind:   10, // Enum
				Range: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": 0},
					"end":   map[string]int{"line": loc.Line - 1, "character": 20},
				},
				SelectionRange: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len(d.Name)},
				},
			})
		}
	}
	return symbols
}

func isWord(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func stringsSplit(s, sep string) []string {
	var res []string
	start := 0
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			res = append(res, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	res = append(res, s[start:])
	return res
}
