// File: pkg/lsp/symbols.go

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

	word, qualifier := extractWordAndQualifier(line, int(p.Character))
	if word == "" {
		return nil
	}

	prog, err := getProgram(uri, source)
	if err != nil || prog == nil {
		return nil
	}

	// Jumping on an import alias resolves to the import statement.
	if qualifier == "" {
		for _, decl := range prog.Decls {
			if imp, ok := decl.(*compiler.ImportDecl); ok {
				alias := imp.Alias
				if alias == "" {
					alias = defaultAlias(imp.Path)
				}
				if alias == word {
					loc := imp.Loc()
					file := loc.File
					if file == "" {
						file = uri
					}
					return map[string]any{
						"uri": fileToURI(file),
						"range": map[string]any{
							"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1},
							"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len("import")},
						},
					}
				}
			}
		}
		// Jumping on the package name resolves to the package clause.
		for _, decl := range prog.Decls {
			if pkg, ok := decl.(*compiler.PackageDecl); ok && pkg.Name == word {
				loc := pkg.Loc()
				file := loc.File
				if file == "" {
					file = uri
				}
				return map[string]any{
					"uri": fileToURI(file),
					"range": map[string]any{
						"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1},
						"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len(word)},
					},
				}
			}
		}
	}

	// Resolve a qualified member through the alias map, or fall back to a
	// plain top-level lookup in the current package.
	targetPkg := ""
	if qualifier != "" {
		targetPkg = prog.ModuleAliases[qualifier]
		if targetPkg == "" {
			targetPkg = qualifier
		}
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
		case *compiler.TraitDecl:
			name = d.Name
		case *compiler.ExternFnDecl:
			name = d.Name
		}
		if name == "" || loc.File == "" {
			continue
		}
		match := false
		if targetPkg != "" {
			match = name == targetPkg+"."+word || (declPackageOf(decl) == targetPkg && shortName(name) == word)
		} else {
			match = name == word || shortName(name) == word
		}
		if match {
			return map[string]any{
				"uri": fileToURI(loc.File),
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
		case *compiler.PackageDecl:
			symbols = append(symbols, documentSymbol{
				Name:   d.Name,
				Detail: "package " + d.Name,
				Kind:   4, // Package
				Range: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": 0},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len("package ") + len(d.Name)},
				},
				SelectionRange: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len("package ")},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len("package ") + len(d.Name)},
				},
			})
		case *compiler.ImportDecl:
			name := d.Path
			if d.Alias != "" {
				name = d.Alias + " (" + d.Path + ")"
			}
			symbols = append(symbols, documentSymbol{
				Name:   name,
				Detail: "import",
				Kind:   2, // Module
				Range: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": 0},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len("import")},
				},
				SelectionRange: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len(d.Path)},
				},
			})
		case *compiler.TraitDecl:
			methods := []documentSymbol{}
			for _, m := range d.Methods {
				mLoc := m.At
				methods = append(methods, documentSymbol{
					Name:   m.Name,
					Detail: "fn",
					Kind:   6, // Method
					Range: map[string]any{
						"start": map[string]int{"line": mLoc.Line - 1, "character": mLoc.Col - 1},
						"end":   map[string]int{"line": mLoc.Line - 1, "character": mLoc.Col - 1 + len(m.Name)},
					},
					SelectionRange: map[string]any{
						"start": map[string]int{"line": mLoc.Line - 1, "character": mLoc.Col - 1},
						"end":   map[string]int{"line": mLoc.Line - 1, "character": mLoc.Col - 1 + len(m.Name)},
					},
				})
			}
			symbols = append(symbols, documentSymbol{
				Name:     d.Name,
				Detail:   "trait",
				Kind:     11, // Interface
				Children: methods,
				Range: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len(d.Name)},
				},
				SelectionRange: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len(d.Name)},
				},
			})
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
