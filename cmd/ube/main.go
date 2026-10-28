package main

import (
	"fmt"
	"os"
	"strings"
)

const version = "0.5.4"

const (
	colorReset  = "\x1b[0m"
	colorGreen  = "\x1b[38;5;48m"
	colorPurple = "\x1b[38;5;141m"
	colorDim    = "\x1b[2m"
)

type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return fmt.Sprintf("program exited with status %d", e.code) }

func main() {
	if err := execute(os.Args[1:]); err != nil {
		if e, ok := err.(exitCodeError); ok {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(e.code)
		}
		formatted := formatDiagnostic(err)
		if formatted == err.Error() {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, formatted)
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
	case "init":
		return initProject()
	case "check":
		return check(args[1:])
	case "build":
		return build(args[1:])
	case "run":
		return run(args[1:])
	case "test":
		return runTests(args[1:])
	case "fmt":
		return runFmt(args[1:])
	case "doc":
		return runDoc(args[1:])
	default:
		if strings.HasSuffix(args[0], ".ube") {
			return run(args)
		}
		return fmt.Errorf("unknown command %q (run 'ube help' for usage)", args[0])
	}
}

func paint(text, color string) string {
	info, err := os.Stdout.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return text
	}
	return color + text + colorReset
}
