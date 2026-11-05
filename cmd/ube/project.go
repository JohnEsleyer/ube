// File: cmd/ube/project.go
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
	source := "package main\n\nfn main() {\n    println(\"Hello from Ube!\")\n}\n"
	if err := os.WriteFile(filepath.Join(name, "main.ube"), []byte(source), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(name, ".gitignore"), []byte("/main\n*.wasm\n"), 0644); err != nil {
		return err
	}
	fmt.Printf("✓ Created %s\n  cd %s && ube run .\n", name, name)
	return nil
}

func initProject() error {
	if _, err := os.Stat("main.ube"); err == nil {
		return fmt.Errorf("main.ube already exists in current directory")
	}
	source := "package main\n\nfn main() {\n    println(\"Hello from Ube!\")\n}\n"
	if err := os.WriteFile("main.ube", []byte(source), 0644); err != nil {
		return err
	}
	if _, err := os.Stat(".gitignore"); os.IsNotExist(err) {
		_ = os.WriteFile(".gitignore", []byte("/main\n*.wasm\n"), 0644)
	}
	fmt.Printf("✓ Initialized project in current directory\n  ube run .\n")
	return nil
}

func help() {
	fmt.Printf("🍠 Ube %s — The sweet, grounded language for games, apps, and backends.\n\nUsage: ube <command> [options]\n       ube [dir|file.ube]\n\nCommands:\n  new <name>                 Create a starter project\n  check [dir|file]           Check syntax, types, and affine ownership\n  build [dir|file] [-o app]  Build a Wasm module (.wasm)\n  run [dir|file] [args...]   Compile to Wasm and run it\n  test [dir|file|./...]      Discover and run package tests\n  fmt [dir|file]             Format Ube source files (--check or -w)\n  doc [file.ube] [sym]       Extract declarations and signatures\n  flow <file.ube>            Visual execution call-tree from main()\n  sig [dir]                  Extracts project symbol signatures (zero body)\n\n  For LLM context extraction and atomic patching, use Textify:\n    https://github.com/JohnEsleyer/textify\n\n  lsp                       Start the language server\n  version                   Show the version\n  help                      Show this help\n\nBuild options:\n  -o <path>                 Set output path (.wasm by default)\n  --wasm                    Build portable WebAssembly/WASI output (default)\n", version)
}
