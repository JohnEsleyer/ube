package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ube run <file.ube> [program args...]")
	}
	start := time.Now()
	ast, report, err := loadAndCheck(args[0])
	if err != nil {
		return err
	}

	base := filepath.Join(os.TempDir(), fmt.Sprintf("ube-run-%d", time.Now().UnixNano()))
	wasmPath := base + ".wasm"
	defer os.Remove(wasmPath)

	wasmBytes, err := compiler.EmitWasm(ast)
	if err != nil {
		return fmt.Errorf("failed to compile to WebAssembly: %w", err)
	}

	if writeErr := os.WriteFile(wasmPath, wasmBytes, 0755); writeErr != nil {
		return fmt.Errorf("failed to write wasm module to temp file: %w", writeErr)
	}

	label := "✓ Compiled Wasm"
	fmt.Printf("%s %s (%s)\n", paint(label, colorPurple), filepath.Base(args[0]), paint(time.Since(start).Round(time.Millisecond).String(), colorDim))
	printPurity(report)

	// Run in-process via embedded Wasmtime (Cranelift JIT).
	if runErr := runWasmtimeInProcess(wasmPath, args[1:]); runErr != nil {
		// In-process execution failed; fall back to an external runtime or native binary.
		if cmd, cmdErr := wasmRuntimeCommand(wasmPath, args[1:]); cmdErr == nil {
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

// runWasmtimeInProcess, runWasmtimeCapture, runWasmCapture, wasmRuntimeCommand remain unchanged for brevity.
// They are already part of a pure-Wasm execution path.
