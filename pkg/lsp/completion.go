// File: pkg/lsp/completion.go

package lsp

import (
	"strings"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func CompletionItems(source string, p lspPosition) []map[string]any {
	items := []map[string]any{}
	lines := strings.Split(source, "\n")
	prefix := ""
	if int(p.Line) < len(lines) {
		line := lines[p.Line]
		c := int(p.Character)
		if c > len(line) {
			c = len(line)
		}
		prefix = strings.TrimSpace(line[:c])
	}

	// Line 0: package declaration.
	if p.Line == 0 && (prefix == "" || strings.HasPrefix("package", prefix)) {
		return []map[string]any{
			{"label": "package main", "kind": 14, "detail": "executable package declaration"},
			{"label": "package", "kind": 14, "detail": "package <name>"},
		}
	}

	// Import path.
	if strings.Contains(prefix, "import \"") || strings.Contains(prefix, "import `") || prefix == "import" || prefix == "import " {
		return []map[string]any{
			{"label": "std/math", "kind": 9, "detail": "standard math library"},
			{"label": "std/strings", "kind": 9, "detail": "standard string utilities"},
			{"label": "std/io", "kind": 9, "detail": "standard I/O and streams"},
			{"label": "std/fs", "kind": 9, "detail": "standard filesystem utilities"},
			{"label": "std/bytes", "kind": 9, "detail": "standard byte buffer utilities"},
			{"label": "std/collections", "kind": 9, "detail": "standard collections (Stack, SimpleMap)"},
			{"label": "std/wasi", "kind": 9, "detail": "low-level WASI bindings"},
		}
	}

	if strings.HasSuffix(prefix, ".") {
		beforeDot := strings.TrimSuffix(prefix, ".")
		qualifier := ""
		for i := len(beforeDot) - 1; i >= 0; i-- {
			if isWord(beforeDot[i]) {
				qualifier = string(beforeDot[i]) + qualifier
			} else {
				break
			}
		}
		if qualifier != "" {
			if pkgItems := qualifiedMemberCompletions(source, qualifier); len(pkgItems) > 0 {
				return pkgItems
			}
		}
		items = append(items,
			map[string]any{"label": "len", "kind": 10, "detail": "length (i32)"},
			map[string]any{"label": "cap", "kind": 10, "detail": "capacity (i32)"},
			map[string]any{"label": "push", "kind": 2, "detail": "push(val) onto slice"},
			map[string]any{"label": "pop", "kind": 2, "detail": "pop() from slice -> T?"},
			map[string]any{"label": "clear", "kind": 2, "detail": "clear() slice length to 0"},
			map[string]any{"label": "contains", "kind": 2, "detail": "contains(val) -> bool"},
			map[string]any{"label": "send", "kind": 2, "detail": "send(val) into channel"},
			map[string]any{"label": "recv", "kind": 2, "detail": "recv() from channel"},
			map[string]any{"label": "close", "kind": 2, "detail": "close() channel"},
			map[string]any{"label": "spawn", "kind": 2, "detail": "pool.spawn(val) -> UbeHandle"},
			map[string]any{"label": "get", "kind": 2, "detail": "pool.get(handle) -> T?"},
			map[string]any{"label": "despawn", "kind": 2, "detail": "pool.despawn(handle) -> bool"},
			map[string]any{"label": "trim", "kind": 2, "detail": "trim() -> string"},
			map[string]any{"label": "starts_with", "kind": 2, "detail": "starts_with(prefix) -> bool"},
			map[string]any{"label": "ends_with", "kind": 2, "detail": "ends_with(suffix) -> bool"},
			map[string]any{"label": "index_of", "kind": 2, "detail": "index_of(substr) -> i32"},
			map[string]any{"label": "split", "kind": 2, "detail": "split(delim) -> []string"},
		)
		return items
	}

	for _, s := range strings.Fields("package pub fn let mut struct enum trait match if else while for in arena promote spawn return break continue defer or_else import as select extern") {
		items = append(items, map[string]any{"label": s, "kind": 14})
	}
	for _, t := range strings.Fields("i32 u32 i64 u64 u8 f32 f64 bool string void Chan Pool UbeHandle StringBuilder") {
		items = append(items, map[string]any{"label": t, "kind": 7})
	}
	builtins := []struct{ name, sig string }{
		{"println", "println(x, ...)"},
		{"print", "print(x, ...)"},
		{"time_now_ms", "time_now_ms() -> i64"},
		{"time_now_ns", "time_now_ns() -> i64"},
		{"sleep_ms", "sleep_ms(ms: i32)"},
		{"yield", "yield()"},
		{"exit_process", "exit_process(code: i32)"},
		{"int_to_str", "int_to_str(val: i64) -> string"},
		{"str_to_int", "str_to_int(s: string) -> i64 ! string"},
		{"read_file", "read_file(path: string) -> string ! string"},
		{"write_file", "write_file(path: string, content: string) -> bool ! string"},
		{"os_args", "os_args() -> []string"},
		{"str_contains", "str_contains(s: string, substr: string) -> bool"},
		{"str_starts_with", "str_starts_with(s: string, prefix: string) -> bool"},
		{"str_ends_with", "str_ends_with(s: string, suffix: string) -> bool"},
		{"str_trim", "str_trim(s: string) -> string"},
		{"str_index_of", "str_index_of(s: string, substr: string) -> i32"},
		{"str_split", "str_split(s: string, delim: string) -> []string"},
		{"env_get", "env_get(key: string) -> string?"},
		{"file_exists", "file_exists(path: string) -> bool"},
		{"path_join", "path_join(a: string, b: string) -> string"},
		{"str_replace", "str_replace(s: string, old: string, new: string) -> string"},
		{"dir_list", "dir_list(path: string) -> []string ! string"},
		{"dir_create", "dir_create(path: string) -> bool ! string"},
		{"file_remove", "file_remove(path: string) -> bool ! string"},
		{"env_set", "env_set(key: string, val: string) -> bool"},
		{"cmd_run", "cmd_run(cmd: string) -> i32"},
		{"abs", "abs(n: i64) -> i64"},
		{"min", "min(a: i64, b: i64) -> i64"},
		{"max", "max(a: i64, b: i64) -> i64"},
		{"sqrt", "sqrt(f: f64) -> f64"},
		{"pow", "pow(b: f64, e: f64) -> f64"},
		{"floor", "floor(f: f64) -> f64"},
		{"ceil", "ceil(f: f64) -> f64"},
	}
	for _, b := range builtins {
		items = append(items, map[string]any{"label": b.name, "kind": 3, "detail": b.sig})
	}

	tokens, err := compiler.NewLexer("", source).Tokenize()
	if err == nil {
		if prog, err := compiler.NewParser(tokens).ParseProgram(); err == nil {
			for _, decl := range prog.Decls {
				switch d := decl.(type) {
				case *compiler.FnDecl:
					items = append(items, map[string]any{"label": d.Name, "kind": 3, "detail": "fn " + d.Name})
				case *compiler.StructDecl:
					items = append(items, map[string]any{"label": d.Name, "kind": 22, "detail": "struct " + d.Name})
				case *compiler.EnumDecl:
					items = append(items, map[string]any{"label": d.Name, "kind": 13, "detail": "enum " + d.Name})
				}
			}
		}
	}
	return items
}

// qualifiedMemberCompletions returns the exported members of the imported
// package referenced by qualifier, or nil when qualifier is not an import.
func qualifiedMemberCompletions(source, qualifier string) []map[string]any {
	prog, err := getProgram("", source)
	if err != nil || prog == nil {
		return nil
	}
	canonicalPkg := prog.ModuleAliases[qualifier]
	if canonicalPkg == "" {
		if path, ok := parseImportsFromSource(source)[qualifier]; ok {
			canonicalPkg = strings.TrimPrefix(defaultAlias(path), "std/")
		} else {
			canonicalPkg = qualifier
		}
	}
	var out []map[string]any
	for _, decl := range prog.Decls {
		if declPackageOf(decl) != canonicalPkg {
			continue
		}
		switch d := decl.(type) {
		case *compiler.FnDecl:
			if d.IsPub && d.Receiver == nil {
				ret := "void"
				if d.RetType != nil {
					ret = d.RetType.String()
				}
				out = append(out, map[string]any{"label": shortName(d.Name), "kind": 3, "detail": "fn " + shortName(d.Name) + "(...) -> " + ret})
			}
		case *compiler.StructDecl:
			if d.IsPub {
				out = append(out, map[string]any{"label": shortName(d.Name), "kind": 22, "detail": "struct " + shortName(d.Name)})
			}
		case *compiler.EnumDecl:
			if d.IsPub {
				out = append(out, map[string]any{"label": shortName(d.Name), "kind": 13, "detail": "enum " + shortName(d.Name)})
			}
		case *compiler.TraitDecl:
			if d.IsPub {
				out = append(out, map[string]any{"label": shortName(d.Name), "kind": 11, "detail": "trait " + shortName(d.Name)})
			}
		}
	}
	return out
}

// parseImportsFromSource extracts alias -> import path pairs from the buffer.
func parseImportsFromSource(source string) map[string]string {
	aliases := make(map[string]string)
	for _, l := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(l)
		if !strings.HasPrefix(trimmed, "import ") {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) < 2 {
			continue
		}
		path := strings.Trim(parts[1], "\"'`")
		alias := defaultAlias(path)
		if len(parts) >= 4 && parts[2] == "as" {
			alias = parts[3]
		}
		aliases[alias] = path
	}
	return aliases
}
