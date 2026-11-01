package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

// runContext generates an AI-ready context summary with signatures and control flow.
func runContext(args []string) error {
	target, outFile, err := parseContextArgs(args)
	if err != nil {
		return err
	}

	var files []string
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("target %q: %w", target, err)
	}

	if info.IsDir() {
		files, err = discoverUbeFiles(target)
		if err != nil {
			return fmt.Errorf("scanning directory %s: %w", target, err)
		}
		if len(files) == 0 {
			return fmt.Errorf("no .ube files found in directory %s", target)
		}
	} else {
		if !strings.HasSuffix(target, ".ube") {
			return fmt.Errorf("target file must have .ube extension: %s", target)
		}
		files = []string{target}
	}

	sort.Strings(files)

	var sb strings.Builder
	sb.WriteString("================================================================================\n")
	sb.WriteString("UBE CODEBASE CONTEXT (Generated for AI / LLM Assistance)\n")
	sb.WriteString(fmt.Sprintf("Target: %s | Source Files: %d\n", target, len(files)))
	sb.WriteString("================================================================================\n\n")

	totalDecls := 0
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			sb.WriteString(fmt.Sprintf("// Error reading %s: %v\n\n", file, err))
			continue
		}

		lexer := compiler.NewLexer(file, string(src))
		tokens, err := lexer.Tokenize()
		if err != nil {
			sb.WriteString(fmt.Sprintf("// Lexer error in %s: %v\n\n", file, err))
			continue
		}

		parser := compiler.NewParser(tokens)
		prog, err := parser.ParseProgram()
		if err != nil {
			sb.WriteString(fmt.Sprintf("// Parser error in %s: %v\n\n", file, err))
			continue
		}

		sb.WriteString(fmt.Sprintf("File: %s\n", file))
		sb.WriteString(strings.Repeat("-", 80) + "\n")

		if len(prog.Decls) == 0 {
			sb.WriteString("  (no top-level declarations)\n\n")
			continue
		}

		for _, decl := range prog.Decls {
			totalDecls++
			switch d := decl.(type) {
			case *compiler.StructDecl:
				sb.WriteString(formatStructSign(d) + "\n")
			case *compiler.EnumDecl:
				sb.WriteString(formatEnumSign(d) + "\n")
			case *compiler.TraitDecl:
				sb.WriteString(formatTraitSign(d) + "\n")
			case *compiler.FnDecl:
				sb.WriteString(formatFnSign(d) + "\n")
				calls, flow := analyzeFnFlow(d)
				if len(calls) > 0 {
					sb.WriteString(fmt.Sprintf("    calls: [%s]\n", strings.Join(calls, ", ")))
				}
				if len(flow) > 0 {
					sb.WriteString(fmt.Sprintf("    flow:  %s\n", strings.Join(flow, ", ")))
				}
				sb.WriteString("\n")
			case *compiler.ExternFnDecl:
				sb.WriteString(formatExternFnSign(d) + "\n\n")
			}
		}
	}

	if totalDecls == 0 {
		sb.WriteString("// No declarations could be extracted from the target.\n")
	}

	if err := os.WriteFile(outFile, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("writing context file %s: %w", outFile, err)
	}

	added, err := ensureGitignore(outFile)
	if err != nil {
		return fmt.Errorf("updating .gitignore: %w", err)
	}

	if added {
		fmt.Printf("✓ Generated codebase context in %s (added to .gitignore)\n", outFile)
	} else {
		fmt.Printf("✓ Generated codebase context in %s\n", outFile)
	}
	return nil
}

// runGuide generates a comprehensive Ube language reference guide for AI.
func runGuide(args []string) error {
	outFile, err := parseGuideArgs(args)
	if err != nil {
		return err
	}

	guideContent := getUbeLanguageGuide()
	if err := os.WriteFile(outFile, []byte(guideContent), 0644); err != nil {
		return fmt.Errorf("writing guide file %s: %w", outFile, err)
	}

	added, err := ensureGitignore(outFile)
	if err != nil {
		return fmt.Errorf("updating .gitignore: %w", err)
	}

	if added {
		fmt.Printf("✓ Generated Ube language guide in %s (added to .gitignore)\n", outFile)
	} else {
		fmt.Printf("✓ Generated Ube language guide in %s\n", outFile)
	}
	return nil
}

// ensureGitignore appends the target filename to the .gitignore next to it if
// one exists. It reports whether a new entry was appended; an existing entry is
// left untouched and the function reports false.
func ensureGitignore(filename string) (bool, error) {
	cleanTarget := filepath.Clean(filename)
	baseTarget := filepath.Base(cleanTarget)
	gitignorePath := filepath.Join(filepath.Dir(cleanTarget), ".gitignore")

	data, err := os.ReadFile(gitignorePath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == baseTarget || trimmed == "/"+baseTarget || trimmed == cleanTarget || trimmed == "/"+cleanTarget {
			return false, nil // Already present
		}
	}

	var sb strings.Builder
	sb.Write(data)
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString(baseTarget + "\n")

	if err := os.WriteFile(gitignorePath, []byte(sb.String()), 0644); err != nil {
		return false, err
	}
	return true, nil
}

func parseContextArgs(args []string) (target string, output string, err error) {
	output = "ube_context.txt"
	target = "."
	targetSet := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-o" {
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("flag -o requires an argument")
			}
			output = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-o=") {
			output = strings.TrimPrefix(arg, "-o=")
		} else if strings.HasPrefix(arg, "--output=") {
			output = strings.TrimPrefix(arg, "--output=")
		} else if strings.HasPrefix(arg, "-") {
			return "", "", fmt.Errorf("unknown flag %q", arg)
		} else {
			if !targetSet {
				target = arg
				targetSet = true
			} else {
				return "", "", fmt.Errorf("unexpected argument %q", arg)
			}
		}
	}
	return target, output, nil
}

func parseGuideArgs(args []string) (output string, err error) {
	output = "ube_guide.md"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-o" {
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag -o requires an argument")
			}
			output = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-o=") {
			output = strings.TrimPrefix(arg, "-o=")
		} else if strings.HasPrefix(arg, "--output=") {
			output = strings.TrimPrefix(arg, "--output=")
		} else if strings.HasPrefix(arg, "-") {
			return "", fmt.Errorf("unknown flag %q", arg)
		} else {
			return "", fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return output, nil
}
