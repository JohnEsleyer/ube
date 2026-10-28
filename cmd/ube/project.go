package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func newProject(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: ube new <project-name>")
	}
	name := args[0]
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("%s already exists", name)
	}
	if err := os.MkdirAll(name, 0755); err != nil {
		return err
	}
	source := "fn main() {\n    println(\"Hello from Ube!\")\n}\n"
	if err := os.WriteFile(filepath.Join(name, "main.ube"), []byte(source), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(name, ".gitignore"), []byte("/main\n*.c\n"), 0644); err != nil {
		return err
	}
	fmt.Printf("✓ Created %s\n  cd %s && ube run main.ube\n", name, name)
	return nil
}

func help() {
	fmt.Printf("🍠 Ube %s\n\nUsage: ube <command> [options]\n       ube <file.ube>\n\nCommands:\n  new <name>                Create a starter project\n  check <file.ube>          Check syntax and types\n  build <file.ube> [-o bin] Build a native executable\n  run <file.ube> [args...]  Compile and run a program\n  lsp                       Start the language server\n  version                   Show the version\n  help                      Show this help\n\nCheck options:\n  --strict                  Reject C FFI and unsafe blocks\n  --format=json             Emit a machine-readable audit report\n\nBuild options:\n  -o <path>                 Set executable output path\n  --emit-c                  Keep generated C beside the output\n  --forbid-unsafe           Reject C FFI and unsafe blocks\n  --freestanding            Build without pthread runtime support\n  --wasm                    Build HTML/WebAssembly with Emscripten\n", version)
}
