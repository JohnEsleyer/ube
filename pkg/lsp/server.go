// File: pkg/lsp/server.go

package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

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

// sendMu serialises all writes to stdout so goroutines don't interleave frames.
var sendMu sync.Mutex

// openDocs holds the latest text for every open file.
var (
	docsMu   sync.RWMutex
	openDocs = map[string]string{}
)

// analysisMu and analysisVersions let us cancel a pending analysis when a newer
// didChange arrives for the same URI before the previous one finishes.
// We pair each URI with a version counter so goroutines can detect staleness.
var (
	analysisMu      sync.Mutex
	analysisCancels = map[string]context.CancelFunc{}
	analysisVersion = map[string]uint64{}
)

var (
	locRE    = regexp.MustCompile(`(?:\[)?([^\]:]+):(\d+):(\d+)(?:\])?`)
	quotedRE = regexp.MustCompile(`'([^']+)'`)
)

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
					"signatureHelpProvider": map[string]any{
						"triggerCharacters": []string{"(", ","},
					},
				},
			}
		case "initialized":
		case "textDocument/didOpen":
			var p struct {
				TextDocument docItem `json:"textDocument"`
			}
			_ = json.Unmarshal(m.Params, &p)
			docsMu.Lock()
			openDocs[p.TextDocument.URI] = p.TextDocument.Text
			docsMu.Unlock()
			schedulePublish(p.TextDocument.URI, p.TextDocument.Text)
		case "textDocument/didChange":
			var p changeParams
			_ = json.Unmarshal(m.Params, &p)
			if len(p.ContentChanges) > 0 {
				text := p.ContentChanges[len(p.ContentChanges)-1].Text
				docsMu.Lock()
				openDocs[p.TextDocument.URI] = text
				docsMu.Unlock()

				// Immediately clear stale squiggles so they never appear "stuck".
				notify("textDocument/publishDiagnostics", map[string]any{
					"uri":         p.TextDocument.URI,
					"diagnostics": []any{},
				})

				schedulePublish(p.TextDocument.URI, text)
			}
		case "textDocument/didSave":
			var p struct {
				TextDocument textDoc `json:"textDocument"`
			}
			_ = json.Unmarshal(m.Params, &p)
			docsMu.RLock()
			text := openDocs[p.TextDocument.URI]
			docsMu.RUnlock()
			schedulePublish(p.TextDocument.URI, text)
		case "textDocument/didClose":
			var p struct {
				TextDocument textDoc `json:"textDocument"`
			}
			_ = json.Unmarshal(m.Params, &p)
			cancelAnalysis(p.TextDocument.URI)
			docsMu.Lock()
			delete(openDocs, p.TextDocument.URI)
			docsMu.Unlock()
			notify("textDocument/publishDiagnostics", map[string]any{
				"uri":         p.TextDocument.URI,
				"diagnostics": []any{},
			})
		case "textDocument/completion":
			var p struct {
				TextDocument textDoc     `json:"textDocument"`
				Position     lspPosition `json:"position"`
			}
			_ = json.Unmarshal(m.Params, &p)
			docsMu.RLock()
			src := openDocs[p.TextDocument.URI]
			docsMu.RUnlock()
			result = CompletionItems(src, p.Position)
		case "textDocument/hover":
			var p struct {
				TextDocument textDoc     `json:"textDocument"`
				Position     lspPosition `json:"position"`
			}
			_ = json.Unmarshal(m.Params, &p)
			docsMu.RLock()
			src := openDocs[p.TextDocument.URI]
			docsMu.RUnlock()
			result = Hover(p.TextDocument.URI, src, p.Position)
		case "textDocument/signatureHelp":
			var p struct {
				TextDocument textDoc     `json:"textDocument"`
				Position     lspPosition `json:"position"`
			}
			_ = json.Unmarshal(m.Params, &p)
			docsMu.RLock()
			src := openDocs[p.TextDocument.URI]
			docsMu.RUnlock()
			result = SignatureHelp(src, p.Position)
		case "textDocument/definition":
			var p struct {
				TextDocument textDoc     `json:"textDocument"`
				Position     lspPosition `json:"position"`
			}
			_ = json.Unmarshal(m.Params, &p)
			docsMu.RLock()
			src := openDocs[p.TextDocument.URI]
			docsMu.RUnlock()
			result = Definition(p.TextDocument.URI, src, p.Position)
		case "textDocument/documentSymbol":
			var p struct {
				TextDocument textDoc `json:"textDocument"`
			}
			_ = json.Unmarshal(m.Params, &p)
			docsMu.RLock()
			src := openDocs[p.TextDocument.URI]
			docsMu.RUnlock()
			result = DocumentSymbols(p.TextDocument.URI, src)
		case "textDocument/formatting":
			var p struct {
				TextDocument textDoc `json:"textDocument"`
			}
			_ = json.Unmarshal(m.Params, &p)
			docsMu.RLock()
			text := openDocs[p.TextDocument.URI]
			docsMu.RUnlock()
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

// send serialises a single LSP frame to stdout, guarded by sendMu so that
// goroutines running publish() cannot interleave with the main loop's replies.
func send(v any) {
	b, _ := json.Marshal(v)
	sendMu.Lock()
	fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(b), b)
	sendMu.Unlock()
}

func notify(method string, params any) {
	send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// schedulePublish cancels any in-flight analysis for uri and starts a new one
// in a background goroutine, so the LSP read loop is never blocked.
func schedulePublish(uri, source string) {
	ctx, cancel := context.WithCancel(context.Background())

	analysisMu.Lock()
	if prev, ok := analysisCancels[uri]; ok {
		prev() // cancel the previous run
	}
	analysisCancels[uri] = cancel
	ver := analysisVersion[uri] + 1
	analysisVersion[uri] = ver
	analysisMu.Unlock()

	go func() {
		defer func() {
			// Only clean up if we are still the active cancellation for this URI.
			analysisMu.Lock()
			if analysisVersion[uri] == ver {
				delete(analysisCancels, uri)
			}
			analysisMu.Unlock()
		}()
		publish(ctx, uri, source)
	}()
}

func cancelAnalysis(uri string) {
	analysisMu.Lock()
	if cancel, ok := analysisCancels[uri]; ok {
		cancel()
		delete(analysisCancels, uri)
	}
	analysisMu.Unlock()
}

func uriToPath(uri string) string {
	p := strings.TrimPrefix(uri, "file://")
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return p
}

// fileToURI converts a filesystem path to a file:// URI suitable for LSP
// Location and DocumentSymbol responses.
func fileToURI(file string) string {
	if strings.HasPrefix(file, "file://") {
		return file
	}
	p := filepath.ToSlash(filepath.Clean(file))
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

// shortName returns the trailing identifier of a possibly package-qualified
// name. "std:math.clamp" -> "clamp".
func shortName(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// defaultAlias derives the implicit import alias from a package path.
func defaultAlias(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".ube")
}

func declPackageOf(d compiler.Decl) string {
	switch decl := d.(type) {
	case *compiler.StructDecl:
		return decl.Package
	case *compiler.EnumDecl:
		return decl.Package
	case *compiler.TraitDecl:
		return decl.Package
	case *compiler.FnDecl:
		return decl.Package
	case *compiler.ExternFnDecl:
		return decl.Package
	default:
		return ""
	}
}

// extractWordAndQualifier returns the identifier under the cursor and, if it
// is the member of a qualified expression (a.b), the qualifier a.
func extractWordAndQualifier(line string, col int) (string, string) {
	if col > len(line) {
		col = len(line)
	}
	a, b := col, col
	for a > 0 && isWord(line[a-1]) {
		a--
	}
	for b < len(line) && isWord(line[b]) {
		b++
	}
	if a == b {
		return "", ""
	}
	word := line[a:b]
	qualifier := ""
	if a > 1 && line[a-1] == '.' {
		qa := a - 1
		for qa > 0 && isWord(line[qa-1]) {
			qa--
		}
		qualifier = line[qa : a-1]
	}
	return word, qualifier
}

// getProgram loads the document as a full package, overlaying unsaved buffer
// contents for the current file and every other open tab. If loading fails it
// falls back to parsing the current buffer alone so hover/definition remain
// available in the presence of unrelated compile errors.
func getProgram(uri, source string) (*compiler.Program, error) {
	filePath := uriToPath(uri)
	cleanFilePath := filepath.Clean(filePath)
	loader := compiler.NewModuleLoader()
	loader.FileReadOverlay = func(p string) ([]byte, bool) {
		cleanP := filepath.Clean(p)
		if uri != "" && cleanP == cleanFilePath {
			return []byte(source), true
		}
		docsMu.RLock()
		defer docsMu.RUnlock()
		for u, text := range openDocs {
			if filepath.Clean(uriToPath(u)) == cleanP {
				return []byte(text), true
			}
		}
		return nil, false
	}
	prog, err := loader.LoadSource(filePath, source)
	if err == nil {
		_ = compiler.Monomorphize(prog)
		return prog, nil
	}
	toks, lexErr := compiler.NewLexer(uri, source).Tokenize()
	if lexErr != nil {
		return nil, err
	}
	fileProg, parseErr := compiler.NewParser(toks).ParseProgram()
	if parseErr != nil {
		return nil, err
	}
	return fileProg, nil
}

// publish runs the full compiler pipeline and emits publishDiagnostics.
// It respects ctx so that a newer didChange can pre-empt an in-flight run.
func publish(ctx context.Context, uri, source string) {
	var diags []map[string]any

	filePath := uriToPath(uri)
	loader := compiler.NewModuleLoader()
	loader.FileReadOverlay = func(p string) ([]byte, bool) {
		cleanP := filepath.Clean(p)
		docsMu.RLock()
		defer docsMu.RUnlock()
		for u, text := range openDocs {
			if filepath.Clean(uriToPath(u)) == cleanP {
				return []byte(text), true
			}
		}
		return nil, false
	}
	prog, err := loader.LoadSource(filePath, source)

	// Check for cancellation after each heavy stage.
	if err == nil && ctx.Err() == nil {
		err = compiler.Monomorphize(prog)
	}
	if err == nil && ctx.Err() == nil {
		err = compiler.NewTypechecker().Check(prog)
	}

	// If a newer run superseded us, discard results — don't publish stale data.
	if ctx.Err() != nil {
		return
	}

	if err != nil {
		for _, errLine := range strings.Split(err.Error(), "\n") {
			errLine = strings.TrimSpace(errLine)
			if errLine == "" {
				continue
			}
			line, col, msg := 0, 0, errLine
			if m := locRE.FindStringSubmatch(errLine); len(m) > 0 {
				errFile := m[1]
				// Only report diagnostics that belong to this document
				if errFile != "" && errFile != uri && errFile != filePath && !strings.HasSuffix(filePath, errFile) {
					continue
				}
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

			// Derive a precise end column from the quoted token in the message
			// (e.g. "Undefined identifier 'amount'" → length 6) instead of the
			// fixed +3 offset that was always wrong for longer identifiers.
			endCol := col + 1 // safe minimum: underline at least one character
			if qm := quotedRE.FindStringSubmatch(msg); len(qm) > 1 {
				endCol = col - 1 + len([]rune(qm[1]))
			}

			diags = append(diags, map[string]any{
				"range": map[string]any{
					"start": map[string]int{"line": line - 1, "character": col - 1},
					"end":   map[string]int{"line": line - 1, "character": endCol},
				},
				"severity": 1,
				"source":   "ube",
				"message":  msg,
			})
		}
	}

	notify("textDocument/publishDiagnostics", map[string]any{"uri": uri, "diagnostics": diags})
}
