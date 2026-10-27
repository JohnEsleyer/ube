package compiler

import (
	"fmt"
	"strings"
	"unicode"
)

var keywords = map[string]TokenType{
	"fn":      TokenFn,
	"let":     TokenLet,
	"mut":     TokenMut,
	"struct":  TokenStruct,
	"enum":    TokenEnum,
	"match":   TokenMatch,
	"if":      TokenIf,
	"else":    TokenElse,
	"while":   TokenWhile,
	"for":     TokenFor,
	"in":      TokenIn,
	"return":  TokenReturn,
	"tuber":   TokenTuber,
	"spawn":   TokenSpawn,
	"select":  TokenSelect,
	"extern":  TokenExtern,
	"or_else": TokenOrElse,
	"import":  TokenImport,
	"true":    TokenBool,
	"false":   TokenBool,
	"null":    TokenNull,
}

type Lexer struct {
	file   string
	source []rune
	pos    int
	line   int
	col    int
}

func NewLexer(file, source string) *Lexer {
	return &Lexer{
		file:   file,
		source: []rune(source),
		pos:    0,
		line:   1,
		col:    1,
	}
}

func (l *Lexer) peek(offset int) rune {
	if l.pos+offset >= len(l.source) {
		return 0
	}
	return l.source[l.pos+offset]
}

func (l *Lexer) advance() rune {
	if l.pos >= len(l.source) {
		return 0
	}
	ch := l.source[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

func (l *Lexer) Tokenize() ([]Token, error) {
	var tokens []Token

	for l.pos < len(l.source) {
		ch := l.peek(0)

		if unicode.IsSpace(ch) {
			l.advance()
			continue
		}

		// Comments
		if ch == '/' && l.peek(1) == '/' {
			for l.pos < len(l.source) && l.peek(0) != '\n' {
				l.advance()
			}
			continue
		}
		if ch == '/' && l.peek(1) == '*' {
			l.advance()
			l.advance()
			for l.pos < len(l.source) && !(l.peek(0) == '*' && l.peek(1) == '/') {
				l.advance()
			}
			if l.pos < len(l.source) {
				l.advance()
				l.advance()
			}
			continue
		}

		loc := SourceLocation{File: l.file, Line: l.line, Col: l.col}

		// Two-character operators
		if ch == '-' && l.peek(1) == '>' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenArrow, Val: "->", Loc: loc})
			continue
		}
		if ch == '=' && l.peek(1) == '>' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenFatArrow, Val: "=>", Loc: loc})
			continue
		}
		if ch == '=' && l.peek(1) == '=' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenEq, Val: "==", Loc: loc})
			continue
		}
		if ch == '!' && l.peek(1) == '=' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenNeq, Val: "!=", Loc: loc})
			continue
		}
		if ch == '<' && l.peek(1) == '=' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenLte, Val: "<=", Loc: loc})
			continue
		}
		if ch == '>' && l.peek(1) == '=' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenGte, Val: ">=", Loc: loc})
			continue
		}

		// Single character delimiters
		simple := map[rune]TokenType{
			':': TokenColon, ',': TokenComma, '.': TokenDot, ';': TokenSemicolon,
			'=': TokenAssign, '?': TokenQuestion, '!': TokenBang, '<': TokenLt,
			'>': TokenGt, '+': TokenPlus, '-': TokenMinus, '*': TokenStar,
			'/': TokenSlash, '%': TokenPercent, '&': TokenAmp, '(': TokenLParen,
			')': TokenRParen, '{': TokenLBrace, '}': TokenRBrace, '[': TokenLBracket,
			']': TokenRBracket,
		}
		if tokType, ok := simple[ch]; ok {
			tokens = append(tokens, Token{Type: tokType, Val: string(l.advance()), Loc: loc})
			continue
		}

		// Strings
		if ch == '"' {
			l.advance()
			var buf strings.Builder
			for l.pos < len(l.source) && l.peek(0) != '"' {
				if l.peek(0) == '\\' {
					l.advance()
					esc := l.advance()
					switch esc {
					case 'n':
						buf.WriteRune('\n')
					case 't':
						buf.WriteRune('\t')
					case '"':
						buf.WriteRune('"')
					case '\\':
						buf.WriteRune('\\')
					default:
						buf.WriteRune(esc)
					}
				} else {
					buf.WriteRune(l.advance())
				}
			}
			if l.peek(0) != '"' {
				return nil, fmt.Errorf("[%s] Error: Unterminated string literal", loc)
			}
			l.advance()
			tokens = append(tokens, Token{Type: TokenString, Val: buf.String(), Loc: loc})
			continue
		}

		// Numbers
		if unicode.IsDigit(ch) {
			var buf strings.Builder
			isFloat := false
			for l.pos < len(l.source) && (unicode.IsDigit(l.peek(0)) || l.peek(0) == '.') {
				if l.peek(0) == '.' {
					if unicode.IsDigit(l.peek(1)) {
						isFloat = true
						buf.WriteRune(l.advance())
					} else {
						break
					}
				} else {
					buf.WriteRune(l.advance())
				}
			}
			if isFloat {
				tokens = append(tokens, Token{Type: TokenFloat, Val: buf.String(), Loc: loc})
			} else {
				tokens = append(tokens, Token{Type: TokenInt, Val: buf.String(), Loc: loc})
			}
			continue
		}

		// Identifiers & Keywords
		if unicode.IsLetter(ch) || ch == '_' {
			var buf strings.Builder
			for l.pos < len(l.source) && (unicode.IsLetter(l.peek(0)) || unicode.IsDigit(l.peek(0)) || l.peek(0) == '_') {
				buf.WriteRune(l.advance())
			}
			id := buf.String()
			tokType, ok := keywords[id]
			if !ok {
				tokType = TokenIdent
			}
			tokens = append(tokens, Token{Type: tokType, Val: id, Loc: loc})
			continue
		}

		// Directives like #c_include, #c_source
		if ch == '#' {
			for l.pos < len(l.source) && l.peek(0) != '\n' {
				l.advance()
			}
			continue
		}

		return nil, fmt.Errorf("[%s] Error: Unexpected character: '%c'", loc, ch)
	}

	tokens = append(tokens, Token{Type: TokenEOF, Val: "", Loc: SourceLocation{File: l.file, Line: l.line, Col: l.col}})
	return tokens, nil
}
