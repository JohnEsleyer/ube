// File: pkg/compiler/ast.go
package compiler

import "fmt"

// SourceLocation tracks position in the original source code.
type SourceLocation struct {
	File string
	Line int
	Col  int
}

func (s SourceLocation) String() string {
	if s.File == "" {
		return fmt.Sprintf("%d:%d", s.Line, s.Col)
	}
	return fmt.Sprintf("%s:%d:%d", s.File, s.Line, s.Col)
}

// Node is the root interface for all AST nodes.
type Node interface {
	Loc() SourceLocation
}

// Expr represents any AST node evaluating to a value.
type Expr interface {
	Node
	exprNode()
}

// Stmt represents any statement or imperative instruction.
type Stmt interface {
	Node
	stmtNode()
}

// Decl represents a top-level module declaration.
type Decl interface {
	Node
	declNode()
}

// -------------------------------------------------------------
// Declarations
// -------------------------------------------------------------

type Program struct {
	At            SourceLocation
	Package       string // declared package name (e.g. "main", "math")
	Decls         []Decl
	ModuleAliases map[string]string // local alias -> canonical package name
	EntryModule   string            // declared package name of the entry file
}

func (p *Program) Loc() SourceLocation { return p.At }

// PackageDecl is the mandatory first declaration in every .ube file.
type PackageDecl struct {
	At   SourceLocation
	Name string
}

func (d *PackageDecl) Loc() SourceLocation { return d.At }
func (d *PackageDecl) declNode()           {}

type ImportDecl struct {
	At    SourceLocation
	Path  string
	Alias string
}

func (d *ImportDecl) Loc() SourceLocation { return d.At }
func (d *ImportDecl) declNode()           {}

type TypeParam struct {
	Name       string
	Constraint string
}

type FieldDecl struct {
	Name      string
	Type      UbeType
	IsMut     bool
	IsView    bool
	IsPub     bool
	Anonymous bool
	Tag       string
}

type StructDecl struct {
	At         SourceLocation
	Name       string
	Package    string
	IsPub      bool
	TypeParams []TypeParam
	Fields     []FieldDecl
}

func (d *StructDecl) Loc() SourceLocation { return d.At }
func (d *StructDecl) declNode()           {}

type VariantDecl struct {
	Name  string
	Types []UbeType
}

type EnumDecl struct {
	At       SourceLocation
	Name     string
	Package  string
	IsPub    bool
	Variants []VariantDecl
}

func (d *EnumDecl) Loc() SourceLocation { return d.At }
func (d *EnumDecl) declNode()           {}

type TraitMethodDecl struct {
	At      SourceLocation
	Name    string
	Params  []FieldDecl
	RetType UbeType
}

type TraitDecl struct {
	At          SourceLocation
	Name        string
	Package     string
	IsPub       bool
	Supertraits []string
	Methods     []TraitMethodDecl
}

func (d *TraitDecl) Loc() SourceLocation { return d.At }
func (d *TraitDecl) declNode()           {}

type FnDecl struct {
	At         SourceLocation
	Name       string
	Package    string
	IsPub      bool
	TypeParams []TypeParam
	Receiver   *FieldDecl
	Params     []FieldDecl
	RetType    UbeType
	Body       *BlockStmt
}

func (d *FnDecl) Loc() SourceLocation { return d.At }
func (d *FnDecl) declNode()           {}

type ExternFnDecl struct {
	At      SourceLocation
	Name    string
	Package string
	Module  string
	IsPub   bool
	Params  []FieldDecl
	RetType UbeType
}

func (d *ExternFnDecl) Loc() SourceLocation { return d.At }
func (d *ExternFnDecl) declNode()           {}

// -------------------------------------------------------------
// Statements
// -------------------------------------------------------------

type BlockStmt struct {
	At    SourceLocation
	Stmts []Stmt
}

func (s *BlockStmt) Loc() SourceLocation { return s.At }
func (s *BlockStmt) stmtNode()           {}

type LetStmt struct {
	At        SourceLocation
	Name      string
	Names     []string
	Mut       bool
	Type      UbeType
	Value     Expr
	ValueType UbeType
}

func (s *LetStmt) Loc() SourceLocation { return s.At }
func (s *LetStmt) stmtNode()           {}

type AssignStmt struct {
	At      SourceLocation
	LHS     Expr
	Op      string
	RHS     Expr
	LHSType UbeType
	RHSType UbeType
}

func (s *AssignStmt) Loc() SourceLocation { return s.At }
func (s *AssignStmt) stmtNode()           {}

type ReturnStmt struct {
	At        SourceLocation
	Value     Expr
	ValueType UbeType
}

func (s *ReturnStmt) Loc() SourceLocation { return s.At }
func (s *ReturnStmt) stmtNode()           {}

type IfStmt struct {
	At   SourceLocation
	Cond Expr
	Then *BlockStmt
	Else Stmt
}

// IfExpr is the expression form of `if`. Both arms are required and must
// yield compatible value types. The value of each arm is the final ExprStmt
// of its block. Statement-position `if` remains an IfStmt so existing code
// continues to work unchanged; the parser only reaches parseIfExpr when an
// `if` appears in expression position (e.g. after `=` or as an argument).
type IfExpr struct {
	At   SourceLocation
	Cond Expr
	Then *BlockStmt
	Else *BlockStmt
	Type UbeType
}

func (s *IfStmt) Loc() SourceLocation { return s.At }
func (s *IfStmt) stmtNode()           {}

func (e *IfExpr) Loc() SourceLocation { return e.At }
func (e *IfExpr) exprNode()           {}

type IfLetStmt struct {
	At       SourceLocation
	VarName  string
	VarType  UbeType // Populated by typechecker
	Expr     Expr
	IsResult bool
	Then     *BlockStmt
	Else     Stmt
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
	VarType UbeType // Populated by typechecker
	Call    Expr
	Body    *BlockStmt
}

func (s *WhileLetStmt) Loc() SourceLocation { return s.At }
func (s *WhileLetStmt) stmtNode()           {}

type ForStmt struct {
	At               SourceLocation
	ItemName         string
	ItemType         UbeType // Populated by typechecker
	Iter             Expr
	IterType         UbeType
	IsCustomIterator bool
	NextMethod       string
	ReceiverIsMut    bool
	Body             *BlockStmt
}

func (s *ForStmt) Loc() SourceLocation { return s.At }
func (s *ForStmt) stmtNode()           {}

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

type ArenaStmt struct {
	At   SourceLocation
	Body *BlockStmt
}

func (s *ArenaStmt) Loc() SourceLocation { return s.At }
func (s *ArenaStmt) stmtNode()           {}

type DeferStmt struct {
	At   SourceLocation
	Call Expr
}

func (s *DeferStmt) Loc() SourceLocation { return s.At }
func (s *DeferStmt) stmtNode()           {}

type MatchArm struct {
	At       SourceLocation
	Variant  string
	Bindings []string
	Body     Stmt
}

type MatchStmt struct {
	At       SourceLocation
	Expr     Expr
	Arms     []MatchArm
	EnumName string
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

// -------------------------------------------------------------
// Expressions
// -------------------------------------------------------------

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
	OperandType UbeType
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
	At           SourceLocation
	Callee       Expr
	Args         []Expr
	PoolElem     UbeType
	ReceiverType UbeType
	ArgTypes     []UbeType
	TypeArgs     []UbeType
	TraitName    string
}

func (e *CallExpr) Loc() SourceLocation { return e.At }
func (e *CallExpr) exprNode()           {}

type MemberExpr struct {
	At         SourceLocation
	Target     Expr
	Member     string
	TargetType UbeType
	EmbedPath  []string
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
	TargetType         UbeType
}

func (e *SliceSubExpr) Loc() SourceLocation { return e.At }
func (e *SliceSubExpr) exprNode()           {}

type FieldInit struct {
	Name string
	Val  Expr
}

type StructInitExpr struct {
	At       SourceLocation
	Name     string
	Fields   []FieldInit
	TypeArgs []UbeType
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

// CastExpr is an explicit Go-style numeric conversion: T(expr). It is the
// only runtime conversion in the language; see typesafety.md section 3.
type CastExpr struct {
	At         SourceLocation
	TargetType UbeType
	Expr       Expr
}

func (e *CastExpr) Loc() SourceLocation { return e.At }
func (e *CastExpr) exprNode()           {}

type OrElseExpr struct {
	At          SourceLocation
	Primary     Expr
	Fallback    Expr
	PrimaryType UbeType
}

func (e *OrElseExpr) Loc() SourceLocation { return e.At }
func (e *OrElseExpr) exprNode()           {}

type ErrorExpr struct {
	At  SourceLocation
	Arg Expr
}

func (e *ErrorExpr) Loc() SourceLocation { return e.At }
func (e *ErrorExpr) exprNode()           {}

type PromoteExpr struct {
	At    SourceLocation
	Value Expr
	Type  UbeType
}

func (e *PromoteExpr) Loc() SourceLocation { return e.At }
func (e *PromoteExpr) exprNode()           {}

type MoveExpr struct {
	At    SourceLocation
	Value Expr
	Type  UbeType
}

func (e *MoveExpr) Loc() SourceLocation { return e.At }
func (e *MoveExpr) exprNode()           {}

type ArenaExpr struct {
	At   SourceLocation
	Body *BlockStmt
	Type UbeType
}

func (e *ArenaExpr) Loc() SourceLocation { return e.At }
func (e *ArenaExpr) exprNode()           {}

type InterpolatedStringExpr struct {
	At    SourceLocation
	Parts []Expr
}

func (e *InterpolatedStringExpr) Loc() SourceLocation { return e.At }
func (e *InterpolatedStringExpr) exprNode()           {}

type RangeExpr struct {
	At         SourceLocation
	Start, End Expr
}

func (e *RangeExpr) Loc() SourceLocation { return e.At }
func (e *RangeExpr) exprNode()           {}

type TraitCoerceExpr struct {
	At       SourceLocation
	Value    Expr
	Trait    string
	Concrete UbeType
}

func (e *TraitCoerceExpr) Loc() SourceLocation { return e.At }
func (e *TraitCoerceExpr) exprNode()           {}

type ArrayToSliceExpr struct {
	At        SourceLocation
	Value     Expr
	ArrayType ArrayType
}

func (e *ArrayToSliceExpr) Loc() SourceLocation { return e.At }
func (e *ArrayToSliceExpr) exprNode()           {}

type TupleExpr struct {
	At    SourceLocation
	Elems []Expr
	Types []UbeType
}

func (e *TupleExpr) Loc() SourceLocation { return e.At }
func (e *TupleExpr) exprNode()           {}
