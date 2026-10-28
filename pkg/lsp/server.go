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
	"github.com/JohnEsleyer/ube/pkg/formatter"
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
					"hoverProvider":              true,
					"definitionProvider":         true,
					"documentSymbolProvider":     true,
					"documentFormattingProvider": true,
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
		case "textDocument/formatting":
			var p struct {
				TextDocument textDoc `json:"textDocument"`
			}
			_ = json.Unmarshal(m.Params, &p)
			text := openDocs[p.TextDocument.URI]
			if formatted, err := formatter.Format(p.TextDocument.URI, text); err == nil {
				lines := strings.Split(text, "\n")
				lastLine := len(lines) - 1
				lastCol := len(lines[lastLine])
				result = []map[string]any{
					{
						"range": map[string]any{
							"start": map[string]int{"line": 0, "character": 0},
							"end":   map[string]int{"line": lastLine, "character": lastCol},
						},
						"newText": formatted,
					},
				}
			} else {
				result = []any{}
			}
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
			err = compiler.Monomorphize(prog)
		}
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
