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
	file, output, _, _, html, err := parseBuildArgs(args)
	if err != nil {
		return err
	}

	if html {
		// Building for HTML requires Emscripten. This is a deployment target
		// choice, not a language feature fallback. We make this explicit.
		return fmt.Errorf("building for --html requires Emscripten (emcc), which is no longer part of the default Ube toolchain. Please use `emcc` directly with the generated .wasm file, or see the documentation for browser integration options")
	}

	if output == "" {
		base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		output = base + ".wasm"
	}

	start := time.Now()
	ast, report, err := loadAndCheck(file)
	if err != nil {
		return err
	}

	wasmBytes, err := compiler.EmitWasm(ast)
	if err != nil {
		return fmt.Errorf("failed to compile to WebAssembly: %w", err)
	}

	if writeErr := os.WriteFile(output, wasmBytes, 0755); writeErr != nil {
		return fmt.Errorf("failed to write output file %s: %w", output, writeErr)
	}

	label := "✓ Built Wasm module"
	fmt.Printf("%s %s (%s)\n", paint(label, colorGreen), output, paint(time.Since(start).Round(time.Millisecond).String(), colorDim))
	printPurity(report)
	return nil
}

func parseBuildArgs(args []string) (file, output, backend string, emitC, html bool, err error) {
	if len(args) == 0 {
		return "", "", "", false, false, fmt.Errorf("usage: ube build <file.ube> [-o app.wasm]")
	}
	file = ""
	output = ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-o":
			if i+1 >= len(args) {
				return "", "", "", false, false, fmt.Errorf("-o requires an output path")
			}
			output = args[i+1]
			i++
		case args[i] == "--html":
			html = true
		case strings.HasPrefix(args[i], "-"):
			return "", "", "", false, false, fmt.Errorf("unknown or unsupported build option %s", args[i])
		default:
			if file != "" {
				return "", "", "", false, false, fmt.Errorf("multiple source files are not supported")
			}
			file = args[i]
		}
	}
	if file == "" {
		return "", "", "", false, false, fmt.Errorf("specify a .ube source file")
	}
	return file, output, "", false, html, nil
}
