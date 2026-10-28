package compiler

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
	case TokenDefer:
		p.advance()
		return &DeferStmt{At: tok.Loc, Call: p.parseExpr()}
	case TokenIf:
		return p.parseIfStmt()
	case TokenWhile:
		return p.parseWhileStmt()
	case TokenFor:
		return p.parseForStmt()
	case TokenScope:
		p.advance()
		return &ScopeStmt{At: tok.Loc, Body: p.parseBlock()}
	case TokenUnsafe:
		p.advance()
		return &UnsafeStmt{At: tok.Loc, Body: p.parseBlock()}
	case TokenSpawn:
		p.advance()
		call := p.parseExpr()
		callExpr, ok := call.(*CallExpr)
		if !ok {
			p.errorAt(tok.Loc, "spawn target must be a direct function call")
		}
		return &SpawnStmt{At: tok.Loc, Call: callExpr}
	case TokenBreak, TokenContinue:
		p.advance()
		return &LoopControlStmt{At: tok.Loc, Continue: tok.Type == TokenContinue}
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
			if p.check(TokenLBrace) {
				defBlock = p.parseBlock()
			} else {
				stmt := p.parseStmt()
				defBlock = &BlockStmt{At: stmt.Loc(), Stmts: []Stmt{stmt}}
			}
			if p.check(TokenComma) {
				p.advance()
			}
			continue
		}

		vName := p.expect(TokenIdent, "Expected binding identifier in select case").Val
		p.expect(TokenAssign, "Expected '=' in select case")
		chExpr := p.parseExpr()
		p.expect(TokenFatArrow, "Expected '=>' after select condition")
		var b *BlockStmt
		if p.check(TokenLBrace) {
			b = p.parseBlock()
		} else {
			stmt := p.parseStmt()
			b = &BlockStmt{At: stmt.Loc(), Stmts: []Stmt{stmt}}
		}
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
