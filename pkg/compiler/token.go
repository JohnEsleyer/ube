// File: pkg/compiler/token.go

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
	TokenNone   TokenType = "NONE"

	// Keywords
	TokenPackage  TokenType = "package"
	TokenPub      TokenType = "pub"
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
	TokenArena    TokenType = "arena"
	TokenPromote  TokenType = "promote"
	TokenMove     TokenType = "move"
	TokenSpawn    TokenType = "spawn"
	TokenBreak    TokenType = "break"
	TokenContinue TokenType = "continue"
	TokenSelect   TokenType = "select"
	TokenExtern   TokenType = "extern"
	TokenOrElse   TokenType = "or_else"
	TokenImport   TokenType = "import"
	TokenDefer    TokenType = "defer"
	TokenTrait    TokenType = "trait"
	TokenAs       TokenType = "as"

	// Format Strings
	TokenFormatString TokenType = "FORMAT_STRING"

	// Symbols & Operators
	TokenArrow         TokenType = "->"
	TokenFatArrow      TokenType = "=>"
	TokenQuestion      TokenType = "?"
	TokenBang          TokenType = "!"
	TokenColon         TokenType = ":"
	TokenComma         TokenType = ","
	TokenDot           TokenType = "."
	TokenDotDot        TokenType = ".."
	TokenSemicolon     TokenType = ";"
	TokenAssign        TokenType = "="
	TokenPlusAssign    TokenType = "+="
	TokenMinusAssign   TokenType = "-="
	TokenStarAssign    TokenType = "*="
	TokenSlashAssign   TokenType = "/="
	TokenPercentAssign TokenType = "%="
	TokenAmpAssign     TokenType = "&="
	TokenPipeAssign    TokenType = "|="
	TokenCaretAssign   TokenType = "^="
	TokenShlAssign     TokenType = "<<="
	TokenShrAssign     TokenType = ">>="
	TokenEq            TokenType = "=="
	TokenNeq           TokenType = "!="
	TokenLt            TokenType = "<"
	TokenLte           TokenType = "<="
	TokenGt            TokenType = ">"
	TokenGte           TokenType = ">="
	TokenShl           TokenType = "<<"
	TokenShr           TokenType = ">>"
	TokenAnd           TokenType = "&&"
	TokenOr            TokenType = "||"
	TokenPlus          TokenType = "+"
	TokenMinus         TokenType = "-"
	TokenStar          TokenType = "*"
	TokenSlash         TokenType = "/"
	TokenPercent       TokenType = "%"
	TokenAmp           TokenType = "&"
	TokenPipe          TokenType = "|"
	TokenCaret         TokenType = "^"
	TokenTilde         TokenType = "~"
	TokenRawString     TokenType = "RAW_STRING"

	// Delimiters
	TokenLParen   TokenType = "("
	TokenRParen   TokenType = ")"
	TokenLBrace   TokenType = "{"
	TokenRBrace   TokenType = "}"
	TokenLBracket TokenType = "["
	TokenRBracket TokenType = "]"
)

type Token struct {
	Type TokenType
	Val  string
	Loc  SourceLocation
}

func (t Token) String() string {
	return fmt.Sprintf("%s(%s) at %s", t.Type, t.Val, t.Loc)
}
