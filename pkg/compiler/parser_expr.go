package compiler

import (
	"fmt"
	"strconv"
)

func (p *Parser) parseExpr() Expr {
	return p.parseOrElse()
}

func (p *Parser) parseOrElse() Expr {
	node := p.parseLogicalOr()
	for p.check(TokenOrElse) {
		loc := p.advance().Loc
		rhs := p.parseLogicalOr()
		node = &OrElseExpr{At: loc, Primary: node, Fallback: rhs}
	}
	return node
}

func (p *Parser) parseLogicalOr() Expr {
	node := p.parseLogicalAnd()
	for p.check(TokenOr) {
		opTok := p.advance()
		node = &BinaryExpr{At: opTok.Loc, Left: node, Op: opTok.Val, Right: p.parseLogicalAnd()}
	}
	return node
}

func (p *Parser) parseLogicalAnd() Expr {
	node := p.parseBitwiseOr()
	for p.check(TokenAnd) {
		opTok := p.advance()
		node = &BinaryExpr{At: opTok.Loc, Left: node, Op: opTok.Val, Right: p.parseBitwiseOr()}
	}
	return node
}

func (p *Parser) parseBitwiseOr() Expr {
	node := p.parseBitwiseXor()
	for p.check(TokenPipe) {
		opTok := p.advance()
		node = &BinaryExpr{At: opTok.Loc, Left: node, Op: opTok.Val, Right: p.parseBitwiseXor()}
	}
	return node
}

func (p *Parser) parseBitwiseXor() Expr {
	node := p.parseBitwiseAnd()
	for p.check(TokenCaret) {
		opTok := p.advance()
		node = &BinaryExpr{At: opTok.Loc, Left: node, Op: opTok.Val, Right: p.parseBitwiseAnd()}
	}
	return node
}

func (p *Parser) parseBitwiseAnd() Expr {
	node := p.parseEquality()
	for p.check(TokenAmp) {
		opTok := p.advance()
		node = &BinaryExpr{At: opTok.Loc, Left: node, Op: opTok.Val, Right: p.parseEquality()}
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
	node := p.parseShift()
	for p.check(TokenLt) || p.check(TokenLte) || p.check(TokenGt) || p.check(TokenGte) {
		opTok := p.advance()
		right := p.parseShift()
		node = &BinaryExpr{At: opTok.Loc, Left: node, Op: opTok.Val, Right: right}
	}
	return node
}

func (p *Parser) parseShift() Expr {
	node := p.parseAdditive()
	for p.check(TokenShl) || p.check(TokenShr) {
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
	if tok.Type == TokenMinus || tok.Type == TokenBang || tok.Type == TokenAmp || tok.Type == TokenStar || tok.Type == TokenTilde {
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
			var member string
			if p.check(TokenSpawn) {
				member = p.advance().Val
			} else {
				member = p.expect(TokenIdent, "Expected member name").Val
			}
			node = &MemberExpr{At: loc, Target: node, Member: member}
		} else if p.check(TokenLBracket) {
			loc := p.advance().Loc
			var idx Expr
			if !p.check(TokenColon) {
				idx = p.parseExpr()
			}
			if p.check(TokenColon) {
				p.advance()
				var end Expr
				if !p.check(TokenRBracket) {
					end = p.parseExpr()
				}
				p.expect(TokenRBracket, "Expected ']'")
				if idx == nil {
					idx = &LiteralExpr{At: loc, Val: "0", Type: PrimitiveType{Name: "i32"}}
				}
				if end == nil {
					end = &LiteralExpr{At: loc, Val: "2147483647", Type: PrimitiveType{Name: "i32"}}
				}
				node = &SliceSubExpr{At: loc, Target: node, Start: idx, End: end}
				continue
			}
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

func normalizeIntLiteral(tok Token) string {
	if len(tok.Val) > 2 && tok.Val[0] == '0' && (tok.Val[1] == 'b' || tok.Val[1] == 'B') {
		if n, err := strconv.ParseUint(tok.Val[2:], 2, 64); err == nil {
			return strconv.FormatUint(n, 10)
		}
	}
	return tok.Val
}

func (p *Parser) parsePrimary() Expr {
	tok := p.peek(0)
	if tok.Type == TokenUnsafe {
		p.advance()
		p.expect(TokenLBrace, "Expected '{' after unsafe")
		value := p.parseExpr()
		p.expect(TokenRBrace, "Expected '}' after unsafe expression")
		return &UnsafeExpr{At: tok.Loc, Value: value}
	}

	switch tok.Type {
	case TokenInt:
		p.advance()
		return &LiteralExpr{At: tok.Loc, Val: normalizeIntLiteral(tok), Type: PrimitiveType{Name: "i32"}}
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
		var elems []Expr
		for !p.check(TokenRBrace) && !p.check(TokenEOF) {
			elems = append(elems, p.parseExpr())
			if p.check(TokenComma) {
				p.advance()
			}
		}
		p.expect(TokenRBrace, "Expected '}' after slice literal")
		return &SliceInitExpr{At: tok.Loc, Elem: elemT, Elements: elems}
	}
	if tok.Type == TokenLBracket && p.peek(1).Type == TokenInt {
		loc := p.advance().Loc
		size, _ := strconv.Atoi(p.advance().Val)
		if size <= 0 {
			p.errorAt(loc, "array size must be greater than 0")
		}
		p.expect(TokenRBracket, "Expected ']' after array size")
		elem := p.parseType()
		p.expect(TokenLBrace, "Expected '{' after array type")
		var elems []Expr
		for !p.check(TokenRBrace) && !p.check(TokenEOF) {
			elems = append(elems, p.parseExpr())
			if p.check(TokenComma) {
				p.advance()
			}
		}
		p.expect(TokenRBrace, "Expected '}' after array literal")
		return &ArrayInitExpr{At: loc, Elem: elem, Size: size, Elements: elems}
	}

	if tok.Type == TokenIdent && tok.Val == "Chan" && p.peek(1).Type == TokenLBracket {
		p.advance()
		p.advance()
		elemT := p.parseType()
		p.expect(TokenRBracket, "Expected ']' after Chan element type")
		return &GenericTypeExpr{At: tok.Loc, BaseType: "Chan", ElemType: elemT}
	}

	if tok.Type == TokenIdent && tok.Val == "Pool" && p.peek(1).Type == TokenLBracket {
		p.advance()
		p.advance()
		elemT := p.parseType()
		p.expect(TokenRBracket, "Expected ']' after Pool element type")
		return &GenericTypeExpr{At: tok.Loc, BaseType: "Pool", ElemType: elemT}
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
