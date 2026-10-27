package compiler

import (
	"fmt"
)

type parseError struct {
	loc SourceLocation
	msg string
}

type Parser struct {
	tokens []Token
	pos    int
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

func (p *Parser) ParseProgram() (prog *Program, err error) {
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(parseError); ok {
				err = fmt.Errorf("[%s] Syntax Error: %s", pe.loc, pe.msg)
				prog = nil
			} else {
				panic(r)
			}
		}
	}()

	loc := p.peek(0).Loc
	var decls []Decl
	for !p.check(TokenEOF) {
		decls = append(decls, p.parseDecl())
	}
	return &Program{At: loc, Decls: decls}, nil
}

func (p *Parser) parseDecl() Decl {
	tok := p.peek(0)
	switch tok.Type {
	case TokenStruct:
		return p.parseStructDecl()
	case TokenEnum:
		return p.parseEnumDecl()
	case TokenExtern:
		return p.parseExternDecl()
	case TokenFn:
		return p.parseFnDecl()
	default:
		p.errorAt(tok.Loc, fmt.Sprintf("Unexpected top-level token '%s'", tok.Val))
		return nil
	}
}

func (p *Parser) parseStructDecl() *StructDecl {
	loc := p.expect(TokenStruct, "Expected 'struct'").Loc
	name := p.expect(TokenIdent, "Expected struct name").Val
	p.expect(TokenLBrace, "Expected '{' after struct name")

	var fields []FieldDecl
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		fName := p.expect(TokenIdent, "Expected field name").Val
		p.expect(TokenColon, "Expected ':' after field name")
		fType := p.parseType()
		fields = append(fields, FieldDecl{Name: fName, Type: fType})
		if p.check(TokenComma) {
			p.advance()
		}
	}
	p.expect(TokenRBrace, "Expected '}' closing struct")
	return &StructDecl{At: loc, Name: name, Fields: fields}
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
		rName := p.expect(TokenIdent, "Expected receiver variable name").Val
		rType := p.parseType()
		p.expect(TokenRParen, "Expected ')' closing receiver")
		receiver = &FieldDecl{Name: rName, Type: rType}
	}

	name := p.expect(TokenIdent, "Expected function name").Val
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
		At:       loc,
		Name:     name,
		Receiver: receiver,
		Params:   params,
		RetType:  retType,
		Body:     body,
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
	} else if tok.Type == TokenIdent {
		idName := p.advance().Val
		switch idName {
		case "i32", "u32", "i64", "u64", "u8", "f32", "f64", "bool", "void":
			t = PrimitiveType{Name: idName}
		case "string":
			t = StringType{}
		default:
			t = CustomType{Name: idName}
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

func (p *Parser) parseBlock() *BlockStmt {
	loc := p.expect(TokenLBrace, "Expected '{' to begin block").Loc
	var stmts []Stmt
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		stmts = append(stmts, p.parseStmt())
	}
	p.expect(TokenRBrace, "Expected '}' to close block")
	return &BlockStmt{At: loc, Stmts: stmts}
}

func (p *Parser) parseStmt() Stmt {
	tok := p.peek(0)

	switch tok.Type {
	case TokenLet:
		return p.parseLetStmt()
	case TokenReturn:
		return p.parseReturnStmt()
	case TokenIf:
		return p.parseIfStmt()
	case TokenWhile:
		return p.parseWhileStmt()
	case TokenFor:
		return p.parseForStmt()
	case TokenScope:
		p.advance()
		return &ScopeStmt{At: tok.Loc, Body: p.parseBlock()}
	case TokenWorker:
		p.advance()
		call := p.parseExpr()
		callExpr, ok := call.(*CallExpr)
		if !ok {
			p.errorAt(tok.Loc, "worker target must be a direct function call")
		}
		return &WorkerStmt{At: tok.Loc, Call: callExpr}
	case TokenMatch:
		return p.parseMatchStmt()
	case TokenSelect:
		return p.parseSelectStmt()
	}

	expr := p.parseExpr()
	if p.check(TokenAssign) {
		p.advance()
		rhs := p.parseExpr()
		return &AssignStmt{At: expr.Loc(), LHS: expr, RHS: rhs}
	}
	return &ExprStmt{At: expr.Loc(), Expr: expr}
}

func (p *Parser) parseSelectStmt() *SelectStmt {
	loc := p.expect(TokenSelect, "Expected 'select'").Loc
	p.expect(TokenLBrace, "Expected '{' after select")

	var cases []SelectCase
	var defBlock *BlockStmt

	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		cLoc := p.peek(0).Loc
		if p.check(TokenIdent) && p.peek(0).Val == "default" {
			p.advance()
			p.expect(TokenFatArrow, "Expected '=>' after default")
			defBlock = p.parseBlock()
			if p.check(TokenComma) {
				p.advance()
			}
			continue
		}

		vName := p.expect(TokenIdent, "Expected binding identifier in select case").Val
		p.expect(TokenAssign, "Expected '=' in select case")
		chExpr := p.parseExpr()
		p.expect(TokenFatArrow, "Expected '=>' after select condition")
		b := p.parseBlock()
		cases = append(cases, SelectCase{At: cLoc, VarName: vName, Chan: chExpr, Body: b})
		if p.check(TokenComma) {
			p.advance()
		}
	}
	p.expect(TokenRBrace, "Expected '}' closing select")
	return &SelectStmt{At: loc, Cases: cases, Default: defBlock}
}

func (p *Parser) parseLetStmt() *LetStmt {
	loc := p.expect(TokenLet, "Expected 'let'").Loc
	isMut := false
	if p.check(TokenMut) {
		p.advance()
		isMut = true
	}
	name := p.expect(TokenIdent, "Expected variable name").Val

	var typeAnnot UbeType
	if p.check(TokenColon) {
		p.advance()
		typeAnnot = p.parseType()
	}

	p.expect(TokenAssign, "Expected '=' in variable declaration")
	init := p.parseExpr()
	return &LetStmt{At: loc, Name: name, Mut: isMut, Type: typeAnnot, Value: init}
}

func (p *Parser) parseReturnStmt() *ReturnStmt {
	loc := p.expect(TokenReturn, "Expected 'return'").Loc
	var val Expr
	if !p.check(TokenRBrace) && !p.check(TokenEOF) && !p.check(TokenSemicolon) {
		val = p.parseExpr()
	}
	return &ReturnStmt{At: loc, Value: val}
}

func (p *Parser) parseIfStmt() Stmt {
	loc := p.expect(TokenIf, "Expected 'if'").Loc
	if p.check(TokenLet) {
		p.advance()
		varName := p.expect(TokenIdent, "Expected identifier in if-let").Val
		p.expect(TokenAssign, "Expected '=' in if-let")
		optExpr := p.parseExpr()
		thenB := p.parseBlock()
		var elseB Stmt
		if p.check(TokenElse) {
			p.advance()
			if p.check(TokenLBrace) {
				elseB = p.parseBlock()
			} else if p.check(TokenIf) {
				elseB = p.parseIfStmt()
			} else {
				p.errorAt(p.peek(0).Loc, "Expected '{' or 'if' after 'else'")
			}
		}
		return &IfLetStmt{At: loc, VarName: varName, Expr: optExpr, Then: thenB, Else: elseB}
	}

	cond := p.parseExpr()
	thenB := p.parseBlock()
	var elseB Stmt
	if p.check(TokenElse) {
		p.advance()
		if p.check(TokenLBrace) {
			elseB = p.parseBlock()
		} else if p.check(TokenIf) {
			elseB = p.parseIfStmt()
		} else {
			p.errorAt(p.peek(0).Loc, "Expected '{' or 'if' after 'else'")
		}
	}
	return &IfStmt{At: loc, Cond: cond, Then: thenB, Else: elseB}
}

func (p *Parser) parseWhileStmt() Stmt {
	loc := p.expect(TokenWhile, "Expected 'while'").Loc
	if p.check(TokenLet) {
		p.advance()
		varName := p.expect(TokenIdent, "Expected identifier in while-let").Val
		p.expect(TokenAssign, "Expected '=' in while-let")
		call := p.parseExpr()
		body := p.parseBlock()
		return &WhileLetStmt{At: loc, VarName: varName, Call: call, Body: body}
	}

	cond := p.parseExpr()
	body := p.parseBlock()
	return &WhileStmt{At: loc, Cond: cond, Body: body}
}

func (p *Parser) parseForStmt() *ForStmt {
	loc := p.expect(TokenFor, "Expected 'for'").Loc
	itemName := p.expect(TokenIdent, "Expected loop variable name").Val
	p.expect(TokenIn, "Expected 'in' in for loop")
	iterExpr := p.parseExpr()
	body := p.parseBlock()
	return &ForStmt{At: loc, ItemName: itemName, Iter: iterExpr, Body: body}
}

func (p *Parser) parseMatchStmt() *MatchStmt {
	loc := p.expect(TokenMatch, "Expected 'match'").Loc
	expr := p.parseExpr()
	p.expect(TokenLBrace, "Expected '{' after match target")

	var arms []MatchArm
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		aLoc := p.peek(0).Loc
		vName := p.expect(TokenIdent, "Expected variant name in match arm").Val
		var bindings []string
		if p.check(TokenLParen) {
			p.advance()
			for !p.check(TokenRParen) && !p.check(TokenEOF) {
				bindings = append(bindings, p.expect(TokenIdent, "Expected binding variable").Val)
				if p.check(TokenComma) {
					p.advance()
				}
			}
			p.expect(TokenRParen, "Expected ')'")
		}
		p.expect(TokenFatArrow, "Expected '=>' after match pattern")
		var body Stmt
		if p.check(TokenLBrace) {
			body = p.parseBlock()
		} else {
			body = p.parseStmt()
		}
		arms = append(arms, MatchArm{At: aLoc, Variant: vName, Bindings: bindings, Body: body})
		if p.check(TokenComma) {
			p.advance()
		}
	}
	p.expect(TokenRBrace, "Expected '}' closing match")
	return &MatchStmt{At: loc, Expr: expr, Arms: arms}
}

func (p *Parser) parseExpr() Expr {
	return p.parseOrElse()
}

func (p *Parser) parseOrElse() Expr {
	node := p.parseEquality()
	if p.check(TokenOrElse) {
		loc := p.advance().Loc
		rhs := p.parseEquality()
		return &OrElseExpr{At: loc, Primary: node, Fallback: rhs}
	}
	return node
}

func (p *Parser) parseEquality() Expr {
	node := p.parseRelational()
	for p.check(TokenEq) || p.check(TokenNeq) {
		opTok := p.advance()
		right := p.parseRelational()
		node = &BinaryExpr{At: opTok.Loc, Left: node, Op: opTok.Val, Right: right}
	}
	return node
}

func (p *Parser) parseRelational() Expr {
	node := p.parseAdditive()
	for p.check(TokenLt) || p.check(TokenLte) || p.check(TokenGt) || p.check(TokenGte) {
		opTok := p.advance()
		right := p.parseAdditive()
		node = &BinaryExpr{At: opTok.Loc, Left: node, Op: opTok.Val, Right: right}
	}
	return node
}

func (p *Parser) parseAdditive() Expr {
	node := p.parseMultiplicative()
	for p.check(TokenPlus) || p.check(TokenMinus) {
		opTok := p.advance()
		right := p.parseMultiplicative()
		node = &BinaryExpr{At: opTok.Loc, Left: node, Op: opTok.Val, Right: right}
	}
	return node
}

func (p *Parser) parseMultiplicative() Expr {
	node := p.parseUnary()
	for p.check(TokenStar) || p.check(TokenSlash) || p.check(TokenPercent) {
		opTok := p.advance()
		right := p.parseUnary()
		node = &BinaryExpr{At: opTok.Loc, Left: node, Op: opTok.Val, Right: right}
	}
	return node
}

func (p *Parser) parseUnary() Expr {
	tok := p.peek(0)
	if tok.Type == TokenMinus || tok.Type == TokenBang || tok.Type == TokenAmp {
		p.advance()
		operand := p.parseUnary()
		return &UnaryExpr{At: tok.Loc, Op: tok.Val, Operand: operand}
	}
	return p.parsePostfix()
}

func (p *Parser) parsePostfix() Expr {
	node := p.parsePrimary()
	for {
		if p.check(TokenQuestion) {
			loc := p.advance().Loc
			node = &TryExpr{At: loc, Operand: node}
		} else if p.check(TokenDot) {
			loc := p.advance().Loc
			member := p.expect(TokenIdent, "Expected member name").Val
			node = &MemberExpr{At: loc, Target: node, Member: member}
		} else if p.check(TokenLBracket) {
			loc := p.advance().Loc
			idx := p.parseExpr()
			p.expect(TokenRBracket, "Expected ']'")
			node = &IndexExpr{At: loc, Target: node, Index: idx}
		} else if p.check(TokenLParen) {
			loc := p.advance().Loc
			var args []Expr
			for !p.check(TokenRParen) && !p.check(TokenEOF) {
				args = append(args, p.parseExpr())
				if p.check(TokenComma) {
					p.advance()
				}
			}
			p.expect(TokenRParen, "Expected ')'")
			node = &CallExpr{At: loc, Callee: node, Args: args}
		} else {
			break
		}
	}
	return node
}

func (p *Parser) parsePrimary() Expr {
	tok := p.peek(0)

	switch tok.Type {
	case TokenInt:
		p.advance()
		return &LiteralExpr{At: tok.Loc, Val: tok.Val, Type: PrimitiveType{Name: "i32"}}
	case TokenFloat:
		p.advance()
		return &LiteralExpr{At: tok.Loc, Val: tok.Val, Type: PrimitiveType{Name: "f32"}}
	case TokenString:
		p.advance()
		return &LiteralExpr{At: tok.Loc, Val: tok.Val, Type: StringType{}}
	case TokenBool:
		p.advance()
		return &LiteralExpr{At: tok.Loc, Val: tok.Val, Type: PrimitiveType{Name: "bool"}}
	case TokenNull:
		p.advance()
		return &LiteralExpr{At: tok.Loc, Val: "null", Type: OptionalType{Inner: PrimitiveType{Name: "void"}}}
	}

	if tok.Type == TokenLBracket && p.peek(1).Type == TokenRBracket {
		p.advance()
		p.advance()
		elemT := p.parseType()
		p.expect(TokenLBrace, "Expected '{' after slice type")
		p.expect(TokenRBrace, "Expected '}' for empty slice literal")
		return &SliceInitExpr{At: tok.Loc, Elem: elemT}
	}

	if tok.Type == TokenIdent && tok.Val == "Chan" && p.peek(1).Type == TokenLBracket {
		p.advance()
		p.advance()
		elemT := p.parseType()
		p.expect(TokenRBracket, "Expected ']' after Chan element type")
		return &IdentExpr{At: tok.Loc, Name: fmt.Sprintf("Chan[%s]", elemT.String())}
	}

	if tok.Type == TokenIdent && tok.Val == "Pool" && p.peek(1).Type == TokenLBracket {
		p.advance()
		p.advance()
		elemT := p.parseType()
		p.expect(TokenRBracket, "Expected ']' after Pool element type")
		return &IdentExpr{At: tok.Loc, Name: fmt.Sprintf("Pool[%s]", elemT.String())}
	}

	if tok.Type == TokenIdent && tok.Val == "error" && p.peek(1).Type == TokenLParen {
		loc := p.advance().Loc
		p.advance()
		arg := p.parseExpr()
		p.expect(TokenRParen, "Expected ')' after error argument")
		return &ErrorExpr{At: loc, Arg: arg}
	}

	if tok.Type == TokenIdent {
		idTok := p.advance()
		if p.check(TokenLBrace) && len(idTok.Val) > 0 && idTok.Val[0] >= 'A' && idTok.Val[0] <= 'Z' {
			p.advance()
			var fields []FieldInit
			for !p.check(TokenRBrace) && !p.check(TokenEOF) {
				fName := p.expect(TokenIdent, "Expected field name").Val
				p.expect(TokenColon, "Expected ':'")
				fVal := p.parseExpr()
				fields = append(fields, FieldInit{Name: fName, Val: fVal})
				if p.check(TokenComma) {
					p.advance()
				}
			}
			p.expect(TokenRBrace, "Expected '}' closing struct literal")
			return &StructInitExpr{At: idTok.Loc, Name: idTok.Val, Fields: fields}
		}
		return &IdentExpr{At: idTok.Loc, Name: idTok.Val}
	}

	if tok.Type == TokenLParen {
		p.advance()
		e := p.parseExpr()
		p.expect(TokenRParen, "Expected ')'")
		return e
	}

	p.errorAt(tok.Loc, fmt.Sprintf("Unexpected token in expression: %s (%s)", tok.Type, tok.Val))
	return nil
}
