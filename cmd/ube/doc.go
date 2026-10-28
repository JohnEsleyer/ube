<search>
func runDoc(args []string) error {
	var file, outputFile string
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" && i+1 < len(args) {
			outputFile = args[i+1]
			i++
		} else if !strings.HasPrefix(args[i], "-") {
			file = args[i]
		}
	}
	if file == "" {
		return fmt.Errorf("usage: ube doc <file.ube> [-o out.md]")
	}
	content, err := os.ReadFile(file)
</search>
<with>
func runDoc(args []string) error {
	var file, outputFile string
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" && i+1 < len(args) {
			outputFile = args[i+1]
			i++
		} else if !strings.HasPrefix(args[i], "-") {
			file = args[i]
		}
	}
	if file == "" {
		return fmt.Errorf("usage: ube doc <file.ube> [-o out.md]")
	}
	content, err := os.ReadFile(file)
</with>
</replace>

<replace path="cmd/ube/doc.go">
<search>
	if outputFile != "" {
		// If an output file is specified, redirect the generated markdown
		_ = os.WriteFile(outputFile, []byte(fmt.Sprintf("# Module `%s`\n", strings.TrimSuffix(baseName, filepath.Ext(baseName)))), 0644)
	}
	return nil
}
</search>
<with>
	if outputFile != "" {
		// If an output file is specified, redirect the generated markdown
		_ = os.WriteFile(outputFile, []byte(fmt.Sprintf("# Module `%s`\n", strings.TrimSuffix(baseName, filepath.Ext(baseName)))), 0644)
	}
	return nil
}
</with>
</replace>

<replace path="cmd/ube/test.go">
<search>
func runTests(args []string) error {
	target := "."
	if len(args) > 0 {
		target = args[0]
	}
</search>
<with>
func runTests(args []string) error {
	target := "."
	runPattern := ""
	for i := 0; i < len(args); i++ {
		if (args[i] == "-run" || args[i] == "--run") && i+1 < len(args) {
			runPattern = args[i+1]
			i++
		} else if !strings.HasPrefix(args[i], "-") {
			target = args[i]
		}
	}
</with>
</replace>

<replace path="cmd/ube/test.go">
<search>
		for _, name := range tests {
			ok, duration, output := runCompiledTest(file, name)
			reportTest(fmt.Sprintf("%s::%s", filepath.Base(file), name), ok, duration, output, &totalPassed, &totalFailed)
		}
</search>
<with>
		for _, name := range tests {
			if runPattern != "" && !strings.Contains(name, runPattern) {
				continue
			}
			ok, duration, output := runCompiledTest(file, name)
			reportTest(fmt.Sprintf("%s::%s", filepath.Base(file), name), ok, duration, output, &totalPassed, &totalFailed)
		}
</with>
</replace>

<replace path="editors/vscode/package.json">
<search>
  "version": "0.5.3",
</search>
<with>
  "version": "0.5.4",
</with>
</replace>

<replace path="editors/vscode/package-lock.json">
<search>
  "name": "ube",
  "version": "0.5.3",
  "lockfileVersion": 3,
  "requires": true,
  "packages": {
    "": {
      "name": "ube",
      "version": "0.5.3",
</search>
<with>
  "name": "ube",
  "version": "0.5.4",
  "lockfileVersion": 3,
  "requires": true,
  "packages": {
    "": {
      "name": "ube",
      "version": "0.5.4",
</with>
</replace>

<replace path="pkg/compiler/ast.go">
<search>
type AssignStmt struct {
	At      SourceLocation
	LHS     Expr
	RHS     Expr
	LHSType UbeType
	RHSType UbeType
}
</search>
<with>
type AssignStmt struct {
	At      SourceLocation
	LHS     Expr
	Op      string
	RHS     Expr
	LHSType UbeType
	RHSType UbeType
}
</with>
</replace>

<replace path="pkg/compiler/ast.go">
<search>
type FieldDecl struct {
	Name string
	Type UbeType
}
</search>
<with>
type FieldDecl struct {
	Name  string
	Type  UbeType
	IsMut bool
}
</with>
</replace>

<replace path="pkg/compiler/ast.go">
<search>
type TraitCoerceExpr struct {
</search>
<with>
type RangeExpr struct {
	At         SourceLocation
	Start, End Expr
}

func (e *RangeExpr) Loc() SourceLocation { return e.At }
func (e *RangeExpr) exprNode()           {}

type TraitCoerceExpr struct {
</with>
</replace>

<replace path="pkg/compiler/token.go">
<search>
	TokenDot       TokenType = "."
	TokenSemicolon TokenType = ";"
	TokenAssign    TokenType = "="
</search>
<with>
	TokenDot       TokenType = "."
	TokenDotDot    TokenType = ".."
	TokenSemicolon TokenType = ";"
	TokenAssign    TokenType = "="
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
</with>
</replace>

<replace path="pkg/compiler/types.go">
<search>
type CustomType struct{ Name string }
</search>
<with>
type RangeType struct{ Elem UbeType }

func (t RangeType) String() string                   { return "Range[" + t.Elem.String() + "]" }
func (t RangeType) IsFlat(env *TypeEnvironment) bool { return true }

type CustomType struct{ Name string }
</with>
</replace>

<replace path="pkg/compiler/lexer.go">
<search>
		loc := SourceLocation{File: l.file, Line: l.line, Col: l.col}
		if ch == '<' && l.peek(1) == '<' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenShl, Val: "<<", Loc: loc})
			continue
		}
		if ch == '>' && l.peek(1) == '>' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenShr, Val: ">>", Loc: loc})
			continue
		}
</search>
<with>
		loc := SourceLocation{File: l.file, Line: l.line, Col: l.col}
		if ch == '.' && l.peek(1) == '.' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenDotDot, Val: "..", Loc: loc})
			continue
		}
		if ch == '<' && l.peek(1) == '<' && l.peek(2) == '=' {
			l.advance()
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenShlAssign, Val: "<<=", Loc: loc})
			continue
		}
		if ch == '>' && l.peek(1) == '>' && l.peek(2) == '=' {
			l.advance()
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenShrAssign, Val: ">>=", Loc: loc})
			continue
		}
		if ch == '<' && l.peek(1) == '<' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenShl, Val: "<<", Loc: loc})
			continue
		}
		if ch == '>' && l.peek(1) == '>' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenShr, Val: ">>", Loc: loc})
			continue
		}
		if ch == '+' && l.peek(1) == '=' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenPlusAssign, Val: "+=", Loc: loc})
			continue
		}
		if ch == '-' && l.peek(1) == '=' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenMinusAssign, Val: "-=", Loc: loc})
			continue
		}
		if ch == '*' && l.peek(1) == '=' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenStarAssign, Val: "*=", Loc: loc})
			continue
		}
		if ch == '/' && l.peek(1) == '=' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenSlashAssign, Val: "/=", Loc: loc})
			continue
		}
		if ch == '%' && l.peek(1) == '=' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenPercentAssign, Val: "%=", Loc: loc})
			continue
		}
		if ch == '&' && l.peek(1) == '=' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenAmpAssign, Val: "&=", Loc: loc})
			continue
		}
		if ch == '|' && l.peek(1) == '=' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenPipeAssign, Val: "|=", Loc: loc})
			continue
		}
		if ch == '^' && l.peek(1) == '=' {
			l.advance()
			l.advance()
			tokens = append(tokens, Token{Type: TokenCaretAssign, Val: "^=", Loc: loc})
			continue
		}
</with>
</replace>

<replace path="pkg/compiler/lexer.go">
<search>
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
</search>
<with>
		if unicode.IsDigit(ch) {
			var buf strings.Builder
			isFloat := false
			for l.pos < len(l.source) && (unicode.IsDigit(l.peek(0)) || l.peek(0) == '.') {
				if l.peek(0) == '.' {
					if l.peek(1) == '.' {
						break
					}
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
</with>
</replace>

<replace path="pkg/compiler/parser.go">
<search>
	var receiver *FieldDecl
	if p.check(TokenLParen) {
		p.advance()
		rName := p.expect(TokenIdent, "Expected receiver variable name").Val
		rType := p.parseType()
		p.expect(TokenRParen, "Expected ')' closing receiver")
		receiver = &FieldDecl{Name: rName, Type: rType}
	}
</search>
<with>
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
</with>
</replace>

<replace path="pkg/compiler/parser_expr.go">
<search>
func (p *Parser) parseExpr() Expr {
	return p.parseOrElse()
}
</search>
<with>
func (p *Parser) parseExpr() Expr {
	return p.parseRange()
}

func (p *Parser) parseRange() Expr {
	node := p.parseOrElse()
	if p.check(TokenDotDot) {
		loc := p.advance().Loc
		rhs := p.parseOrElse()
		return &RangeExpr{At: loc, Start: node, End: rhs}
	}
	return node
}
</with>
</replace>

<replace path="pkg/compiler/parser_stmt.go">
<search>
	expr := p.parseExpr()
	if p.check(TokenAssign) {
		p.advance()
		rhs := p.parseExpr()
		return &AssignStmt{At: expr.Loc(), LHS: expr, RHS: rhs}
	}
	return &ExprStmt{At: expr.Loc(), Expr: expr}
</search>
<with>
	expr := p.parseExpr()
	switch p.peek(0).Type {
	case TokenAssign, TokenPlusAssign, TokenMinusAssign, TokenStarAssign, TokenSlashAssign, TokenPercentAssign,
		TokenAmpAssign, TokenPipeAssign, TokenCaretAssign, TokenShlAssign, TokenShrAssign:
		opTok := p.advance()
		rhs := p.parseExpr()
		return &AssignStmt{At: expr.Loc(), LHS: expr, Op: opTok.Val, RHS: rhs}
	}
	return &ExprStmt{At: expr.Loc(), Expr: expr}
</with>
</replace>

<replace path="pkg/compiler/parser_stmt.go">
<search>
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
</search>
<with>
	var arms []MatchArm
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		aLoc := p.peek(0).Loc
		vName := p.expect(TokenIdent, "Expected variant name in match arm").Val
		var bindings []string
		if vName != "_" && p.check(TokenLParen) {
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
</with>
</replace>

<replace path="pkg/compiler/monomorphize.go">
<search>
	receiver := &FieldDecl{
		Name: tmpl.Receiver.Name,
		Type: CustomType{Name: specializedStruct},
	}
</search>
<with>
	receiver := &FieldDecl{
		Name:  tmpl.Receiver.Name,
		Type:  CustomType{Name: specializedStruct},
		IsMut: tmpl.Receiver.IsMut,
	}
</with>
</replace>

<replace path="pkg/compiler/monomorphize.go">
<search>
	case *AssignStmt:
		return &AssignStmt{
			At:      s.At,
			LHS:     substituteExpr(s.LHS, subMap),
			RHS:     substituteExpr(s.RHS, subMap),
			LHSType: substituteType(s.LHSType, subMap),
			RHSType: substituteType(s.RHSType, subMap),
		}
</search>
<with>
	case *AssignStmt:
		return &AssignStmt{
			At:      s.At,
			LHS:     substituteExpr(s.LHS, subMap),
			Op:      s.Op,
			RHS:     substituteExpr(s.RHS, subMap),
			LHSType: substituteType(s.LHSType, subMap),
			RHSType: substituteType(s.RHSType, subMap),
		}
</with>
</replace>

<replace path="pkg/compiler/monomorphize.go">
<search>
	case *TraitCoerceExpr:
		return &TraitCoerceExpr{At: e.At, Value: substituteExpr(e.Value, subMap), Trait: e.Trait, Concrete: substituteType(e.Concrete, subMap)}
</search>
<with>
	case *RangeExpr:
		return &RangeExpr{At: e.At, Start: substituteExpr(e.Start, subMap), End: substituteExpr(e.End, subMap)}
	case *TraitCoerceExpr:
		return &TraitCoerceExpr{At: e.At, Value: substituteExpr(e.Value, subMap), Trait: e.Trait, Concrete: substituteType(e.Concrete, subMap)}
</with>
</replace>

<replace path="pkg/compiler/typechecker.go">
<search>
type FuncSignature struct {
	Params   []FieldDecl
	RetType  UbeType
	Escaping []bool
}
</search>
<with>
type FuncSignature struct {
	Params      []FieldDecl
	RetType     UbeType
	Escaping    []bool
	ReceiverMut bool
}
</with>
</replace>

<replace path="pkg/compiler/typechecker.go">
<search>
	tc.Global.Funcs["str_split"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "delim", Type: str}}, RetType: SliceType{Elem: str}}

	// StringBuilder receiver methods
</search>
<with>
	tc.Global.Funcs["str_split"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "delim", Type: str}}, RetType: SliceType{Elem: str}}
	tc.Global.Funcs["dir_list"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: ResultType{Ok: SliceType{Elem: str}, Err: str}}
	tc.Global.Funcs["dir_create"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: ResultType{Ok: boolType, Err: str}}
	tc.Global.Funcs["file_remove"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: ResultType{Ok: boolType, Err: str}}
	tc.Global.Funcs["env_set"] = FuncSignature{Params: []FieldDecl{{Name: "key", Type: str}, {Name: "val", Type: str}}, RetType: boolType}
	tc.Global.Funcs["cmd_run"] = FuncSignature{Params: []FieldDecl{{Name: "cmd", Type: str}}, RetType: i32}
	tc.Global.Funcs["abs"] = FuncSignature{Params: []FieldDecl{{Name: "n", Type: i64}}, RetType: i64}
	tc.Global.Funcs["min"] = FuncSignature{Params: []FieldDecl{{Name: "a", Type: i64}, {Name: "b", Type: i64}}, RetType: i64}
	tc.Global.Funcs["max"] = FuncSignature{Params: []FieldDecl{{Name: "a", Type: i64}, {Name: "b", Type: i64}}, RetType: i64}
	tc.Global.Funcs["sqrt"] = FuncSignature{Params: []FieldDecl{{Name: "f", Type: PrimitiveType{Name: "f64"}}}, RetType: PrimitiveType{Name: "f64"}}
	tc.Global.Funcs["pow"] = FuncSignature{Params: []FieldDecl{{Name: "base", Type: PrimitiveType{Name: "f64"}}, {Name: "exp", Type: PrimitiveType{Name: "f64"}}}, RetType: PrimitiveType{Name: "f64"}}
	tc.Global.Funcs["floor"] = FuncSignature{Params: []FieldDecl{{Name: "f", Type: PrimitiveType{Name: "f64"}}}, RetType: PrimitiveType{Name: "f64"}}
	tc.Global.Funcs["ceil"] = FuncSignature{Params: []FieldDecl{{Name: "f", Type: PrimitiveType{Name: "f64"}}}, RetType: PrimitiveType{Name: "f64"}}

	// StringBuilder receiver methods
</with>
</replace>

<replace path="pkg/compiler/typechecker.go">
<search>
		lhsT = tc.resolveType(lhsT)
		rhsT = tc.resolveType(rhsT)
		s.LHSType = lhsT
		s.RHSType = rhsT
</search>
<with>
		lhsT = tc.resolveType(lhsT)
		rhsT = tc.resolveType(rhsT)
		s.LHSType = lhsT
		s.RHSType = rhsT
		if s.Op == "" {
			s.Op = "="
		}
		if s.Op != "=" {
			if s.Op == "+=" && lhsT.String() == "string" && rhsT.String() == "string" {
				// string concatenation via +=
			} else if !integerType(lhsT) && lhsT.String() != "f32" && lhsT.String() != "f64" {
				return fmt.Errorf("[%s] compound assignment operator '%s' requires numeric type, got '%s'", s.At, s.Op, lhsT)
			} else {
				if err := tc.assertCompat(lhsT, rhsT, s.At); err != nil {
					return fmt.Errorf("[%s] type mismatch in compound assignment '%s': %w", s.At, s.Op, err)
				}
			}
		}
</with>
</replace>

<replace path="pkg/compiler/typechecker.go">
<search>
		var elemT UbeType
		switch it := iterT.(type) {
		case SliceType:
			elemT = it.Elem
		case ArrayType:
			elemT = it.Elem
		default:
			return fmt.Errorf("[%s] for-in requires slice or array type, got '%s'", s.At, iterT)
		}
</search>
<with>
		var elemT UbeType
		switch it := iterT.(type) {
		case SliceType:
			elemT = it.Elem
		case ArrayType:
			elemT = it.Elem
		case RangeType:
			elemT = it.Elem
		default:
			return fmt.Errorf("[%s] for-in requires slice, array, or range (start..end), got '%s'", s.At, iterT)
		}
</with>
</replace>

<replace path="pkg/compiler/typechecker.go">
<search>
		for _, arm := range s.Arms {
			if seenVariants[arm.Variant] {
				return fmt.Errorf("[%s] duplicate match pattern '%s'", arm.At, arm.Variant)
			}
			seenVariants[arm.Variant] = true

			expected, exists := vMap[arm.Variant]
			if !exists {
				return fmt.Errorf("[%s] enum '%s' has no variant '%s'", arm.At, custom.Name, arm.Variant)
			}
</search>
<with>
		for _, arm := range s.Arms {
			if seenVariants[arm.Variant] {
				return fmt.Errorf("[%s] duplicate match pattern '%s'", arm.At, arm.Variant)
			}
			seenVariants[arm.Variant] = true

			if arm.Variant == "_" {
				armEnv := childTypeEnvironment(env)
				if blk, ok := arm.Body.(*BlockStmt); ok {
					if err := tc.checkBlock(blk, armEnv); err != nil {
						return err
					}
				} else {
					if err := tc.checkStmt(arm.Body, armEnv); err != nil {
						return err
					}
				}
				continue
			}

			expected, exists := vMap[arm.Variant]
			if !exists {
				return fmt.Errorf("[%s] enum '%s' has no variant '%s'", arm.At, custom.Name, arm.Variant)
			}
</with>
</replace>

<replace path="pkg/compiler/typechecker.go">
<search>
			fnKey := d.Name
			if d.Receiver != nil {
				fnKey = fmt.Sprintf("%s.%s", d.Receiver.Type, d.Name)
			}
			tc.Global.Funcs[fnKey] = FuncSignature{Params: d.Params, RetType: d.RetType}
</search>
<with>
			fnKey := d.Name
			if d.Receiver != nil {
				fnKey = fmt.Sprintf("%s.%s", d.Receiver.Type, d.Name)
			}
			tc.Global.Funcs[fnKey] = FuncSignature{
				Params:      d.Params,
				RetType:     d.RetType,
				ReceiverMut: d.Receiver != nil && d.Receiver.IsMut,
			}
</with>
</replace>

<replace path="pkg/compiler/typechecker.go">
<search>
			if fn.Receiver != nil {
				env.Vars[fn.Receiver.Name] = VarInfo{Type: fn.Receiver.Type, IsMut: false, Depth: 0, ValueDepth: 0}
			}
</search>
<with>
			if fn.Receiver != nil {
				env.Vars[fn.Receiver.Name] = VarInfo{Type: fn.Receiver.Type, IsMut: fn.Receiver.IsMut, Depth: 0, ValueDepth: 0}
			}
</with>
</replace>

<replace path="pkg/compiler/typechecker_expr.go">
<search>
	case *ArrayToSliceExpr:
		_, err := tc.inferExpr(e.ArrayExpr, env)
		if err != nil {
			return nil, err
		}
		return SliceType{Elem: e.ElemType}, nil
</search>
<with>
	case *RangeExpr:
		startT, err := tc.inferExpr(e.Start, env)
		if err != nil {
			return nil, err
		}
		endT, err := tc.inferExpr(e.End, env)
		if err != nil {
			return nil, err
		}
		if !integerType(startT) {
			return nil, fmt.Errorf("[%s] range start must be an integer, got '%s'", e.At, startT)
		}
		if !integerType(endT) {
			return nil, fmt.Errorf("[%s] range end must be an integer, got '%s'", e.At, endT)
		}
		return RangeType{Elem: startT}, nil

	case *ArrayToSliceExpr:
		_, err := tc.inferExpr(e.ArrayExpr, env)
		if err != nil {
			return nil, err
		}
		return SliceType{Elem: e.ElemType}, nil
</with>
</replace>

<replace path="pkg/compiler/typechecker_expr.go">
<search>
			if sl, ok := tgtT.(SliceType); ok && mem.Member == "pop" {
				if len(e.Args) != 0 {
					return nil, fmt.Errorf("[%s] pop expects 0 arguments", e.At)
				}
				return OptionalType{Inner: sl.Elem}, nil
			}
</search>
<with>
			if sl, ok := tgtT.(SliceType); ok && mem.Member == "pop" {
				if len(e.Args) != 0 {
					return nil, fmt.Errorf("[%s] pop expects 0 arguments", e.At)
				}
				return OptionalType{Inner: sl.Elem}, nil
			}
			if sl, ok := tgtT.(SliceType); ok && mem.Member == "clear" {
				if len(e.Args) != 0 {
					return nil, fmt.Errorf("[%s] clear expects 0 arguments", e.At)
				}
				if root := assignmentRoot(mem.Target); root != "" {
					if info, found := env.LookupVar(root); found && !info.IsMut {
						return nil, fmt.Errorf("[%s] Cannot call 'clear' on immutable slice '%s'", e.At, root)
					}
				}
				return PrimitiveType{Name: "void"}, nil
			}
			if sl, ok := tgtT.(SliceType); ok && mem.Member == "contains" {
				if len(e.Args) != 1 {
					return nil, fmt.Errorf("[%s] contains expects 1 argument", e.At)
				}
				argT, err := tc.inferExpr(e.Args[0], env)
				if err != nil {
					return nil, err
				}
				if err := tc.assertCompat(sl.Elem, argT, e.At); err != nil {
					return nil, err
				}
				return PrimitiveType{Name: "bool"}, nil
			}
</with>
</replace>

<replace path="pkg/compiler/typechecker_expr.go">
<search>
			methodKey := fmt.Sprintf("%s.%s", tgtT, mem.Member)
			if sig, ok := tc.Global.Funcs[methodKey]; ok {
				e.ReceiverType = tgtT
				if err := tc.checkCallArgs(e.Args, sig.Params, sig.Escaping, env, e.At); err != nil {
					return nil, err
				}
</search>
<with>
			methodKey := fmt.Sprintf("%s.%s", tgtT, mem.Member)
			if sig, ok := tc.Global.Funcs[methodKey]; ok {
				e.ReceiverType = tgtT
				if sig.ReceiverMut {
					if root := assignmentRoot(mem.Target); root != "" {
						if vInfo, found := env.LookupVar(root); found && !vInfo.IsMut {
							return nil, fmt.Errorf("[%s] cannot call mutating method '%s' on immutable value '%s'", e.At, mem.Member, root)
						}
					}
				}
				if err := tc.checkCallArgs(e.Args, sig.Params, sig.Escaping, env, e.At); err != nil {
					return nil, err
				}
</with>
</replace>

<replace path="pkg/compiler/typechecker_safety.go">
<search>
	case *TraitCoerceExpr:
		return tc.exprEscapesParam(e.Value, name)
</search>
<with>
	case *RangeExpr:
		return tc.exprEscapesParam(e.Start, name) || tc.exprEscapesParam(e.End, name)
	case *TraitCoerceExpr:
		return tc.exprEscapesParam(e.Value, name)
</with>
</replace>

<replace path="pkg/compiler/codegen.go">
<search>
	sliceMutParams  map[string]map[int]bool
	loopLabels      []loopLabel
</search>
<with>
	sliceMutParams    map[string]map[int]bool
	receiverMutParams map[string]bool
	loopLabels        []loopLabel
</with>
</replace>

<replace path="pkg/compiler/codegen.go">
<search>
		sliceMutParams: make(map[string]map[int]bool),
		traitDecls:     make(map[string]*TraitDecl),
</search>
<with>
		sliceMutParams:    make(map[string]map[int]bool),
		receiverMutParams: make(map[string]bool),
		traitDecls:        make(map[string]*TraitDecl),
</with>
</replace>

<replace path="pkg/compiler/codegen.go">
<search>
	g.toCType(ResultType{Ok: PrimitiveType{Name: "i64"}, Err: StringType{}})
	g.toCType(SliceType{Elem: StringType{}})
</search>
<with>
	g.toCType(ResultType{Ok: PrimitiveType{Name: "i64"}, Err: StringType{}})
	g.toCType(SliceType{Elem: StringType{}})
	g.toCType(ResultType{Ok: SliceType{Elem: StringType{}}, Err: StringType{}})
</with>
</replace>

<replace path="pkg/compiler/codegen.go">
<search>
			if fn.Receiver != nil {
				name = fmt.Sprintf("%s_%s", fn.Receiver.Type, fn.Name)
				params = append(params, g.toCType(fn.Receiver.Type))
			}
</search>
<with>
			if fn.Receiver != nil {
				name = fmt.Sprintf("%s_%s", fn.Receiver.Type, fn.Name)
				recCT := g.toCType(fn.Receiver.Type)
				if fn.Receiver.IsMut {
					recCT += "*"
					g.receiverMutParams[name] = true
				}
				params = append(params, recCT)
			}
</with>
</replace>

<replace path="pkg/compiler/codegen_stmt.go">
<search>
		if d.Receiver != nil {
			fnName = fmt.Sprintf("%s_%s", d.Receiver.Type, d.Name)
			params = append(params, fmt.Sprintf("%s %s", g.toCType(d.Receiver.Type), d.Receiver.Name))
		}
</search>
<with>
		if d.Receiver != nil {
			fnName = fmt.Sprintf("%s_%s", d.Receiver.Type, d.Name)
			recCT := g.toCType(d.Receiver.Type)
			if d.Receiver.IsMut {
				recCT += "*"
				g.sliceParamVars[d.Receiver.Name] = true
			}
			params = append(params, fmt.Sprintf("%s %s", recCT, d.Receiver.Name))
		}
</with>
</replace>

<replace path="pkg/compiler/codegen_stmt.go">
<search>
		if resT, isRes := s.LHSType.(ResultType); isRes {
			cType := g.toCType(resT)
			if _, isErr := s.RHS.(*ErrorExpr); isErr {
				g.emit(fmt.Sprintf("%s = (%s){ .ok = false, .err = %s };", lhsExpr, cType, rhsExpr))
				return
			}
			if _, rhsIsRes := s.RHSType.(ResultType); rhsIsRes {
				g.emit(fmt.Sprintf("%s = %s;", lhsExpr, rhsExpr))
				return
			}
			g.emit(fmt.Sprintf("%s = (%s){ .ok = true, .val = %s };", lhsExpr, cType, rhsExpr))
			return
		}
		g.emit(fmt.Sprintf("%s = %s;", lhsExpr, rhsExpr))
</search>
<with>
		if resT, isRes := s.LHSType.(ResultType); isRes {
			cType := g.toCType(resT)
			if _, isErr := s.RHS.(*ErrorExpr); isErr {
				g.emit(fmt.Sprintf("%s = (%s){ .ok = false, .err = %s };", lhsExpr, cType, rhsExpr))
				return
			}
			if _, rhsIsRes := s.RHSType.(ResultType); rhsIsRes {
				g.emit(fmt.Sprintf("%s = %s;", lhsExpr, rhsExpr))
				return
			}
			g.emit(fmt.Sprintf("%s = (%s){ .ok = true, .val = %s };", lhsExpr, cType, rhsExpr))
			return
		}
		if s.Op != "" && s.Op != "=" {
			if s.Op == "+=" && s.LHSType != nil && s.LHSType.String() == "string" {
				g.emit(fmt.Sprintf("%s = ube_str_concat(%s, %s);", lhsExpr, lhsExpr, rhsExpr))
				return
			}
			g.emit(fmt.Sprintf("%s %s %s;", lhsExpr, s.Op, rhsExpr))
			return
		}
		g.emit(fmt.Sprintf("%s = %s;", lhsExpr, rhsExpr))
</with>
</replace>

<replace path="pkg/compiler/codegen_stmt.go">
<search>
	case *ForStmt:
		labels := loopLabel{breakTo: g.newTmp() + "_break", continueTo: g.newTmp() + "_continue"}
		iterTmp := g.newTmp()
</search>
<with>
	case *ForStmt:
		labels := loopLabel{breakTo: g.newTmp() + "_break", continueTo: g.newTmp() + "_continue"}
		if rng, ok := s.Iter.(*RangeExpr); ok {
			startTmp := g.newTmp()
			endTmp := g.newTmp()
			elemCT := "int32_t"
			if rType, ok := s.IterType.(RangeType); ok {
				elemCT = g.toCType(rType.Elem)
			}
			g.emit(fmt.Sprintf("%s %s = %s;", elemCT, startTmp, g.genExpr(rng.Start)))
			g.emit(fmt.Sprintf("%s %s = %s;", elemCT, endTmp, g.genExpr(rng.End)))
			g.emit(fmt.Sprintf("for (%s %s = %s; %s < %s; %s++) {", elemCT, s.ItemName, startTmp, s.ItemName, endTmp, s.ItemName))
			g.indent++
			g.loopLabels = append(g.loopLabels, labels)
			g.loopSelectDepth = append(g.loopSelectDepth, len(g.selectGroups))
			g.loopDeferDepth = append(g.loopDeferDepth, len(g.deferScopes))
			g.loopScopeDepth = append(g.loopScopeDepth, len(g.scopeMarks))
			g.genBlock(s.Body)
			g.loopDeferDepth = g.loopDeferDepth[:len(g.loopDeferDepth)-1]
			g.loopScopeDepth = g.loopScopeDepth[:len(g.loopScopeDepth)-1]
			g.emit(labels.continueTo + ":;")
			g.loopLabels = g.loopLabels[:len(g.loopLabels)-1]
			g.loopSelectDepth = g.loopSelectDepth[:len(g.loopSelectDepth)-1]
			g.indent--
			g.emit("}")
			g.emit(labels.breakTo + ":;")
			return
		}
		iterTmp := g.newTmp()
</with>
</replace>

<replace path="pkg/compiler/codegen_stmt.go">
<search>
		for _, arm := range s.Arms {
			g.emit(fmt.Sprintf("case %s_%s: {", s.EnumName, arm.Variant))
			g.indent++
			for i, b := range arm.Bindings {
				g.emit(fmt.Sprintf("__auto_type %s = %s.as.%s._%d;", b, tmp, arm.Variant, i))
			}
			if blk, ok := arm.Body.(*BlockStmt); ok {
				g.genBlock(blk)
			} else {
				g.pushDeferScope()
				g.genStmt(arm.Body)
				g.finishDeferScope()
			}
			g.emit("break;")
			g.indent--
			g.emit("}")
		}
</search>
<with>
		for _, arm := range s.Arms {
			if arm.Variant == "_" {
				g.emit("default: {")
				g.indent++
				if blk, ok := arm.Body.(*BlockStmt); ok {
					g.genBlock(blk)
				} else {
					g.pushDeferScope()
					g.genStmt(arm.Body)
					g.finishDeferScope()
				}
				g.emit("break;")
				g.indent--
				g.emit("}")
				continue
			}
			g.emit(fmt.Sprintf("case %s_%s: {", s.EnumName, arm.Variant))
			g.indent++
			for i, b := range arm.Bindings {
				g.emit(fmt.Sprintf("__auto_type %s = %s.as.%s._%d;", b, tmp, arm.Variant, i))
			}
			if blk, ok := arm.Body.(*BlockStmt); ok {
				g.genBlock(blk)
			} else {
				g.pushDeferScope()
				g.genStmt(arm.Body)
				g.finishDeferScope()
			}
			g.emit("break;")
			g.indent--
			g.emit("}")
		}
</with>
</replace>

<replace path="pkg/compiler/codegen_expr.go">
<search>
			if e.ReceiverType != nil {
				fnName := fmt.Sprintf("%s_%s", e.ReceiverType.String(), mem.Member)
				args := []string{g.genExpr(mem.Target)}
				for i, arg := range e.Args {
					value := g.genExpr(arg)
					if g.sliceMutParams[fnName][i] {
						value = "&(" + value + ")"
					}
					args = append(args, value)
				}
				return fmt.Sprintf("%s(%s)", fnName, strings.Join(args, ", "))
			}
</search>
<with>
			if e.ReceiverType != nil {
				fnName := fmt.Sprintf("%s_%s", e.ReceiverType.String(), mem.Member)
				targetExpr := g.genExpr(mem.Target)
				if g.receiverMutParams[fnName] {
					targetExpr = "&(" + targetExpr + ")"
				}
				args := []string{targetExpr}
				for i, arg := range e.Args {
					value := g.genExpr(arg)
					if g.sliceMutParams[fnName][i] {
						value = "&(" + value + ")"
					}
					args = append(args, value)
				}
				return fmt.Sprintf("%s(%s)", fnName, strings.Join(args, ", "))
			}
</with>
</replace>

<replace path="pkg/compiler/codegen_expr.go">
<search>
			if mem.Member == "pop" {
				sliceType := ""
				if sl, ok := mem.TargetType.(SliceType); ok {
					sliceType = g.sliceElemMangle(sl.Elem)
					g.toCType(OptionalType{Inner: sl.Elem})
				} else if id, ok := mem.Target.(*IdentExpr); ok && g.sliceVars[id.Name] != "" {
					sliceType = g.sliceVars[id.Name]
				} else {
					sliceType = mangleTypeName(g.genExpr(mem.Target))
				}
				return fmt.Sprintf("ube_slice_%s_pop(&%s)", sliceType, g.genExpr(mem.Target))
			}
</search>
<with>
			if mem.Member == "pop" {
				sliceType := ""
				if sl, ok := mem.TargetType.(SliceType); ok {
					sliceType = g.sliceElemMangle(sl.Elem)
					g.toCType(OptionalType{Inner: sl.Elem})
				} else if id, ok := mem.Target.(*IdentExpr); ok && g.sliceVars[id.Name] != "" {
					sliceType = g.sliceVars[id.Name]
				} else {
					sliceType = mangleTypeName(g.genExpr(mem.Target))
				}
				return fmt.Sprintf("ube_slice_%s_pop(&%s)", sliceType, g.genExpr(mem.Target))
			}
			if mem.Member == "clear" {
				return fmt.Sprintf("((void)((%s).len = 0))", g.genExpr(mem.Target))
			}
			if mem.Member == "contains" {
				elemIsStr := false
				if sl, ok := mem.TargetType.(SliceType); ok {
					if sl.Elem.String() == "string" {
						elemIsStr = true
					}
				}
				cond := "_cs->data[_i] == _cv"
				if elemIsStr {
					cond = "ube_str_eq(_cs->data[_i], _cv)"
				}
				return fmt.Sprintf("({ __auto_type* _cs = &(%s); __auto_type _cv = (%s); bool _found = false; for (size_t _i = 0; _i < _cs->len; _i++) { if (%s) { _found = true; break; } } _found; })",
					g.genExpr(mem.Target), g.genExpr(e.Args[0]), cond)
			}
</with>
</replace>

<replace path="pkg/compiler/codegen_expr.go">
<search>
			builtins := map[string]string{
				"time_now_ms": "ube_time_now_ms", "sleep_ms": "ube_sleep_ms", "yield": "ube_yield",
				"exit_process": "ube_exit_process", "int_to_str": "ube_int_to_str", "str_to_int": "ube_str_to_int",
				"read_file": "ube_read_file", "write_file": "ube_write_file", "os_args": "ube_os_args",
				"str_contains": "ube_str_contains", "str_starts_with": "ube_str_starts_with", "str_ends_with": "ube_str_ends_with",
				"str_trim": "ube_str_trim", "str_index_of": "ube_str_index_of", "str_split": "ube_str_split",
				"env_get": "ube_env_get", "file_exists": "ube_file_exists", "path_join": "ube_path_join",
				"str_replace": "ube_str_replace",
			}
</search>
<with>
			builtins := map[string]string{
				"time_now_ms": "ube_time_now_ms", "sleep_ms": "ube_sleep_ms", "yield": "ube_yield",
				"exit_process": "ube_exit_process", "int_to_str": "ube_int_to_str", "str_to_int": "ube_str_to_int",
				"read_file": "ube_read_file", "write_file": "ube_write_file", "os_args": "ube_os_args",
				"str_contains": "ube_str_contains", "str_starts_with": "ube_str_starts_with", "str_ends_with": "ube_str_ends_with",
				"str_trim": "ube_str_trim", "str_index_of": "ube_str_index_of", "str_split": "ube_str_split",
				"env_get": "ube_env_get", "file_exists": "ube_file_exists", "path_join": "ube_path_join",
				"str_replace": "ube_str_replace",
				"dir_list": "ube_dir_list", "dir_create": "ube_dir_create", "file_remove": "ube_file_remove",
				"env_set": "ube_env_set", "cmd_run": "ube_cmd_run",
				"abs": "ube_abs", "min": "ube_min", "max": "ube_max",
				"sqrt": "ube_sqrt", "pow": "ube_pow", "floor": "ube_floor", "ceil": "ube_ceil",
			}
</with>
</replace>

<replace path="pkg/compiler/codegen_analysis.go">
<search>
	case *CallExpr:
		if m, ok := e.Callee.(*MemberExpr); ok && (m.Member == "push" || m.Member == "pop") && assignmentRoot(m.Target) == name {
			return true
		}
</search>
<with>
	case *CallExpr:
		if m, ok := e.Callee.(*MemberExpr); ok && (m.Member == "push" || m.Member == "pop" || m.Member == "clear") && assignmentRoot(m.Target) == name {
			return true
		}
</with>
</replace>

<replace path="pkg/compiler/codegen_analysis.go">
<search>
	case *TraitCoerceExpr:
		return g.exprMutatesName(e.Value, name)
</search>
<with>
	case *RangeExpr:
		return g.exprMutatesName(e.Start, name) || g.exprMutatesName(e.End, name)
	case *TraitCoerceExpr:
		return g.exprMutatesName(e.Value, name)
</with>
</replace>

<replace path="pkg/formatter/formatter.go">
<search>
func isOperator(s string) bool {
	switch s {
	case "+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=",
		"&&", "||", "=", "->", "=>", "&", "|", "^", "<<", ">>", "~", "!",
		"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=":
		return true
	}
	return false
}
</search>
<with>
func isOperator(s string) bool {
	switch s {
	case "+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=",
		"&&", "||", "=", "->", "=>", "&", "|", "^", "<<", ">>", "~", "!",
		"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "<<=", ">>=", "..":
		return true
	}
	return false
}
</with>
</replace>

<replace path="pkg/lsp/completion.go">
<search>
			map[string]any{"label": "push", "kind": 2, "detail": "push(val) onto slice"},
			map[string]any{"label": "pop", "kind": 2, "detail": "pop() from slice -> T?"},
</search>
<with>
			map[string]any{"label": "push", "kind": 2, "detail": "push(val) onto slice"},
			map[string]any{"label": "pop", "kind": 2, "detail": "pop() from slice -> T?"},
			map[string]any{"label": "clear", "kind": 2, "detail": "clear() slice length to 0"},
			map[string]any{"label": "contains", "kind": 2, "detail": "contains(val) -> bool"},
</with>
</replace>

<replace path="pkg/lsp/completion.go">
<search>
		{"path_join", "path_join(a: string, b: string) -> string"},
		{"str_replace", "str_replace(s: string, old: string, new: string) -> string"},
	}
</search>
<with>
		{"path_join", "path_join(a: string, b: string) -> string"},
		{"str_replace", "str_replace(s: string, old: string, new: string) -> string"},
		{"dir_list", "dir_list(path: string) -> []string ! string"},
		{"dir_create", "dir_create(path: string) -> bool ! string"},
		{"file_remove", "file_remove(path: string) -> bool ! string"},
		{"env_set", "env_set(key: string, val: string) -> bool"},
		{"cmd_run", "cmd_run(cmd: string) -> i32"},
		{"abs", "abs(n: i64) -> i64"},
		{"min", "min(a: i64, b: i64) -> i64"},
		{"max", "max(a: i64, b: i64) -> i64"},
		{"sqrt", "sqrt(f: f64) -> f64"},
		{"pow", "pow(b: f64, e: f64) -> f64"},
		{"floor", "floor(f: f64) -> f64"},
		{"ceil", "ceil(f: f64) -> f64"},
	}
</with>
</replace>

<replace path="pkg/lsp/hover.go">
<search>
		"path_join":       "`fn path_join(a: string, b: string) -> string`\n*Joins two path segments with a slash separator.*",
		"str_replace":     "`fn str_replace(s: string, old: string, new: string) -> string`\n*Replaces occurrences of old with new in string.*",
</search>
<with>
		"path_join":       "`fn path_join(a: string, b: string) -> string`\n*Joins two path segments with a slash separator.*",
		"str_replace":     "`fn str_replace(s: string, old: string, new: string) -> string`\n*Replaces occurrences of old with new in string.*",
		"dir_list":        "`fn dir_list(path: string) -> []string ! string`\n*Lists files and directories at path.*",
		"dir_create":      "`fn dir_create(path: string) -> bool ! string`\n*Creates a directory (including parent directories).*",
		"file_remove":     "`fn file_remove(path: string) -> bool ! string`\n*Deletes a file.*",
		"env_set":         "`fn env_set(key: string, val: string) -> bool`\n*Sets an environment variable.*",
		"cmd_run":         "`fn cmd_run(cmd: string) -> i32`\n*Executes a system shell command and returns the exit status code.*",
		"abs":             "`fn abs(n: i64) -> i64`\n*Returns the absolute value of an integer.*",
		"min":             "`fn min(a: i64, b: i64) -> i64`\n*Returns the minimum of two integers.*",
		"max":             "`fn max(a: i64, b: i64) -> i64`\n*Returns the maximum of two integers.*",
		"sqrt":            "`fn sqrt(f: f64) -> f64`\n*Returns the square root of a floating-point number.*",
		"pow":             "`fn pow(b: f64, e: f64) -> f64`\n*Returns base raised to the power exp.*",
		"floor":           "`fn floor(f: f64) -> f64`\n*Returns the largest integer less than or equal to f.*",
		"ceil":            "`fn ceil(f: f64) -> f64`\n*Returns the smallest integer greater than or equal to f.*",
</with>
</replace>

<replace path="runtime/ube_runtime.h">
<search>
#ifndef UBE_FREESTANDING
#include <pthread.h>
#include <unistd.h>
#include <sched.h>
#endif
</search>
<with>
#ifndef UBE_FREESTANDING
#include <pthread.h>
#include <unistd.h>
#include <sched.h>
#include <dirent.h>
#include <sys/stat.h>
#endif
</with>
</replace>

<replace path="runtime/ube_runtime.h">
<search>
#ifndef UbeSlice_string_DEFINED
#define UbeSlice_string_DEFINED
typedef struct { string* data; size_t len; size_t cap; } UbeSlice_string;
#endif
</search>
<with>
#ifndef UbeSlice_string_DEFINED
#define UbeSlice_string_DEFINED
typedef struct { string* data; size_t len; size_t cap; } UbeSlice_string;
#endif
#ifndef UbeResult_slice_string_string_DEFINED
#define UbeResult_slice_string_string_DEFINED
typedef struct { bool ok; union { UbeSlice_string val; string err; }; } UbeResult_slice_string_string;
#endif
</with>
</replace>

<replace path="runtime/ube_runtime.h">
<search>
static inline UbeOption_string ube_env_get(string key) {
</search>
<with>
static inline UbeResult_slice_string_string ube_dir_list(string path) {
    UbeResult_slice_string_string res;
    char dir_buf[1024];
    if (path.len >= sizeof(dir_buf)) {
        res.ok = false;
        res.err = ube_str_lit("path too long");
        return res;
    }
    memcpy(dir_buf, path.data, path.len);
    dir_buf[path.len] = '\0';
#ifndef UBE_FREESTANDING
    DIR* d = opendir(dir_buf);
    if (!d) {
        res.ok = false;
        res.err = ube_str_lit("cannot open directory");
        return res;
    }
    size_t count = 0;
    struct dirent* ent;
    while ((ent = readdir(d)) != NULL) {
        if (strcmp(ent->d_name, ".") == 0 || strcmp(ent->d_name, "..") == 0) continue;
        count++;
    }
    rewinddir(d);
    string* items = count ? (string*)ube_bump_alloc(count * sizeof(string)) : NULL;
    size_t idx = 0;
    while ((ent = readdir(d)) != NULL && idx < count) {
        if (strcmp(ent->d_name, ".") == 0 || strcmp(ent->d_name, "..") == 0) continue;
        size_t name_len = strlen(ent->d_name);
        char* mem = (char*)ube_bump_alloc(name_len + 1);
        memcpy(mem, ent->d_name, name_len);
        mem[name_len] = '\0';
        items[idx++] = (string){ mem, name_len };
    }
    closedir(d);
    res.ok = true;
    res.val = (UbeSlice_string){ .data = items, .len = idx, .cap = idx };
    return res;
#else
    res.ok = false;
    res.err = ube_str_lit("dir_list not supported in freestanding");
    return res;
#endif
}

static inline UbeResult_bool_string ube_dir_create(string path) {
    UbeResult_bool_string res;
    char buf[1024];
    if (path.len >= sizeof(buf)) {
        res.ok = false;
        res.err = ube_str_lit("path too long");
        return res;
    }
    memcpy(buf, path.data, path.len);
    buf[path.len] = '\0';
#ifndef UBE_FREESTANDING
    for (char* p = buf + 1; *p; p++) {
        if (*p == '/') {
            *p = '\0';
            mkdir(buf, 0755);
            *p = '/';
        }
    }
    if (mkdir(buf, 0755) != 0 && errno != EEXIST) {
        res.ok = false;
        res.err = ube_str_lit("cannot create directory");
        return res;
    }
    res.ok = true;
    res.val = true;
    return res;
#else
    res.ok = false;
    res.err = ube_str_lit("dir_create not supported in freestanding");
    return res;
#endif
}

static inline UbeResult_bool_string ube_file_remove(string path) {
    UbeResult_bool_string res;
    char buf[1024];
    if (path.len >= sizeof(buf)) {
        res.ok = false;
        res.err = ube_str_lit("path too long");
        return res;
    }
    memcpy(buf, path.data, path.len);
    buf[path.len] = '\0';
    if (remove(buf) != 0) {
        res.ok = false;
        res.err = ube_str_lit("cannot remove file");
        return res;
    }
    res.ok = true;
    res.val = true;
    return res;
}

static inline bool ube_env_set(string key, string val) {
    char k[256];
    char v[1024];
    if (key.len >= sizeof(k) || val.len >= sizeof(v)) return false;
    memcpy(k, key.data, key.len);
    k[key.len] = '\0';
    memcpy(v, val.data, val.len);
    v[val.len] = '\0';
#ifndef UBE_FREESTANDING
    return setenv(k, v, 1) == 0;
#else
    return false;
#endif
}

static inline int32_t ube_cmd_run(string cmd) {
    char buf[2048];
    if (cmd.len >= sizeof(buf)) return -1;
    memcpy(buf, cmd.data, cmd.len);
    buf[cmd.len] = '\0';
    return (int32_t)system(buf);
}

static inline int64_t ube_abs(int64_t n) { return n < 0 ? -n : n; }
static inline int64_t ube_min(int64_t a, int64_t b) { return a < b ? a : b; }
static inline int64_t ube_max(int64_t a, int64_t b) { return a > b ? a : b; }
static inline double ube_sqrt(double n) { return sqrt(n); }
static inline double ube_pow(double b, double e) { return pow(b, e); }
static inline double ube_floor(double n) { return floor(n); }
static inline double ube_ceil(double n) { return ceil(n); }

static inline UbeOption_string ube_env_get(string key) {
</with>
</replace>

<file path="pkg/compiler/v054_test.go">
package compiler_test

import (
	"strings"
	"testing"
)

// TEST 1: Compound assignment operators
func TestV054_CompoundAssignments(t *testing.T) {
	src := `fn main() {
		let mut a = 10
		a += 5
		a -= 3
		a *= 4
		a /= 2
		a %= 7
		println(a)

		let mut b = 0b0011
		b <<= 2
		b >>= 1
		b |= 0b1000
		b &= 0b1111
		b ^= 0b0010
		println(b)

		let mut s = "sweet"
		s += " ube"
		println(s)
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("compound assignment test failed: %v", err)
	}
	if got, want := runBinary(t, bin), "3\n12\nsweet ube"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TEST 2: Range loops (for i in start..end)
func TestV054_RangeLoops(t *testing.T) {
	src := `fn main() {
		let mut sum = 0
		for i in 0..5 {
			sum += i
		}
		println(sum)

		let mut count = 0
		for j in 10..13 {
			count += j
		}
		println(count)
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("range loop test failed: %v", err)
	}
	if got, want := runBinary(t, bin), "10\n33"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TEST 3: Wildcard arm in match (_ => ...)
func TestV054_MatchWildcard(t *testing.T) {
	src := `enum Direction {
		North,
		South,
		East,
		West
	}

	fn to_str(d: Direction) -> string {
		match d {
			North => return "north",
			_ => return "other"
		}
	}

	fn main() {
		println(to_str(Direction.North))
		println(to_str(Direction.East))
		println(to_str(Direction.West))
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("match wildcard test failed: %v", err)
	}
	if got, want := runBinary(t, bin), "north\nother\nother"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TEST 4: Mutating struct receiver methods
func TestV054_MutatingReceiverMethod(t *testing.T) {
	src := `struct Counter {
		val: i32
	}

	fn (mut c Counter) increment(step: i32) {
		c.val += step
	}

	fn (c Counter) get() -> i32 {
		return c.val
	}

	fn main() {
		let mut cnt = Counter{ val: 10 }
		cnt.increment(5)
		cnt.increment(7)
		println(cnt.get())
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("mutating receiver method test failed: %v", err)
	}
	if got, want := runBinary(t, bin), "22"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TEST 5: Slice clear and contains
func TestV054_SliceClearAndContains(t *testing.T) {
	src := `fn main() {
		let mut numbers = []i32{}
		numbers.push(10)
		numbers.push(20)
		numbers.push(30)
		println(numbers.contains(20))
		println(numbers.contains(99))

		numbers.clear()
		println(numbers.len)
		println(numbers.contains(10))

		let mut names = []string{}
		names.push("ube")
		names.push("taro")
		println(names.contains("ube"))
		println(names.contains("potato"))
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("slice clear and contains test failed: %v", err)
	}
	if got, want := runBinary(t, bin), "true\nfalse\n0\nfalse\ntrue\nfalse"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TEST 6: Standard library Math and OS builtins
func TestV054_MathAndOsBuiltins(t *testing.T) {
	src := `fn main() {
		println(abs(-42))
		println(min(15, 7))
		println(max(15, 7))
		println(sqrt(16.0))
		println(pow(2.0, 3.0))
		println(floor(3.7))
		println(ceil(3.2))

		env_set("UBE_TEST_V054", "active")
		let val = env_get("UBE_TEST_V054") or_else "missing"
		println(val)
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("math and os builtins test failed: %v", err)
	}
	expected := strings.TrimSpace("42\n7\n15\n4\n8\n3\n4\nactive")
	if got := strings.TrimSpace(runBinary(t, bin)); got != expected {
		t.Fatalf("got %q, want %q", got, expected)
	}
}
