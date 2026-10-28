package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

// runTests discovers Ube test files (and test_* functions inside them),
// compiles each test into a temporary harness, and reports timing and status.
func runTests(args []string) error {
	target := "."
	if len(args) > 0 {
		target = args[0]
	}

	files, err := discoverTestFiles(target)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Printf("No test files found in %s\n", target)
		return nil
	}

	totalPassed, totalFailed := 0, 0
	start := time.Now()
	for _, file := range files {
		ast, _, err := loadAndCheck(file)
		if err != nil {
			fmt.Printf("• Testing %s ... %s\n%s\n", filepath.Base(file), paint("FAIL (check)", "\x1b[38;5;196m"), formatDiagnostic(err))
			totalFailed++
			continue
		}

		tests := discoverTestFuncs(ast)
		if len(tests) == 0 {
			if !hasMain(ast) {
				fmt.Printf("• Testing %s ... %s\n  no main function or test_ functions found\n", filepath.Base(file), paint("FAIL (check)", "\x1b[38;5;196m"))
				totalFailed++
				continue
			}
			ok, duration, output := runCompiledTest(file, "")
			reportTest(filepath.Base(file), ok, duration, output, &totalPassed, &totalFailed)
			continue
		}

		for _, name := range tests {
			ok, duration, output := runCompiledTest(file, name)
			reportTest(fmt.Sprintf("%s::%s", filepath.Base(file), name), ok, duration, output, &totalPassed, &totalFailed)
		}
	}

	fmt.Printf("\nTest run finished in %s. %d passed, %d failed.\n",
		time.Since(start).Round(time.Millisecond), totalPassed, totalFailed)
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

func discoverTestFiles(target string) ([]string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{target}, nil
	}

	var files []string
	err = filepath.Walk(target, func(path string, f os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if f.IsDir() {
			switch f.Name() {
			case ".git", "node_modules", "bin", "target":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".ube") {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasSuffix(base, "_test.ube") || strings.HasPrefix(base, "test_") {
			files = append(files, path)
			return nil
		}
		if data, readErr := os.ReadFile(path); readErr == nil && strings.Contains(string(data), "fn test_") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func discoverTestFuncs(prog *compiler.Program) []string {
	var names []string
	for _, decl := range prog.Decls {
		fn, ok := decl.(*compiler.FnDecl)
		if !ok || fn.Receiver != nil {
			continue
		}
		if !strings.HasPrefix(fn.Name, "test_") || len(fn.Params) != 0 {
			continue
		}
		if fn.RetType != nil && fn.RetType.String() != "void" {
			continue
		}
		names = append(names, fn.Name)
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

// runCompiledTest loads the file fresh, optionally injects a harness main that
// invokes a single test_ function, then builds and executes it.
func runCompiledTest(file, testName string) (bool, time.Duration, string) {
	ast, _, err := loadAndCheck(file)
	if err != nil {
		return false, 0, formatDiagnostic(err)
	}
	if testName != "" {
		injectTestHarness(ast, testName)
		if err := compiler.NewTypechecker().Check(ast); err != nil {
			return false, 0, formatDiagnostic(err)
		}
	}

	dir, err := os.MkdirTemp("", "ube-test-*")
	if err != nil {
		return false, 0, err.Error()
	}
	defer os.RemoveAll(dir)

	cPath := filepath.Join(dir, "test.c")
	binPath := filepath.Join(dir, "test-bin")
	if err := compileNative(ast, cPath, binPath, false, false, false); err != nil {
		return false, 0, err.Error()
	}

	start := time.Now()
	out, err := exec.Command(binPath).CombinedOutput()
	duration := time.Since(start).Round(time.Millisecond)
	if err != nil {
		return false, duration, string(out) + "\n" + err.Error()
	}
	return true, duration, string(out)
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
		RetType: compiler.PrimitiveType{Name: "void"},
		Body:    body,
	})
}
