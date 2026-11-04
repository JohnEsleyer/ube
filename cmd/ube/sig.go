package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

// runSig outputs high-level symbol declarations across the project without function bodies.
func runSig(args []string) error {
	var (
		target = "."
	)

	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			target = arg
		}
	}

	files, err := discoverUbeFiles(target)
	if err != nil {
		return fmt.Errorf("discovering files in %s: %w", target, err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no .ube files found in %s", target)
	}

	var sb strings.Builder
	sb.WriteString("// Ube Project Symbol Signatures\n\n")

	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		lexer := compiler.NewLexer(file, string(src))
		tokens, err := lexer.Tokenize()
		if err != nil {
			continue
		}
		parser := compiler.NewParser(tokens)
		prog, err := parser.ParseProgram()
		if err != nil {
			continue
		}

		var fileSb strings.Builder
		for _, decl := range prog.Decls {
			switch d := decl.(type) {
			case *compiler.StructDecl:
				fileSb.WriteString(formatStructSign(d) + "\n")
			case *compiler.EnumDecl:
				fileSb.WriteString(formatEnumSign(d) + "\n")
			case *compiler.TraitDecl:
				fileSb.WriteString(formatTraitSign(d) + "\n")
			case *compiler.FnDecl:
				fileSb.WriteString(formatFnSign(d) + "\n")
			case *compiler.ExternFnDecl:
				fileSb.WriteString(formatExternFnSign(d) + "\n")
			}
		}

		if fileSb.Len() > 0 {
			sb.WriteString(fmt.Sprintf("// --- %s ---\n", file))
			sb.WriteString(fileSb.String())
			sb.WriteString("\n")
		}
	}

	res := strings.TrimSpace(sb.String())
	fmt.Println(res)

	return nil
}
