package main

import (
	"fmt"
	"os"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: ubec <source.ube> [-o output.c]")
		os.Exit(1)
	}

	srcFile := os.Args[1]
	outPath := "output.c"
	for i := 2; i < len(os.Args)-1; i++ {
		if os.Args[i] == "-o" {
			outPath = os.Args[i+1]
		}
	}

	loader := compiler.NewModuleLoader()
	ast, err := loader.Load(srcFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ubec] Compile Error: %v\n", err)
		os.Exit(1)
	}

	// 3. Static Type Checking
	typechecker := compiler.NewTypechecker()
	if err := typechecker.Check(ast); err != nil {
		fmt.Fprintf(os.Stderr, "[ubec] Type Error: %v\n", err)
		os.Exit(1)
	}

	// 4. Code Generation
	generator := compiler.NewGenerator(ast)
	cSource := generator.Generate()

	if err := os.WriteFile(outPath, []byte(cSource), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", outPath, err)
		os.Exit(1)
	}

	fmt.Printf("[ubec] Successfully compiled %s -> %s\n", srcFile, outPath)
}
