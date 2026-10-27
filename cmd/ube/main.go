package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/JohnEsleyer/ube/pkg/compiler"
	ubert "github.com/JohnEsleyer/ube/runtime"
)

const version = "0.5.0"

const (
	colorReset  = "\x1b[0m"
	colorGreen  = "\x1b[38;5;48m"
	colorPurple = "\x1b[38;5;141m"
	colorDim    = "\x1b[2m"
)

type purityReport struct{ externs []string }
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return fmt.Sprintf("program exited with status %d", e.code) }

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		if e, ok := err.(exitCodeError); ok {
			os.Exit(e.code)
		}
		os.Exit(1)
	}
}

func execute(args []string) error {
	if len(args) == 0 {
		help()
		return nil
	}
	switch args[0] {
	case "help", "-h", "--help":
		help()
		return nil
	case "version", "-v", "--version":
		fmt.Printf("ube %s\n", version)
		return nil
	case "lsp":
		runLSP()
		return nil
	case "new":
		return newProject(args[1:])
	case "check":
		return check(args[1:])
	case "build":
		return build(args[1:])
	case "run":
		return run(args[1:])
	default:
		if strings.HasSuffix(args[0], ".ube") {
			return run(args)
		}
		return fmt.Errorf("unknown command %q (run 'ube help' for usage)", args[0])
	}
}

func loadAndCheck(file string) (*compiler.Program, purityReport, error) {
	ast, err := compiler.NewModuleLoader().Load(file)
	if err != nil {
		return nil, purityReport{}, err
	}
	if err = compiler.NewTypechecker().Check(ast); err != nil {
		return nil, purityReport{}, err
	}
	report := purityReport{}
	for _, decl := range ast.Decls {
		if foreign, ok := decl.(*compiler.ExternFnDecl); ok {
			report.externs = append(report.externs, foreign.Name)
		}
	}
	return ast, report, nil
}

func check(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ube check <file.ube>")
	}
	start := time.Now()
	_, report, err := loadAndCheck(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("%s %s (%s)\n", paint("✓ Checked", colorGreen), filepath.Base(args[0]), paint(time.Since(start).Round(time.Millisecond).String(), colorDim))
	printPurity(report)
	return nil
}

func build(args []string) error {
	file, output, emitC, err := parseBuildArgs(args)
	if err != nil {
		return err
	}
	if output == "" {
		output = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	}
	start := time.Now()
	ast, report, err := loadAndCheck(file)
	if err != nil {
		return err
	}
	cPath := filepath.Join(os.TempDir(), fmt.Sprintf("ube-%d.c", time.Now().UnixNano()))
	if emitC {
		cPath = output + ".c"
	} else {
		defer os.Remove(cPath)
	}
	if err = compileNative(ast, cPath, output, true); err != nil {
		return err
	}
	fmt.Printf("%s %s (%s)\n", paint("✓ Built", colorGreen), output, paint(time.Since(start).Round(time.Millisecond).String(), colorDim))
	printPurity(report)
	return nil
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ube run <file.ube> [program args...]")
	}
	start := time.Now()
	ast, report, err := loadAndCheck(args[0])
	if err != nil {
		return err
	}
	binary := filepath.Join(os.TempDir(), fmt.Sprintf("ube-run-%d", time.Now().UnixNano()))
	cPath := binary + ".c"
	defer os.Remove(cPath)
	defer os.Remove(binary)
	if err = compileNative(ast, cPath, binary, false); err != nil {
		return err
	}
	fmt.Printf("%s %s (%s)\n", paint("✓ Compiled", colorPurple), filepath.Base(args[0]), paint(time.Since(start).Round(time.Millisecond).String(), colorDim))
	printPurity(report)
	cmd := exec.Command(binary, args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err = cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return exitCodeError{e.ExitCode()}
		}
		return err
	}
	return nil
}

func parseBuildArgs(args []string) (string, string, bool, error) {
	if len(args) == 0 {
		return "", "", false, fmt.Errorf("usage: ube build <file.ube> [-o binary] [--emit-c]")
	}
	file, output, emitC := "", "", false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 >= len(args) {
				return "", "", false, fmt.Errorf("-o requires an output path")
			}
			output = args[i+1]
			i++
		case "--emit-c":
			emitC = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", false, fmt.Errorf("unknown build option %s", args[i])
			}
			if file != "" {
				return "", "", false, fmt.Errorf("multiple source files are not supported")
			}
			file = args[i]
		}
	}
	if file == "" {
		return "", "", false, fmt.Errorf("specify a .ube source file")
	}
	return file, output, emitC, nil
}

func compileNative(ast *compiler.Program, cPath, output string, optimize bool) error {
	if err := os.WriteFile(cPath, []byte(compiler.NewGenerator(ast).Generate()), 0644); err != nil {
		return err
	}
	runtimeDir, err := ubert.EnsureRuntime()
	if err != nil {
		return fmt.Errorf("initialize runtime header: %w", err)
	}
	cc := ""
	for _, candidate := range []string{"cc", "gcc", "clang"} {
		if path, e := exec.LookPath(candidate); e == nil {
			cc = path
			break
		}
	}
	if cc == "" {
		return fmt.Errorf("no C compiler found (install GCC or Clang)")
	}
	args := []string{"-std=gnu11", "-pthread", "-I" + runtimeDir, cPath, "-o", output}
	if optimize {
		args = append(args, "-O2")
	}
	if out, e := exec.Command(cc, args...).CombinedOutput(); e != nil {
		return fmt.Errorf("native compilation failed:\n%s", strings.TrimSpace(string(out)))
	}
	return nil
}

func printPurity(report purityReport) {
	if len(report.externs) == 0 {
		fmt.Println("  Program: Pure Ube (no declared C FFI)")
		fmt.Println("  Safety:  Uses Ube-managed runtime; this label is not a formal memory-safety proof")
		return
	}
	fmt.Printf("  Program: C FFI (foreign functions: %s)\n", strings.Join(report.externs, ", "))
	fmt.Println("  Safety:  Memory safety also depends on the external C libraries")
}

func paint(text, color string) string {
	info, err := os.Stdout.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return text
	}
	return color + text + colorReset
}

func newProject(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: ube new <project-name>")
	}
	name := args[0]
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("%s already exists", name)
	}
	if err := os.MkdirAll(name, 0755); err != nil {
		return err
	}
	source := "fn main() {\n    println(\"Hello from Ube!\")\n}\n"
	if err := os.WriteFile(filepath.Join(name, "main.ube"), []byte(source), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(name, ".gitignore"), []byte("/main\n*.c\n"), 0644); err != nil {
		return err
	}
	fmt.Printf("✓ Created %s\n  cd %s && ube run main.ube\n", name, name)
	return nil
}

func help() {
	fmt.Printf("🍠 Ube %s\n\nUsage: ube <command> [options]\n       ube <file.ube>\n\nCommands:\n  new <name>                Create a starter project\n  check <file.ube>          Check syntax and types\n  build <file.ube> [-o bin] Build a native executable\n  run <file.ube> [args...]  Compile and run a program\n  lsp                       Start the language server\n  version                   Show the version\n  help                      Show this help\n\nBuild options:\n  -o <path>                 Set executable output path\n  --emit-c                  Keep generated C beside the output\n", version)
}
