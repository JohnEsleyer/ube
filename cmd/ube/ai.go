package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

// runContext generates an AI-ready context summary with signatures and control flow.
func runContext(args []string) error {
	target, outFile, err := parseContextArgs(args)
	if err != nil {
		return err
	}

	var files []string
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("target %q: %w", target, err)
	}

	if info.IsDir() {
		files, err = discoverUbeFiles(target)
		if err != nil {
			return fmt.Errorf("scanning directory %s: %w", target, err)
		}
		if len(files) == 0 {
			return fmt.Errorf("no .ube files found in directory %s", target)
		}
	} else {
		if !strings.HasSuffix(target, ".ube") {
			return fmt.Errorf("target file must have .ube extension: %s", target)
		}
		files = []string{target}
	}

	sort.Strings(files)

	var sb strings.Builder
	sb.WriteString("================================================================================\n")
	sb.WriteString("UBE CODEBASE CONTEXT (Generated for AI / LLM Assistance)\n")
	sb.WriteString(fmt.Sprintf("Target: %s | Source Files: %d\n", target, len(files)))
	sb.WriteString("================================================================================\n\n")

	totalDecls := 0
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			sb.WriteString(fmt.Sprintf("// Error reading %s: %v\n\n", file, err))
			continue
		}

		lexer := compiler.NewLexer(file, string(src))
		tokens, err := lexer.Tokenize()
		if err != nil {
			sb.WriteString(fmt.Sprintf("// Lexer error in %s: %v\n\n", file, err))
			continue
		}

		parser := compiler.NewParser(tokens)
		prog, err := parser.ParseProgram()
		if err != nil {
			sb.WriteString(fmt.Sprintf("// Parser error in %s: %v\n\n", file, err))
			continue
		}

		sb.WriteString(fmt.Sprintf("File: %s\n", file))
		sb.WriteString(strings.Repeat("-", 80) + "\n")

		if len(prog.Decls) == 0 {
			sb.WriteString("  (no top-level declarations)\n\n")
			continue
		}

		for _, decl := range prog.Decls {
			totalDecls++
			switch d := decl.(type) {
			case *compiler.StructDecl:
				sb.WriteString(formatStructSign(d) + "\n")
			case *compiler.EnumDecl:
				sb.WriteString(formatEnumSign(d) + "\n")
			case *compiler.TraitDecl:
				sb.WriteString(formatTraitSign(d) + "\n")
			case *compiler.FnDecl:
				sb.WriteString(formatFnSign(d) + "\n")
				calls, flow := analyzeFnFlow(d)
				if len(calls) > 0 {
					sb.WriteString(fmt.Sprintf("    calls: [%s]\n", strings.Join(calls, ", ")))
				}
				if len(flow) > 0 {
					sb.WriteString(fmt.Sprintf("    flow:  %s\n", strings.Join(flow, ", ")))
				}
				sb.WriteString("\n")
			case *compiler.ExternFnDecl:
				sb.WriteString(formatExternFnSign(d) + "\n\n")
			}
		}
	}

	if totalDecls == 0 {
		sb.WriteString("// No declarations could be extracted from the target.\n")
	}

	if err := os.WriteFile(outFile, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("writing context file %s: %w", outFile, err)
	}

	added, err := ensureGitignore(outFile)
	if err != nil {
		return fmt.Errorf("updating .gitignore: %w", err)
	}

	if added {
		fmt.Printf("✓ Generated codebase context in %s (added to .gitignore)\n", outFile)
	} else {
		fmt.Printf("✓ Generated codebase context in %s\n", outFile)
	}
	return nil
}

// runGuide generates a comprehensive Ube language reference guide for AI.
func runGuide(args []string) error {
	outFile, err := parseGuideArgs(args)
	if err != nil {
		return err
	}

	guideContent := getUbeLanguageGuide()
	if err := os.WriteFile(outFile, []byte(guideContent), 0644); err != nil {
		return fmt.Errorf("writing guide file %s: %w", outFile, err)
	}

	added, err := ensureGitignore(outFile)
	if err != nil {
		return fmt.Errorf("updating .gitignore: %w", err)
	}

	if added {
		fmt.Printf("✓ Generated Ube language guide in %s (added to .gitignore)\n", outFile)
	} else {
		fmt.Printf("✓ Generated Ube language guide in %s\n", outFile)
	}
	return nil
}

// ensureGitignore appends the target filename to the .gitignore next to it if
// one exists. It reports whether a new entry was appended; an existing entry is
// left untouched and the function reports false.
func ensureGitignore(filename string) (bool, error) {
	cleanTarget := filepath.Clean(filename)
	baseTarget := filepath.Base(cleanTarget)
	gitignorePath := filepath.Join(filepath.Dir(cleanTarget), ".gitignore")

	data, err := os.ReadFile(gitignorePath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == baseTarget || trimmed == "/"+baseTarget || trimmed == cleanTarget || trimmed == "/"+cleanTarget {
			return false, nil // Already present
		}
	}

	var sb strings.Builder
	sb.Write(data)
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString(baseTarget + "\n")

	if err := os.WriteFile(gitignorePath, []byte(sb.String()), 0644); err != nil {
		return false, err
	}
	return true, nil
}

func parseContextArgs(args []string) (target string, output string, err error) {
	output = "ube_context.txt"
	target = "."
	targetSet := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-o" {
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("flag -o requires an argument")
			}
			output = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-o=") {
			output = strings.TrimPrefix(arg, "-o=")
		} else if strings.HasPrefix(arg, "--output=") {
			output = strings.TrimPrefix(arg, "--output=")
		} else if strings.HasPrefix(arg, "-") {
			return "", "", fmt.Errorf("unknown flag %q", arg)
		} else {
			if !targetSet {
				target = arg
				targetSet = true
			} else {
				return "", "", fmt.Errorf("unexpected argument %q", arg)
			}
		}
	}
	return target, output, nil
}

func parseGuideArgs(args []string) (output string, err error) {
	output = "ube_guide.md"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-o" {
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag -o requires an argument")
			}
			output = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-o=") {
			output = strings.TrimPrefix(arg, "-o=")
		} else if strings.HasPrefix(arg, "--output=") {
			output = strings.TrimPrefix(arg, "--output=")
		} else if strings.HasPrefix(arg, "-") {
			return "", fmt.Errorf("unknown flag %q", arg)
		} else {
			return "", fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return output, nil
}

// AST Flow and Signature Analysis

type flowVisitor struct {
	calls map[string]bool
	flow  map[string]bool
}

func analyzeFnFlow(fn *compiler.FnDecl) (calls []string, flow []string) {
	if fn.Body == nil {
		return nil, nil
	}
	v := &flowVisitor{
		calls: make(map[string]bool),
		flow:  make(map[string]bool),
	}
	v.walkStmt(fn.Body)

	for c := range v.calls {
		calls = append(calls, c)
	}
	sort.Strings(calls)

	orderedFlowTags := []string{
		"spawn", "select", "arena", "promote", "match",
		"if-let", "while-let", "for-in", "while", "try?", "or_else", "defer", "unsafe",
	}
	for _, tag := range orderedFlowTags {
		if v.flow[tag] {
			flow = append(flow, tag)
		}
	}
	return calls, flow
}

func (v *flowVisitor) walkStmt(st compiler.Stmt) {
	if st == nil {
		return
	}
	switch s := st.(type) {
	case *compiler.BlockStmt:
		for _, child := range s.Stmts {
			v.walkStmt(child)
		}
	case *compiler.LetStmt:
		v.walkExpr(s.Value)
	case *compiler.AssignStmt:
		v.walkExpr(s.LHS)
		v.walkExpr(s.RHS)
	case *compiler.ReturnStmt:
		v.walkExpr(s.Value)
	case *compiler.IfStmt:
		v.walkExpr(s.Cond)
		v.walkStmt(s.Then)
		v.walkStmt(s.Else)
	case *compiler.IfLetStmt:
		v.flow["if-let"] = true
		v.walkExpr(s.Expr)
		v.walkStmt(s.Then)
		v.walkStmt(s.Else)
	case *compiler.WhileStmt:
		v.flow["while"] = true
		v.walkExpr(s.Cond)
		v.walkStmt(s.Body)
	case *compiler.WhileLetStmt:
		v.flow["while-let"] = true
		v.walkExpr(s.Call)
		v.walkStmt(s.Body)
	case *compiler.ForStmt:
		v.flow["for-in"] = true
		v.walkExpr(s.Iter)
		v.walkStmt(s.Body)
	case *compiler.ScopeStmt:
		v.flow["arena"] = true
		v.walkStmt(s.Body)
	case *compiler.UnsafeStmt:
		v.flow["unsafe"] = true
		v.walkStmt(s.Body)
	case *compiler.SpawnStmt:
		v.flow["spawn"] = true
		if s.Call != nil {
			v.walkExpr(s.Call)
		}
	case *compiler.MatchStmt:
		v.flow["match"] = true
		v.walkExpr(s.Expr)
		for _, arm := range s.Arms {
			v.walkStmt(arm.Body)
		}
	case *compiler.SelectStmt:
		v.flow["select"] = true
		for _, sc := range s.Cases {
			v.walkExpr(sc.Chan)
			v.walkStmt(sc.Body)
		}
		if s.Default != nil {
			v.walkStmt(s.Default)
		}
	case *compiler.ExprStmt:
		v.walkExpr(s.Expr)
	case *compiler.DeferStmt:
		v.flow["defer"] = true
		v.walkExpr(s.Call)
	}
}

func (v *flowVisitor) walkExpr(ex compiler.Expr) {
	if ex == nil {
		return
	}
	switch e := ex.(type) {
	case *compiler.BinaryExpr:
		v.walkExpr(e.Left)
		v.walkExpr(e.Right)
	case *compiler.UnaryExpr:
		v.walkExpr(e.Operand)
	case *compiler.CallExpr:
		cName := calleeName(e.Callee)
		if cName != "" {
			v.calls[cName] = true
		}
		v.walkExpr(e.Callee)
		for _, arg := range e.Args {
			v.walkExpr(arg)
		}
	case *compiler.MemberExpr:
		v.walkExpr(e.Target)
	case *compiler.IndexExpr:
		v.walkExpr(e.Target)
		v.walkExpr(e.Index)
	case *compiler.SliceSubExpr:
		v.walkExpr(e.Target)
		v.walkExpr(e.Start)
		v.walkExpr(e.End)
	case *compiler.StructInitExpr:
		for _, f := range e.Fields {
			v.walkExpr(f.Val)
		}
	case *compiler.SliceInitExpr:
		for _, el := range e.Elements {
			v.walkExpr(el)
		}
	case *compiler.ArrayInitExpr:
		for _, el := range e.Elements {
			v.walkExpr(el)
		}
	case *compiler.TryExpr:
		v.flow["try?"] = true
		v.walkExpr(e.Operand)
	case *compiler.OrElseExpr:
		v.flow["or_else"] = true
		v.walkExpr(e.Primary)
		v.walkExpr(e.Fallback)
	case *compiler.ErrorExpr:
		v.walkExpr(e.Arg)
	case *compiler.UnsafeExpr:
		v.flow["unsafe"] = true
		v.walkExpr(e.Value)
	case *compiler.PromoteExpr:
		v.flow["promote"] = true
		v.walkExpr(e.Value)
	case *compiler.ArenaExpr:
		v.flow["arena"] = true
		v.walkStmt(e.Body)
	case *compiler.InterpolatedStringExpr:
		for _, p := range e.Parts {
			v.walkExpr(p)
		}
	case *compiler.RangeExpr:
		v.walkExpr(e.Start)
		v.walkExpr(e.End)
	case *compiler.TraitCoerceExpr:
		v.walkExpr(e.Value)
	case *compiler.ArrayToSliceExpr:
		v.walkExpr(e.ArrayExpr)
	}
}

// builtinMethods are language/runtime methods that are not project dependencies,
// so they are omitted from the call list to keep it focused on user code.
var builtinMethods = map[string]bool{
	"push": true, "pop": true, "clear": true, "contains": true,
	"len": true, "cap": true,
	"new": true, "send": true, "recv": true, "close": true,
	"spawn": true, "despawn": true, "get": true,
	"append": true, "append_str": true, "append_int": true,
	"append_i64": true, "append_char": true, "append_bool": true,
	"to_string": true,
}

func calleeName(e compiler.Expr) string {
	switch c := e.(type) {
	case *compiler.IdentExpr:
		return c.Name
	case *compiler.MemberExpr:
		if builtinMethods[c.Member] {
			return ""
		}
		if id, ok := c.Target.(*compiler.IdentExpr); ok {
			return id.Name + "." + c.Member
		}
		return c.Member
	default:
		return ""
	}
}

func formatTypeParams(params []compiler.TypeParam) string {
	if len(params) == 0 {
		return ""
	}
	var parts []string
	for _, tp := range params {
		if tp.Constraint != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", tp.Name, tp.Constraint))
		} else {
			parts = append(parts, tp.Name)
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func formatStructSign(s *compiler.StructDecl) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("struct %s%s {\n", s.Name, formatTypeParams(s.TypeParams)))
	for _, f := range s.Fields {
		mutPrefix := ""
		if f.IsMut {
			mutPrefix = "mut "
		}
		sb.WriteString(fmt.Sprintf("    %s%s: %s\n", mutPrefix, f.Name, f.Type.String()))
	}
	sb.WriteString("}\n")
	return sb.String()
}

func formatEnumSign(e *compiler.EnumDecl) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("enum %s {\n", e.Name))
	for _, v := range e.Variants {
		if len(v.Types) == 0 {
			sb.WriteString(fmt.Sprintf("    %s\n", v.Name))
		} else {
			var typeStrs []string
			for _, t := range v.Types {
				typeStrs = append(typeStrs, t.String())
			}
			sb.WriteString(fmt.Sprintf("    %s(%s)\n", v.Name, strings.Join(typeStrs, ", ")))
		}
	}
	sb.WriteString("}\n")
	return sb.String()
}

func formatTraitSign(t *compiler.TraitDecl) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("trait %s {\n", t.Name))
	for _, m := range t.Methods {
		var params []string
		for _, p := range m.Params {
			params = append(params, fmt.Sprintf("%s: %s", p.Name, p.Type.String()))
		}
		ret := ""
		if m.RetType != nil {
			ret = fmt.Sprintf(" -> %s", m.RetType.String())
		}
		sb.WriteString(fmt.Sprintf("    fn %s(%s)%s\n", m.Name, strings.Join(params, ", "), ret))
	}
	sb.WriteString("}\n")
	return sb.String()
}

func formatFnSign(fn *compiler.FnDecl) string {
	receiver := ""
	if fn.Receiver != nil {
		mutPrefix := ""
		if fn.Receiver.IsMut {
			mutPrefix = "mut "
		}
		receiver = fmt.Sprintf("(%s%s %s) ", mutPrefix, fn.Receiver.Name, fn.Receiver.Type.String())
	}

	var params []string
	for _, p := range fn.Params {
		mutPrefix := ""
		if p.IsMut {
			mutPrefix = "mut "
		}
		params = append(params, fmt.Sprintf("%s%s: %s", mutPrefix, p.Name, p.Type.String()))
	}

	ret := ""
	if fn.RetType != nil {
		ret = fmt.Sprintf(" -> %s", fn.RetType.String())
	}

	return fmt.Sprintf("fn %s%s%s(%s)%s", receiver, fn.Name, formatTypeParams(fn.TypeParams), strings.Join(params, ", "), ret)
}

func formatExternFnSign(fn *compiler.ExternFnDecl) string {
	var params []string
	for _, p := range fn.Params {
		params = append(params, fmt.Sprintf("%s: %s", p.Name, p.Type.String()))
	}
	ret := ""
	if fn.RetType != nil {
		ret = fmt.Sprintf(" -> %s", fn.RetType.String())
	}
	return fmt.Sprintf("extern fn %s(%s)%s", fn.Name, strings.Join(params, ", "), ret)
}

// getUbeLanguageGuide returns a self-contained Ube reference aimed at LLMs.
// "@@@" is replaced with a Markdown code fence and "@@" with a backtick so the
// raw string can stay readable.
func getUbeLanguageGuide() string {
	guide := strings.ReplaceAll(ubeLanguageGuide, "@@@", "```")
	return strings.ReplaceAll(guide, "@@", "`")
}

const ubeLanguageGuide = `# The Ube Programming Language: AI Reference Guide

This document is a self-contained specification of Ube intended for AI models.
Read it before writing or modifying Ube code.

## 1. What is Ube?

Ube is a statically typed, ahead-of-time systems language that transpiles to
C99 and is compiled with the system C compiler (gcc/clang, -O2). Its design
goals are:

- **Zero Garbage Collection:** memory is managed with regional bump arenas that
  are released in constant time. There are no GC pauses and no reference
  counting.
- **Go-like ergonomics with C-level performance:** familiar @@fn@@/@@let@@ syntax,
  channels, tasks, and pattern matching over tagged enums.
- **Shared-nothing concurrency:** data crossing a task or channel boundary is
  copied by value and must be a "flat" type. This removes data races by
  construction.

Ube is **not** Rust, Go, or C, and borrow-checker or GC idioms do not apply.

## 2. Source files and the CLI

- Programs live in @@.ube@@ files. A file may pull in another file with
  @@import "relative/path.ube"@@; imported declarations become available
  globally (there are no namespaces).
- Comments use @@//@@.
- Typical commands:

@@@sh
ube new myapp          # scaffold a project
ube check main.ube     # parse + typecheck
ube build main.ube -o app
ube run main.ube
ube test               # discover and run test files
ube fmt -w .           # format sources
ube doc main.ube       # print declarations and signatures
ube context            # write an AI-oriented codebase summary
ube guide              # write this guide to ube_guide.md
@@@

## 3. Variables and types

Variables are immutable unless declared with @@mut@@. Types are inferred when
omitted.

@@@ube
fn main() {
    let name = "Ube"          // string (immutable binding)
    let mut count: i32 = 2    // mutable binding
    count = count + 1
    count += 3
    println(f"{name}: {count}")
}
@@@

### Primitive types

- Signed integers: @@i8@@, @@i16@@, @@i32@@, @@i64@@
- Unsigned integers: @@u8@@, @@u16@@, @@u32@@, @@u64@@
- Floats: @@f32@@, @@f64@@
- @@bool@@ with literals @@true@@ and @@false@@
- @@string@@ (UTF-8 bytes, heap/arena allocated, **not flat**)
- @@void@@ for functions that return nothing
- Raw pointers @@*T@@ (only usable inside @@unsafe@@)

### Composite types

- Slices: @@[]T@@ (dynamic, arena allocated)
- Arrays: @@[N]T@@ (fixed size, value type)
- Optionals: @@T?@@ (value or @@null@@)
- Results: @@T ! E@@ (success value or error value)
- Channels: @@Chan[T]@@
- Pools: @@Pool[T]@@
- @@StringBuilder@@
- User structs and enums by name

### Strings

String literals use double quotes. Prefix a literal with @@f@@ to enable
interpolation with @@{expr}@@; plain strings are literal:

@@@ube
fn main() {
    let who = "world"
    println("hello {who}")            // prints: hello {who}
    println(f"hello {who}, 1 + 1 = {1 + 1}")   // prints: hello world, 1 + 1 = 2
}
@@@

@@import@@ example:

@@@ube
import "lib.ube"
@@@

Strings are indexed as bytes (@@s[i]@@ yields @@u8@@) and sliced with
@@s[start:end]@@ (end exclusive), which returns a new string:

@@@ube
fn main() {
    let s = "active_item"
    println(s[0:6])   // active
}
@@@

## 4. Functions and methods

@@@ube
fn add(a: i32, b: i32) -> i32 {
    return a + b
}

struct Rect {
    width: i32,
    height: i32
}

// Value receiver: the struct is copied.
fn (r Rect) area() -> i32 {
    return r.width * r.height
}

// Mutable receiver: the struct is mutated in place.
fn (mut r Rect) scale(factor: i32) {
    r.width *= factor
    r.height *= factor
}
@@@

Functions are declared at top level. There are no closures; anonymous
functions do not exist. Use @@defer@@ to schedule a call at scope exit:

@@@ube
fn cleanup() { println("done") }

fn main() {
    defer cleanup()
    println("working")
}
@@@

## 5. Structs, enums, and pattern matching

Structs are value types. Literals use @@Name { field: value, ... }@@.

@@@ube
struct Point {
    x: f32,
    y: f32
}

fn main() {
    let p = Point { x: 1.0, y: 2.5 }
    println(p.x)
}
@@@

Enums are tagged unions. Variants may carry zero or more payload types, and
@@match@@ destructures them exhaustively:

@@@ube
enum Shape {
    Circle(f32),
    Rect(f32, f32),
    Empty
}

fn describe(s: Shape) {
    match s {
        Circle(r) => println(f"circle radius {r}"),
        Rect(w, h) => println(f"rect {w}x{h}"),
        Empty => println("empty")
    }
}
@@@

Loops:

@@@ube
fn main() {
    for i in 0..10 {           // range loop, end exclusive
        println(i)
    }

    for item in []i32{1, 2, 3} {
        println(item)
    }

    let mut n = 0
    while n < 3 {
        n += 1
    }
}
@@@

## 6. Errors and optionals

A function that can fail returns a result type written @@Ok ! Err@@:

@@@ube
fn parse_port(code: i32) -> i32 ! string {
    if code < 0 {
        return error("invalid port")
    }
    return code
}
@@@

- @@expr?@@ propagates an error to the caller (only valid in a function whose
  return type is a result).
- @@expr or_else fallback@@ extracts the success value or uses the fallback.

@@@ube
fn boot() -> i32 ! string {
    let port = parse_port(8080)?
    return port
}

fn fallback_port() -> i32 {
    return parse_port(-1) or_else 80
}
@@@

Optionals are written @@T?@@ and use @@null@@:

@@@ube
fn find_user(id: i32) -> string? {
    if id == 1 {
        return "Alice"
    }
    return null
}

fn main() {
    if let name = find_user(1) {
        println(f"found {name}")
    } else {
        println("not found")
    }
}
@@@

@@while let@@ also unwraps optionals and reads channels until they close:

@@@ube
fn drain(queue: Chan[i32]) {
    while let item = queue.recv() {
        println(item)
    }
}
@@@

## 7. Memory model: arenas, scopes, and promote (CRITICAL)

Ube has **no garbage collector**. Heap-like data (slices, strings produced at
runtime, string builders) is allocated from a bump arena associated with the
current region.

### Arena blocks

@@arena { ... }@@ and @@scope { ... }@@ are synonyms. Each block enters a new
arena level; every allocation made inside is reclaimed in one instruction when
the block exits. The block may be used as a statement or as an expression:

@@@ube
fn main() {
    // Statement form
    arena {
        let mut temp = []i32{}
        temp.push(1)
        println(temp.len)
    } // temp's memory is reclaimed here

    // Expression form: the final expression is the block's value
    let doubled = arena {
        let mut local = []i32{1, 2, 3}
        promote(local)
    }
    println(doubled.len)
}
@@@

### The upward reference invariant

An inner arena must not store or mutate data owned by an outer region, because
the inner arena is released first. The compiler rejects such code at
typecheck time, for example:

- @@outer_slice.push(x)@@ inside an arena where @@outer_slice@@ was declared
  outside it;
- assigning an inner slice or string to an outer variable;
- returning a value allocated in an inner arena.

Diagnostics mention: "Outer slice mutation attempted within inner arena ...
use promote(...) to export data".

### promote

To move data out of an arena, copy it into the parent region with
@@promote(value)@@. Promotion is a deep copy:

- slices are reallocated in the parent arena (nested slices, structs, and
  optionals are promoted recursively);
- strings are copied;
- pointers have their pointee copied.

@@@ube
fn main() {
    let mut output = []i32{}

    arena {
        let mut temp = []i32{}
        temp.push(10)
        temp.push(20)
        output = promote(temp)
    }

    println(output.len)  // 2
    println(output[0])   // 10
}
@@@

Rule of thumb: **anything that outlives an arena block must be promoted**.

## 8. Concurrency: tasks, channels, select, pools

- @@spawn f(args...)@@ starts a concurrent task. The target must be a direct
  call, and all arguments must be flat.
- @@Chan[T].new(capacity)@@ creates a buffered channel.
- @@ch.send(v)@@, @@ch.recv()@@, @@ch.close()@@.
- @@select@@ waits on channel reads, with an optional @@default@@ arm.

@@@ube
struct Job {
    id: i32,
    weight: i32
}

fn worker(jobs: Chan[Job], results: Chan[i32]) {
    while let j = jobs.recv() {
        results.send(j.weight * 2)
    }
}

fn main() {
    let jobs = Chan[Job].new(4)
    let results = Chan[i32].new(4)

    spawn worker(jobs, results)

    jobs.send(Job { id: 1, weight: 50 })
    jobs.close()

    select {
        res = results => println(f"result {res}"),
        default => println("no result yet")
    }
}
@@@

### Flat type restriction

Values sent through a channel or passed to a spawned task must be **flat**:
primitives, fixed arrays of flat types, enums/structs whose fields are all
flat, and channels of flat types. The following are **not** flat and are
rejected at compile time:

- @@string@@, @@StringBuilder@@
- slices @@[]T@@
- raw pointers @@*T@@
- structs or enums containing any of the above
- @@Pool[T]@@

This guarantees no pointer ever crosses a task boundary, so there is no shared
mutable state and no locking.

### Object pools

@@Pool[T]@@ provides generational handles to reusable slots:

@@@ube
struct Item { id: i32 }

fn main() {
    let mut pool = Pool[Item].new(4)
    let h = pool.spawn(Item { id: 101 })

    if let item = pool.get(h) {
        println(item.id)
    }

    pool.despawn(h)
}
@@@

## 9. Generics and traits

Generics are monomorphized at compile time:

@@@ube
struct Box[T] {
    val: T
}

fn identity[T](x: T) -> T {
    return x
}

fn main() {
    // Generic arguments are explicit at call sites.
    let b = Box[i32] { val: 5 }
    println(identity[i32](b.val))
}
@@@

Traits provide dynamic dispatch. A value can be passed where a trait is
expected and is boxed automatically:

@@@ube
trait Greeter {
    fn greet() -> string
}

struct Human {
    name: string
}

fn (h Human) greet() -> string {
    return f"Hello, {h.name}"
}

fn announce(g: Greeter) {
    println(g.greet())
}

fn main() {
    announce(Human { name: "Ada" })
}
@@@

## 10. Unsafe and C FFI

@@unsafe { ... }@@ blocks are required for raw pointer operations. @@extern fn@@
declares a C function. Code using @@unsafe@@ or FFI is flagged by @@ube check
--strict@@.

## 11. Built-in functions and methods

- Printing: @@print(...)@@, @@println(...)@@, @@println_str(s)@@
- Slices: @@s.len@@, @@s.cap@@, @@s.push(v)@@, @@s.pop()@@, @@s.clear()@@,
  @@s.contains(v)@@, @@s[i]@@
- Strings: @@s.len@@, @@s[i]@@, @@s[a:b]@@, @@str_trim@@, @@str_split@@,
  @@str_replace@@, @@str_contains@@, @@str_starts_with@@, @@str_ends_with@@,
  @@str_index_of@@, @@str_to_int@@, @@int_to_str@@
- Files and OS: @@read_file@@, @@write_file@@, @@file_exists@@, @@file_remove@@,
  @@path_join@@, @@dir_list@@, @@dir_create@@, @@env_get@@, @@env_set@@,
  @@os_args@@, @@cmd_run@@
- Math: @@abs@@, @@min@@, @@max@@, @@sqrt@@, @@pow@@, @@floor@@, @@ceil@@
- Time and scheduling: @@time_now_ms@@, @@sleep_ms@@, @@yield@@, @@exit_process@@
- StringBuilder: @@format@@, @@append@@, @@append_str@@, @@append_int@@,
  @@append_i64@@, @@append_char@@, @@append_bool@@, @@to_string@@

## 12. Summary of rules an AI must respect

1. Prefer @@let@@; add @@mut@@ only when reassignment or mutation is needed.
2. Never assume a GC: data escaping an @@arena@@/@@scope@@ must be @@promote@@d.
3. Never mutate outer-owned slices or strings from inside an inner arena.
4. Only flat values may cross @@spawn@@ and @@Chan@@ boundaries; strings and
   slices are not flat.
5. Use @@?@@ and @@or_else@@ for results, @@if let@@/@@while let@@ for optionals.
6. Raw pointers require @@unsafe@@; avoid them unless interfacing with C.
7. There are no closures or exceptions; split code across files with
   @@import "file.ube"@@ when needed.
`
