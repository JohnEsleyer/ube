package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/JohnEsleyer/ube/pkg/compiler"
	ubert "github.com/JohnEsleyer/ube/runtime"
)

func build(args []string) error {
	file, output, backend, emitC, html, err := parseBuildArgs(args)
	if err != nil {
		return err
	}
	if output == "" {
		base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		if html {
			output = base + ".html"
		} else {
			output = base + ".wasm"
		}
	}
	start := time.Now()
	ast, report, err := loadAndCheck(file)
	if err != nil {
		return err
	}

	// 1. Direct WebAssembly Binary Emission (Instant, Zero C SDK dependency)
	var fallbackReason string
	if backend != "c" && !html && !emitC {
		if wasmBytes, emitErr := compiler.EmitWasm(ast); emitErr == nil {
			if writeErr := os.WriteFile(output, wasmBytes, 0755); writeErr == nil {
				label := "✓ Built Wasm module (direct)"
				fmt.Printf("%s %s (%s)\n", paint(label, colorGreen), output, paint(time.Since(start).Round(time.Millisecond).String(), colorDim))
				printPurity(report)
				return nil
			}
		} else {
			fallbackReason = emitErr.Error()
		}
	}

	// 2. Fallback to C compilation pipeline (for HTML or complex runtime features)
	cPath := filepath.Join(os.TempDir(), fmt.Sprintf("ube-%d.c", time.Now().UnixNano()))
	if emitC {
		cPath = strings.TrimSuffix(output, filepath.Ext(output)) + ".c"
	} else {
		defer os.Remove(cPath)
	}

	wasmOutput, toolUsed, err := compileWasmWithTool(ast, cPath, output, html)
	if err != nil {
		return err
	}
	label := "✓ Built Wasm module"
	switch {
	case html:
		label = "✓ Built WebAssembly web app"
	case !wasmOutput:
		label = "✓ Built native fallback"
	case toolUsed != "":
		if fallbackReason != "" {
			label = fmt.Sprintf("✓ Built Wasm module (%s fallback)", toolUsed)
		} else {
			label = fmt.Sprintf("✓ Built Wasm module (%s)", toolUsed)
		}
	}
	fmt.Printf("%s %s (%s)\n", paint(label, colorGreen), output, paint(time.Since(start).Round(time.Millisecond).String(), colorDim))
	printPurity(report)
	return nil
}

func parseBuildArgs(args []string) (string, string, string, bool, bool, error) {
	if len(args) == 0 {
		return "", "", "", false, false, fmt.Errorf("usage: ube build <file.ube> [-o app.wasm] [--backend=wasm|c] [--html] [--emit-c]")
	}
	file, output, backend, emitC, html := "", "", "wasm", false, false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-o":
			if i+1 >= len(args) {
				return "", "", "", false, false, fmt.Errorf("-o requires an output path")
			}
			output = args[i+1]
			i++
		case args[i] == "--emit-c":
			emitC = true
		case args[i] == "--html":
			html = true
		case args[i] == "--wasm":
			backend = "wasm"
		case args[i] == "--backend=wasm" || (args[i] == "--backend" && i+1 < len(args) && args[i+1] == "wasm"):
			backend = "wasm"
			if args[i] == "--backend" {
				i++
			}
		case args[i] == "--backend=c" || (args[i] == "--backend" && i+1 < len(args) && args[i+1] == "c"):
			backend = "c"
			if args[i] == "--backend" {
				i++
			}
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", "", false, false, fmt.Errorf("unknown build option %s", args[i])
			}
			if file != "" {
				return "", "", "", false, false, fmt.Errorf("multiple source files are not supported")
			}
			file = args[i]
		}
	}
	if file == "" {
		return "", "", "", false, false, fmt.Errorf("specify a .ube source file")
	}
	return file, output, backend, emitC, html, nil
}

// compileWasm emits a WebAssembly module by default. HTML output is delegated to
// Emscripten. When no Wasm toolchain (or WASI sysroot) is available it falls back
// to a native C build so development can continue; the boolean reports whether
// the final artifact is actually WebAssembly.
func compileWasm(ast *compiler.Program, cPath, output string, html bool) (bool, error) {
	ok, _, err := compileWasmWithTool(ast, cPath, output, html)
	return ok, err
}

func compileWasmWithTool(ast *compiler.Program, cPath, output string, html bool) (bool, string, error) {
	if !html {
		if wasmBytes, emitErr := compiler.EmitWasm(ast); emitErr == nil {
			if writeErr := os.WriteFile(output, wasmBytes, 0755); writeErr == nil {
				return true, "direct", nil
			}
		}
	}
	if err := os.WriteFile(cPath, []byte(compiler.NewGenerator(ast).Generate()), 0644); err != nil {
		return false, "", err
	}
	runtimeDir, err := ubert.EnsureRuntime()
	if err != nil {
		return false, "", fmt.Errorf("initialize runtime header: %w", err)
	}
	_ = os.Remove(output)

	if html {
		emcc, err := exec.LookPath("emcc")
		if err != nil {
			return false, "", fmt.Errorf("emcc (Emscripten) is required to build HTML canvas outputs")
		}
		cmdArgs := []string{"-O2", "-I" + runtimeDir, cPath, "-o", output, "-sWASM=1", "-sEXIT_RUNTIME=1"}
		if out, e := exec.Command(emcc, cmdArgs...).CombinedOutput(); e != nil {
			return false, "", fmt.Errorf("HTML Wasm build failed:\n%s", strings.TrimSpace(string(out)))
		}
		return true, "emcc", nil
	}

	// 1. clang can drive wasm-ld directly when a WASI sysroot is installed.
	if clang, err := exec.LookPath("clang"); err == nil {
		for _, target := range []string{"wasm32-wasi", "wasm32-wasip1"} {
			cmdArgs := []string{"--target=" + target, "-O2", "-I" + runtimeDir, cPath, "-o", output, "-lm"}
			if out, e := exec.Command(clang, cmdArgs...).CombinedOutput(); e == nil {
				if fileNonEmpty(output) {
					return true, "clang/wasm32-wasi", nil
				}
			} else if strings.Contains(string(out), "unknown target") {
				continue
			}
		}
	}

	// 2. zig cc provides a self-contained wasm32-wasi toolchain.
	if zig, err := exec.LookPath("zig"); err == nil {
		cmdArgs := []string{"cc", "-target", "wasm32-wasi", "-O2", "-I" + runtimeDir, cPath, "-o", output, "-lm"}
		if _, e := exec.Command(zig, cmdArgs...).CombinedOutput(); e == nil {
			if fileNonEmpty(output) {
				return true, "zig/wasm32-wasi", nil
			}
		}
	}

	// 3. emcc can emit a standalone Wasm module as well.
	if emcc, err := exec.LookPath("emcc"); err == nil {
		cmdArgs := []string{"-O2", "-I" + runtimeDir, cPath, "-o", output, "-sSTANDALONE_WASM=1", "-sEXIT_RUNTIME=1"}
		if _, e := exec.Command(emcc, cmdArgs...).CombinedOutput(); e == nil {
			if fileNonEmpty(output) {
				return true, "emcc", nil
			}
		}
	}

	// 4. Native C fallback keeps `ube build` and `ube run` usable without a
	// Wasm SDK. The artifact is a normal host executable.
	if err := compileNative(ast, cPath, output, true, false); err != nil {
		return false, "", err
	}
	return false, "", nil
}

func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// compileNative retains the original native pipeline for the test runner and
// for direct callers that need a host executable.
func compileNative(ast *compiler.Program, cPath, output string, optimize bool, wasm bool) error {
	if wasm {
		_, err := compileWasm(ast, cPath, output, false)
		return err
	}
	if err := os.WriteFile(cPath, []byte(compiler.NewGenerator(ast).Generate()), 0644); err != nil {
		return err
	}
	runtimeDir, err := ubert.EnsureRuntime()
	if err != nil {
		return fmt.Errorf("initialize runtime header: %w", err)
	}

	cc := ""
	for _, candidate := range []string{"clang", "gcc", "cc"} {
		if path, e := exec.LookPath(candidate); e == nil {
			cc = path
			break
		}
	}
	if cc == "" {
		return fmt.Errorf("no C or Wasm compiler found (install clang, emcc, zig, or gcc)")
	}

	cmdArgs := []string{"-std=gnu11", "-I" + runtimeDir, cPath, "-o", output, "-lm"}
	if optimize {
		cmdArgs = append(cmdArgs, "-O2")
	}
	if out, e := exec.Command(cc, cmdArgs...).CombinedOutput(); e != nil {
		return fmt.Errorf("native compilation failed:\n%s", strings.TrimSpace(string(out)))
	}
	return nil
}
