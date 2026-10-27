package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
}

type lspPosition struct {
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

type docItem struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}

type textDoc struct {
	URI string `json:"uri"`
}

type changeParams struct {
	TextDocument   textDoc `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

type documentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          map[string]any   `json:"range"`
	SelectionRange map[string]any   `json:"selectionRange"`
	Children       []documentSymbol `json:"children,omitempty"`
}

var openDocs = map[string]string{}
var locRE = regexp.MustCompile(`(?:\[)?([^\]:]+):(\d+):(\d+)(?:\])?`)

func Run() {
	in := bufio.NewReader(os.Stdin)
	for {
		length := 0
		for {
			line, err := in.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "Content-Length:") {
				fmt.Sscanf(line, "Content-Length: %d", &length)
			}
		}
		if length <= 0 {
			continue
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(in, body); err != nil {
			return
		}
		var m rpcMessage
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		var result any
		switch m.Method {
		case "initialize":
			result = map[string]any{
				"capabilities": map[string]any{
					"textDocumentSync": 1,
					"completionProvider": map[string]any{
						"triggerCharacters": []string{".", ":"},
					},
					"hoverProvider":          true,
					"definitionProvider":     true,
					"documentSymbolProvider": true,
				},
			}
		case "initialized":
		case "textDocument/didOpen":
			var p struct {
				TextDocument docItem `json:"textDocument"`
			}
			_ = json.Unmarshal(m.Params, &p)
			openDocs[p.TextDocument.URI] = p.TextDocument.Text
			publish(p.TextDocument.URI, p.TextDocument.Text)
		case "textDocument/didChange":
			var p changeParams
			_ = json.Unmarshal(m.Params, &p)
			if len(p.ContentChanges) > 0 {
				openDocs[p.TextDocument.URI] = p.ContentChanges[len(p.ContentChanges)-1].Text
				publish(p.TextDocument.URI, openDocs[p.TextDocument.URI])
			}
		case "textDocument/didSave":
			var p struct {
				TextDocument textDoc `json:"textDocument"`
			}
			_ = json.Unmarshal(m.Params, &p)
			publish(p.TextDocument.URI, openDocs[p.TextDocument.URI])
		case "textDocument/didClose":
			var p struct {
				TextDocument textDoc `json:"textDocument"`
			}
			_ = json.Unmarshal(m.Params, &p)
			delete(openDocs, p.TextDocument.URI)
			notify("textDocument/publishDiagnostics", map[string]any{"uri": p.TextDocument.URI, "diagnostics": []any{}})
		case "textDocument/completion":
			var p struct {
				TextDocument textDoc     `json:"textDocument"`
				Position     lspPosition `json:"position"`
			}
			_ = json.Unmarshal(m.Params, &p)
			result = CompletionItems(openDocs[p.TextDocument.URI], p.Position)
		case "textDocument/hover":
			var p struct {
				TextDocument textDoc     `json:"textDocument"`
				Position     lspPosition `json:"position"`
			}
			_ = json.Unmarshal(m.Params, &p)
			result = Hover(p.TextDocument.URI, openDocs[p.TextDocument.URI], p.Position)
		case "textDocument/definition":
			var p struct {
				TextDocument textDoc     `json:"textDocument"`
				Position     lspPosition `json:"position"`
			}
			_ = json.Unmarshal(m.Params, &p)
			result = Definition(p.TextDocument.URI, openDocs[p.TextDocument.URI], p.Position)
		case "textDocument/documentSymbol":
			var p struct {
				TextDocument textDoc `json:"textDocument"`
			}
			_ = json.Unmarshal(m.Params, &p)
			result = DocumentSymbols(p.TextDocument.URI, openDocs[p.TextDocument.URI])
		case "shutdown":
			result = nil
		case "exit":
			return
		}
		if len(m.ID) > 0 {
			send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		}
	}
}

func send(v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(b), b)
}

func notify(method string, params any) {
	send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func publish(uri, source string) {
	var diags []map[string]any
	tokens, err := compiler.NewLexer(uri, source).Tokenize()
	if err == nil {
		var prog *compiler.Program
		prog, err = compiler.NewParser(tokens).ParseProgram()
		if err == nil {
			err = compiler.NewTypechecker().Check(prog)
		}
	}
	if err != nil {
		for _, errLine := range strings.Split(err.Error(), "\n") {
			errLine = strings.TrimSpace(errLine)
			if errLine == "" {
				continue
			}
			line, col, msg := 0, 0, errLine
			if m := locRE.FindStringSubmatch(errLine); len(m) > 0 {
				fmt.Sscanf(m[2], "%d", &line)
				fmt.Sscanf(m[3], "%d", &col)
				msg = strings.TrimSpace(strings.TrimPrefix(errLine, m[0]))
			}
			if line < 1 {
				line = 1
			}
			if col < 1 {
				col = 1
			}
			diags = append(diags, map[string]any{
				"range": map[string]any{
					"start": map[string]int{"line": line - 1, "character": col - 1},
					"end":   map[string]int{"line": line - 1, "character": col + 3},
				},
				"severity": 1,
				"source":   "ube",
				"message":  msg,
			})
		}
	}
	notify("textDocument/publishDiagnostics", map[string]any{"uri": uri, "diagnostics": diags})
}

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

	for _, s := range strings.Fields("fn let mut struct enum match if else while for in scope unsafe spawn return break continue defer or_else import select extern") {
		items = append(items, map[string]any{"label": s, "kind": 14})
	}
	for _, t := range strings.Fields("i32 u32 i64 u64 u8 f32 f64 bool string void Chan Pool UbeHandle") {
		items = append(items, map[string]any{"label": t, "kind": 7})
	}
	builtins := []struct{ name, sig string }{
		{"println", "println(x)"},
		{"print", "print(x)"},
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

func Hover(uri, source string, p lspPosition) any {
	lines := strings.Split(source, "\n")
	if int(p.Line) >= len(lines) {
		return nil
	}
	line := lines[p.Line]
	c := int(p.Character)
	if c > len(line) {
		c = len(line)
	}
	a := c
	for a > 0 && isWord(line[a-1]) {
		a--
	}
	b := c
	for b < len(line) && isWord(line[b]) {
		b++
	}
	if a == b {
		return nil
	}
	word := line[a:b]

	builtins := map[string]string{
		"println":         "`fn println(val)`\n*Polymorphically prints a primitive, string, or pointer followed by a newline.*",
		"print":           "`fn print(val)`\n*Polymorphically prints a primitive or string without a newline.*",
		"time_now_ms":     "`fn time_now_ms() -> i64`\n*Returns the current Unix timestamp in milliseconds.*",
		"sleep_ms":        "`fn sleep_ms(ms: i32)`\n*Suspends the current thread for the specified milliseconds.*",
		"yield":           "`fn yield()`\n*Cooperatively yields the current Vine fiber execution.*",
		"exit_process":    "`fn exit_process(code: i32)`\n*Immediately terminates the process with the given exit status.*",
		"int_to_str":      "`fn int_to_str(val: i64) -> string`\n*Formats an integer as an arena-backed string.*",
		"str_to_int":      "`fn str_to_int(s: string) -> i64 ! string`\n*Parses an integer from a string.*",
		"read_file":       "`fn read_file(path: string) -> string ! string`\n*Reads an entire file into an arena string.*",
		"write_file":      "`fn write_file(path: string, content: string) -> bool ! string`\n*Writes content to a file.*",
		"os_args":         "`fn os_args() -> []string`\n*Returns command-line arguments.*",
		"str_contains":    "`fn str_contains(s: string, substr: string) -> bool`\n*Checks if string contains substring.*",
		"str_starts_with": "`fn str_starts_with(s: string, prefix: string) -> bool`\n*Checks if string starts with prefix.*",
		"str_ends_with":   "`fn str_ends_with(s: string, suffix: string) -> bool`\n*Checks if string ends with suffix.*",
		"scope":           "`scope { ... }`\n*Lexical memory arena watermark block. Reclaimed on block exit.*",
		"spawn":           "`spawn fn(args...)`\n*Schedules a concurrent Vine fiber onto the worker thread pool.*",
	}
	if doc, ok := builtins[word]; ok {
		return map[string]any{"contents": map[string]string{"kind": "markdown", "value": doc}}
	}

	tokens, err := compiler.NewLexer(uri, source).Tokenize()
	if err == nil {
		if prog, err := compiler.NewParser(tokens).ParseProgram(); err == nil {
			for _, decl := range prog.Decls {
				switch d := decl.(type) {
				case *compiler.FnDecl:
					if d.Name == word {
						var params []string
						for _, param := range d.Params {
							params = append(params, fmt.Sprintf("%s: %s", param.Name, param.Type))
						}
						ret := "void"
						if d.RetType != nil {
							ret = d.RetType.String()
						}
						recv := ""
						if d.Receiver != nil {
							recv = fmt.Sprintf("(%s %s) ", d.Receiver.Name, d.Receiver.Type)
						}
						sig := fmt.Sprintf("```ube\nfn %s%s(%s) -> %s\n```", recv, d.Name, strings.Join(params, ", "), ret)
						return map[string]any{"contents": map[string]string{"kind": "markdown", "value": sig}}
					}
				case *compiler.StructDecl:
					if d.Name == word {
						var fields []string
						for _, f := range d.Fields {
							fields = append(fields, fmt.Sprintf("    %s: %s,", f.Name, f.Type))
						}
						code := fmt.Sprintf("```ube\nstruct %s {\n%s\n}\n```", d.Name, strings.Join(fields, "\n"))
						return map[string]any{"contents": map[string]string{"kind": "markdown", "value": code}}
					}
				case *compiler.EnumDecl:
					if d.Name == word {
						var variants []string
						for _, v := range d.Variants {
							variants = append(variants, "    "+v.Name)
						}
						code := fmt.Sprintf("```ube\nenum %s {\n%s\n}\n```", d.Name, strings.Join(variants, "\n"))
						return map[string]any{"contents": map[string]string{"kind": "markdown", "value": code}}
					}
				}
			}
		}
	}

	detail := "Ube symbol"
	if strings.Contains(line, "fn "+word+"(") {
		detail = "Ube function"
	} else if strings.Contains(line, "let "+word) {
		detail = "Ube local variable"
	}
	return map[string]any{"contents": map[string]string{"kind": "markdown", "value": "```ube\n" + word + "\n```\n*" + detail + "*"}}
}

func Definition(uri, source string, p lspPosition) any {
	lines := strings.Split(source, "\n")
	if int(p.Line) >= len(lines) {
		return nil
	}
	line := lines[p.Line]
	c := int(p.Character)
	if c > len(line) {
		c = len(line)
	}
	a, b := c, c
	for a > 0 && isWord(line[a-1]) {
		a--
	}
	for b < len(line) && isWord(line[b]) {
		b++
	}
	if a == b {
		return nil
	}
	word := line[a:b]

	tokens, err := compiler.NewLexer(uri, source).Tokenize()
	if err != nil {
		return nil
	}
	prog, err := compiler.NewParser(tokens).ParseProgram()
	if err != nil {
		return nil
	}
	for _, decl := range prog.Decls {
		loc := decl.Loc()
		name := ""
		switch d := decl.(type) {
		case *compiler.FnDecl:
			name = d.Name
		case *compiler.StructDecl:
			name = d.Name
		case *compiler.EnumDecl:
			name = d.Name
		case *compiler.ExternFnDecl:
			name = d.Name
		}
		if name == word {
			return map[string]any{
				"uri": uri,
				"range": map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len(word)},
				},
			}
		}
	}
	return nil
}

func DocumentSymbols(uri, source string) []documentSymbol {
	symbols := []documentSymbol{}
	tokens, err := compiler.NewLexer(uri, source).Tokenize()
	if err != nil {
		return symbols
	}
	prog, err := compiler.NewParser(tokens).ParseProgram()
	if err != nil {
		return symbols
	}
	for _, decl := range prog.Decls {
		loc := decl.Loc()
		switch d := decl.(type) {
		case *compiler.FnDecl:
			symbols = append(symbols, documentSymbol{
				Name:   d.Name,
				Detail: "fn",
				Kind:   12, // Function
				Range: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": 0},
					"end":   map[string]int{"line": loc.Line - 1, "character": 20},
				},
				SelectionRange: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len(d.Name)},
				},
			})
		case *compiler.StructDecl:
			fields := []documentSymbol{}
			for _, f := range d.Fields {
				fields = append(fields, documentSymbol{
					Name:   f.Name,
					Detail: f.Type.String(),
					Kind:   8, // Field
					Range: map[string]any{
						"start": map[string]int{"line": loc.Line - 1, "character": 0},
						"end":   map[string]int{"line": loc.Line - 1, "character": 20},
					},
					SelectionRange: map[string]any{
						"start": map[string]int{"line": loc.Line - 1, "character": 0},
						"end":   map[string]int{"line": loc.Line - 1, "character": len(f.Name)},
					},
				})
			}
			symbols = append(symbols, documentSymbol{
				Name:     d.Name,
				Detail:   "struct",
				Kind:     23, // Struct
				Children: fields,
				Range: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": 0},
					"end":   map[string]int{"line": loc.Line - 1, "character": 20},
				},
				SelectionRange: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len(d.Name)},
				},
			})
		case *compiler.EnumDecl:
			symbols = append(symbols, documentSymbol{
				Name:   d.Name,
				Detail: "enum",
				Kind:   10, // Enum
				Range: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": 0},
					"end":   map[string]int{"line": loc.Line - 1, "character": 20},
				},
				SelectionRange: map[string]any{
					"start": map[string]int{"line": loc.Line - 1, "character": loc.Col - 1},
					"end":   map[string]int{"line": loc.Line - 1, "character": loc.Col - 1 + len(d.Name)},
				},
			})
		}
	}
	return symbols
}

func isWord(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
