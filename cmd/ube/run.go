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
	fileName := filepath.Base(args[0])
	stopAnim := startSpinner("compiling " + fileName + "...")
	start := time.Now()

	ast, _, err := loadAndCheck(args[0])
	if err != nil {
		stopAnim()
		return err
	}

	base := filepath.Join(os.TempDir(), fmt.Sprintf("ube-run-%d", time.Now().UnixNano()))
	wasmPath := base + ".wasm"
	defer os.Remove(wasmPath)

	wasmBytes, err := compiler.EmitWasm(ast)
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
