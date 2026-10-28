package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JohnEsleyer/ube/pkg/compiler"
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
