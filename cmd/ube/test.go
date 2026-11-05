package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

// testGroup is one package directory that contains tests. Cross-file package
// scope means tests are compiled as a package, not as isolated files.
type testGroup struct {
	dir        string
	label      string
	fileFilter string // optional absolute file path for `ube test path/to/file.ube`
}

// runTests discovers Ube test packages, compiles each package with all of its
// `_test.ube` files, and executes every `test_` function via embedded Wasmtime.
func runTests(args []string) error {
	target := "."
	runPattern := ""
	ai := false
	targetSet := false
	for i := 0; i < len(args); i++ {
		if (args[i] == "-run" || args[i] == "--run") && i+1 < len(args) {
			runPattern = args[i+1]
			i++
		} else if args[i] == "--ai" {
			ai = true
		} else if !strings.HasPrefix(args[i], "-") {
			if targetSet {
				return fmt.Errorf("test accepts one package directory, source file, or ./... pattern")
			}
			target = args[i]
			targetSet = true
		}
	}

	groups, err := discoverTestGroups(target)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		fmt.Printf("No test packages found in %s\n", target)
		return nil
	}

	totalPassed, totalFailed := 0, 0
	start := time.Now()
	for _, group := range groups {
		ast, err := compiler.NewModuleLoader().LoadTests(group.dir)
		if err != nil {
			if ai {
				fmt.Fprintln(os.Stderr, formatDiagnosticAI(err))
			} else {
				fmt.Printf("• Testing %s ... %s\n%s\n", group.label, paint("FAIL (check)", "\x1b[38;5;196m"), formatDiagnostic(err))
			}
			totalFailed++
			continue
		}
		if err := compiler.NewTypechecker().Check(ast); err != nil {
			if ai {
				fmt.Fprintln(os.Stderr, formatDiagnosticAI(err))
			} else {
				fmt.Printf("• Testing %s ... %s\n%s\n", group.label, paint("FAIL (check)", "\x1b[38;5;196m"), formatDiagnostic(err))
			}
			totalFailed++
			continue
		}

		tests := discoverTestFuncs(ast, group.fileFilter)
		if len(tests) == 0 {
			if !hasMain(ast) {
				fmt.Printf("• Testing %s ... %s\n  no main function or test_ functions found\n", group.label, paint("FAIL (check)", "\x1b[38;5;196m"))
				totalFailed++
				continue
			}
			ok, duration, output := runCompiledTest(group, "")
			reportTest(group.label, ok, duration, output, &totalPassed, &totalFailed)
			continue
		}

		for _, name := range tests {
			if runPattern != "" && !strings.Contains(name, runPattern) {
				continue
			}
			ok, duration, output := runCompiledTest(group, name)
			reportTest(fmt.Sprintf("%s::%s", group.label, name), ok, duration, output, &totalPassed, &totalFailed)
		}
	}

	passedStr := paint(fmt.Sprintf("%d passed", totalPassed), colorGreen)
	failedStr := fmt.Sprintf("%d failed", totalFailed)
	if totalFailed > 0 {
		failedStr = paint(failedStr, "\x1b[38;5;196m")
	}
	fmt.Printf("\nTest run finished in %s. %s, %s.\n",
		time.Since(start).Round(time.Millisecond), passedStr, failedStr)
	if totalFailed > 0 {
		return fmt.Errorf("%d tests failed", totalFailed)
	}
	return nil
}

func reportTest(name string, ok bool, duration time.Duration, output string, passed, failed *int) {
	if ok {
		fmt.Printf("• %s ... %s (%s)\n", name, paint("PASS", colorGreen), duration)
		(*passed)++
		return
	}
	fmt.Printf("• %s ... %s (%s)\n", name, paint("FAIL", "\x1b[38;5;196m"), duration)
	if trimmed := strings.TrimRight(output, "\n"); trimmed != "" {
		fmt.Println(trimmed)
	}
	(*failed)++
}

// discoverTestGroups turns a CLI target into package directories. A plain
// directory means exactly that package; a `...` suffix walks nested packages,
// mirroring `go test ./...`.
func discoverTestGroups(target string) ([]testGroup, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}

	root, recursive := recursiveTarget(abs)
	if !recursive {
		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return []testGroup{{
				dir:        filepath.Dir(abs),
				label:      filepath.Base(abs),
				fileFilter: filepath.Clean(abs),
			}}, nil
		}
		if !hasTestSources(abs) {
			return nil, nil
		}
		return []testGroup{{dir: abs, label: targetLabel(abs)}}, nil
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	var groups []testGroup
	err = filepath.Walk(root, func(path string, f os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !f.IsDir() {
			return nil
		}
		switch f.Name() {
		case ".git", "node_modules", "bin", "target":
			return filepath.SkipDir
		}
		if hasTestSources(path) {
			groups = append(groups, testGroup{dir: path, label: targetLabel(path)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].dir < groups[j].dir })
	return groups, nil
}

// recursiveTarget recognizes Go-style `./...` and `dir/...` targets.
func recursiveTarget(target string) (string, bool) {
	slash := filepath.ToSlash(target)
	if slash == "..." {
		return ".", true
	}
	if strings.HasSuffix(slash, "/...") {
		root := strings.TrimSuffix(slash, "/...")
		if root == "" {
			root = "."
		}
		return filepath.FromSlash(root), true
	}
	return target, false
}

func hasTestSources(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ube") {
			continue
		}
		base := entry.Name()
		if strings.HasSuffix(base, "_test.ube") || strings.HasPrefix(base, "test_") {
			return true
		}
		path := filepath.Join(dir, base)
		if data, readErr := os.ReadFile(path); readErr == nil && strings.Contains(string(data), "fn test_") {
			return true
		}
	}
	return false
}

func discoverTestFuncs(prog *compiler.Program, fileFilter string) []string {
	var names []string
	for _, decl := range prog.Decls {
		fn, ok := decl.(*compiler.FnDecl)
		if !ok || fn.Receiver != nil {
			continue
		}
		name := fn.Name
		short := name
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			short = name[i+1:]
		}
		if !strings.HasPrefix(short, "test_") || len(fn.Params) != 0 {
			continue
		}
		if fn.RetType != nil && fn.RetType.String() != "void" {
			continue
		}
		if fileFilter != "" && filepath.Clean(fn.At.File) != fileFilter {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func hasMain(prog *compiler.Program) bool {
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*compiler.FnDecl); ok && fn.Name == "main" {
			return true
		}
	}
	return false
}

// runCompiledTest loads the package with its test files, optionally injects a
// harness main, compiles directly to Wasm bytecode, and executes via Wasmtime.
func runCompiledTest(group testGroup, testName string) (bool, time.Duration, string) {
	ast, err := compiler.NewModuleLoader().LoadTests(group.dir)
	if err != nil {
		return false, 0, formatDiagnostic(err)
	}
	if testName != "" {
		injectTestHarness(ast, testName)
	}
	if err := compiler.NewTypechecker().Check(ast); err != nil {
		return false, 0, formatDiagnostic(err)
	}

	dir, err := os.MkdirTemp("", "ube-test-*")
	if err != nil {
		return false, 0, err.Error()
	}
	defer os.RemoveAll(dir)

	wasmPath := filepath.Join(dir, "test.wasm")
	wasmBytes, emitErr := compiler.EmitWasm(ast)
	if emitErr != nil {
		return false, 0, emitErr.Error()
	}
	if err := os.WriteFile(wasmPath, wasmBytes, 0755); err != nil {
		return false, 0, err.Error()
	}

	start := time.Now()
	out, err := runWasmCapture(wasmPath, nil)
	duration := time.Since(start).Round(time.Millisecond)
	if err != nil {
		return false, duration, out + "\n" + err.Error()
	}
	return true, duration, out
}

func injectTestHarness(prog *compiler.Program, testName string) {
	loc := prog.At
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*compiler.FnDecl); ok && fn.Name == "main" {
			fn.Name = "__ube_test_file_main"
		}
	}
	call := &compiler.CallExpr{
		At:     loc,
		Callee: &compiler.IdentExpr{At: loc, Name: testName},
	}
	stmt := &compiler.ExprStmt{At: loc, Expr: call}
	body := &compiler.BlockStmt{At: loc, Stmts: []compiler.Stmt{stmt}}
	prog.Decls = append(prog.Decls, &compiler.FnDecl{
		At:      loc,
		Name:    "main",
		Package: prog.Package,
		RetType: compiler.PrimitiveType{Name: "void"},
		Body:    body,
	})
}
