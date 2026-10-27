package compiler

import "fmt"

type TokenType string

const (
	TokenEOF    TokenType = "EOF"
	TokenIdent  TokenType = "IDENT"
	TokenInt    TokenType = "INT"
	TokenFloat  TokenType = "FLOAT"
	TokenString TokenType = "STRING"
	TokenBool   TokenType = "BOOL"
	TokenNull   TokenType = "NULL"

	// Keywords
	TokenFn       TokenType = "fn"
	TokenLet      TokenType = "let"
	TokenMut      TokenType = "mut"
	TokenStruct   TokenType = "struct"
	TokenEnum     TokenType = "enum"
	TokenMatch    TokenType = "match"
	TokenIf       TokenType = "if"
	TokenElse     TokenType = "else"
	TokenWhile    TokenType = "while"
	TokenFor      TokenType = "for"
	TokenIn       TokenType = "in"
	TokenReturn   TokenType = "return"
	TokenScope    TokenType = "scope"
	TokenSpawn    TokenType = "spawn"
	TokenBreak    TokenType = "break"
	TokenContinue TokenType = "continue"
	TokenSelect   TokenType = "select"
	TokenExtern   TokenType = "extern"
	TokenOrElse   TokenType = "or_else"
	TokenImport   TokenType = "import"
	TokenDefer    TokenType = "defer"
	TokenUnsafe   TokenType = "unsafe"

	// Symbols & Operators
	TokenArrow     TokenType = "->"
	TokenFatArrow  TokenType = "=>"
	TokenQuestion  TokenType = "?"
	TokenBang      TokenType = "!"
	TokenColon     TokenType = ":"
	TokenComma     TokenType = ","
	TokenDot       TokenType = "."
	TokenSemicolon TokenType = ";"
	TokenAssign    TokenType = "="
	TokenEq        TokenType = "=="
	TokenNeq       TokenType = "!="
	TokenLt        TokenType = "<"
	TokenLte       TokenType = "<="
	TokenGt        TokenType = ">"
	TokenGte       TokenType = ">="
	TokenAnd       TokenType = "&&"
	TokenOr        TokenType = "||"
	TokenPlus      TokenType = "+"
	TokenMinus     TokenType = "-"
	TokenStar      TokenType = "*"
	TokenSlash     TokenType = "/"
	TokenPercent   TokenType = "%"
	TokenAmp       TokenType = "&"

	// Delimiters
	TokenLParen   TokenType = "("
	TokenRParen   TokenType = ")"
	TokenLBrace   TokenType = "{"
	TokenRBrace   TokenType = "}"
	TokenLBracket TokenType = "["
	TokenRBracket TokenType = "]"
)

type SourceLocation struct {
	File string
	Line int
	Col  int
}

func (s SourceLocation) String() string {
	return fmt.Sprintf("%s:%d:%d", s.File, s.Line, s.Col)
}

type Token struct {
	Type TokenType
	Val  string
	Loc  SourceLocation
}
