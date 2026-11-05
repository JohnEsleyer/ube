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
	programArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target = args[0]
		programArgs = args[1:]
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
	if runErr := runWasmtimeInProcess(wasmPath, programArgs); runErr != nil {
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
