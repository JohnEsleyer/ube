package main

import (
	"fmt"
	"path/filepath"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func runFlow(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ube flow <file.ube>")
	}
	file := args[0]
	ast, _, err := loadAndCheck(file)
	if err != nil {
		return err
	}

	fnMap := make(map[string]*compiler.FnDecl)
	for _, decl := range ast.Decls {
		if fn, ok := decl.(*compiler.FnDecl); ok {
			key := fn.Name
			if fn.Receiver != nil {
				key = fmt.Sprintf("%s.%s", fn.Receiver.Type, fn.Name)
			}
			fnMap[key] = fn
		}
	}

	mainFn, hasMain := fnMap["main"]
	if !hasMain {
		return fmt.Errorf("no 'fn main()' entry point found in %s", filepath.Base(file))
	}

	fmt.Printf("%s Execution Flow Tree for %s\n", paint("🍠", colorPurple), filepath.Base(file))
	fmt.Println("main() [entry]")

	visited := make(map[string]bool)
	printFnFlow(mainFn, fnMap, " ", visited)
	return nil
}

func printFnFlow(fn *compiler.FnDecl, fnMap map[string]*compiler.FnDecl, prefix string, visited map[string]bool) {
	calls, flow := analyzeFnFlow(fn)

	for _, fl := range flow {
		switch fl {
		case "spawn":
			fmt.Printf("%s├── [async] spawn task\n", prefix)
		case "arena":
			fmt.Printf("%s├── [memory] arena { ... }\n", prefix)
		case "select":
			fmt.Printf("%s├── [branch] select\n", prefix)
		case "match":
			fmt.Printf("%s├── [branch] match\n", prefix)
		}
	}

	for i, c := range calls {
		isLast := i == len(calls)-1
		connector := "├── "
		if isLast {
			connector = "└── "
		}

		if subFn, exists := fnMap[c]; exists {
			fmt.Printf("%s%s%s()\n", prefix, connector, c)
			if !visited[c] {
				visited[c] = true
				nextPrefix := prefix + "│   "
				if isLast {
					nextPrefix = prefix + "    "
				}
				printFnFlow(subFn, fnMap, nextPrefix, visited)
			}
		} else {
			fmt.Printf("%s%s%s() [builtin/extern]\n", prefix, connector, c)
		}
	}
}
