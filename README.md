<search>
# The Ube Programming Language

**The sweet, grounded systems language.**
*Easy to program like Go. Fast like C. Zero Garbage Collection.*

**Latest version: `0.5.3`**
</search>
<with>
# The Ube Programming Language

**The sweet, grounded systems language.**
*Easy to program like Go. Fast like C. Zero Garbage Collection.*

**Latest version: `0.5.3`**
</with>
</replace>

<replace path="cmd/ube/main.go">
<search>
const version = "0.5.2"
</search>
<with>
const version = "0.5.3"
</with>
</replace>

<replace path="cmd/ube/main.go">
<search>
	case "fmt":
		return runFmt(args[1:])
	default:
</search>
<with>
	case "fmt":
		return runFmt(args[1:])
	case "doc":
		return runDoc(args[1:])
	default:
</with>
</replace>

<replace path="cmd/ube/project.go">
<search>
  fmt [dir|file]            Format Ube source files (--check or -w)
  lsp                       Start the language server
</search>
<with>
  fmt [dir|file]            Format Ube source files (--check or -w)
  doc <file.ube>            Generate Markdown documentation from source
  lsp                       Start the language server
</with>
</replace>

<file path="cmd/ube/doc.go">
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func runDoc(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ube doc <file.ube>")
	}
	file := args[0]
	content, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	tokens, err := compiler.NewLexer(file, string(content)).Tokenize()
	if err != nil {
		return err
	}
	prog, err := compiler.NewParser(tokens).ParseProgram()
	if err != nil {
		return err
	}

	baseName := filepath.Base(file)
	fmt.Printf("# Module `%s`\n\n", strings.TrimSuffix(baseName, filepath.Ext(baseName)))

	var structs []*compiler.StructDecl
	var enums []*compiler.EnumDecl
	var traits []*compiler.TraitDecl
	var functions []*compiler.FnDecl
	var externs []*compiler.ExternFnDecl

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *compiler.StructDecl:
			structs = append(structs, d)
		case *compiler.EnumDecl:
			enums = append(enums, d)
		case *compiler.TraitDecl:
			traits = append(traits, d)
		case *compiler.FnDecl:
			functions = append(functions, d)
		case *compiler.ExternFnDecl:
			externs = append(externs, d)
		}
	}

	if len(structs) > 0 {
		fmt.Println("## Structs\n")
		for _, s := range structs {
			fmt.Printf("### `struct %s`\n\n", s.Name)
			if len(s.Fields) > 0 {
				fmt.Println("| Field | Type |")
				fmt.Println("| :--- | :--- |")
				for _, f := range s.Fields {
					fmt.Printf("| `%s` | `%s` |\n", f.Name, f.Type)
				}
				fmt.Println()
			}
		}
	}

	if len(enums) > 0 {
		fmt.Println("## Enums\n")
		for _, e := range enums {
			fmt.Printf("### `enum %s`\n\n", e.Name)
			for _, v := range e.Variants {
				if len(v.Types) > 0 {
					var types []string
					for _, t := range v.Types {
						types = append(types, t.String())
					}
					fmt.Printf("- `%s(%s)`\n", v.Name, strings.Join(types, ", "))
				} else {
					fmt.Printf("- `%s`\n", v.Name)
				}
			}
			fmt.Println()
		}
	}

	if len(traits) > 0 {
		fmt.Println("## Traits\n")
		for _, tr := range traits {
			fmt.Printf("### `trait %s`\n\n", tr.Name)
			for _, m := range tr.Methods {
				var params []string
				for _, p := range m.Params {
					params = append(params, fmt.Sprintf("%s: %s", p.Name, p.Type))
				}
				ret := "void"
				if m.RetType != nil {
					ret = m.RetType.String()
				}
				fmt.Printf("- `fn %s(%s) -> %s`\n", m.Name, strings.Join(params, ", "), ret)
			}
			fmt.Println()
		}
	}

	if len(functions) > 0 {
		fmt.Println("## Functions\n")
		for _, fn := range functions {
			var params []string
			for _, p := range fn.Params {
				params = append(params, fmt.Sprintf("%s: %s", p.Name, p.Type))
			}
			ret := "void"
			if fn.RetType != nil {
				ret = fn.RetType.String()
			}
			if fn.Receiver != nil {
				fmt.Printf("- `fn (%s %s) %s(%s) -> %s`\n", fn.Receiver.Name, fn.Receiver.Type, fn.Name, strings.Join(params, ", "), ret)
			} else {
				fmt.Printf("- `fn %s(%s) -> %s`\n", fn.Name, strings.Join(params, ", "), ret)
			}
		}
		fmt.Println()
	}

	if len(externs) > 0 {
		fmt.Println("## External Foreign Functions (C FFI)\n")
		for _, ex := range externs {
			var params []string
			for _, p := range ex.Params {
				params = append(params, fmt.Sprintf("%s: %s", p.Name, p.Type))
			}
			ret := "void"
			if ex.RetType != nil {
				ret = ex.RetType.String()
			}
			fmt.Printf("- `extern fn %s(%s) -> %s`\n", ex.Name, strings.Join(params, ", "), ret)
		}
		fmt.Println()
	}

	return nil
}
