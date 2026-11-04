package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func build(args []string) error {
	file, output, ai, err := parseBuildArgs(args)
	if err != nil {
		return err
	}

	if output == "" {
		base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		output = base + ".wasm"
	}

	fileName := filepath.Base(file)
	stopAnim := startSpinner("compiling " + fileName + "...")
	start := time.Now()

	ast, _, err := loadAndCheck(file)
	if err != nil {
		stopAnim()
		if ai {
			return fmt.Errorf("%s", formatDiagnosticAI(err))
		}
		return err
	}

	wasmBytes, err := compiler.EmitWasm(ast)
	if err != nil {
		stopAnim()
		return fmt.Errorf("failed to compile to WebAssembly: %w", err)
	}

	if writeErr := os.WriteFile(output, wasmBytes, 0755); writeErr != nil {
		stopAnim()
		return fmt.Errorf("failed to write output file %s: %w", output, writeErr)
	}

	elapsed := time.Since(start).Round(time.Millisecond)
	stopAnim()

	fmt.Printf("%s %s %s\n", paint("◆", colorPurpleBold), paint("Compiled", colorPurple), paint(fmt.Sprintf("%s -> %s (%s)", fileName, output, elapsed), colorDim))
	return nil
}

func parseBuildArgs(args []string) (file, output string, ai bool, err error) {
	if len(args) == 0 {
		return "", "", false, fmt.Errorf("usage: ube build <file.ube> [-o app.wasm]")
	}
	file = ""
	output = ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-o":
			if i+1 >= len(args) {
				return "", "", false, fmt.Errorf("-o requires an output path")
			}
			output = args[i+1]
			i++
		case args[i] == "--wasm":
			// Default target, accepted as valid flag
		case args[i] == "--ai":
			ai = true
		case args[i] == "--html":
			return "", "", false, fmt.Errorf("building for --html requires Emscripten (emcc), which is no longer part of the default Ube toolchain. Please use `emcc` directly with the generated .wasm file, or see the documentation for browser integration options")
		case strings.HasPrefix(args[i], "-"):
			return "", "", false, fmt.Errorf("unknown or unsupported build option %s", args[i])
		default:
			if file != "" {
				return "", "", false, fmt.Errorf("multiple source files are not supported")
			}
			file = args[i]
		}
	}
	if file == "" {
		return "", "", false, fmt.Errorf("specify a .ube source file")
	}
	return file, output, ai, nil
}
