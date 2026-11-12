package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/JohnEsleyer/ube/pkg/targets"
)

var buildOptLevel string
var buildTarget string = "wasi1"

func optimizeWasmFile(wasmPath, optLevel string) (bool, error) {
	if optLevel == "" {
		return false, nil
	}
	wasmOpt, err := exec.LookPath("wasm-opt")
	if err != nil {
		return false, nil
	}
	cmd := exec.Command(wasmOpt, optLevel, wasmPath, "-o", wasmPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("wasm-opt %s failed: %w\n%s", optLevel, err, string(out))
	}
	return true, nil
}

func build(args []string) error {
	file, output, ai, err := parseBuildArgs(args)
	if err != nil {
		return err
	}

	if output == "" {
		output = defaultWasmOutput(file)
	}

	fileName := targetLabel(file)
	stopAnim := startSpinner("compiling " + fileName + "...")
	start := time.Now()

	backend := getBackend(buildTarget)
	applyStdlibOverlay(backend)

	ast, _, err := loadAndCheck(file)
	if err != nil {
		stopAnim()
		if ai {
			return fmt.Errorf("%s", formatDiagnosticAI(err))
		}
		return err
	}
	if err := requireExecutablePackage(ast); err != nil {
		stopAnim()
		return err
	}

	wasmBytes, err := backend.Compile(ast, targets.TargetConfig{
		OptLevel: buildOptLevel,
	})
	if err != nil {
		stopAnim()
		return fmt.Errorf("failed to compile to WebAssembly: %w", err)
	}

	if writeErr := os.WriteFile(output, wasmBytes, 0755); writeErr != nil {
		stopAnim()
		return fmt.Errorf("failed to write output file %s: %w", output, writeErr)
	}

	optApplied := false
	if buildOptLevel != "" {
		applied, optErr := optimizeWasmFile(output, buildOptLevel)
		if optErr != nil {
			stopAnim()
			return optErr
		}
		optApplied = applied
	}

	elapsed := time.Since(start).Round(time.Millisecond)
	stopAnim()

	optTag := ""
	if optApplied {
		optTag = fmt.Sprintf(" [%s]", buildOptLevel)
	}
	targetTag := ""
	if buildTarget == "wasi2" {
		targetTag = " [wasi 0.2 component]"
	}
	fmt.Printf("%s %s %s\n", paint("◆", colorPurpleBold), paint("Compiled", colorPurple), paint(fmt.Sprintf("%s -> %s%s%s (%s)", fileName, output, targetTag, optTag, elapsed), colorDim))
	return nil
}

func parseBuildArgs(args []string) (file, output string, ai bool, err error) {
	file = "."
	targetSet := false
	output = ""
	buildTarget = "wasi1"
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-o":
			if i+1 >= len(args) {
				return "", "", false, fmt.Errorf("-o requires an output path")
			}
			output = args[i+1]
			i++
		case args[i] == "--wasm" || args[i] == "--wasi1" || args[i] == "--target=wasi1" || args[i] == "--target=wasi":
			buildTarget = "wasi1"
		case args[i] == "--wasi2" || args[i] == "--target=wasi2" || args[i] == "--target=wasi0.2" || args[i] == "--target=component":
			buildTarget = "wasi2"
		case args[i] == "--target" || args[i] == "-t":
			if i+1 >= len(args) {
				return "", "", false, fmt.Errorf("%s requires a target (wasi1 or wasi2)", args[i])
			}
			t := args[i+1]
			if t == "wasi2" || t == "wasi0.2" || t == "component" {
				buildTarget = "wasi2"
			} else if t == "wasi1" || t == "wasi" {
				buildTarget = "wasi1"
			} else {
				return "", "", false, fmt.Errorf("unsupported target %q (valid targets: wasi1, wasi2)", t)
			}
			i++
		case args[i] == "--ai":
			ai = true
		case args[i] == "--release":
			buildOptLevel = "-O3"
		case args[i] == "-O" || args[i] == "-O0" || args[i] == "-O1" || args[i] == "-O2" || args[i] == "-O3" || args[i] == "-Os" || args[i] == "-Oz":
			if args[i] == "-O" {
				buildOptLevel = "-O3"
			} else {
				buildOptLevel = args[i]
			}
		case args[i] == "--html":
			return "", "", false, fmt.Errorf("building for --html requires Emscripten (emcc), which is no longer part of the default Ube toolchain. Please use `emcc` directly with the generated .wasm file, or see the documentation for browser integration options")
		case strings.HasPrefix(args[i], "-"):
			return "", "", false, fmt.Errorf("unknown or unsupported build option %s (use --release or -O[0-3|s|z] for optimization)", args[i])
		default:
			if targetSet {
				return "", "", false, fmt.Errorf("build accepts one package directory or source file")
			}
			file = args[i]
			targetSet = true
		}
	}
	return file, output, ai, nil
}
