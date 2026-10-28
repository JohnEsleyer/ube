// Package formatter implements Ube's canonical source formatter. It is shared
// by the `ube fmt` CLI command and the language server's
// textDocument/formatting provider.
package formatter

import (
	"strings"
	"unicode"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

type tokenKind int

const (
	tokWord tokenKind = iota
	tokString
	tokOperator
	tokPunct
	tokComment
)

type formatToken struct {
	kind tokenKind
	text string
}

// Format takes Ube source code and returns the canonical formatted version.
// The source is parsed first so that files with syntax errors are never
// rewritten.
func Format(filename, source string) (string, error) {
	tokens, err := compiler.NewLexer(filename, source).Tokenize()
	if err != nil {
		return "", err
	}
	if _, err := compiler.NewParser(tokens).ParseProgram(); err != nil {
		return "", err
	}

	normalized := strings.ReplaceAll(source, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")

	var formatted []string
	indent := 0

	appendLine := func(line string) {
		formatted = append(formatted, line)
	}

	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			if len(formatted) > 0 && formatted[len(formatted)-1] != "" {
				appendLine("")
			}
			continue
		}

		// Comment-only lines keep the current indentation.
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			appendLine(strings.Repeat("    ", maxInt(indent, 0)) + formatLine(trimmed))
			continue
		}

		leadingClosers := 0
		for _, r := range trimmed {
			if r == '}' {
				leadingClosers++
			} else {
				break
			}
		}
		effectiveIndent := indent - leadingClosers
		if effectiveIndent < 0 {
			effectiveIndent = 0
		}

		// Exactly one blank line between top-level declarations.
		if effectiveIndent == 0 && indent == 0 && isDeclStart(trimmed) && len(formatted) > 0 && formatted[len(formatted)-1] != "" {
			appendLine("")
		}

		appendLine(strings.Repeat("    ", effectiveIndent) + formatLine(trimmed))

		indent += braceDelta(trimmed)
		if indent < 0 {
			indent = 0
		}
	}

	for len(formatted) > 0 && formatted[0] == "" {
		formatted = formatted[1:]
	}
	for len(formatted) > 0 && formatted[len(formatted)-1] == "" {
		formatted = formatted[:len(formatted)-1]
	}

	return strings.Join(formatted, "\n") + "\n", nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isDeclStart(line string) bool {
	for _, prefix := range []string{"fn ", "struct ", "enum ", "trait ", "extern "} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func braceDelta(line string) int {
	delta := 0
	for _, tok := range lexLine(line) {
		if tok.kind != tokPunct {
			continue
		}
		switch tok.text {
		case "{":
			delta++
		case "}":
			delta--
		}
	}
	return delta
}

func lexLine(line string) []formatToken {
	var tokens []formatToken
	i := 0
	for i < len(line) {
		c := line[i]
		if c == ' ' || c == '\t' {
			i++
			continue
		}

		if c == '/' && i+1 < len(line) && line[i+1] == '/' {
			tokens = append(tokens, formatToken{kind: tokComment, text: line[i:]})
			break
		}
		if c == '/' && i+1 < len(line) && line[i+1] == '*' {
			end := strings.Index(line[i+2:], "*/")
			if end < 0 {
				tokens = append(tokens, formatToken{kind: tokComment, text: line[i:]})
				break
			}
			end += i + 4
			tokens = append(tokens, formatToken{kind: tokComment, text: line[i:end]})
			i = end
			continue
		}

		if c == '"' || ((c == 'f' || c == 'F') && i+1 < len(line) && line[i+1] == '"') {
			start := i
			if c != '"' {
				i++
			}
			i++ // opening quote
			for i < len(line) {
				if line[i] == '\\' && i+1 < len(line) {
					i += 2
					continue
				}
				if line[i] == '"' {
					i++
					break
				}
				i++
			}
			tokens = append(tokens, formatToken{kind: tokString, text: line[start:i]})
			continue
		}

		if isIdentStart(c) {
			start := i
			for i < len(line) && isIdentPart(line[i]) {
				i++
			}
			tokens = append(tokens, formatToken{kind: tokWord, text: line[start:i]})
			continue
		}

		if isDigit(c) {
			start := i
			for i < len(line) && isIdentPart(line[i]) {
				i++
			}
			if i+1 < len(line) && line[i] == '.' && isDigit(line[i+1]) {
				i++
				for i < len(line) && isDigit(line[i]) {
					i++
				}
			}
			tokens = append(tokens, formatToken{kind: tokWord, text: line[start:i]})
			continue
		}

		matched := ""
		for _, op := range []string{"<<=", ">>=", "->", "=>", "==", "!=", "<=", ">=", "&&", "||", "<<", ">>", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^="} {
			if strings.HasPrefix(line[i:], op) {
				matched = op
				break
			}
		}
		if matched != "" {
			tokens = append(tokens, formatToken{kind: tokOperator, text: matched})
			i += len(matched)
			continue
		}

		text := string(c)
		kind := tokPunct
		if isOperator(text) {
			kind = tokOperator
		}
		tokens = append(tokens, formatToken{kind: kind, text: text})
		i++
	}
	return tokens
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isOperator(s string) bool {
	switch s {
	case "+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=",
		"&&", "||", "=", "->", "=>", "&", "|", "^", "<<", ">>", "~", "!",
		"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=":
		return true
	}
	return false
}

func isWordLike(tok formatToken) bool {
	return tok.kind == tokWord || tok.kind == tokString
}

func isUnaryOperator(op, prev string) bool {
	switch op {
	case "-", "!", "&", "*", "~":
	default:
		return false
	}
	if prev == "" {
		return true
	}
	if isOperator(prev) {
		return true
	}
	switch prev {
	case "(", "[", "{", ",", ":", "=", "return", "let", "if", "while", "match", "in", "else", "or_else", "=>":
		return true
	}
	return false
}

func formatLine(line string) string {
	tokens := lexLine(line)
	var out strings.Builder
	prev := ""
	prevUnary := false

	for i, tok := range tokens {
		if tok.kind == tokComment {
			if out.Len() > 0 && !strings.HasSuffix(out.String(), " ") {
				out.WriteByte(' ')
			}
			out.WriteString(normalizeComment(tok.text))
			break
		}

		text := tok.text
		space := true

		switch {
		case i == 0:
			space = false
		case text == ")" || text == "]" || text == "," || text == ";" || text == "." || text == ":" || text == "?":
			space = false
		case text == "}":
			space = prev != "" && prev != "{" && prev != "(" && prev != "[" && prev != "."
		case text == "(":
			space = !(isWordLike(formatToken{kind: tokenKindOf(prev)}) || prev == ")" || prev == "]" || prev == "}")
		case text == "[":
			space = !(isWordLike(formatToken{kind: tokenKindOf(prev)}) || prev == ")" || prev == "]" || prev == "}")
		case text == "{":
			space = !(prev == "(" || prev == "[" || prev == ".")
		case tok.kind == tokOperator:
			if isUnaryOperator(text, prev) {
				switch prev {
				case "", "(", "[", "{", ",", ":", "=", "return", "let", "if", "while", "match", "in", "else", "or_else", "=>":
					space = false
				default:
					space = !isOperator(prev)
				}
			} else {
				space = true
			}
		default:
			space = prev != "" && prev != "(" && prev != "[" && prev != "]" && prev != "." && !prevUnary
		}

		if space && out.Len() > 0 && !strings.HasSuffix(out.String(), " ") {
			out.WriteByte(' ')
		}
		out.WriteString(text)
		prevUnary = tok.kind == tokOperator && isUnaryOperator(text, prev)
		prev = text
	}

	return strings.TrimRight(out.String(), " ")
}

func tokenKindOf(text string) tokenKind {
	if text == "" {
		return tokPunct
	}
	if isIdentStart(text[0]) || isDigit(text[0]) {
		return tokWord
	}
	return tokPunct
}

func normalizeComment(comment string) string {
	if !strings.HasPrefix(comment, "//") {
		return comment
	}
	rest := comment[2:]
	if rest == "" {
		return "//"
	}
	if !unicode.IsSpace(rune(rest[0])) {
		return "// " + rest
	}
	return "//" + rest
}
