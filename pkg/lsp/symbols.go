// File: pkg/lsp/symbols.go

package lsp

import (
	"fmt"
	"strings"

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

func makeLspRange(startLine, startChar, endLine, endChar int) map[string]any {
	return map[string]any{
		"start": map[string]int{"line": startLine, "character": startChar},
		"end":   map[string]int{"line": endLine, "character": endChar},
	}
}

// findIdentifierOnLine returns the byte column of ident in line, requiring
// word boundaries on both sides. It returns -1 when no standalone match exists.
func findIdentifierOnLine(line, ident string) int {
	if ident == "" {
		return -1
	}
	idx := 0
	for idx < len(line) {
		pos := strings.Index(line[idx:], ident)
		if pos == -1 {
			return -1
		}
		actualPos := idx + pos
		leftOK := actualPos == 0 || !isWord(line[actualPos-1])
		rightEnd := actualPos + len(ident)
		rightOK := rightEnd >= len(line) || !isWord(line[rightEnd])
		if leftOK && rightOK {
			return actualPos
		}
		idx = actualPos + 1
	}
	return -1
}

// findMatchingBrace scans forward from startLine and returns the line and
// end-of-line column of the brace that closes the first opening brace.
func findMatchingBrace(lines []string, startLine int) (int, int) {
	depth := 0
	foundOpen := false
	for l := startLine; l < len(lines); l++ {
		line := lines[l]
		inString := false
		var quoteChar byte
		for c := 0; c < len(line); c++ {
			ch := line[c]
			if inString {
				if ch == '\\' && c+1 < len(line) {
					c++
					continue
				}
				if ch == quoteChar {
					inString = false
				}
				continue
			}
			if ch == '"' || ch == '`' {
				inString = true
				quoteChar = ch
				continue
			}
			if ch == '/' && c+1 < len(line) && line[c+1] == '/' {
				break
			}
			if ch == '{' {
				depth++
				foundOpen = true
			} else if ch == '}' {
				depth--
				if foundOpen && depth <= 0 {
					return l, len(line)
				}
			}
		}
	}
	if startLine < len(lines) {
		return startLine, len(lines[startLine])
	}
	return startLine, 0
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
	sourceLines := strings.Split(source, "\n")
	numLines := len(sourceLines)
	if numLines == 0 {
		return symbols
	}

	clampLine := func(line int) int {
		if line < 0 {
			return 0
		}
		if line >= numLines {
			return numLines - 1
		}
		return line
	}

	for _, decl := range prog.Decls {
		loc := decl.Loc()
		startLine := clampLine(loc.Line - 1)
		lineText := sourceLines[startLine]

		switch d := decl.(type) {
		case *compiler.PackageDecl:
			nameCol := findIdentifierOnLine(lineText, d.Name)
			if nameCol == -1 {
				nameCol = strings.Index(lineText, d.Name)
			}
			if nameCol == -1 {
				nameCol = 0
			}
			nameLen := len(d.Name)
			if nameCol+nameLen > len(lineText) {
				nameLen = len(lineText) - nameCol
				if nameLen < 0 {
					nameLen = 0
				}
			}

			symbols = append(symbols, documentSymbol{
				Name:           d.Name,
				Detail:         "package " + d.Name,
				Kind:           4, // Package
				Range:          makeLspRange(startLine, 0, startLine, len(lineText)),
				SelectionRange: makeLspRange(startLine, nameCol, startLine, nameCol+nameLen),
			})

		case *compiler.ImportDecl:
			targetName := d.Path
			if d.Alias != "" {
				targetName = d.Alias
			}
			nameCol := findIdentifierOnLine(lineText, targetName)
			if nameCol == -1 {
				nameCol = strings.Index(lineText, targetName)
			}
			if nameCol == -1 {
				nameCol = strings.Index(lineText, d.Path)
			}
			if nameCol == -1 {
				nameCol = 0
			}
			targetLen := len(targetName)
			if nameCol+targetLen > len(lineText) {
				targetLen = len(lineText) - nameCol
				if targetLen < 0 {
					targetLen = 0
				}
			}

			dispName := d.Path
			if d.Alias != "" {
				dispName = d.Alias + " (" + d.Path + ")"
			}

			symbols = append(symbols, documentSymbol{
				Name:           dispName,
				Detail:         "import",
				Kind:           2, // Module
				Range:          makeLspRange(startLine, 0, startLine, len(lineText)),
				SelectionRange: makeLspRange(startLine, nameCol, startLine, nameCol+targetLen),
			})

		case *compiler.TraitDecl:
			endLine, endChar := findMatchingBrace(sourceLines, startLine)
			tName := shortName(d.Name)
			nameCol := findIdentifierOnLine(lineText, tName)
			if nameCol == -1 {
				nameCol = strings.Index(lineText, tName)
			}
			if nameCol == -1 {
				nameCol = 0
			}
			nameLen := len(tName)
			if nameCol+nameLen > len(lineText) {
				nameLen = len(lineText) - nameCol
				if nameLen < 0 {
					nameLen = 0
				}
			}

			methods := []documentSymbol{}
			for _, m := range d.Methods {
				mLine := clampLine(m.At.Line - 1)
				if mLine < startLine {
					mLine = startLine
				}
				if mLine > endLine {
					mLine = endLine
				}
				mLineText := sourceLines[mLine]
				mNameCol := findIdentifierOnLine(mLineText, m.Name)
				if mNameCol == -1 {
					mNameCol = strings.Index(mLineText, m.Name)
				}
				if mNameCol == -1 {
					mNameCol = 0
				}
				mNameLen := len(m.Name)
				if mNameCol+mNameLen > len(mLineText) {
					mNameLen = len(mLineText) - mNameCol
					if mNameLen < 0 {
						mNameLen = 0
					}
				}

				detail := "fn"
				if m.RetType != nil {
					detail = "fn -> " + m.RetType.String()
				}

				methods = append(methods, documentSymbol{
					Name:           m.Name,
					Detail:         detail,
					Kind:           6, // Method
					Range:          makeLspRange(mLine, 0, mLine, len(mLineText)),
					SelectionRange: makeLspRange(mLine, mNameCol, mLine, mNameCol+mNameLen),
				})
			}

			symbols = append(symbols, documentSymbol{
				Name:           tName,
				Detail:         "trait",
				Kind:           11, // Interface
				Children:       methods,
				Range:          makeLspRange(startLine, 0, endLine, endChar),
				SelectionRange: makeLspRange(startLine, nameCol, startLine, nameCol+nameLen),
			})

		case *compiler.FnDecl:
			endLine := startLine
			endChar := len(lineText)
			if d.Body != nil {
				endLine, endChar = findMatchingBrace(sourceLines, startLine)
			}

			fnName := shortName(d.Name)
			nameCol := findIdentifierOnLine(lineText, fnName)
			if nameCol == -1 {
				nameCol = strings.Index(lineText, fnName)
			}
			if nameCol == -1 {
				nameCol = 0
			}
			nameLen := len(fnName)
			if nameCol+nameLen > len(lineText) {
				nameLen = len(lineText) - nameCol
				if nameLen < 0 {
					nameLen = 0
				}
			}

			kind := 12 // Function
			detail := "fn"
			if d.Receiver != nil {
				kind = 6 // Method
				detail = fmt.Sprintf("(%s) fn", d.Receiver.Type)
			}
			if d.RetType != nil && d.RetType.String() != "void" {
				detail += " -> " + d.RetType.String()
			}

			symbols = append(symbols, documentSymbol{
				Name:           fnName,
				Detail:         detail,
				Kind:           kind,
				Range:          makeLspRange(startLine, 0, endLine, endChar),
				SelectionRange: makeLspRange(startLine, nameCol, startLine, nameCol+nameLen),
			})

		case *compiler.StructDecl:
			endLine, endChar := findMatchingBrace(sourceLines, startLine)
			sName := shortName(d.Name)
			nameCol := findIdentifierOnLine(lineText, sName)
			if nameCol == -1 {
				nameCol = strings.Index(lineText, sName)
			}
			if nameCol == -1 {
				nameCol = 0
			}
			nameLen := len(sName)
			if nameCol+nameLen > len(lineText) {
				nameLen = len(lineText) - nameCol
				if nameLen < 0 {
					nameLen = 0
				}
			}

			fields := []documentSymbol{}
			searchLine := startLine
			for _, f := range d.Fields {
				fLine := -1
				fCol := -1
				for l := searchLine; l <= endLine && l < numLines; l++ {
					col := findIdentifierOnLine(sourceLines[l], f.Name)
					if col != -1 {
						fLine = l
						fCol = col
						searchLine = l
						break
					}
				}
				if fLine == -1 {
					fLine = startLine
					fCol = 0
				}
				fLineText := sourceLines[fLine]
				fNameLen := len(f.Name)
				if fCol+fNameLen > len(fLineText) {
					fNameLen = len(fLineText) - fCol
					if fNameLen < 0 {
						fNameLen = 0
					}
				}

				fields = append(fields, documentSymbol{
					Name:           f.Name,
					Detail:         f.Type.String(),
					Kind:           8, // Field
					Range:          makeLspRange(fLine, 0, fLine, len(fLineText)),
					SelectionRange: makeLspRange(fLine, fCol, fLine, fCol+fNameLen),
				})
			}

			symbols = append(symbols, documentSymbol{
				Name:           sName,
				Detail:         "struct",
				Kind:           23, // Struct
				Children:       fields,
				Range:          makeLspRange(startLine, 0, endLine, endChar),
				SelectionRange: makeLspRange(startLine, nameCol, startLine, nameCol+nameLen),
			})

		case *compiler.EnumDecl:
			endLine, endChar := findMatchingBrace(sourceLines, startLine)
			eName := shortName(d.Name)
			nameCol := findIdentifierOnLine(lineText, eName)
			if nameCol == -1 {
				nameCol = strings.Index(lineText, eName)
			}
			if nameCol == -1 {
				nameCol = 0
			}
			nameLen := len(eName)
			if nameCol+nameLen > len(lineText) {
				nameLen = len(lineText) - nameCol
				if nameLen < 0 {
					nameLen = 0
				}
			}

			variants := []documentSymbol{}
			searchLine := startLine
			for _, v := range d.Variants {
				vLine := -1
				vCol := -1
				for l := searchLine; l <= endLine && l < numLines; l++ {
					col := findIdentifierOnLine(sourceLines[l], v.Name)
					if col != -1 {
						vLine = l
						vCol = col
						searchLine = l
						break
					}
				}
				if vLine == -1 {
					vLine = startLine
					vCol = 0
				}
				vLineText := sourceLines[vLine]
				vNameLen := len(v.Name)
				if vCol+vNameLen > len(vLineText) {
					vNameLen = len(vLineText) - vCol
					if vNameLen < 0 {
						vNameLen = 0
					}
				}

				detail := ""
				if len(v.Types) > 0 {
					var tStrs []string
					for _, vt := range v.Types {
						tStrs = append(tStrs, vt.String())
					}
					detail = "(" + strings.Join(tStrs, ", ") + ")"
				}

				variants = append(variants, documentSymbol{
					Name:           v.Name,
					Detail:         detail,
					Kind:           22, // EnumMember
					Range:          makeLspRange(vLine, 0, vLine, len(vLineText)),
					SelectionRange: makeLspRange(vLine, vCol, vLine, vCol+vNameLen),
				})
			}

			symbols = append(symbols, documentSymbol{
				Name:           eName,
				Detail:         "enum",
				Kind:           10, // Enum
				Children:       variants,
				Range:          makeLspRange(startLine, 0, endLine, endChar),
				SelectionRange: makeLspRange(startLine, nameCol, startLine, nameCol+nameLen),
			})

		case *compiler.ExternFnDecl:
			fnName := d.Name
			nameCol := findIdentifierOnLine(lineText, fnName)
			if nameCol == -1 {
				nameCol = strings.Index(lineText, fnName)
			}
			if nameCol == -1 {
				nameCol = 0
			}
			nameLen := len(fnName)
			if nameCol+nameLen > len(lineText) {
				nameLen = len(lineText) - nameCol
				if nameLen < 0 {
					nameLen = 0
				}
			}

			detail := "extern fn"
			if d.RetType != nil && d.RetType.String() != "void" {
				detail += " -> " + d.RetType.String()
			}

			symbols = append(symbols, documentSymbol{
				Name:           fnName,
				Detail:         detail,
				Kind:           12, // Function
				Range:          makeLspRange(startLine, 0, startLine, len(lineText)),
				SelectionRange: makeLspRange(startLine, nameCol, startLine, nameCol+nameLen),
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
