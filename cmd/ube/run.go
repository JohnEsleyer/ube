package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func run(args []string) error {
	target := "."
	var programArgs []string
	memTrace := false
	ai := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--mem-trace" || arg == "-mem-trace" {
			memTrace = true
		} else if arg == "--ai" {
			ai = true
		} else if target == "." && !strings.HasPrefix(arg, "-") {
			target = arg
		} else {
			programArgs = append(programArgs, arg)
		}
	}

	fileName := targetLabel(target)
	stopAnim := startSpinner("compiling " + fileName + "...")
	start := time.Now()

	ast, _, err := loadAndCheck(target)
	if err != nil {
		stopAnim()
		return err
	}
	if err := requireExecutablePackage(ast); err != nil {
		stopAnim()
		return err
	}

	base := filepath.Join(os.TempDir(), fmt.Sprintf("ube-run-%d", time.Now().UnixNano()))
	wasmPath := base + ".wasm"
	defer os.Remove(wasmPath)

	var tracker *MemoryTracker
	if memTrace {
		tracker = newMemoryTracker()
	}

	cfg := compiler.EmitWasmConfig{
		MemTrace: memTrace,
	}
	wasmBytes, err := compiler.EmitWasmWithConfig(ast, cfg)
	if err != nil {
		stopAnim()
		return fmt.Errorf("failed to compile to WebAssembly: %w", err)
	}

	if writeErr := os.WriteFile(wasmPath, wasmBytes, 0755); writeErr != nil {
		stopAnim()
		return fmt.Errorf("failed to write wasm module to temp file: %w", writeErr)
	}

	elapsed := time.Since(start).Round(time.Millisecond)
	stopAnim()

	fmt.Printf("%s %s %s\n", paint("◆", colorPurpleBold), paint("Compiled", colorPurple), paint(fmt.Sprintf("%s (%s)", fileName, elapsed), colorDim))

	// Run in-process via embedded Wasmtime (Cranelift JIT).
	runErr := runWasmtimeInProcessWithTracker(wasmPath, programArgs, tracker)
	if tracker != nil {
		tracker.PrintReport(ai)
	}
	if runErr != nil {
		// In-process execution failed; fall back to an external runtime or native binary.
		if cmd, cmdErr := wasmRuntimeCommand(wasmPath, programArgs); cmdErr == nil {
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			if execErr := cmd.Run(); execErr != nil {
				if e, ok := execErr.(*exec.ExitError); ok {
					return exitCodeError{e.ExitCode()}
				}
				return execErr
			}
			return nil
		}
		return runErr
	}
	return nil
}
