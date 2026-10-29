package compiler_test

import (
	"strings"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/formatter"
)

// TEST 1: Generic struct instantiation and receiver methods
func TestFeatures_GenericStructAndMethods(t *testing.T) {
	src := `struct Box[T] {
		val: T
	}

	fn (b Box[T]) get() -> T {
		return b.val
	}

	fn main() {
		let b1 = Box[i32]{ val: 123 }
		println(b1.get())

		let b2 = Box[string]{ val: "sweet" }
		println(b2.get())
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("generic struct test failed: %v", err)
	}
	if got, want := runBinary(t, bin), "123\nsweet"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TEST 2: Generic functions
func TestFeatures_GenericFunction(t *testing.T) {
	src := `struct Pair[A, B] {
		first: A,
		second: B
	}

	fn make_pair[A, B](a: A, b: B) -> Pair[A, B] {
		return Pair[A, B]{ first: a, second: b }
	}

	fn main() {
		let p = make_pair[i32, string](99, "ube")
		println(p.first)
		println(p.second)
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("generic function test failed: %v", err)
	}
	if got, want := runBinary(t, bin), "99\nube"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TEST 3: String Interpolation (f"...")
func TestFeatures_StringInterpolation(t *testing.T) {
	src := `fn main() {
		let lang = "Ube"
		let year = 2025
		let active = true
		let msg = f"Language: {lang}, Year: {year}, Active: {active}"
		println(msg)
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("string interpolation failed: %v", err)
	}
	if got, want := runBinary(t, bin), "Language: Ube, Year: 2025, Active: true"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TEST 4: Builtin format(...)
func TestFeatures_BuiltinFormat(t *testing.T) {
	src := `fn main() {
		let s = format("Count: {}, Value: {}", 42, "sweet")
		println(s)
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("format test failed: %v", err)
	}
	if got, want := runBinary(t, bin), "Count: 42, Value: sweet"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TEST 4b: Arena-backed StringBuilder
func TestFeatures_StringBuilderV06(t *testing.T) {
	src := `fn main() {
		let mut sb = StringBuilder.new(64)
		sb.append_str("count=")
		sb.append_i64(42)
		sb.append_char(44)
		sb.append_bool(true)
		println(sb.to_string())
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("StringBuilder test failed: %v", err)
	}
	if got, want := runBinary(t, bin), "count=42,true"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TEST 5: Trait constraint verification
func TestFeatures_TraitConstraint(t *testing.T) {
	srcValid := `trait Stringer {
		fn to_string() -> string
	}

	struct Person {
		name: string
	}

	fn (p Person) to_string() -> string {
		return p.name
	}

	fn print_val[T: Stringer](val: T) {
		println(val.to_string())
	}

	fn main() {
		let p = Person{ name: "John" }
		print_val[Person](p)
	}`

	bin, _, err := compileToNativeBinary(t, srcValid)
	if err != nil {
		t.Fatalf("trait constraint valid case failed: %v", err)
	}
	if got, want := runBinary(t, bin), "John"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	srcInvalid := `trait Stringer {
		fn to_string() -> string
	}

	struct Item {
		id: i32
	}

	fn print_val[T: Stringer](val: T) {
		println(val.to_string())
	}

	fn main() {
		let it = Item{ id: 10 }
		print_val[Item](it)
	}`

	errInvalid := typecheckSource(t, srcInvalid)
	if errInvalid == nil {
		t.Fatal("expected trait constraint mismatch to be rejected")
	}
	if !strings.Contains(errInvalid.Error(), "does not satisfy trait") {
		t.Fatalf("expected trait violation message, got: %v", errInvalid)
	}
}

// TEST 5b: Dynamic trait objects (fat pointers + vtables)
func TestFeatures_DynamicTraitDispatch(t *testing.T) {
	src := `trait Stringer {
		fn to_string() -> string
	}

	struct Person {
		name: string
	}

	struct Robot {
		id: i32
	}

	fn (p Person) to_string() -> string {
		return p.name
	}

	fn (r Robot) to_string() -> string {
		return f"R{r.id}"
	}

	fn main() {
		let items = []Stringer{ Person{ name: "Ada" }, Robot{ id: 7 } }
		for item in items {
			println(item.to_string())
		}
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("dynamic trait dispatch failed: %v", err)
	}
	if got, want := runBinary(t, bin), "Ada\nR7"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TEST 6: Code Formatter (ube fmt)
func TestFeatures_ArenaAndPromote(t *testing.T) {
	// 1. Verify compile-time rejection of direct mutation across arena boundaries
	badSrc := `
	fn main() {
		let mut list = []i32{}
		arena {
			let temp = []i32{1, 2}
			list = temp
		}
	}
	`
	err := typecheckSource(t, badSrc)
	if err == nil {
		t.Fatalf("expected compile-time rejection for assigning inner slice to outer list")
	}
	if !strings.Contains(err.Error(), "Outer slice mutation attempted within inner arena") {
		t.Fatalf("expected friendly error containing 'Outer slice mutation attempted within inner arena', got: %v", err)
	}

	// 2. Verify compiler rejection when calling .push() on an outer slice from inside an arena
	badPushSrc := `
	fn main() {
		let mut list = []i32{}
		arena {
			list.push(42)
		}
	}
	`
	err = typecheckSource(t, badPushSrc)
	if err == nil {
		t.Fatalf("expected compile-time rejection for pushing to outer list inside arena")
	}
	if !strings.Contains(err.Error(), "Outer slice mutation attempted within inner arena") {
		t.Fatalf("expected friendly error containing 'Outer slice mutation attempted within inner arena', got: %v", err)
	}

	// 3. Verify successful promotion across arena boundaries at runtime
	goodSrc := `
	fn main() {
		let mut output = []i32{}
		arena {
			let mut temp = []i32{}
			temp.push(10)
			temp.push(20)
			output = promote(temp)
		}
		println(output.len)
		println(output[0])
		println(output[1])
	}
	`
	bin, _, err := compileToNativeBinary(t, goodSrc)
	if err != nil {
		t.Fatalf("promotion compile failed: %v", err)
	}
	if got, want := runBinary(t, bin), "2\n10\n20"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// 4. Verify deep promotion for nested collections ([][]i32)
	nestedSrc := `
	fn main() {
		let mut matrix = [][]i32{}
		arena {
			let r1 = []i32{10, 20}
			let r2 = []i32{30, 40}
			let local_mat = [][]i32{r1, r2}
			matrix = promote(local_mat)
		}
		println(matrix.len)
		println(matrix[0][0])
		println(matrix[0][1])
		println(matrix[1][0])
		println(matrix[1][1])
	}
	`
	bin, _, err = compileToNativeBinary(t, nestedSrc)
	if err != nil {
		t.Fatalf("nested collection promotion compile failed: %v", err)
	}
	if got, want := runBinary(t, bin), "2\n10\n20\n30\n40"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// 5. Verify promote with struct return from arena expression block
	structReturnSrc := `
	struct Item {
		name: string,
		count: i32,
		tags: []i32
	}

	fn main() {
		let item = arena {
			let temp_name = "active_item"
			let vals = []i32{7, 8}
			promote(Item { name: temp_name[0:6], count: 99, tags: vals })
		}
		println(item.name)
		println(item.count)
		println(item.tags.len)
		println(item.tags[0])
		println(item.tags[1])
	}
	`
	bin, _, err = compileToNativeBinary(t, structReturnSrc)
	if err != nil {
		t.Fatalf("struct arena expression return failed: %v", err)
	}
	if got, want := runBinary(t, bin), "active\n99\n2\n7\n8"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFeatures_CodeFormatter(t *testing.T) {
	input := `fn test(  a:i32,b:string   )->i32{
let x=10+20
return x
}
`
	formatted, err := formatter.Format("test.ube", input)
	if err != nil {
		t.Fatalf("formatter failed: %v", err)
	}
	if !strings.Contains(formatted, "fn test(a: i32, b: string) -> i32 {") {
		t.Fatalf("unexpected formatting output:\n%s", formatted)
	}
	if !strings.Contains(formatted, "    let x = 10 + 20") {
		t.Fatalf("expected indented line:\n%s", formatted)
	}
}
