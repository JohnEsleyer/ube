package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/JohnEsleyer/ube/pkg/compiler"
	"github.com/bytecodealliance/wasmtime-go/v29"
)

// compileWasm compiles the given Program directly into WebAssembly bytecode.
func compileWasm(prog *compiler.Program, file, testName string, wantC bool) (bool, string) {
	wasmBytes, err := compiler.EmitWasm(prog)
	if err != nil {
		return false, ""
	}
	out := filepath.Join(os.TempDir(), "ube-test-"+testName+".wasm")
	if werr := os.WriteFile(out, wasmBytes, 0755); werr != nil {
		return false, ""
	}
	return true, out
}

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
	store.SetWasi(wasiCfg)
	instance, err := linker.Instantiate(store, module)
	if err != nil {
		return err
	}
	if start := instance.GetFunc(store, "_start"); start != nil {
		_, err = start.Call(store)
		return err
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
	store.SetWasi(wasiCfg)
	instance, err := linker.Instantiate(store, module)
	if err != nil {
		return "", err
	}
	var callErr error
	if start := instance.GetFunc(store, "_start"); start != nil {
		_, callErr = start.Call(store)
	}
	out, readErr := os.ReadFile(outFile)
	if readErr != nil {
		return "", readErr
	}
	return string(out), callErr
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
