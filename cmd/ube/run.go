package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/JohnEsleyer/ube/pkg/compiler"
	"github.com/bytecodealliance/wasmtime-go/v29"
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
	cPath := base + ".c"
	wasmPath := base + ".wasm"
	defer os.Remove(cPath)
	defer os.Remove(wasmPath)
	defer os.Remove(base)

	var wasmOutput bool
	// Try direct in-memory Wasm bytecode emission first (sub-millisecond compile)
	if wasmBytes, emitErr := compiler.EmitWasm(ast); emitErr == nil {
		if writeErr := os.WriteFile(wasmPath, wasmBytes, 0755); writeErr == nil {
			wasmOutput = true
		}
	}

	// Fallback to C compilation pipeline if direct wasm hit unsupported constructs
	if !wasmOutput {
		var err error
		wasmOutput, err = compileWasm(ast, cPath, wasmPath, false)
		if err != nil {
			return err
		}
	}

	label := "✓ Compiled Wasm"
	if wasmOutput {
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
			// No external Wasm runtime found; compile native as last resort.
			if e := compileNative(ast, cPath, base, false, false); e != nil {
				return runErr
			}
			cmd := exec.Command(base, args[1:]...)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			if execErr := cmd.Run(); execErr != nil {
				if e, ok := execErr.(*exec.ExitError); ok {
					return exitCodeError{e.ExitCode()}
				}
				return execErr
			}
		}
		return nil
	}

	// No Wasm toolchain available – build produced a native executable at wasmPath.
	label = "✓ Compiled native fallback"
	fmt.Printf("%s %s (%s)\n", paint(label, colorPurple), filepath.Base(args[0]), paint(time.Since(start).Round(time.Millisecond).String(), colorDim))
	printPurity(report)
	cmd := exec.Command(wasmPath, args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err = cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return exitCodeError{e.ExitCode()}
		}
		return err
	}
	return nil
}

// runWasmtimeInProcess executes the WebAssembly module directly within the ube
// process using Wasmtime's embedded Cranelift JIT engine. WASI stdio, environment
// variables, and the current working directory are inherited from the host process.
func runWasmtimeInProcess(wasmPath string, programArgs []string) error {
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return fmt.Errorf("failed to read wasm module: %w", err)
	}

	engine := wasmtime.NewEngine()
	store := wasmtime.NewStore(engine)

	wasiConfig := wasmtime.NewWasiConfig()
	wasiConfig.InheritStdout()
	wasiConfig.InheritStderr()
	wasiConfig.InheritStdin()
	wasiConfig.InheritEnv()

	// argv[0] is conventionally the module path.
	argv := append([]string{wasmPath}, programArgs...)
	wasiConfig.SetArgv(argv)

	// Pre-open the current working directory so WASI file I/O works.
	if err := wasiConfig.PreopenDir(".", ".", wasmtime.DIR_READ|wasmtime.DIR_WRITE, wasmtime.FILE_READ|wasmtime.FILE_WRITE); err != nil {
		// Non-fatal: the program may not need filesystem access.
		_ = err
	}

	store.SetWasi(wasiConfig)

	linker := wasmtime.NewLinker(engine)
	if err := linker.DefineWasi(); err != nil {
		return fmt.Errorf("failed to link WASI: %w", err)
	}

	module, err := wasmtime.NewModule(engine, wasmBytes)
	if err != nil {
		return fmt.Errorf("failed to compile wasm module: %w", err)
	}

	instance, err := linker.Instantiate(store, module)
	if err != nil {
		return fmt.Errorf("failed to instantiate wasm module: %w", err)
	}

	entry := instance.GetFunc(store, "_start")
	if entry == nil {
		return errors.New("wasm module does not export entrypoint '_start'")
	}

	_, err = entry.Call(store)
	if err != nil {
		var wasmErr *wasmtime.Error
		if errors.As(err, &wasmErr) {
			if code, ok := wasmErr.ExitStatus(); ok {
				if code == 0 {
					return nil
				}
				return exitCodeError{code: int(code)}
			}
		}
		return fmt.Errorf("wasm execution error: %w", err)
	}
	return nil
}

func runWasmtimeCapture(wasmPath string, programArgs []string) (string, error) {
	engine := wasmtime.NewEngine()
	store := wasmtime.NewStore(engine)

	outPath := filepath.Join(os.TempDir(), fmt.Sprintf("ube-wasm-out-%d.tmp", time.Now().UnixNano()))
	defer os.Remove(outPath)
	if err := os.WriteFile(outPath, nil, 0644); err != nil {
		return "", err
	}

	wasiConfig := wasmtime.NewWasiConfig()
	wasiConfig.InheritEnv()
	if err := wasiConfig.SetStdoutFile(outPath); err != nil {
		return "", err
	}
	if err := wasiConfig.SetStderrFile(outPath); err != nil {
		return "", err
	}
	wasiConfig.SetArgv(append([]string{filepath.Base(wasmPath)}, programArgs...))
	store.SetWasi(wasiConfig)

	linker := wasmtime.NewLinker(engine)
	if err := linker.DefineWasi(); err != nil {
		return "", fmt.Errorf("wasmtime define wasi: %w", err)
	}

	module, err := wasmtime.NewModuleFromFile(engine, wasmPath)
	if err != nil {
		return "", fmt.Errorf("wasmtime compile module: %w", err)
	}

	instance, err := linker.Instantiate(store, module)
	if err != nil {
		return "", fmt.Errorf("wasmtime instantiate: %w", err)
	}

	entry := instance.GetFunc(store, "_start")
	if entry == nil {
		return "", errors.New("wasm module does not export entrypoint '_start'")
	}

	_, callErr := entry.Call(store)
	outBytes, _ := os.ReadFile(outPath)
	outStr := string(outBytes)

	if callErr != nil {
		var wasmErr *wasmtime.Error
		if errors.As(callErr, &wasmErr) {
			if code, ok := wasmErr.ExitStatus(); ok {
				if code == 0 {
					return outStr, nil
				}
				return outStr, exitCodeError{code: int(code)}
			}
		}
		return outStr, fmt.Errorf("wasm execution error: %w", callErr)
	}
	return outStr, nil
}

func runWasmCapture(wasmPath string, programArgs []string) (string, error) {
	if out, err := runWasmtimeCapture(wasmPath, programArgs); err == nil {
		return out, nil
	} else if out != "" {
		return out, err
	}
	if cmd, cmdErr := wasmRuntimeCommand(wasmPath, programArgs); cmdErr == nil {
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := exec.Command(wasmPath, programArgs...).CombinedOutput()
	return string(out), err
}

// wasmRuntimeCommand returns an exec.Cmd that runs wasmPath using an external
// Wasm runtime found on PATH. It is kept as a fallback in case the in-process
// Wasmtime runner encounters an error.
func wasmRuntimeCommand(path string, args []string) (*exec.Cmd, error) {
	if wasmer, err := exec.LookPath("wasmer"); err == nil {
		return exec.Command(wasmer, append([]string{"run", path, "--"}, args...)...), nil
	}
	if node, err := exec.LookPath("node"); err == nil {
		const nodeWASIShims = `
(async () => {
  const { WASI } = require('node:wasi');
  const fs = require('node:fs');
  const wasi = new WASI({
    version: 'preview1',
    args: process.argv.slice(1),
    env: process.env,
    preopens: { '/': '/' }
  });
  const module = await WebAssembly.compile(fs.readFileSync(process.argv[1]));
  const instance = await WebAssembly.instantiate(module, wasi.getImportObject());
  const code = wasi.start(instance);
  process.exit(typeof code === 'number' ? code : 0);
})().catch((err) => { console.error(err); process.exit(1); });
`
		return exec.Command(node, append([]string{"-e", nodeWASIShims, path}, args...)...), nil
	}
	if deno, err := exec.LookPath("deno"); err == nil {
		return exec.Command(deno, append([]string{"run", "--allow-read", "--allow-env", "--allow-sys", path}, args...)...), nil
	}
	if wasm3, err := exec.LookPath("wasm3"); err == nil {
		return exec.Command(wasm3, append([]string{path}, args...)...), nil
	}
	return nil, fmt.Errorf("no external WebAssembly runtime found (wasmer, node, deno, wasm3)")
}
