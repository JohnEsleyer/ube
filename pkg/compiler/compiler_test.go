package compiler_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func compilePipeline(t *testing.T, source string) (*compiler.Program, error) {
	t.Helper()
	tokens, err := compiler.NewLexer("test.ube", source).Tokenize()
	if err != nil {
		return nil, err
	}
	prog, err := compiler.NewParser(tokens).ParseProgram()
	if err != nil {
		return nil, err
	}
	if err := compiler.NewTypechecker().Check(prog); err != nil {
		return nil, err
	}
	return prog, nil
}

func TestLexerOperatorsAndEscapes(t *testing.T) {
	code := `&& || == != <= >= -> => ? ! + - * / % & = "Hello\n\t\"Ube\"\\"`
	tokens, err := compiler.NewLexer("test.ube", code).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	want := []compiler.TokenType{compiler.TokenAnd, compiler.TokenOr, compiler.TokenEq, compiler.TokenNeq, compiler.TokenLte, compiler.TokenGte, compiler.TokenArrow, compiler.TokenFatArrow, compiler.TokenQuestion, compiler.TokenBang, compiler.TokenPlus, compiler.TokenMinus, compiler.TokenStar, compiler.TokenSlash, compiler.TokenPercent, compiler.TokenAmp, compiler.TokenAssign, compiler.TokenString, compiler.TokenEOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(want))
	}
	for i, typ := range want {
		if tokens[i].Type != typ {
			t.Errorf("token %d: got %s, want %s", i, tokens[i].Type, typ)
		}
	}
	if got, wantString := tokens[len(tokens)-2].Val, "Hello\n\t\"Ube\"\\"; got != wantString {
		t.Errorf("decoded string %q, want %q", got, wantString)
	}
}

func TestLoopControlBoundaries(t *testing.T) {
	for _, src := range []string{`fn main() { break }`, `fn main() { continue }`, `fn main() { if true { break } }`, `fn main() { scope { continue } }`} {
		if _, err := compilePipeline(t, src); err == nil {
			t.Errorf("expected loop-control rejection for %s", src)
		}
	}
	valid := `fn main() {
		while true {
			if true { break }
			continue
		}
		let s = []i32{}
		for n in s {
			if n == 0 { continue }
			break
		}
	}`
	if _, err := compilePipeline(t, valid); err != nil {
		t.Fatalf("valid loop control rejected: %v", err)
	}
}

func TestTypecheckerRejectsBasicMismatches(t *testing.T) {
	cases := []string{
		`fn main() { let mut n: i32 = 1; n = true }`,
		`fn main() { if 42 {} }`,
		`fn main() { while "no" {} }`,
		`fn main() { let n = 42; for x in n {} }`,
		`fn main() { let mut s = []i32{}; s.push("wrong") }`,
		`fn get() -> i32 { return "wrong" } fn main() {}`,
		`fn main() { let b = 10 && 20 }`,
	}
	for _, src := range cases {
		if _, err := compilePipeline(t, src); err == nil {
			t.Errorf("expected type error for %s", src)
		}
	}
	if _, err := compilePipeline(t, `fn main() {
		let a = true && false
		let b = (10 > 5) || (2 == 3)
	}`); err != nil {
		t.Fatalf("valid logical expressions rejected: %v", err)
	}
}

func TestEndToEndGeneratedC(t *testing.T) {
	cc, err := exec.LookPath("gcc")
	if err != nil {
		cc, err = exec.LookPath("clang")
	}
	if err != nil {
		t.Skip("gcc or clang is required for the generated-C integration test")
	}
	prog, err := compilePipeline(t, `fn main() {
		let mut s = []i32{}
		let mut i = 0
		while i < 12 {
			s.push(i)
			i = i + 1
		}
		let mut total = 0
		for n in s { total = total + n }
		println(total)
		println(true)
		println("ok")
	}`)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	cfile, binary := filepath.Join(tmp, "out.c"), filepath.Join(tmp, "out")
	if err := os.WriteFile(cfile, []byte(compiler.NewGenerator(prog).Generate()), 0600); err != nil {
		t.Fatal(err)
	}
	runtimePath, err := filepath.Abs("../../runtime")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command(cc, "-std=gnu11", "-O2", "-pthread", "-I"+runtimePath, cfile, "-o", binary)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("C compilation failed: %v\n%s", err, out)
	}
	out, err := exec.Command(binary).CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(string(out)), "66\ntrue\nok"; got != want {
		t.Fatalf("output %q, want %q", got, want)
	}
}

func TestStandalonePrograms(t *testing.T) {
	cc, err := exec.LookPath("gcc")
	if err != nil {
		cc, err = exec.LookPath("clang")
	}
	if err != nil {
		t.Skip("gcc or clang is required for standalone Ube integration tests")
	}
	cases := []struct{ file, want string }{
		{"test_basics.ube", "11\ntrue\ntrue\n16"},
		{"test_slice_growth.ube", "2450"},
		{"test_scope_nesting.ube", "100\n10\n1\n2"},
	}
	runtimePath, err := filepath.Abs("../../runtime")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("../../tests", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			prog, err := compilePipeline(t, string(source))
			if err != nil {
				t.Fatalf("compile pipeline: %v", err)
			}
			tmp := t.TempDir()
			cfile, binary := filepath.Join(tmp, "out.c"), filepath.Join(tmp, "out")
			if err := os.WriteFile(cfile, []byte(compiler.NewGenerator(prog).Generate()), 0600); err != nil {
				t.Fatal(err)
			}
			build := exec.Command(cc, "-std=gnu11", "-O2", "-pthread", "-I"+runtimePath, cfile, "-o", binary)
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("C compilation failed: %v\n%s", err, out)
			}
			out, err := exec.Command(binary).CombinedOutput()
			if err != nil {
				t.Fatalf("program failed: %v\n%s", err, out)
			}
			if got := strings.TrimSpace(string(out)); got != tc.want {
				t.Fatalf("output %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLexer(t *testing.T) {
	code := "fn let mut struct enum match if while spawn scope break continue Chan 42 \"hello\" true null"
	lexer := compiler.NewLexer("test.ube", code)
	toks, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Lexer error: %v", err)
	}
	expected := []compiler.TokenType{
		compiler.TokenFn, compiler.TokenLet, compiler.TokenMut,
		compiler.TokenStruct, compiler.TokenEnum, compiler.TokenMatch,
		compiler.TokenIf, compiler.TokenWhile, compiler.TokenSpawn,
		compiler.TokenScope, compiler.TokenBreak, compiler.TokenContinue,
		compiler.TokenIdent, compiler.TokenInt,
		compiler.TokenString, compiler.TokenBool, compiler.TokenNull,
		compiler.TokenEOF,
	}

	for i, exp := range expected {
		if toks[i].Type != exp {
			t.Errorf("Index %d: expected token %s, got %s", i, exp, toks[i].Type)
		}
	}
}

func TestParserAndTypechecker(t *testing.T) {
	code := `
	struct Particle {
		x: f32,
		y: f32,
		active: bool
	}

	fn make_particle() -> Particle {
		let p = Particle{ x: 10.5, y: 20.0, active: true }
		return p
	}

	fn main() {
		let mut pt = make_particle()
		pt.x = 99.0
	}
	`
	lexer := compiler.NewLexer("test.ube", code)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Lexer failed: %v", err)
	}

	parser := compiler.NewParser(tokens)
	ast, err := parser.ParseProgram()
	if err != nil {
		t.Fatalf("Parser failed: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(ast); err != nil {
		t.Fatalf("Typechecker failed: %v", err)
	}
}

func TestTypecheckerRejectsImmutableAssignment(t *testing.T) {
	badCode := `
	fn main() {
		let val = 10
		val = 20
	}
	`
	tokens, _ := compiler.NewLexer("test.ube", badCode).Tokenize()
	ast, _ := compiler.NewParser(tokens).ParseProgram()
	tc := compiler.NewTypechecker()

	if err := tc.Check(ast); err == nil {
		t.Errorf("Expected immutable assignment error, but typecheck passed")
	}
}

func TestTypecheckerRejectsNonFlatChannel(t *testing.T) {
	badCode := `
	struct User {
		name: string
	}

	fn handle_user(ch: Chan[User]) {
	}
	`
	tokens, _ := compiler.NewLexer("test.ube", badCode).Tokenize()
	ast, _ := compiler.NewParser(tokens).ParseProgram()
	tc := compiler.NewTypechecker()

	if err := tc.Check(ast); err == nil {
		t.Errorf("Expected rejection of non-flat Chan[User], but typecheck passed")
	}
}

func runSourceNative(t *testing.T, source string) string {
	t.Helper()
	cc, err := exec.LookPath("gcc")
	if err != nil {
		cc, err = exec.LookPath("clang")
	}
	if err != nil {
		t.Skip("gcc or clang required for native execution tests")
	}
	prog, err := compilePipeline(t, source)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	tmp := t.TempDir()
	cfile, binary := filepath.Join(tmp, "test_out.c"), filepath.Join(tmp, "test_bin")
	if err := os.WriteFile(cfile, []byte(compiler.NewGenerator(prog).Generate()), 0600); err != nil {
		t.Fatal(err)
	}
	runtimePath, err := filepath.Abs("../../runtime")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cc, "-std=gnu11", "-O2", "-pthread", "-I"+runtimePath, cfile, "-o", binary)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("C compilation error: %v\n%s", err, out)
	}
	runCmd := exec.Command(binary)
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("runtime error: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestArenaChunkRecyclingStress(t *testing.T) {
	src := `fn main() {
		let mut loop_cnt = 0
		let mut total_verified = 0
		while loop_cnt < 300 {
			scope {
				let mut big = []i32{}
				let mut i = 0
				while i < 15000 {
					big.push(i)
					i = i + 1
				}
				total_verified = total_verified + big[14999]
			}
			loop_cnt = loop_cnt + 1
		}
		println(total_verified)
	}`
	if out := runSourceNative(t, src); out != "4499700" {
		t.Fatalf("expected 4499700, got %s", out)
	}
}

func TestFunctionAllocatingAndReturning(t *testing.T) {
	src := `
	fn make_range(n: i32) -> []i32 {
		let mut s = []i32{}
		let mut i = 0
	while i < n {
		s.push(i * 10)
		i = i + 1
	}
		return s
	}
	fn append_suffix(base: string) -> string { return base + "_sweet" }
	fn push_caller_slice(target: []i32, val: i32) { target.push(val) }
	fn main() {
		let mut numbers = make_range(4)
		push_caller_slice(numbers, 999)
		for num in numbers { println(num) }
		let s = append_suffix("ube")
		println(s)
	}`
	expected := "0\n10\n20\n30\n999\nube_sweet"
	if out := runSourceNative(t, src); out != expected {
		t.Fatalf("got %q, want %q", out, expected)
	}
}

func TestEventDrivenSelect(t *testing.T) {
	src := `fn sender(ch: Chan[i32]) { ch.send(12345) }
	fn main() {
		let ch1 = Chan[i32].new(2)
		let ch2 = Chan[i32].new(2)
		spawn sender(ch2)
		select {
			v1 = ch1 => println(v1),
			v2 = ch2 => println(v2)
		}
	}`
	if out := runSourceNative(t, src); out != "12345" {
		t.Fatalf("expected 12345, got %q", out)
	}
}

func TestSelectWithDefault(t *testing.T) {
	src := `fn main() {
		let empty_ch = Chan[i32].new(2)
		select { val = empty_ch => println(val), default => println(42) }
	}`
	if out := runSourceNative(t, src); out != "42" {
		t.Fatalf("expected default output 42, got %q", out)
	}
}

func TestGenerationalPoolHandleValidation(t *testing.T) {
	src := `struct Item { id: i32 }
	fn main() {
		let mut pool = Pool[Item].new(4)
		let h1 = pool.spawn(Item{ id: 101 })
		if let it = pool.get(h1) { println(it.id) }
		pool.despawn(h1)
		if let stale = pool.get(h1) { println(-1) } else { println(0) }
		let h2 = pool.spawn(Item{ id: 202 })
		if let stale2 = pool.get(h1) { println(-2) } else { println(1) }
		if let current = pool.get(h2) { println(current.id) }
	}`
	expected := "101\n0\n1\n202"
	if out := runSourceNative(t, src); out != expected {
		t.Fatalf("got %q, want %q", out, expected)
	}
}

func TestCompileTimeScopeEscapeRejections(t *testing.T) {
	cases := []struct{ name, src string }{
		{"reject assigning inner scope reference to outer variable", `fn main() {
			let mut outer = []i32{}
			scope {
				let mut inner = []i32{}
				inner.push(1)
				outer = inner
			}
		}`},
		{"reject mutating outer slice via push inside inner scope", `fn main() {
			let mut outer = []i32{}
			scope { outer.push(10) }
		}`},
		{"reject returning reference allocated in inner scope", `fn bad() -> []i32 {
			scope {
				let mut s = []i32{}
				s.push(1)
				return s
			}
			return []i32{}
		}
		fn main() {}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compilePipeline(t, tc.src)
			if err == nil {
				t.Fatal("expected scope safety rejection")
			}
			if !strings.Contains(err.Error(), "Safety Error") {
				t.Fatalf("expected Safety Error, got: %v", err)
			}
		})
	}
}

func TestMultiArgSpawnAndReceivers(t *testing.T) {
	src := `struct Calc { factor: i32 }
	fn (c Calc) mul(a: i32, b: i32) -> i32 { return a * b * c.factor }
	fn worker(ch: Chan[i32], mult: i32) { ch.send(10 * mult) }
	fn main() {
		let c = Calc{ factor: 2 }
		println(c.mul(3, 4))
		let ch = Chan[i32].new(1)
		spawn worker(ch, 5)
		select { value = ch => println(value) }
	}`
	if out := runSourceNative(t, src); out != "24\n50" {
		t.Fatalf("expected '24\\n50', got %q", out)
	}
}

func TestParserMultiErrorRecovery(t *testing.T) {
	code := "fn first( { }\nfn second( { }"
	tokens, err := compiler.NewLexer("test.ube", code).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	_, parseErr := compiler.NewParser(tokens).ParseProgram()
	if parseErr == nil {
		t.Fatal("expected syntax errors, got nil")
	}
	if lines := strings.Split(parseErr.Error(), "\n"); len(lines) < 2 {
		t.Fatalf("expected at least 2 collected syntax errors, got: %v", parseErr)
	}
}
