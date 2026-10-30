package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

type parseError struct {
	loc SourceLocation
	msg string
}

type Parser struct {
	tokens []Token
	pos    int
	errors []parseError
}

func NewParser(tokens []Token) *Parser {
	return &Parser{tokens: tokens, pos: 0}
}

func (p *Parser) peek(offset int) Token {
	idx := p.pos + offset
	if idx >= len(p.tokens) {
		if len(p.tokens) > 0 {
			return p.tokens[len(p.tokens)-1]
		}
		return Token{Type: TokenEOF}
	}
	return p.tokens[idx]
}

func (p *Parser) check(t TokenType) bool {
	return p.peek(0).Type == t
}

func (p *Parser) advance() Token {
	tok := p.peek(0)
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

func (p *Parser) errorAt(loc SourceLocation, msg string) {
	panic(parseError{loc: loc, msg: msg})
}

func (p *Parser) expect(t TokenType, msg string) Token {
	if p.check(t) {
		return p.advance()
	}
	tok := p.peek(0)
	p.errorAt(tok.Loc, fmt.Sprintf("%s (expected '%s', got '%s')", msg, t, tok.Type))
	return Token{}
}

func (p *Parser) ParseProgram() (*Program, error) {
	loc := p.peek(0).Loc
	var decls []Decl
	for !p.check(TokenEOF) {
		before := p.pos
		d, perr := p.parseDeclSafe()
		if perr != nil {
			p.errors = append(p.errors, *perr)
			p.synchronizeDecl()
		} else if d != nil {
			decls = append(decls, d)
		}
		if p.pos == before && !p.check(TokenEOF) {
			p.advance()
		}
	}
	if len(p.errors) > 0 {
		msgs := make([]string, 0, len(p.errors))
		for _, pe := range p.errors {
			msgs = append(msgs, fmt.Sprintf("[%s] Syntax Error: %s", pe.loc, pe.msg))
		}
		return nil, fmt.Errorf("%s", strings.Join(msgs, "\n"))
	}
	return &Program{At: loc, Decls: decls}, nil
}

func (p *Parser) synchronizeDecl() {
	for !p.check(TokenEOF) {
		switch p.peek(0).Type {
		case TokenPub, TokenFn, TokenStruct, TokenEnum, TokenExtern, TokenImport, TokenTrait:
			return
		}
		p.advance()
	}
}

func (p *Parser) parseDeclSafe() (d Decl, perr *parseError) {
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(parseError); ok {
				perr = &pe
				d = nil
			} else {
				panic(r)
			}
		}
	}()
	return p.parseDecl(), nil
}

func (p *Parser) parseDecl() Decl {
	isPub := false
	if p.check(TokenPub) {
		p.advance()
		isPub = true
	}

	tok := p.peek(0)
	switch tok.Type {
	case TokenImport:
		if isPub {
			p.errorAt(tok.Loc, "'pub' cannot be applied to 'import'")
		}
		loc := p.advance().Loc
		path := p.expect(TokenString, "Expected import path").Val
		return &ImportDecl{At: loc, Path: path}
	case TokenStruct:
		d := p.parseStructDecl()
		d.IsPub = isPub
		return d
	case TokenEnum:
		d := p.parseEnumDecl()
		d.IsPub = isPub
		return d
	case TokenTrait:
		d := p.parseTraitDecl()
		d.IsPub = isPub
		return d
	case TokenExtern:
		d := p.parseExternDecl()
		d.IsPub = isPub
		return d
	case TokenFn:
		d := p.parseFnDecl()
		d.IsPub = isPub
		return d
	default:
		p.errorAt(tok.Loc, fmt.Sprintf("Unexpected top-level token '%s'", tok.Val))
		return nil
	}
}

func (p *Parser) parseTraitDecl() *TraitDecl {
	loc := p.expect(TokenTrait, "Expected 'trait'").Loc
	name := p.expect(TokenIdent, "Expected trait name").Val
	p.expect(TokenLBrace, "Expected '{' after trait name")

	var methods []TraitMethodDecl
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		mLoc := p.expect(TokenFn, "Expected 'fn' in trait declaration").Loc
		mName := p.expect(TokenIdent, "Expected method name").Val
		p.expect(TokenLParen, "Expected '(' for method parameters")
		var params []FieldDecl
		for !p.check(TokenRParen) && !p.check(TokenEOF) {
			pName := p.expect(TokenIdent, "Expected parameter name").Val
			p.expect(TokenColon, "Expected ':'")
			pType := p.parseType()
			params = append(params, FieldDecl{Name: pName, Type: pType})
			if p.check(TokenComma) {
				p.advance()
			}
		}
		p.expect(TokenRParen, "Expected ')'")
		var retType UbeType = PrimitiveType{Name: "void"}
		if p.check(TokenArrow) {
			p.advance()
			retType = p.parseType()
		}
		methods = append(methods, TraitMethodDecl{
			At:      mLoc,
			Name:    mName,
			Params:  params,
			RetType: retType,
		})
		if p.check(TokenSemicolon) || p.check(TokenComma) {
			p.advance()
		}
	}
	p.expect(TokenRBrace, "Expected '}' closing trait")
	return &TraitDecl{At: loc, Name: name, Methods: methods}
}

func (p *Parser) parseStructDecl() *StructDecl {
	loc := p.expect(TokenStruct, "Expected 'struct'").Loc
	name := p.expect(TokenIdent, "Expected struct name").Val
	var typeParams []TypeParam
	if p.check(TokenLBracket) {
		p.advance()
		for !p.check(TokenRBracket) && !p.check(TokenEOF) {
			tName := p.expect(TokenIdent, "Expected type parameter").Val
			constraint := ""
			if p.check(TokenColon) {
				p.advance()
				constraint = p.expect(TokenIdent, "Expected trait constraint").Val
			}
			typeParams = append(typeParams, TypeParam{Name: tName, Constraint: constraint})
			if p.check(TokenComma) {
				p.advance()
			}
		}
		p.expect(TokenRBracket, "Expected ']' after type parameters")
	}

	p.expect(TokenLBrace, "Expected '{' after struct name")

	var fields []FieldDecl
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		fPub := false
		fMut := false
		for p.check(TokenPub) || p.check(TokenMut) {
			if p.check(TokenPub) {
				p.advance()
				fPub = true
			} else if p.check(TokenMut) {
				p.advance()
				fMut = true
			}
		}
		fName := p.expect(TokenIdent, "Expected field name").Val
		p.expect(TokenColon, "Expected ':' after field name")
		fType := p.parseType()
		fields = append(fields, FieldDecl{Name: fName, Type: fType, IsMut: fMut, IsPub: fPub})
		if p.check(TokenComma) {
			p.advance()
		}
	}
	p.expect(TokenRBrace, "Expected '}' closing struct")
	return &StructDecl{At: loc, Name: name, TypeParams: typeParams, Fields: fields}
}

func (p *Parser) parseEnumDecl() *EnumDecl {
	loc := p.expect(TokenEnum, "Expected 'enum'").Loc
	name := p.expect(TokenIdent, "Expected enum name").Val
	p.expect(TokenLBrace, "Expected '{' after enum name")

	var variants []VariantDecl
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		vName := p.expect(TokenIdent, "Expected variant name").Val
		var vTypes []UbeType
		if p.check(TokenLParen) {
			p.advance()
			for !p.check(TokenRParen) && !p.check(TokenEOF) {
				vTypes = append(vTypes, p.parseType())
				if p.check(TokenComma) {
					p.advance()
				}
			}
			p.expect(TokenRParen, "Expected ')' after variant parameters")
		}
		variants = append(variants, VariantDecl{Name: vName, Types: vTypes})
		if p.check(TokenComma) {
			p.advance()
		}
	}
	p.expect(TokenRBrace, "Expected '}' closing enum")
	return &EnumDecl{At: loc, Name: name, Variants: variants}
}

func (p *Parser) parseExternDecl() *ExternFnDecl {
	loc := p.expect(TokenExtern, "Expected 'extern'").Loc
	p.expect(TokenFn, "Expected 'fn' after extern")
	name := p.expect(TokenIdent, "Expected extern function name").Val
	p.expect(TokenLParen, "Expected '(' for parameter list")

	var params []FieldDecl
	for !p.check(TokenRParen) && !p.check(TokenEOF) {
		pName := p.expect(TokenIdent, "Expected parameter name").Val
		p.expect(TokenColon, "Expected ':' after parameter name")
		pType := p.parseType()
		params = append(params, FieldDecl{Name: pName, Type: pType})
		if p.check(TokenComma) {
			p.advance()
		}
	}
	p.expect(TokenRParen, "Expected ')'")

	var retType UbeType = PrimitiveType{Name: "void"}
	if p.check(TokenArrow) {
		p.advance()
		retType = p.parseType()
	}
	return &ExternFnDecl{At: loc, Name: name, Params: params, RetType: retType}
}

func (p *Parser) parseFnDecl() *FnDecl {
	loc := p.expect(TokenFn, "Expected 'fn'").Loc
	var receiver *FieldDecl
	if p.check(TokenLParen) {
		p.advance()
		isMutRecv := false
		if p.check(TokenMut) {
			p.advance()
			isMutRecv = true
		}
		rName := p.expect(TokenIdent, "Expected receiver variable name").Val
		rType := p.parseType()
		p.expect(TokenRParen, "Expected ')' closing receiver")
		receiver = &FieldDecl{Name: rName, Type: rType, IsMut: isMutRecv}
	}

	name := p.expect(TokenIdent, "Expected function name").Val
	var typeParams []TypeParam
	if p.check(TokenLBracket) {
		p.advance()
		for !p.check(TokenRBracket) && !p.check(TokenEOF) {
			tName := p.expect(TokenIdent, "Expected type parameter").Val
			constraint := ""
			if p.check(TokenColon) {
				p.advance()
				constraint = p.expect(TokenIdent, "Expected trait constraint").Val
			}
			typeParams = append(typeParams, TypeParam{Name: tName, Constraint: constraint})
			if p.check(TokenComma) {
				p.advance()
			}
		}
		p.expect(TokenRBracket, "Expected ']' after type parameters")
	}

	p.expect(TokenLParen, "Expected '(' for arguments")

	var params []FieldDecl
	for !p.check(TokenRParen) && !p.check(TokenEOF) {
		pName := p.expect(TokenIdent, "Expected parameter name").Val
		p.expect(TokenColon, "Expected ':'")
		pType := p.parseType()
		params = append(params, FieldDecl{Name: pName, Type: pType})
		if p.check(TokenComma) {
			p.advance()
		}
	}
	p.expect(TokenRParen, "Expected ')'")

	var retType UbeType = PrimitiveType{Name: "void"}
	if p.check(TokenArrow) {
		p.advance()
		retType = p.parseType()
	}

	body := p.parseBlock()
	return &FnDecl{
		At:         loc,
		Name:       name,
		TypeParams: typeParams,
		Receiver:   receiver,
		Params:     params,
		RetType:    retType,
		Body:       body,
	}
}

func (p *Parser) parseType() UbeType {
	tok := p.peek(0)
	var t UbeType

	if tok.Type == TokenStar {
		p.advance()
		base := p.parseType()
		t = PointerType{Base: base}
	} else if tok.Type == TokenLBracket && p.peek(1).Type == TokenRBracket {
		p.advance()
		p.advance()
		elem := p.parseType()
		t = SliceType{Elem: elem}
	} else if tok.Type == TokenLBracket && p.peek(1).Type == TokenInt {
		p.advance()
		n, _ := strconv.Atoi(p.advance().Val)
		if n <= 0 {
			p.errorAt(tok.Loc, "array size must be greater than 0")
		}
		p.expect(TokenRBracket, "Expected ']' after array size")
		t = ArrayType{Elem: p.parseType(), Size: n}
	} else if tok.Type == TokenIdent && tok.Val == "Chan" {
		p.advance()
		p.expect(TokenLBracket, "Expected '[' for Chan element type")
		elem := p.parseType()
		p.expect(TokenRBracket, "Expected ']' after Chan element type")
		t = ChanType{Elem: elem}
	} else if tok.Type == TokenIdent && tok.Val == "Pool" {
		p.advance()
		p.expect(TokenLBracket, "Expected '[' for Pool element type")
		elem := p.parseType()
		p.expect(TokenRBracket, "Expected ']' after Pool element type")
		t = PoolType{Elem: elem}
	} else if tok.Type == TokenIdent && tok.Val == "StringBuilder" {
		p.advance()
		t = StringBuilderType{}
	} else if tok.Type == TokenIdent {
		idName := p.advance().Val
		if p.check(TokenLBracket) {
			p.advance()
			var typeArgs []UbeType
			for !p.check(TokenRBracket) && !p.check(TokenEOF) {
				typeArgs = append(typeArgs, p.parseType())
				if p.check(TokenComma) {
					p.advance()
				}
			}
			p.expect(TokenRBracket, "Expected ']' after type arguments")
			t = GenericInstanceType{Base: idName, TypeArgs: typeArgs}
		} else {
			switch idName {
			case "i32", "u32", "i64", "u64", "u8", "f32", "f64", "bool", "void":
				t = PrimitiveType{Name: idName}
			case "string":
				t = StringType{}
			default:
				t = CustomType{Name: idName}
			}
		}
	} else {
		p.errorAt(tok.Loc, fmt.Sprintf("Invalid type annotation: '%s'", tok.Val))
	}

	if p.check(TokenQuestion) {
		p.advance()
		t = OptionalType{Inner: t}
	}

	if p.check(TokenBang) {
		p.advance()
		errT := p.parseType()
		t = ResultType{Ok: t, Err: errT}
	}

	return t
}
