package compiler_test

import (
	"strings"
	"testing"
)

// ----------------------------------------------------------------------------
// TEST 1: Nested slice types compile and run cleanly with mangled identifiers.
// ----------------------------------------------------------------------------
func TestIssue_NestedSliceTypeMangling(t *testing.T) {
	src := `fn main() {
		let mut grid = [][]i32{}
		let mut row = []i32{}
		row.push(42)
		grid.push(row)
		println(grid[0][0])
	}`

	bin, cSource, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("nested slice failed to compile: %v\nC Source:\n%s", err, cSource)
	}
	if strings.Contains(cSource, "UbeSlice_[]i32") {
		t.Errorf("unmangled identifier UbeSlice_[]i32 found in C source:\n%s", cSource)
	}
	if got, want := runBinary(t, bin), "42"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 2: Indexing rvalue array return values compiles and evaluates correctly.
// ----------------------------------------------------------------------------
func TestIssue_ArrayRvalueIndexingFailsInC(t *testing.T) {
	src := `fn get_coords() -> [3]i32 {
		return [3]i32{10, 20, 30}
	}

	fn main() {
		let x = get_coords()[0]
		println(x)
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("indexing rvalue array failed to compile: %v", err)
	}
	if got, want := runBinary(t, bin), "10"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 3: for..in evaluates iterable expression exactly once.
// ----------------------------------------------------------------------------
func TestIssue_ForInIterRepeatedEvaluation(t *testing.T) {
	src := `fn get_items() -> []i32 {
		println(999)
		let mut s = []i32{}
		s.push(1)
		s.push(2)
		return s
	}

	fn main() {
		for x in get_items() {
			println(x)
		}
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("failed to compile for-in program: %v", err)
	}

	out := runBinary(t, bin)
	occurrences := strings.Count(out, "999")
	if occurrences != 1 {
		t.Fatalf("expected get_items() to run exactly once, but 999 appeared %d times. Full output:\n%s", occurrences, out)
	}
	if got, want := out, "999\n1\n2"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 4: Slicing a dynamic slice produces a valid sub-slice.
// ----------------------------------------------------------------------------
func TestIssue_SliceSubExprOnSliceEmitsStringSlice(t *testing.T) {
	src := `fn main() {
		let mut s = []i32{}
		s.push(10)
		s.push(20)
		s.push(30)
		let sub = s[1:3]
		for n in sub {
			println(n)
		}
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("slice sub-slicing failed to compile: %v", err)
	}
	if got, want := runBinary(t, bin), "20\n30"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 5: Untyped 'let x = null' is rejected by the typechecker.
// ----------------------------------------------------------------------------
func TestIssue_UntypedNullLiteralFailsInC(t *testing.T) {
	src := `fn main() {
		let x = null
	}`

	err := typecheckSource(t, src)
	if err == nil {
		t.Fatal("expected typechecker to reject untyped null initialization")
	}
	if !strings.Contains(err.Error(), "requires a type annotation") {
		t.Fatalf("expected diagnostic requiring type annotation, got: %v", err)
	}
}

// ----------------------------------------------------------------------------
// TEST 6: Assigning a primitive value or null to an optional variable succeeds.
// ----------------------------------------------------------------------------
func TestIssue_AssignValueToOptionalFailsInC(t *testing.T) {
	src := `fn main() {
		let mut opt: i32? = null
		opt = 42
		println(opt or_else 0)
		opt = null
		println(opt or_else 99)
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("optional assignment failed to compile: %v", err)
	}
	if got, want := runBinary(t, bin), "42\n99"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 7: Typechecker validates StructInitExpr fields, existence, and types.
// ----------------------------------------------------------------------------
func TestIssue_StructInitUncheckedFieldsAndTypes(t *testing.T) {
	srcInvalidFields := `struct Point {
		x: i32,
		y: i32
	}

	fn main() {
		let p = Point{ nonexistent_field: "hello", x: true }
	}`

	err := typecheckSource(t, srcInvalidFields)
	if err == nil {
		t.Fatal("expected typechecker to reject invalid field names/types in struct literal")
	}

	srcUndefinedStruct := `fn main() {
		let p = UnknownStructType{ a: 123 }
	}`

	errUndef := typecheckSource(t, srcUndefinedStruct)
	if errUndef == nil {
		t.Fatal("expected typechecker to reject undeclared struct type")
	}
}

// ----------------------------------------------------------------------------
// TEST 8: IndexExpr and SliceSubExpr require integer bounds.
// ----------------------------------------------------------------------------
func TestIssue_IndexAndSliceExpressionsUntypecheckedIndices(t *testing.T) {
	srcSliceIndex := `fn main() {
		let mut s = []i32{}
		let x = s["not_an_integer"]
	}`

	err := typecheckSource(t, srcSliceIndex)
	if err == nil {
		t.Fatal("expected typechecker to reject string index")
	}
	if !strings.Contains(err.Error(), "must be an integer") {
		t.Fatalf("expected integer error, got: %v", err)
	}

	srcStringSlice := `fn main() {
		let s = "hello"
		let sub = s[true:"wrong"]
	}`

	errSlice := typecheckSource(t, srcStringSlice)
	if errSlice == nil {
		t.Fatal("expected typechecker to reject non-integer slice bounds")
	}
	if !strings.Contains(errSlice.Error(), "must be an integer") {
		t.Fatalf("expected integer error, got: %v", errSlice)
	}
}

// ----------------------------------------------------------------------------
// TEST 9: Unary and binary operators enforce operand types.
// ----------------------------------------------------------------------------
func TestIssue_InvalidUnaryAndBinaryOperatorOperands(t *testing.T) {
	srcUnary := `fn main() {
		let a = !"not_a_bool"
	}`
	if err := typecheckSource(t, srcUnary); err == nil {
		t.Fatal("expected typechecker to reject ! on string")
	}

	srcMultiplyStrings := `fn main() {
		let s = "hello" * "world"
	}`
	if err := typecheckSource(t, srcMultiplyStrings); err == nil {
		t.Fatal("expected typechecker to reject multiplying two strings")
	}

	srcStringComparison := `fn main() {
		let cmp = "abc" < "xyz"
	}`
	if err := typecheckSource(t, srcStringComparison); err == nil {
		t.Fatal("expected typechecker to reject < on strings")
	}
}

// ----------------------------------------------------------------------------
// TEST 10: while-let unwraps OptionalType in the type environment.
// ----------------------------------------------------------------------------
func TestIssue_WhileLetDoesNotUnwrapOptional(t *testing.T) {
	src := `fn get_next() -> i32? {
		return null
	}

	fn main() {
		while let x = get_next() {
			let unwrapped: i32 = x
			println(unwrapped)
		}
	}`

	err := typecheckSource(t, src)
	if err != nil {
		t.Fatalf("while-let should unwrap optional to i32, got error: %v", err)
	}

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("while-let program failed to compile: %v", err)
	}
	if got := runBinary(t, bin); got != "" {
		t.Fatalf("expected empty output, got: %q", got)
	}
}

// ----------------------------------------------------------------------------
// TEST 11: Variable re-declaration in the same block is rejected by typechecker.
// ----------------------------------------------------------------------------
func TestIssue_VariableRedeclarationInSameBlockFailsInC(t *testing.T) {
	src := `fn main() {
		let x = 10
		let x = 20
		println(x)
	}`

	err := typecheckSource(t, src)
	if err == nil {
		t.Fatal("expected typechecker to reject variable re-declaration in same scope")
	}
	if !strings.Contains(err.Error(), "already declared") {
		t.Fatalf("expected 'already declared' diagnostic, got: %v", err)
	}
}

// ----------------------------------------------------------------------------
// TEST 12: Struct equality comparison (==) is rejected by typechecker.
// ----------------------------------------------------------------------------
func TestIssue_StructEqualityComparisonFailsInC(t *testing.T) {
	src := `struct Vec2 {
		x: i32,
		y: i32
	}

	fn main() {
		let a = Vec2{ x: 1, y: 2 }
		let b = Vec2{ x: 1, y: 2 }
		let eq = a == b
	}`

	err := typecheckSource(t, src)
	if err == nil {
		t.Fatal("expected typechecker to reject == on struct types")
	}
	if !strings.Contains(err.Error(), "does not support operator") {
		t.Fatalf("expected unsupported operator error, got: %v", err)
	}
}
