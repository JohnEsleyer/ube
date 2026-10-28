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
	file, output, emitC, forbidUnsafe, freestanding, wasm, err := parseBuildArgs(args)
	if err != nil {
		return err
	}
	if output == "" {
		output = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	}
	start := time.Now()
	ast, report, err := loadAndCheck(file)
	if err != nil {
		return err
	}
	if forbidUnsafe && unsafePresent(report) {
		return fmt.Errorf("build rejected by --forbid-unsafe: C FFI or unsafe blocks are present")
	}
	cPath := filepath.Join(os.TempDir(), fmt.Sprintf("ube-%d.c", time.Now().UnixNano()))
	if emitC {
		cPath = output + ".c"
	} else {
		defer os.Remove(cPath)
	}
	if wasm && !strings.HasSuffix(output, ".html") {
		output += ".html"
	}
	if err = compileNative(ast, cPath, output, true, freestanding, wasm); err != nil {
		return err
	}
	fmt.Printf("%s %s (%s)\n", paint("✓ Built", colorGreen), output, paint(time.Since(start).Round(time.Millisecond).String(), colorDim))
	printPurity(report)
	return nil
}

func parseBuildArgs(args []string) (string, string, bool, bool, bool, bool, error) {
	if len(args) == 0 {
		return "", "", false, false, false, false, fmt.Errorf("usage: ube build <file.ube> [-o binary] [--emit-c] [--forbid-unsafe] [--freestanding] [--wasm]")
	}
	file, output, emitC, forbidUnsafe, freestanding, wasm := "", "", false, false, false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 >= len(args) {
				return "", "", false, false, false, false, fmt.Errorf("-o requires an output path")
			}
			output = args[i+1]
			i++
		case "--emit-c":
			emitC = true
		case "--forbid-unsafe":
			forbidUnsafe = true
		case "--freestanding":
			freestanding = true
		case "--wasm":
			wasm = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", false, false, false, false, fmt.Errorf("unknown build option %s", args[i])
			}
			if file != "" {
				return "", "", false, false, false, false, fmt.Errorf("multiple source files are not supported")
			}
			file = args[i]
		}
	}
	if file == "" {
		return "", "", false, false, false, false, fmt.Errorf("specify a .ube source file")
	}
	if freestanding && wasm {
		return "", "", false, false, false, false, fmt.Errorf("choose either --freestanding or --wasm")
	}
	return file, output, emitC, forbidUnsafe, freestanding, wasm, nil
}

func compileNative(ast *compiler.Program, cPath, output string, optimize bool, freestanding bool, wasm bool) error {
	if err := os.WriteFile(cPath, []byte(compiler.NewGenerator(ast).Generate()), 0644); err != nil {
		return err
	}
	runtimeDir, err := ubert.EnsureRuntime()
	if err != nil {
		return fmt.Errorf("initialize runtime header: %w", err)
	}
	if wasm {
		cc, e := exec.LookPath("emcc")
		if e != nil {
			return fmt.Errorf("emcc (Emscripten) is required for --wasm")
		}
		args := []string{"-O2", "-DUBE_FREESTANDING", "-I" + runtimeDir, cPath, "-o", output, "-sWASM=1"}
		if out, e := exec.Command(cc, args...).CombinedOutput(); e != nil {
			return fmt.Errorf("wasm compilation failed:\n%s", strings.TrimSpace(string(out)))
		}
		return nil
	}
	cc := ""
	for _, candidate := range []string{"cc", "gcc", "clang"} {
		if path, e := exec.LookPath(candidate); e == nil {
			cc = path
			break
		}
	}
	if cc == "" {
		return fmt.Errorf("no C compiler found (install GCC or Clang)")
	}
	args := []string{"-std=gnu11", "-I" + runtimeDir, cPath, "-o", output, "-lm"}
	if freestanding {
		args = append(args, "-DUBE_FREESTANDING")
	} else {
		args = append(args, "-pthread")
	}
	if optimize {
		args = append(args, "-O2")
	}
	if out, e := exec.Command(cc, args...).CombinedOutput(); e != nil {
		return fmt.Errorf("native compilation failed:\n%s", strings.TrimSpace(string(out)))
	}
	return nil
}
