package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func runDoc(args []string) error {
	if len(args) == 0 {
		if _, err := os.Stat("main.ube"); err == nil {
			args = []string{"main.ube"}
		} else {
			fmt.Println("Usage: ube doc <file.ube> [symbol]")
			fmt.Println("\nExtract and inspect declarations, signatures, and types from an Ube source file.")
			return nil
		}
	}

	targetFile := args[0]
	targetSymbol := ""
	if len(args) > 1 {
		targetSymbol = args[1]
	}

	src, err := os.ReadFile(targetFile)
	if err != nil {
		return fmt.Errorf("reading file %s: %w", targetFile, err)
	}

	lexer := compiler.NewLexer(targetFile, string(src))
	tokens, err := lexer.Tokenize()
	if err != nil {
		return fmt.Errorf("lexing %s: %w", targetFile, err)
	}

	parser := compiler.NewParser(tokens)
	prog, err := parser.ParseProgram()
	if err != nil {
		return fmt.Errorf("parsing %s: %w", targetFile, err)
	}

	count := 0
	fmt.Printf("// Documentation for %s\n\n", targetFile)

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *compiler.StructDecl:
			if targetSymbol != "" && !strings.EqualFold(d.Name, targetSymbol) {
				continue
			}
			printStructDoc(d)
			count++

		case *compiler.EnumDecl:
			if targetSymbol != "" && !strings.EqualFold(d.Name, targetSymbol) {
				continue
			}
			printEnumDoc(d)
			count++

		case *compiler.TraitDecl:
			if targetSymbol != "" && !strings.EqualFold(d.Name, targetSymbol) {
				continue
			}
			printTraitDoc(d)
			count++

		case *compiler.FnDecl:
			if targetSymbol != "" && !strings.EqualFold(d.Name, targetSymbol) {
				continue
			}
			printFnDoc(d)
			count++

		case *compiler.ExternFnDecl:
			if targetSymbol != "" && !strings.EqualFold(d.Name, targetSymbol) {
				continue
			}
			printExternFnDoc(d)
			count++
		}
	}

	if targetSymbol != "" && count == 0 {
		fmt.Printf("Symbol '%s' not found in %s\n", targetSymbol, targetFile)
	}

	return nil
}

func printStructDoc(s *compiler.StructDecl) {
	typeParams := ""
	if len(s.TypeParams) > 0 {
		var parts []string
		for _, tp := range s.TypeParams {
			if tp.Constraint != "" {
				parts = append(parts, fmt.Sprintf("%s: %s", tp.Name, tp.Constraint))
			} else {
				parts = append(parts, tp.Name)
			}
		}
		typeParams = "[" + strings.Join(parts, ", ") + "]"
	}

	pubPrefix := ""
	if s.IsPub {
		pubPrefix = "pub "
	}
	fmt.Printf("%sstruct %s%s {\n", pubPrefix, s.Name, typeParams)
	for _, f := range s.Fields {
		fPub := ""
		if f.IsPub {
			fPub = "pub "
		}
		mutPrefix := ""
		if f.IsMut {
			mutPrefix = "mut "
		}
		fmt.Printf("    %s%s%s: %s\n", fPub, mutPrefix, f.Name, f.Type.String())
	}
	fmt.Printf("}\n\n")
}

func printEnumDoc(e *compiler.EnumDecl) {
	pubPrefix := ""
	if e.IsPub {
		pubPrefix = "pub "
	}
	fmt.Printf("%senum %s {\n", pubPrefix, e.Name)
	for _, v := range e.Variants {
		if len(v.Types) == 0 {
			fmt.Printf("    %s\n", v.Name)
		} else {
			var typeStrs []string
			for _, t := range v.Types {
				typeStrs = append(typeStrs, t.String())
			}
			fmt.Printf("    %s(%s)\n", v.Name, strings.Join(typeStrs, ", "))
		}
	}
	fmt.Printf("}\n\n")
}

func printTraitDoc(t *compiler.TraitDecl) {
	pubPrefix := ""
	if t.IsPub {
		pubPrefix = "pub "
	}
	fmt.Printf("%strait %s {\n", pubPrefix, t.Name)
	for _, m := range t.Methods {
		var params []string
		for _, p := range m.Params {
			params = append(params, fmt.Sprintf("%s: %s", p.Name, p.Type.String()))
		}
		ret := ""
		if m.RetType != nil {
			ret = fmt.Sprintf(" -> %s", m.RetType.String())
		}
		fmt.Printf("    fn %s(%s)%s\n", m.Name, strings.Join(params, ", "), ret)
	}
	fmt.Printf("}\n\n")
}

func printFnDoc(fn *compiler.FnDecl) {
	pubPrefix := ""
	if fn.IsPub {
		pubPrefix = "pub "
	}
	receiver := ""
	if fn.Receiver != nil {
		mutPrefix := ""
		if fn.Receiver.IsMut {
			mutPrefix = "mut "
		}
		receiver = fmt.Sprintf("(%s%s %s) ", mutPrefix, fn.Receiver.Name, fn.Receiver.Type.String())
	}

	typeParams := ""
	if len(fn.TypeParams) > 0 {
		var parts []string
		for _, tp := range fn.TypeParams {
			if tp.Constraint != "" {
				parts = append(parts, fmt.Sprintf("%s: %s", tp.Name, tp.Constraint))
			} else {
				parts = append(parts, tp.Name)
			}
		}
		typeParams = "[" + strings.Join(parts, ", ") + "]"
	}

	var params []string
	for _, p := range fn.Params {
		mutPrefix := ""
		if p.IsMut {
			mutPrefix = "mut "
		}
		params = append(params, fmt.Sprintf("%s%s: %s", mutPrefix, p.Name, p.Type.String()))
	}

	ret := ""
	if fn.RetType != nil {
		ret = fmt.Sprintf(" -> %s", fn.RetType.String())
	}

	fmt.Printf("%sfn %s%s%s(%s)%s\n\n", pubPrefix, receiver, fn.Name, typeParams, strings.Join(params, ", "), ret)
}

func printExternFnDoc(fn *compiler.ExternFnDecl) {
	var params []string
	for _, p := range fn.Params {
		params = append(params, fmt.Sprintf("%s: %s", p.Name, p.Type.String()))
	}
	ret := ""
	if fn.RetType != nil {
		ret = fmt.Sprintf(" -> %s", fn.RetType.String())
	}
	fmt.Printf("extern fn %s(%s)%s\n\n", fn.Name, strings.Join(params, ", "), ret)
}
