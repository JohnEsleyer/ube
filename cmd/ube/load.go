package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

// runLoad executes LLM request directives (<trace>, <def>, <select>, <file>, <grep>)
func runLoad(args []string) error {
	var (
		clipIn  bool
		clipOut bool
		toStdout bool
		redact   bool
		target   string
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-ci", "--clip-in":
			clipIn = true
		case "-c", "--clip":
			clipOut = true
		case "-s", "--stdout":
			toStdout = true
		case "-r", "--redact":
			redact = true
		default:
			if !strings.HasPrefix(arg, "-") && target == "" {
				target = arg
			}
		}
	}

	var reqText string
	if clipIn {
		c, err := readClipboard()
		if err != nil {
			return fmt.Errorf("reading clipboard: %w", err)
		}
		reqText = c
	} else if target != "" {
		data, err := os.ReadFile(target)
		if err != nil {
			return fmt.Errorf("reading request file %s: %w", target, err)
		}
		reqText = string(data)
	} else {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
			reqText = string(data)
		} else {
			return errors.New("no request input provided. Use -ci (clipboard), a file path, or pipe via stdin")
		}
	}

	if strings.TrimSpace(reqText) == "" {
		return errors.New("request input is empty")
	}

	out, err := processLoadRequest(reqText)
	if err != nil {
		return err
	}

	if redact {
		out = redactSecrets(out)
	}

	if clipOut {
		if err := writeClipboard(out); err != nil {
			return fmt.Errorf("copying to clipboard: %w", err)
		}
		fmt.Printf("✓ Loaded context copied to clipboard (%d bytes)\n", len(out))
	}

	if toStdout || (!clipOut && !clipIn) {
		fmt.Println(out)
	}

	return nil
}

var (
	traceRe          = regexp.MustCompile(`<trace\s+symbol="([^"]+)"(?:\s+depth="([^"]+)")?[^>]*/>`)
	defRe            = regexp.MustCompile(`<def\s+symbol="([^"]+)"[^>]*/>`)
	loadFileRe       = regexp.MustCompile(`<file\s+path="([^"]+)"(?:\s+search="([^"]+)")?(?:\s+context="([^"]+)")?(?:\s+for-patch="([^"]+)")?[^>]*/>`)
	grepRe           = regexp.MustCompile(`<grep\s+pattern="([^"]+)"(?:\s+glob="([^"]+)")?[^>]*/>`)
	selectRe         = regexp.MustCompile(`<select\s+([^>]+?)(?:/>|>(.*?)</select>)`)
	xrefRe           = regexp.MustCompile(`<xref\s+([^>]+?)(?:/>|>(.*?)</xref>)`)
	featureContextRe = regexp.MustCompile(`(?s)<feature-context[^>]*>(.*?)</feature-context>`)

	fcTargetTypeRe  = regexp.MustCompile(`<target-type\s+name="([^"]+)"[^>]*/>`)
	fcEntrypointRe  = regexp.MustCompile(`<entrypoint\s+([^>]+)/>`)
	fcTouchpointsRe = regexp.MustCompile(`<touchpoints\s+([^>]+)/>`)
)

func processLoadRequest(req string) (string, error) {
	var sb strings.Builder
	handled := false

	// Load ASTs across current project
	ubeFiles, _ := discoverUbeFiles(".")
	parsedFiles := make(map[string]*compiler.Program)
	rawSources := make(map[string]string)

	for _, file := range ubeFiles {
		src, err := os.ReadFile(file)
		if err == nil {
			rawSources[file] = string(src)
			lexer := compiler.NewLexer(file, string(src))
			tokens, _ := lexer.Tokenize()
			parser := compiler.NewParser(tokens)
			if prog, err := parser.ParseProgram(); err == nil {
				parsedFiles[file] = prog
			}
		}
	}

	// 1. Process <feature-context>...</feature-context>
	for _, m := range featureContextRe.FindAllStringSubmatch(req, -1) {
		handled = true
		sb.WriteString(renderFeatureContext(m[1], parsedFiles, rawSources))
		sb.WriteString("\n")
	}

	// 2. Process <trace symbol="..." depth="..." />
	for _, m := range traceRe.FindAllStringSubmatch(req, -1) {
		handled = true
		sym := m[1]
		depth := 2
		if m[2] != "" {
			if d, err := strconv.Atoi(m[2]); err == nil && d > 0 {
				depth = d
			}
		}
		sb.WriteString(renderTrace(sym, depth, parsedFiles, rawSources))
		sb.WriteString("\n")
	}

	// 3. Process <def symbol="..." />
	for _, m := range defRe.FindAllStringSubmatch(req, -1) {
		handled = true
		sym := m[1]
		sb.WriteString(renderDef(sym, parsedFiles, rawSources))
		sb.WriteString("\n")
	}

	// 4. Process <select ... />
	for _, m := range selectRe.FindAllStringSubmatch(req, -1) {
		handled = true
		attrs := parseTagAttributes(m[1])
		if caseTarget, ok := attrs["case"]; ok && caseTarget != "" {
			path := attrs["path"]
			fnName := attrs["fn"]
			sb.WriteString(renderSelectCase(path, fnName, caseTarget, rawSources))
			sb.WriteString("\n")
		} else if methods, ok := attrs["methods"]; ok && methods != "" {
			stName := attrs["struct"]
			if stName == "" {
				stName = attrs["type"]
			}
			sb.WriteString(renderSelectMethods(stName, methods, parsedFiles, rawSources))
			sb.WriteString("\n")
		}
	}

	// 5. Process <xref ... />
	for _, m := range xrefRe.FindAllStringSubmatch(req, -1) {
		handled = true
		attrs := parseTagAttributes(m[1])
		sym := attrs["symbol"]
		if sym == "" {
			sym = attrs["name"]
		}
		action := attrs["action"]
		glob := attrs["glob"]
		sb.WriteString(renderXRef(sym, action, glob, parsedFiles, rawSources))
		sb.WriteString("\n")
	}

	// 6. Process <file path="..." search="..." />
	for _, m := range loadFileRe.FindAllStringSubmatch(req, -1) {
		handled = true
		path := m[1]
		search := m[2]
		ctxLines := 5
		if m[3] != "" {
			if c, err := strconv.Atoi(m[3]); err == nil {
				ctxLines = c
			}
		}
		forPatch := m[4] == "true"
		sb.WriteString(renderFileExtract(path, search, ctxLines, forPatch))
		sb.WriteString("\n")
	}

	// 7. Process <grep pattern="..." />
	for _, m := range grepRe.FindAllStringSubmatch(req, -1) {
		handled = true
		pat := m[1]
		glob := m[2]
		sb.WriteString(renderGrep(pat, glob, rawSources))
		sb.WriteString("\n")
	}

	if !handled {
		return "", errors.New("no recognized directives (<trace>, <def>, <file>, <grep>, <select>, <xref>, <feature-context>) found in request")
	}

	return strings.TrimSpace(sb.String()), nil
}

func renderTrace(symbol string, depth int, parsed map[string]*compiler.Program, raw map[string]string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("// --- TRACE: %s (depth %d) ---\n", symbol, depth))

	visited := make(map[string]bool)
	var traceStep func(curr string, d int)
	traceStep = func(curr string, d int) {
		if d < 0 || visited[curr] {
			return
		}
		visited[curr] = true

		defCode, calls := findFnAndCalls(curr, parsed, raw)
		if defCode != "" {
			sb.WriteString(defCode)
			sb.WriteString("\n\n")
		} else {
			// Try finding struct or enum
			typeCode := findTypeDef(curr, parsed, raw)
			if typeCode != "" {
				sb.WriteString(typeCode)
				sb.WriteString("\n\n")
			}
		}

		if d > 0 {
			for _, call := range calls {
				traceStep(call, d-1)
			}
		}
	}

	traceStep(symbol, depth)
	return strings.TrimSpace(sb.String())
}

func renderDef(symbol string, parsed map[string]*compiler.Program, raw map[string]string) string {
	defCode, _ := findFnAndCalls(symbol, parsed, raw)
	if defCode != "" {
		return defCode
	}
	typeCode := findTypeDef(symbol, parsed, raw)
	if typeCode != "" {
		return typeCode
	}
	return fmt.Sprintf("// symbol '%s' not found", symbol)
}

func findFnAndCalls(symbol string, parsed map[string]*compiler.Program, raw map[string]string) (string, []string) {
	for file, prog := range parsed {
		for _, decl := range prog.Decls {
			if fn, ok := decl.(*compiler.FnDecl); ok {
				matches := fn.Name == symbol
				if !matches && fn.Receiver != nil {
					fullName := fmt.Sprintf("%s.%s", fn.Receiver.Type.String(), fn.Name)
					matches = fullName == symbol
				}
				if matches {
					calls, _ := analyzeFnFlow(fn)
					snippet := extractDeclSource(raw[file], fn.Loc().Line)
					return fmt.Sprintf("// File: %s\n%s", file, snippet), calls
				}
			}
		}
	}
	return "", nil
}

func findTypeDef(symbol string, parsed map[string]*compiler.Program, raw map[string]string) string {
	for file, prog := range parsed {
		for _, decl := range prog.Decls {
			switch d := decl.(type) {
			case *compiler.StructDecl:
				if d.Name == symbol {
					return fmt.Sprintf("// File: %s\n%s", file, extractDeclSource(raw[file], d.Loc().Line))
				}
			case *compiler.EnumDecl:
				if d.Name == symbol {
					return fmt.Sprintf("// File: %s\n%s", file, extractDeclSource(raw[file], d.Loc().Line))
				}
			case *compiler.TraitDecl:
				if d.Name == symbol {
					return fmt.Sprintf("// File: %s\n%s", file, extractDeclSource(raw[file], d.Loc().Line))
				}
			}
		}
	}
	return ""
}

func extractDeclSource(src string, startLine int) string {
	lines := strings.Split(src, "\n")
	if startLine <= 0 || startLine > len(lines) {
		return ""
	}
	idx := startLine - 1
	var out []string
	depth := 0
	started := false

	for i := idx; i < len(lines); i++ {
		line := lines[i]
		out = append(out, line)
		for _, ch := range line {
			if ch == '{' {
				depth++
				started = true
			} else if ch == '}' {
				depth--
			}
		}
		if started && depth <= 0 {
			break
		}
	}
	return strings.Join(out, "\n")
}

func renderFileExtract(path, search string, ctxLines int, forPatch bool) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("// Error reading %s: %v", path, err)
	}
	lines := strings.Split(string(data), "\n")
	if search == "" {
		if forPatch {
			return fmt.Sprintf("<file path=\"%s\">\n%s\n</file>", path, strings.TrimSpace(string(data)))
		}
		return fmt.Sprintf("// File: %s\n%s", path, string(data))
	}

	matchIdx := -1
	for i, l := range lines {
		if strings.Contains(l, search) {
			matchIdx = i
			break
		}
	}
	if matchIdx == -1 {
		return fmt.Sprintf("// Search '%s' not found in %s", search, path)
	}

	start := matchIdx - ctxLines
	if start < 0 {
		start = 0
	}
	end := matchIdx + ctxLines + 1
	if end > len(lines) {
		end = len(lines)
	}

	var sb strings.Builder
	if forPatch {
		sb.WriteString(fmt.Sprintf("<search path=\"%s\">\n", path))
		for i := start; i < end; i++ {
			sb.WriteString(lines[i] + "\n")
		}
		sb.WriteString("</search>")
	} else {
		sb.WriteString(fmt.Sprintf("// --- %s (lines %d-%d) ---\n", path, start+1, end))
		for i := start; i < end; i++ {
			sb.WriteString(fmt.Sprintf("%4d | %s\n", i+1, lines[i]))
		}
	}
	return strings.TrimSpace(sb.String())
}

func renderGrep(pat, glob string, raw map[string]string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("// --- GREP: %s ---\n", pat))
	for file, src := range raw {
		if glob != "" {
			if m, _ := filepath.Match(glob, filepath.Base(file)); !m {
				continue
			}
		}
		lines := strings.Split(src, "\n")
		for i, line := range lines {
			if strings.Contains(line, pat) {
				sb.WriteString(fmt.Sprintf("%s:%d: %s\n", file, i+1, strings.TrimSpace(line)))
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

func redactSecrets(s string) string {
	keyRe := regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|bearer)\s*[:=]\s*["']?([A-Za-z0-9_\-\.]{8,})["']?`)
	return keyRe.ReplaceAllString(s, `$1: [REDACTED]`)
}

func parseTagAttributes(attrStr string) map[string]string {
	attrs := make(map[string]string)
	re := regexp.MustCompile(`([a-zA-Z0-9_-]+)="([^"]*)"`)
	matches := re.FindAllStringSubmatch(attrStr, -1)
	for _, m := range matches {
		attrs[m[1]] = m[2]
	}
	return attrs
}

func renderSelectCase(path, fnName, caseTarget string, rawSources map[string]string) string {
	src := ""
	if path != "" {
		if s, ok := rawSources[path]; ok {
			src = s
		} else {
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Sprintf("// Error reading %s: %v", path, err)
			}
			src = string(data)
			rawSources[path] = src
		}
	} else {
		for p, s := range rawSources {
			if strings.Contains(s, "fn "+fnName) || strings.Contains(s, "func "+fnName) {
				path = p
				src = s
				break
			}
		}
		if src == "" {
			return fmt.Sprintf("// Function '%s' not found", fnName)
		}
	}

	lines := strings.Split(src, "\n")
	startLine := -1
	endLine := -1

	fnStart := 0
	fnEnd := len(lines)
	if fnName != "" {
		for i, line := range lines {
			if strings.Contains(line, "fn "+fnName) || strings.Contains(line, "func "+fnName) {
				fnStart = i
				depth := 0
				started := false
				for j := i; j < len(lines); j++ {
					for _, ch := range lines[j] {
						if ch == '{' {
							depth++
							started = true
						} else if ch == '}' {
							depth--
						}
					}
					if started && depth <= 0 {
						fnEnd = j + 1
						break
					}
				}
				break
			}
		}
	}

	for i := fnStart; i < fnEnd && i < len(lines); i++ {
		line := lines[i]
		if strings.Contains(line, caseTarget) {
			startLine = i
			break
		}
	}

	if startLine == -1 {
		return fmt.Sprintf("// Case '%s' not found in %s", caseTarget, path)
	}

	depth := 0
	hasBrace := false
	for i := startLine; i < fnEnd && i < len(lines); i++ {
		for _, ch := range lines[i] {
			if ch == '{' {
				depth++
				hasBrace = true
			} else if ch == '}' {
				depth--
			}
		}
		if hasBrace && depth <= 0 {
			endLine = i + 1
			break
		}
	}

	if !hasBrace || endLine == -1 {
		endLine = startLine + 1
		for i := startLine + 1; i < fnEnd && i < len(lines); i++ {
			trimmed := strings.TrimSpace(lines[i])
			if strings.HasPrefix(trimmed, "case ") || strings.HasPrefix(trimmed, "default:") || strings.HasPrefix(trimmed, "}") {
				endLine = i
				break
			}
			endLine = i + 1
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("// --- SELECT: %s in %s (lines %d-%d) ---\n", caseTarget, path, startLine+1, endLine))
	for i := startLine; i < endLine; i++ {
		sb.WriteString(lines[i] + "\n")
	}
	return strings.TrimSpace(sb.String())
}

func renderSelectMethods(structName, methodsStr string, parsed map[string]*compiler.Program, rawSources map[string]string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("// --- SELECT: methods of %s ---\n", structName))
	methodList := strings.Split(methodsStr, ",")
	foundAny := false

	for _, m := range methodList {
		method := strings.TrimSpace(m)
		if method == "" {
			continue
		}

		found := false
		for file, prog := range parsed {
			for _, decl := range prog.Decls {
				if fn, ok := decl.(*compiler.FnDecl); ok {
					isMethod := false
					if fn.Receiver != nil {
						recName := fn.Receiver.Type.String()
						if recName == structName || strings.HasSuffix(recName, structName) {
							isMethod = (fn.Name == method)
						}
					}
					if !isMethod {
						fullName := fmt.Sprintf("%s.%s", structName, method)
						isMethod = (fn.Name == fullName)
					}
					if isMethod {
						snippet := extractDeclSource(rawSources[file], fn.Loc().Line)
						sb.WriteString(fmt.Sprintf("// Method: %s.%s (%s)\n%s\n\n", structName, method, file, snippet))
						found = true
						foundAny = true
						break
					}
				}
			}
			if found {
				break
			}
		}

		if !found {
			for file, src := range rawSources {
				lines := strings.Split(src, "\n")
				for i, line := range lines {
					if (strings.Contains(line, "fn (") || strings.Contains(line, "func (")) &&
						strings.Contains(line, structName) && strings.Contains(line, method) {
						snippet := extractDeclSource(src, i+1)
						sb.WriteString(fmt.Sprintf("// Method: %s.%s (%s)\n%s\n\n", structName, method, file, snippet))
						found = true
						foundAny = true
						break
					}
				}
				if found {
					break
				}
			}
		}

		if !found {
			sb.WriteString(fmt.Sprintf("// Method %s.%s not found\n\n", structName, method))
		}
	}

	if !foundAny {
		return fmt.Sprintf("// No methods found for %s", structName)
	}

	return strings.TrimSpace(sb.String())
}

func renderXRef(symbol, action, glob string, parsed map[string]*compiler.Program, rawSources map[string]string) string {
	var sb strings.Builder
	actionDesc := action
	if actionDesc == "" {
		actionDesc = "all"
	}
	sb.WriteString(fmt.Sprintf("// --- XREF: %s (action: %s) ---\n", symbol, actionDesc))

	sb.WriteString("// Definitions:\n")
	defCount := 0
	defLines := make(map[string]bool)

	for file, prog := range parsed {
		for _, decl := range prog.Decls {
			switch d := decl.(type) {
			case *compiler.StructDecl:
				if d.Name == symbol {
					sb.WriteString(fmt.Sprintf("  %s:%d: struct %s\n", file, d.Loc().Line, d.Name))
					defLines[fmt.Sprintf("%s:%d", file, d.Loc().Line)] = true
					defCount++
				}
			case *compiler.EnumDecl:
				if d.Name == symbol {
					sb.WriteString(fmt.Sprintf("  %s:%d: enum %s\n", file, d.Loc().Line, d.Name))
					defLines[fmt.Sprintf("%s:%d", file, d.Loc().Line)] = true
					defCount++
				}
			case *compiler.TraitDecl:
				if d.Name == symbol {
					sb.WriteString(fmt.Sprintf("  %s:%d: trait %s\n", file, d.Loc().Line, d.Name))
					defLines[fmt.Sprintf("%s:%d", file, d.Loc().Line)] = true
					defCount++
				}
			case *compiler.FnDecl:
				if d.Name == symbol {
					sb.WriteString(fmt.Sprintf("  %s:%d: fn %s(...)\n", file, d.Loc().Line, d.Name))
					defLines[fmt.Sprintf("%s:%d", file, d.Loc().Line)] = true
					defCount++
				}
			}
		}
	}

	if defCount == 0 {
		sb.WriteString("  (none found in parsed AST)\n")
	}

	if action != "def" {
		sb.WriteString("\n// References:\n")
		refCount := 0
		for file, src := range rawSources {
			if glob != "" {
				if m, _ := filepath.Match(glob, filepath.Base(file)); !m {
					continue
				}
			}
			lines := strings.Split(src, "\n")
			for i, line := range lines {
				lineKey := fmt.Sprintf("%s:%d", file, i+1)
				if defLines[lineKey] {
					continue
				}
				if !containsWord(line, symbol) {
					continue
				}

				trimmed := strings.TrimSpace(line)
				if action == "call" {
					if !strings.Contains(trimmed, symbol+"(") && !strings.Contains(trimmed, "."+symbol+"(") {
						continue
					}
				} else if action == "write" {
					if !strings.Contains(trimmed, symbol+" =") &&
						!strings.Contains(trimmed, symbol+" :=") &&
						!strings.Contains(trimmed, symbol+" +=") &&
						!strings.Contains(trimmed, symbol+" -=") &&
						!strings.Contains(trimmed, "mut "+symbol) {
						continue
					}
				}

				sb.WriteString(fmt.Sprintf("  %s:%d: %s\n", file, i+1, trimmed))
				refCount++
			}
		}
		if refCount == 0 {
			sb.WriteString("  (no references found)\n")
		}
	}

	return strings.TrimSpace(sb.String())
}

func containsWord(line, word string) bool {
	idx := 0
	for {
		i := strings.Index(line[idx:], word)
		if i == -1 {
			return false
		}
		pos := idx + i
		validBefore := (pos == 0) || !isWordChar(rune(line[pos-1]))
		endPos := pos + len(word)
		validAfter := (endPos == len(line)) || !isWordChar(rune(line[endPos]))
		if validBefore && validAfter {
			return true
		}
		idx = pos + 1
	}
}

func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

func renderFeatureContext(body string, parsed map[string]*compiler.Program, rawSources map[string]string) string {
	var sb strings.Builder
	sb.WriteString("// =============================================================================\n")
	sb.WriteString("// FEATURE CONTEXT BUNDLE\n")
	sb.WriteString("// =============================================================================\n\n")

	for _, m := range fcTargetTypeRe.FindAllStringSubmatch(body, -1) {
		name := strings.TrimSpace(m[1])
		sb.WriteString(fmt.Sprintf("// --- TYPE: %s ---\n", name))
		sb.WriteString(renderDef(name, parsed, rawSources))
		sb.WriteString("\n\n")
	}

	for _, m := range fcEntrypointRe.FindAllStringSubmatch(body, -1) {
		attrs := parseTagAttributes(m[1])
		sym := attrs["symbol"]
		if sym == "" {
			sym = attrs["name"]
		}
		depth := 2
		if dStr, ok := attrs["depth"]; ok {
			if d, err := strconv.Atoi(dStr); err == nil && d > 0 {
				depth = d
			}
		}
		if file, ok := attrs["file"]; ok && file != "" {
			if _, loaded := rawSources[file]; !loaded {
				if data, err := os.ReadFile(file); err == nil {
					rawSources[file] = string(data)
					lexer := compiler.NewLexer(file, string(data))
					if tokens, err := lexer.Tokenize(); err == nil {
						parser := compiler.NewParser(tokens)
						if prog, err := parser.ParseProgram(); err == nil {
							parsed[file] = prog
						}
					}
				}
			}
		}
		sb.WriteString(renderTrace(sym, depth, parsed, rawSources))
		sb.WriteString("\n\n")
	}

	for _, m := range fcTouchpointsRe.FindAllStringSubmatch(body, -1) {
		attrs := parseTagAttributes(m[1])
		pat := attrs["pattern"]
		glob := attrs["glob"]
		sb.WriteString(renderGrep(pat, glob, rawSources))
		sb.WriteString("\n\n")
	}

	return strings.TrimSpace(sb.String())
}
