// File: cmd/ube/run_helpers.go
package main

import (
	"errors"
	"os"
	"os/exec"

	"github.com/bytecodealliance/wasmtime-go/v29"
)

// runWasmCapture executes a Wasm file in-process and returns captured stdout
// plus any execution error.
func runWasmCapture(wasmPath string, args []string) (string, error) {
	return runWasmtimeCapture(wasmPath, args)
}

// runWasmtimeInProcess executes the given wasm file in-process using the
// embedded Wasmtime Cranelift JIT engine.
func runWasmtimeInProcess(wasmPath string, args []string) error {
	engine := wasmtime.NewEngine()
	module, err := wasmtime.NewModuleFromFile(engine, wasmPath)
	if err != nil {
		return err
	}
	store := wasmtime.NewStore(engine)
	linker := wasmtime.NewLinker(engine)
	if err := linker.DefineWasi(); err != nil {
		return err
	}
	wasiCfg := wasmtime.NewWasiConfig()
	wasiCfg.SetArgv(append([]string{wasmPath}, args...))
	wasiCfg.InheritStdin()
	wasiCfg.InheritStdout()
	wasiCfg.InheritStderr()
	if err := wasiCfg.PreopenDir(".", ".", wasmtime.DIR_READ|wasmtime.DIR_WRITE, wasmtime.FILE_READ|wasmtime.FILE_WRITE); err != nil {
		return err
	}
	store.SetWasi(wasiCfg)
	instance, err := linker.Instantiate(store, module)
	if err != nil {
		return err
	}
	if start := instance.GetFunc(store, "_start"); start != nil {
		_, err = start.Call(store)
		if err != nil {
			if code, ok := wasiExitStatus(err); ok {
				if code == 0 {
					return nil
				}
				return exitCodeError{code}
			}
			return err
		}
		return nil
	}
	return nil
}

// runWasmtimeCapture executes the wasm file in-process and returns its
// captured stdout plus any execution error.
func runWasmtimeCapture(wasmPath string, args []string) (string, error) {
	engine := wasmtime.NewEngine()
	module, err := wasmtime.NewModuleFromFile(engine, wasmPath)
	if err != nil {
		return "", err
	}
	store := wasmtime.NewStore(engine)
	linker := wasmtime.NewLinker(engine)
	if err := linker.DefineWasi(); err != nil {
		return "", err
	}
	outFile := wasmPath + ".out"
	errFile := wasmPath + ".err"
	defer os.Remove(outFile)
	defer os.Remove(errFile)

	wasiCfg := wasmtime.NewWasiConfig()
	wasiCfg.SetArgv(append([]string{wasmPath}, args...))
	wasiCfg.SetStdoutFile(outFile)
	wasiCfg.SetStderrFile(errFile)
	if err := wasiCfg.PreopenDir(".", ".", wasmtime.DIR_READ|wasmtime.DIR_WRITE, wasmtime.FILE_READ|wasmtime.FILE_WRITE); err != nil {
		return "", err
	}
	store.SetWasi(wasiCfg)
	instance, err := linker.Instantiate(store, module)
	if err != nil {
		return "", err
	}
	var callErr error
	if start := instance.GetFunc(store, "_start"); start != nil {
		_, callErr = start.Call(store)
		// Leave non-zero WASI exit traps in callErr so the `ube test`
		// runner reports them per-test rather than aborting the whole
		// run on the first failing test. A zero exit status is a clean
		// return.
		if callErr != nil {
			if code, ok := wasiExitStatus(callErr); ok && code == 0 {
				callErr = nil
			}
		}
	}
	out, readErr := os.ReadFile(outFile)
	if readErr != nil {
		return "", readErr
	}
	errBytes, _ := os.ReadFile(errFile)
	if len(errBytes) > 0 {
		if len(out) > 0 {
			out = append(out, errBytes...)
		} else {
			out = errBytes
		}
	}
	return string(out), callErr
}

// wasiExitStatus extracts the WASI exit code from a Wasmtime trap, if the
// running Wasmtime binding exposes it. In wasmtime-go/v29 this is
// Trap.ExitStatus(), which returns (int32, bool).
func wasiExitStatus(err error) (int, bool) {
	if e, ok := err.(*wasmtime.Error); ok {
		if status, ok := e.ExitStatus(); ok {
			return int(status), true
		}
	}
	return 0, false
}

// wasmRuntimeCommand returns an external Wasm runtime command if one is
// available on the host.
func wasmRuntimeCommand(wasmPath string, args []string) (*exec.Cmd, error) {
	for _, runtime := range []string{"wasmtime", "wasmer"} {
		if path, err := exec.LookPath(runtime); err == nil {
			return exec.Command(path, append([]string{wasmPath}, args...)...), nil
		}
	}
	return nil, errors.New("no external Wasm runtime found")
}
