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

	if strings.HasSuffix(prefix, ".") {
		items = append(items,
			map[string]any{"label": "len", "kind": 10, "detail": "length (i32)"},
			map[string]any{"label": "cap", "kind": 10, "detail": "capacity (i32)"},
			map[string]any{"label": "push", "kind": 2, "detail": "push(val) onto slice"},
			map[string]any{"label": "pop", "kind": 2, "detail": "pop() from slice -> T?"},
			map[string]any{"label": "ptr", "kind": 10, "detail": "raw byte pointer *u8 (unsafe)"},
			map[string]any{"label": "send", "kind": 2, "detail": "send(val) into channel"},
			map[string]any{"label": "recv", "kind": 2, "detail": "recv() from channel"},
			map[string]any{"label": "close", "kind": 2, "detail": "close() channel"},
			map[string]any{"label": "spawn", "kind": 2, "detail": "pool.spawn(val) -> UbeHandle"},
			map[string]any{"label": "get", "kind": 2, "detail": "pool.get(handle) -> T?"},
			map[string]any{"label": "despawn", "kind": 2, "detail": "pool.despawn(handle) -> bool"},
		)
		return items
	}

	for _, s := range strings.Fields("fn let mut struct enum trait match if else while for in scope unsafe spawn return break continue defer or_else import select extern") {
		items = append(items, map[string]any{"label": s, "kind": 14})
	}
	for _, t := range strings.Fields("i32 u32 i64 u64 u8 f32 f64 bool string void Chan Pool UbeHandle StringBuilder") {
		items = append(items, map[string]any{"label": t, "kind": 7})
	}
	builtins := []struct{ name, sig string }{
		{"println", "println(x, ...)"},
		{"print", "print(x, ...)"},
		{"time_now_ms", "time_now_ms() -> i64"},
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
