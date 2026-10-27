package main

import (
	"encoding/json"
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

type purityReport struct {
	externs   []string
	hasUnsafe bool
}

type safetyJSON struct {
	File             string   `json:"file"`
	OK               bool     `json:"ok"`
	Policy           string   `json:"policy"`
	HasUnsafe        bool     `json:"has_unsafe"`
	ForeignFunctions []string `json:"foreign_functions"`
	Error            string   `json:"error,omitempty"`
}
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
	checker := compiler.NewTypechecker()
	report := purityReport{hasUnsafe: checker.HasUnsafe}
	for _, decl := range ast.Decls {
		if foreign, ok := decl.(*compiler.ExternFnDecl); ok {
			report.externs = append(report.externs, foreign.Name)
		}
	}
	if err = checker.Check(ast); err != nil {
		report.hasUnsafe = checker.HasUnsafe
		return ast, report, err
	}
	report.hasUnsafe = checker.HasUnsafe
	return ast, report, nil
}

func check(args []string) error {
	formatJSON, strict, file := false, false, ""
	for _, arg := range args {
		switch arg {
		case "--format=json":
			formatJSON = true
		case "--strict":
			strict = true
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("unknown check option %s", arg)
			}
			if file != "" {
				return fmt.Errorf("check accepts one source file")
			}
			file = arg
		}
	}
	if file == "" {
		return fmt.Errorf("usage: ube check <file.ube> [--strict] [--format=json]")
	}
	start := time.Now()
	_, report, err := loadAndCheck(file)
	if err != nil {
		if formatJSON {
			printSafetyJSON(reportJSON(file, report, false, policyName(strict), err.Error()))
			return err
		}
		return err
	}
	if strict && unsafePresent(report) {
		err := fmt.Errorf("strict safety check failed: C FFI or unsafe blocks are present")
		if formatJSON {
			printSafetyJSON(reportJSON(file, report, false, "strict", err.Error()))
			return err
		}
		return err
	}
	if formatJSON {
		printSafetyJSON(reportJSON(file, report, true, policyName(strict), ""))
		return nil
	}
	fmt.Printf("%s %s (%s)\n", paint("✓ Checked", colorGreen), filepath.Base(file), paint(time.Since(start).Round(time.Millisecond).String(), colorDim))
	printPurity(report)
	return nil
}

func build(args []string) error {
	file, output, emitC, forbidUnsafe, err := parseBuildArgs(args)
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
	if forbidUnsafe && unsafePresent(report) {
		return fmt.Errorf("build rejected by --forbid-unsafe: C FFI or unsafe blocks are present")
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

func parseBuildArgs(args []string) (string, string, bool, bool, error) {
	if len(args) == 0 {
		return "", "", false, false, fmt.Errorf("usage: ube build <file.ube> [-o binary] [--emit-c] [--forbid-unsafe]")
	}
	file, output, emitC, forbidUnsafe := "", "", false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 >= len(args) {
				return "", "", false, false, fmt.Errorf("-o requires an output path")
			}
			output = args[i+1]
			i++
		case "--emit-c":
			emitC = true
		case "--forbid-unsafe":
			forbidUnsafe = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", false, false, fmt.Errorf("unknown build option %s", args[i])
			}
			if file != "" {
				return "", "", false, false, fmt.Errorf("multiple source files are not supported")
			}
			file = args[i]
		}
	}
	if file == "" {
		return "", "", false, false, fmt.Errorf("specify a .ube source file")
	}
	return file, output, emitC, forbidUnsafe, nil
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
	switch {
	case report.hasUnsafe:
		fmt.Println("  Safety: Tier 3 — Raw unsafe boundary")
		if len(report.externs) > 0 {
			fmt.Printf("  C FFI:  %s\n", strings.Join(report.externs, ", "))
		}
		fmt.Println("  Note:   Memory safety depends on the unsafe code and external library contracts")
	case len(report.externs) > 0:
		fmt.Println("  Safety: Tier 2 — FFI declared; no unsafe blocks detected")
		fmt.Printf("  C FFI:  %s\n", strings.Join(report.externs, ", "))
		fmt.Println("  Note:   Wrapper audit status cannot be inferred by the compiler")
	default:
		fmt.Println("  Safety: Tier 1 — Pure Ube (no C FFI or unsafe blocks)")
		fmt.Println("  Note:   Classification is an audit aid, not a formal proof")
	}
}

func unsafePresent(report purityReport) bool { return report.hasUnsafe || len(report.externs) > 0 }
func policyName(strict bool) string {
	if strict {
		return "strict"
	}
	return "informational"
}
func reportJSON(file string, report purityReport, ok bool, policy, message string) safetyJSON {
	return safetyJSON{File: file, OK: ok, Policy: policy, HasUnsafe: report.hasUnsafe, ForeignFunctions: report.externs, Error: message}
}
func printSafetyJSON(value safetyJSON) {
	if value.ForeignFunctions == nil {
		value.ForeignFunctions = []string{}
	}
	_ = json.NewEncoder(os.Stdout).Encode(value)
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
	fmt.Printf("🍠 Ube %s\n\nUsage: ube <command> [options]\n       ube <file.ube>\n\nCommands:\n  new <name>                Create a starter project\n  check <file.ube>          Check syntax and types\n  build <file.ube> [-o bin] Build a native executable\n  run <file.ube> [args...]  Compile and run a program\n  lsp                       Start the language server\n  version                   Show the version\n  help                      Show this help\n\nCheck options:\n  --strict                  Reject C FFI and unsafe blocks\n  --format=json             Emit a machine-readable audit report\n\nBuild options:\n  -o <path>                 Set executable output path\n  --emit-c                  Keep generated C beside the output\n  --forbid-unsafe           Reject C FFI and unsafe blocks\n", version)
}
