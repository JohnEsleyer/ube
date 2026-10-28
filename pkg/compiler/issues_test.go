package compiler_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

// Helper to compile through typechecking, generate C, and invoke the C compiler.
func compileToNativeBinary(t *testing.T, source string) (binaryPath string, cSource string, cErr error) {
	t.Helper()
	cc, err := exec.LookPath("gcc")
	if err != nil {
		cc, err = exec.LookPath("clang")
	}
	if err != nil {
		t.Skip("gcc or clang is required to verify native compilation bugs")
	}

	tokens, err := compiler.NewLexer("test.ube", source).Tokenize()
	if err != nil {
		t.Fatalf("Lexer error: %v", err)
	}
	prog, err := compiler.NewParser(tokens).ParseProgram()
	if err != nil {
		t.Fatalf("Parser error: %v", err)
	}
	if err := compiler.NewTypechecker().Check(prog); err != nil {
		t.Fatalf("Typechecker error: %v", err)
	}

	cSource = compiler.NewGenerator(prog).Generate()
	tmp := t.TempDir()
	cfile := filepath.Join(tmp, "out.c")
	binaryPath = filepath.Join(tmp, "out")

	if err := os.WriteFile(cfile, []byte(cSource), 0600); err != nil {
		t.Fatal(err)
	}

	runtimePath, err := filepath.Abs("../../runtime")
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(cc, "-std=gnu11", "-O2", "-pthread", "-I"+runtimePath, cfile, "-o", binaryPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", cSource, fmt.Errorf("C compilation failed: %v\n%s", err, out)
	}
	return binaryPath, cSource, nil
}

func runBinary(t *testing.T, binaryPath string) string {
	t.Helper()
	out, err := exec.Command(binaryPath).CombinedOutput()
	if err != nil {
		t.Fatalf("program failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func typecheckSource(t *testing.T, source string) error {
	t.Helper()
	tokens, err := compiler.NewLexer("test.ube", source).Tokenize()
	if err != nil {
		t.Fatalf("Lexer error: %v", err)
	}
	prog, err := compiler.NewParser(tokens).ParseProgram()
	if err != nil {
		t.Fatalf("Parser error: %v", err)
	}
	return compiler.NewTypechecker().Check(prog)
}

// ----------------------------------------------------------------------------
// TEST 1: or_else on OptionalType must use has_val, not the Result field ok.
// ----------------------------------------------------------------------------
func TestIssue_OrElseOnOptional(t *testing.T) {
	src := `fn main() {
		let opt: i32? = null
		let val = opt or_else 42
		println(val)

		let present: i32? = 7
		println(present or_else 0)
	}`

	bin, cSource, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("or_else on Optional failed to compile: %v", err)
	}
	if strings.Contains(cSource, "_t.ok") {
		t.Errorf("optional or_else generated Result-only field access (_t.ok):\n%s", cSource)
	}
	if got, want := runBinary(t, bin), "42\n7"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 2: Early return in fn main() must emit 'return 0;', not bare 'return;'.
// ----------------------------------------------------------------------------
func TestIssue_EarlyReturnInMain(t *testing.T) {
	src := `fn main() {
		if true {
			return
		}
		println(123)
	}`

	bin, cSource, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("early return in main failed to compile: %v", err)
	}
	for _, line := range strings.Split(cSource, "\n") {
		if strings.TrimSpace(line) == "return;" {
			t.Fatalf("found bare 'return;' inside function returning int (main):\n%s", cSource)
		}
	}
	if got := runBinary(t, bin); got != "" {
		t.Fatalf("expected no output after early return, got %q", got)
	}
}

// ----------------------------------------------------------------------------
// TEST 3: Passing fixed-size arrays to functions uses a named C typedef.
// ----------------------------------------------------------------------------
func TestIssue_FixedSizeArrayParam(t *testing.T) {
	src := `fn sum_arr(a: [3]i32) -> i32 {
		return a[0] + a[1] + a[2]
	}
	fn main() {
		let arr = [3]i32{10, 20, 30}
		println(sum_arr(arr))
	}`

	bin, cSource, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("function taking [3]i32 failed to compile: %v", err)
	}
	if !strings.Contains(cSource, "UbeArray_") {
		t.Errorf("expected a named array typedef in generated C:\n%s", cSource)
	}
	if got, want := runBinary(t, bin), "60"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 4: '?' must reject incompatible error types.
// ----------------------------------------------------------------------------
func TestIssue_TryOperatorMismatchedErrors(t *testing.T) {
	src := `fn fail_with_string() -> i32 ! string {
		return error("something went wrong")
	}

	fn caller() -> i32 ! i32 {
		let x = fail_with_string()?
		return x
	}

	fn main() {}`

	err := typecheckSource(t, src)
	if err == nil {
		t.Fatal("typechecker allowed '?' with mismatched error types (string into i32)")
	}
	if !strings.Contains(err.Error(), "cannot propagate") {
		t.Fatalf("expected an error-propagation diagnostic, got: %v", err)
	}

	valid := `fn fail_with_string() -> i32 ! string {
		return error("something went wrong")
	}

	fn caller() -> i32 ! string {
		let x = fail_with_string()?
		return x
	}

	fn main() {}`
	if err := typecheckSource(t, valid); err != nil {
		t.Fatalf("compatible '?' was rejected: %v", err)
	}
}

// ----------------------------------------------------------------------------
// TEST 5: spawn on a receiver method must schedule the method, not drop it.
// ----------------------------------------------------------------------------
func TestIssue_SpawnReceiverMethod(t *testing.T) {
	src := `struct Worker {
		id: i32
	}

	fn (w Worker) run() {
		println(w.id)
	}

	fn main() {
		let w = Worker{ id: 42 }
		spawn w.run()
		sleep_ms(200)
	}`

	bin, cSource, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("spawn w.run() failed to compile: %v", err)
	}
	if strings.Contains(cSource, "invalid spawn call omitted") {
		t.Fatalf("'spawn w.run()' was silently dropped from generated code:\n%s", cSource)
	}
	if !strings.Contains(cSource, "Worker_run(") {
		t.Fatalf("expected the spawned wrapper to call Worker_run:\n%s", cSource)
	}
	if got, want := runBinary(t, bin), "42"; got != want {
		t.Fatalf("spawned receiver method produced %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 6: Hexadecimal and binary literals are single integer tokens.
// ----------------------------------------------------------------------------
func TestIssue_HexAndBinaryLiterals(t *testing.T) {
	src := `fn main() {
		let a = 0xFF
		let b = 0b1010
		println(a)
		println(b)
	}`

	tokens, err := compiler.NewLexer("test.ube", src).Tokenize()
	if err != nil {
		t.Fatalf("Unexpected lexer error: %v", err)
	}

	foundHex, foundBinary := false, false
	for i, tok := range tokens {
		if tok.Type == compiler.TokenInt && tok.Val == "0xFF" {
			foundHex = true
		}
		if tok.Type == compiler.TokenInt && tok.Val == "0b1010" {
			foundBinary = true
		}
		if i+1 < len(tokens) && tok.Type == compiler.TokenInt && tok.Val == "0" &&
			tokens[i+1].Type == compiler.TokenIdent {
			t.Fatalf("lexer split an integer literal into TokenInt(0) and TokenIdent(%s)", tokens[i+1].Val)
		}
	}
	if !foundHex || !foundBinary {
		t.Fatalf("expected full hex/binary integer tokens, got: %+v", tokens)
	}

	if _, err := compiler.NewParser(tokens).ParseProgram(); err != nil {
		t.Fatalf("parser rejected hexadecimal/binary literals: %v", err)
	}

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("hex/binary program failed to compile: %v", err)
	}
	if got, want := runBinary(t, bin), "255\n10"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 7: for..in iterates fixed-size arrays as well as slices.
// ----------------------------------------------------------------------------
func TestIssue_ArrayForIn(t *testing.T) {
	src := `fn main() {
		let arr = [3]i32{1, 2, 3}
		for x in arr {
			println(x)
		}
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("for-in over a fixed-size array failed to compile: %v", err)
	}
	if got, want := runBinary(t, bin), "1\n2\n3"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 8: Slice mutation propagates through helper calls.
// ----------------------------------------------------------------------------
func TestIssue_SliceMutationAcrossHelperFunctions(t *testing.T) {
	src := `fn push_inner(s: []i32) {
		s.push(42)
	}

	fn push_outer(s: []i32) {
		push_inner(s)
	}

	fn main() {
		let mut numbers = []i32{}
		push_outer(numbers)
		println(numbers.len)
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}
	if got, want := runBinary(t, bin), "1"; got != want {
		t.Fatalf("slice mutation was lost across helper calls: got %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 9: Scope-allocated references may not escape through function arguments.
// ----------------------------------------------------------------------------
func TestIssue_ScopeEscapeViaFunctionArgument(t *testing.T) {
	src := `fn attach(dst: [][]i32, src: []i32) {
		dst.push(src)
	}

	fn main() {
		let mut outer = [][]i32{}
		scope {
			let mut inner = []i32{}
			inner.push(100)
			attach(outer, inner)
		}
		println(outer[0][0])
	}`

	err := typecheckSource(t, src)
	if err == nil {
		t.Fatal("typechecker allowed an inner-scope reference to escape through a function argument")
	}
	if !strings.Contains(err.Error(), "Safety Error") {
		t.Fatalf("expected a Safety Error, got: %v", err)
	}

	// A function that merely reads a slice must still accept scoped values.
	valid := `fn read_len(s: []i32) -> i32 {
		return s.len
	}

	fn main() {
		scope {
			let inner = []i32{1}
			println(read_len(inner))
		}
	}`
	bin, _, err := compileToNativeBinary(t, valid)
	if err != nil {
		t.Fatalf("non-escaping helper call was rejected: %v", err)
	}
	if got, want := runBinary(t, bin), "1"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 10: select must not busy-spin on a closed-and-empty channel.
// ----------------------------------------------------------------------------
func TestIssue_SelectClosedChannelDoesNotLivelock(t *testing.T) {
	src := `fn sender(ch: Chan[i32]) {
		sleep_ms(150)
		ch.send(7)
	}

	fn main() {
		let ch_closed = Chan[i32].new(1)
		let ch_open = Chan[i32].new(1)
		ch_closed.close()

		spawn sender(ch_open)

		select {
			val1 = ch_closed => println(val1),
			val2 = ch_open => println(val2)
		}
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	cmd := exec.Command(bin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("program failed: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(string(out)), "7"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if cmd.ProcessState == nil {
		t.Fatal("missing process state")
	}
	cpu := cmd.ProcessState.UserTime() + cmd.ProcessState.SystemTime()
	if cpu > 80*time.Millisecond {
		t.Fatalf("select busy-spun on a closed channel: used %v of CPU", cpu)
	}
}

// ----------------------------------------------------------------------------
// TEST 11: Option & Result assignment, return, and initialization from identical types
// ----------------------------------------------------------------------------
func TestIssue_OptionAndResultPassthrough(t *testing.T) {
	src := `fn get_opt() -> i32? {
		let x: i32? = 100
		return x
	}

	fn get_res(fail: bool) -> i32 ! string {
		if fail {
			return error("failed")
		}
		return 200
	}

	fn forward_res(fail: bool) -> i32 ! string {
		return get_res(fail)
	}

	fn main() {
		let mut a: i32? = null
		let b = get_opt()
		a = b
		println(a or_else 0)

		let r1: i32 ! string = forward_res(false)
		println(r1 or_else 0)

		let mut r2: i32 ! string = 0
		r2 = forward_res(true)
		let val = r2 or_else -1
		println(val)
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("failed to compile option/result passthrough: %v", err)
	}
	if got, want := runBinary(t, bin), "100\n200\n-1"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 12: Bounds-checked string indexing
// ----------------------------------------------------------------------------
func TestIssue_StringIndexingBoundsCheck(t *testing.T) {
	src := `fn main() {
		let s = "hello"
		let ch = s[0]
		println(ch == 104)
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("failed to compile string indexing: %v", err)
	}
	if got, want := runBinary(t, bin), "true"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// TEST 13: Duplicate match arms are rejected at typecheck time
// ----------------------------------------------------------------------------
func TestIssue_DuplicateMatchArmRejected(t *testing.T) {
	src := `enum Status { Active, Inactive }
	fn test(s: Status) {
		match s {
			Active => println(1),
			Active => println(2),
			Inactive => println(0)
		}
	}
	fn main() {}`

	err := typecheckSource(t, src)
	if err == nil {
		t.Fatal("expected typechecker to reject duplicate match arm")
	}
	if !strings.Contains(err.Error(), "duplicate match pattern") {
		t.Fatalf("expected duplicate match pattern diagnostic, got: %v", err)
	}
}

// ----------------------------------------------------------------------------
// TEST 14: Non-optional while let is rejected
// ----------------------------------------------------------------------------
func TestIssue_InvalidWhileLetTarget(t *testing.T) {
	src := `fn main() {
		let x = 42
		while let item = x {
			println(item)
		}
	}`

	err := typecheckSource(t, src)
	if err == nil {
		t.Fatal("expected typechecker to reject while-let on integer")
	}
	if !strings.Contains(err.Error(), "while-let requires an optional type") {
		t.Fatalf("expected while-let error, got: %v", err)
	}
}
