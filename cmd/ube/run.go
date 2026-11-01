package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

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

	wasmOutput, err := compileWasm(ast, cPath, wasmPath, false)
	if err != nil {
		return err
	}

	label := "✓ Compiled Wasm"
	var cmd *exec.Cmd
	if wasmOutput {
		cmd, err = wasmRuntimeCommand(wasmPath, args[1:])
		if err != nil {
			// A Wasm toolchain produced a module, but no compatible Wasm runtime
			// is installed. Fall back to native so `ube run` still works locally.
			if e := compileNative(ast, cPath, base, false, false, false); e != nil {
				return err
			}
			label = "✓ Compiled native fallback"
			cmd = exec.Command(base, args[1:]...)
		}
	} else {
		label = "✓ Compiled native fallback"
		cmd = exec.Command(wasmPath, args[1:]...)
	}

	fmt.Printf("%s %s (%s)\n", paint(label, colorPurple), filepath.Base(args[0]), paint(time.Since(start).Round(time.Millisecond).String(), colorDim))
	printPurity(report)

	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err = cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return exitCodeError{e.ExitCode()}
		}
		return err
	}
	return nil
}

func wasmRuntimeCommand(path string, args []string) (*exec.Cmd, error) {
	if wasmtime, err := exec.LookPath("wasmtime"); err == nil {
		return exec.Command(wasmtime, append([]string{path}, args...)...), nil
	}
	if wasmer, err := exec.LookPath("wasmer"); err == nil {
		return exec.Command(wasmer, append([]string{"run", path, "--"}, args...)...), nil
	}
	if node, err := exec.LookPath("node"); err == nil {
		return exec.Command(node, append([]string{"-e", nodeWASIShims, path}, args...)...), nil
	}
	if deno, err := exec.LookPath("deno"); err == nil {
		return exec.Command(deno, append([]string{"run", "--allow-read", "--allow-env", "--allow-sys", path}, args...)...), nil
	}
	if wasm3, err := exec.LookPath("wasm3"); err == nil {
		return exec.Command(wasm3, append([]string{path}, args...)...), nil
	}
	return nil, fmt.Errorf("a WebAssembly runtime is required to run %s (install wasmtime, wasmer, node, deno, or wasm3)", strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
}
