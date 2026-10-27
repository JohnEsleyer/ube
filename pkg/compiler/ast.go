package compiler

type Node interface {
	Loc() SourceLocation
}

// Expressions
type Expr interface {
	Node
	exprNode()
}

type LiteralExpr struct {
	At   SourceLocation
	Val  string
	Type UbeType
}

func (e *LiteralExpr) Loc() SourceLocation { return e.At }
func (e *LiteralExpr) exprNode()           {}

type IdentExpr struct {
	At   SourceLocation
	Name string
}

func (e *IdentExpr) Loc() SourceLocation { return e.At }
func (e *IdentExpr) exprNode()           {}

type BinaryExpr struct {
	At          SourceLocation
	Left, Right Expr
	Op          string
	Type        UbeType
}

func (e *BinaryExpr) Loc() SourceLocation { return e.At }
func (e *BinaryExpr) exprNode()           {}

type UnaryExpr struct {
	At      SourceLocation
	Op      string
	Operand Expr
}

func (e *UnaryExpr) Loc() SourceLocation { return e.At }
func (e *UnaryExpr) exprNode()           {}

type CallExpr struct {
	At       SourceLocation
	Callee   Expr
	Args     []Expr
	PoolElem UbeType
}

func (e *CallExpr) Loc() SourceLocation { return e.At }
func (e *CallExpr) exprNode()           {}

type MemberExpr struct {
	At     SourceLocation
	Target Expr
	Member string
}

func (e *MemberExpr) Loc() SourceLocation { return e.At }
func (e *MemberExpr) exprNode()           {}

type IndexExpr struct {
	At         SourceLocation
	Target     Expr
	Index      Expr
	TargetType UbeType
}

func (e *IndexExpr) Loc() SourceLocation { return e.At }
func (e *IndexExpr) exprNode()           {}

type SliceSubExpr struct {
	At                 SourceLocation
	Target, Start, End Expr
}

func (e *SliceSubExpr) Loc() SourceLocation { return e.At }
func (e *SliceSubExpr) exprNode()           {}

type StructInitExpr struct {
	At     SourceLocation
	Name   string
	Fields []FieldInit
}
type FieldInit struct {
	Name string
	Val  Expr
}

func (e *StructInitExpr) Loc() SourceLocation { return e.At }
func (e *StructInitExpr) exprNode()           {}

type SliceInitExpr struct {
	At       SourceLocation
	Elem     UbeType
	Elements []Expr
}

func (e *SliceInitExpr) Loc() SourceLocation { return e.At }
func (e *SliceInitExpr) exprNode()           {}

type ArrayInitExpr struct {
	At       SourceLocation
	Elem     UbeType
	Size     int
	Elements []Expr
}

func (e *ArrayInitExpr) Loc() SourceLocation { return e.At }
func (e *ArrayInitExpr) exprNode()           {}

type GenericTypeExpr struct {
	At       SourceLocation
	BaseType string
	ElemType UbeType
}

func (e *GenericTypeExpr) Loc() SourceLocation { return e.At }
func (e *GenericTypeExpr) exprNode()           {}

type TryExpr struct {
	At      SourceLocation
	Operand Expr
}

func (e *TryExpr) Loc() SourceLocation { return e.At }
func (e *TryExpr) exprNode()           {}

type OrElseExpr struct {
	At       SourceLocation
	Primary  Expr
	Fallback Expr
}

func (e *OrElseExpr) Loc() SourceLocation { return e.At }
func (e *OrElseExpr) exprNode()           {}

type ErrorExpr struct {
	At  SourceLocation
	Arg Expr
}

func (e *ErrorExpr) Loc() SourceLocation { return e.At }
func (e *ErrorExpr) exprNode()           {}

type UnsafeExpr struct {
	At    SourceLocation
	Value Expr
}

func (e *UnsafeExpr) Loc() SourceLocation { return e.At }
func (e *UnsafeExpr) exprNode()           {}

// Statements
type Stmt interface {
	Node
	stmtNode()
}

type BlockStmt struct {
	At    SourceLocation
	Stmts []Stmt
}

func (s *BlockStmt) Loc() SourceLocation { return s.At }
func (s *BlockStmt) stmtNode()           {}

type LetStmt struct {
	At    SourceLocation
	Name  string
	Mut   bool
	Type  UbeType
	Value Expr
}

func (s *LetStmt) Loc() SourceLocation { return s.At }
func (s *LetStmt) stmtNode()           {}

type AssignStmt struct {
	At  SourceLocation
	LHS Expr
	RHS Expr
}

func (s *AssignStmt) Loc() SourceLocation { return s.At }
func (s *AssignStmt) stmtNode()           {}

type ReturnStmt struct {
	At    SourceLocation
	Value Expr
}

func (s *ReturnStmt) Loc() SourceLocation { return s.At }
func (s *ReturnStmt) stmtNode()           {}

type IfStmt struct {
	At   SourceLocation
	Cond Expr
	Then *BlockStmt
	Else Stmt
}

func (s *IfStmt) Loc() SourceLocation { return s.At }
func (s *IfStmt) stmtNode()           {}

type IfLetStmt struct {
	At      SourceLocation
	VarName string
	Expr    Expr
	Then    *BlockStmt
	Else    Stmt
}

func (s *IfLetStmt) Loc() SourceLocation { return s.At }
func (s *IfLetStmt) stmtNode()           {}

type WhileStmt struct {
	At   SourceLocation
	Cond Expr
	Body *BlockStmt
}

func (s *WhileStmt) Loc() SourceLocation { return s.At }
func (s *WhileStmt) stmtNode()           {}

type WhileLetStmt struct {
	At      SourceLocation
	VarName string
	Call    Expr
	Body    *BlockStmt
}

func (s *WhileLetStmt) Loc() SourceLocation { return s.At }
func (s *WhileLetStmt) stmtNode()           {}

type ForStmt struct {
	At       SourceLocation
	ItemName string
	Iter     Expr
	Body     *BlockStmt
}

func (s *ForStmt) Loc() SourceLocation { return s.At }
func (s *ForStmt) stmtNode()           {}

type ScopeStmt struct {
	At   SourceLocation
	Body *BlockStmt
}

func (s *ScopeStmt) Loc() SourceLocation { return s.At }
func (s *ScopeStmt) stmtNode()           {}

type UnsafeStmt struct {
	At   SourceLocation
	Body *BlockStmt
}

func (s *UnsafeStmt) Loc() SourceLocation { return s.At }
func (s *UnsafeStmt) stmtNode()           {}

type LoopControlStmt struct {
	At       SourceLocation
	Continue bool
}

func (s *LoopControlStmt) Loc() SourceLocation { return s.At }
func (s *LoopControlStmt) stmtNode()           {}

type SpawnStmt struct {
	At   SourceLocation
	Call *CallExpr
}

func (s *SpawnStmt) Loc() SourceLocation { return s.At }
func (s *SpawnStmt) stmtNode()           {}

type MatchStmt struct {
	At       SourceLocation
	Expr     Expr
	Arms     []MatchArm
	EnumName string
}
type MatchArm struct {
	At       SourceLocation
	Variant  string
	Bindings []string
	Body     Stmt
}

func (s *MatchStmt) Loc() SourceLocation { return s.At }
func (s *MatchStmt) stmtNode()           {}

type SelectCase struct {
	At       SourceLocation
	VarName  string
	Chan     Expr
	ChanElem UbeType
	Body     *BlockStmt
}

type SelectStmt struct {
	At      SourceLocation
	Cases   []SelectCase
	Default *BlockStmt
}

func (s *SelectStmt) Loc() SourceLocation { return s.At }
func (s *SelectStmt) stmtNode()           {}

type ExprStmt struct {
	At   SourceLocation
	Expr Expr
}

func (s *ExprStmt) Loc() SourceLocation { return s.At }
func (s *ExprStmt) stmtNode()           {}

type DeferStmt struct {
	At   SourceLocation
	Call Expr
}

func (s *DeferStmt) Loc() SourceLocation { return s.At }
func (s *DeferStmt) stmtNode()           {}

// Top-Level Declarations
type Decl interface {
	Node
	declNode()
}

type ImportDecl struct {
	At   SourceLocation
	Path string
}

func (d *ImportDecl) Loc() SourceLocation { return d.At }
func (d *ImportDecl) declNode()           {}

type StructDecl struct {
	At     SourceLocation
	Name   string
	Fields []FieldDecl
}
type FieldDecl struct {
	Name string
	Type UbeType
}

func (d *StructDecl) Loc() SourceLocation { return d.At }
func (d *StructDecl) declNode()           {}

type EnumDecl struct {
	At       SourceLocation
	Name     string
	Variants []VariantDecl
}
type VariantDecl struct {
	Name  string
	Types []UbeType
}

func (d *EnumDecl) Loc() SourceLocation { return d.At }
func (d *EnumDecl) declNode()           {}

type FnDecl struct {
	At       SourceLocation
	Name     string
	Receiver *FieldDecl
	Params   []FieldDecl
	RetType  UbeType
	Body     *BlockStmt
}

func (d *FnDecl) Loc() SourceLocation { return d.At }
func (d *FnDecl) declNode()           {}

type ExternFnDecl struct {
	At      SourceLocation
	Name    string
	Params  []FieldDecl
	RetType UbeType
}

func (d *ExternFnDecl) Loc() SourceLocation { return d.At }
func (d *ExternFnDecl) declNode()           {}

type Program struct {
	At    SourceLocation
	Decls []Decl
}

func (p *Program) Loc() SourceLocation { return p.At }
