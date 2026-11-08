package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const version = "1.4.0"

const (
	colorReset      = "\x1b[0m"
	colorGreen      = "\x1b[38;5;48m"
	colorPurple     = "\x1b[38;2;147;51;234m" // Deep Ube Purple
	colorPurpleBold = "\x1b[1;38;2;147;51;234m"
	colorDim        = "\x1b[2m"
)

func startSpinner(msg string) func() {
	stop := make(chan struct{})
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	go func() {
		idx := 0
		for {
			select {
			case <-stop:
				fmt.Print("\r\033[K")
				return
			case <-time.After(60 * time.Millisecond):
				fmt.Printf("\r%s %s", paint(frames[idx%len(frames)], colorPurple), paint(msg, colorDim))
				idx++
			}
		}
	}()
	return func() {
		close(stop)
		fmt.Print("\r\033[K")
	}
}

type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return fmt.Sprintf("program exited with status %d", e.code) }

func main() {
	if err := execute(os.Args[1:]); err != nil {
		if e, ok := err.(exitCodeError); ok {
			// The diagnostic text (if any) was already written to fd 2
			// by the generated _start function; do not duplicate it here.
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
	case "flow":
		return runFlow(args[1:])

	case "sig", "signatures":
		return runSig(args[1:])

	default:
		if strings.HasSuffix(args[0], ".ube") {
			return run(args)
		}
		if info, err := os.Stat(args[0]); err == nil && info.IsDir() {
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
