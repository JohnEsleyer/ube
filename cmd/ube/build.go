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
	file, output, emitC, html, forbidUnsafe, err := parseBuildArgs(args)
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
	if forbidUnsafe && unsafePresent(report) {
		return fmt.Errorf("build rejected by --forbid-unsafe: raw unsafe escape hatches are present")
	}

	cPath := filepath.Join(os.TempDir(), fmt.Sprintf("ube-%d.c", time.Now().UnixNano()))
	if emitC {
		cPath = strings.TrimSuffix(output, filepath.Ext(output)) + ".c"
	} else {
		defer os.Remove(cPath)
	}

	wasmOutput, err := compileWasm(ast, cPath, output, html)
	if err != nil {
		return err
	}
	label := "✓ Built Wasm module"
	switch {
	case html:
		label = "✓ Built WebAssembly web app"
	case !wasmOutput:
		label = "✓ Built native fallback"
	}
	fmt.Printf("%s %s (%s)\n", paint(label, colorGreen), output, paint(time.Since(start).Round(time.Millisecond).String(), colorDim))
	printPurity(report)
	return nil
}

func parseBuildArgs(args []string) (string, string, bool, bool, bool, error) {
	if len(args) == 0 {
		return "", "", false, false, false, fmt.Errorf("usage: ube build <file.ube> [-o app.wasm] [--html] [--emit-c] [--forbid-unsafe]")
	}
	file, output, emitC, html, forbidUnsafe := "", "", false, false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 >= len(args) {
				return "", "", false, false, false, fmt.Errorf("-o requires an output path")
			}
			output = args[i+1]
			i++
		case "--emit-c":
			emitC = true
		case "--forbid-unsafe":
			forbidUnsafe = true
		case "--html":
			html = true
		case "--wasm":
			// Wasm is the default target; accepted as an explicit alias.
		case "--freestanding":
			// Kept as a compatibility no-op: the runtime is always freestanding.
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", false, false, false, fmt.Errorf("unknown build option %s", args[i])
			}
			if file != "" {
				return "", "", false, false, false, fmt.Errorf("multiple source files are not supported")
			}
			file = args[i]
		}
	}
	if file == "" {
		return "", "", false, false, false, fmt.Errorf("specify a .ube source file")
	}
	return file, output, emitC, html, forbidUnsafe, nil
}

// compileWasm emits a WebAssembly module by default. HTML output is delegated to
// Emscripten. When no Wasm toolchain (or WASI sysroot) is available it falls back
// to a native C build so development can continue; the boolean reports whether
// the final artifact is actually WebAssembly.
func compileWasm(ast *compiler.Program, cPath, output string, html bool) (bool, error) {
	if err := os.WriteFile(cPath, []byte(compiler.NewGenerator(ast).Generate()), 0644); err != nil {
		return false, err
	}
	runtimeDir, err := ubert.EnsureRuntime()
	if err != nil {
		return false, fmt.Errorf("initialize runtime header: %w", err)
	}
	_ = os.Remove(output)

	if html {
		emcc, err := exec.LookPath("emcc")
		if err != nil {
			return false, fmt.Errorf("emcc (Emscripten) is required to build HTML canvas outputs")
		}
		cmdArgs := []string{"-O2", "-I" + runtimeDir, cPath, "-o", output, "-sWASM=1", "-sEXIT_RUNTIME=1"}
		if out, e := exec.Command(emcc, cmdArgs...).CombinedOutput(); e != nil {
			return false, fmt.Errorf("HTML Wasm build failed:\n%s", strings.TrimSpace(string(out)))
		}
		return true, nil
	}

	// 1. clang can drive wasm-ld directly when a WASI sysroot is installed.
	if clang, err := exec.LookPath("clang"); err == nil {
		for _, target := range []string{"wasm32-wasi", "wasm32-wasip1"} {
			cmdArgs := []string{"--target=" + target, "-O2", "-I" + runtimeDir, cPath, "-o", output, "-lm"}
			if out, e := exec.Command(clang, cmdArgs...).CombinedOutput(); e == nil {
				if fileNonEmpty(output) {
					return true, nil
				}
			} else if strings.Contains(string(out), "unknown target") {
				continue
			}
		}
	}

	// 2. zig cc provides a self-contained wasm32-wasi toolchain.
	if zig, err := exec.LookPath("zig"); err == nil {
		cmdArgs := []string{"cc", "-target", "wasm32-wasi", "-O2", "-I" + runtimeDir, cPath, "-o", output, "-lm"}
		if out, e := exec.Command(zig, cmdArgs...).CombinedOutput(); e == nil {
			if fileNonEmpty(output) {
				return true, nil
			}
		} else {
			_ = out
		}
	}

	// 3. emcc can emit a standalone Wasm module as well.
	if emcc, err := exec.LookPath("emcc"); err == nil {
		cmdArgs := []string{"-O2", "-I" + runtimeDir, cPath, "-o", output, "-sSTANDALONE_WASM=1", "-sEXIT_RUNTIME=1"}
		if out, e := exec.Command(emcc, cmdArgs...).CombinedOutput(); e == nil {
			if fileNonEmpty(output) {
				return true, nil
			}
		} else {
			_ = out
		}
	}

	// 4. Native C fallback keeps `ube build` and `ube run` usable without a
	// Wasm SDK. The artifact is a normal host executable.
	if err := compileNative(ast, cPath, output, true, false, false); err != nil {
		return false, err
	}
	return false, nil
}

func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// compileNative retains the original native pipeline for the test runner and
// for direct callers that need a host executable.
func compileNative(ast *compiler.Program, cPath, output string, optimize bool, freestanding bool, wasm bool) error {
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
	_ = freestanding // The runtime is always freestanding/Wasm-portable now.

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
