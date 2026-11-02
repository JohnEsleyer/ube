package lsp

import (
	"strings"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

type paramInfo struct {
	Label string `json:"label"`
}

type signatureInformation struct {
	Label      string      `json:"label"`
	Parameters []paramInfo `json:"parameters"`
}

func SignatureHelp(source string, p lspPosition) any {
	lines := strings.Split(source, "\n")
	if int(p.Line) >= len(lines) {
		return nil
	}
	line := lines[p.Line]
	col := int(p.Character)
	if col > len(line) {
		col = len(line)
	}
	sub := line[:col]

	lastOpen := strings.LastIndex(sub, "(")
	if lastOpen == -1 {
		return nil
	}

	fnPart := strings.TrimSpace(sub[:lastOpen])
	fnName := ""
	for i := len(fnPart) - 1; i >= 0; i-- {
		ch := fnPart[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			fnName = string(ch) + fnName
		} else {
			break
		}
	}
	if fnName == "" {
		return nil
	}

	commaCount := strings.Count(sub[lastOpen:], ",")

	builtinSignatures := map[string][]string{
		"println":         {"val: any", "..."},
		"print":           {"val: any", "..."},
		"format":          {"fmt: string", "..."},
		"time_now_ms":     {},
		"time_now_ns":     {},
		"sleep_ms":        {"ms: i32"},
		"yield":           {},
		"exit_process":    {"code: i32"},
		"str_contains":    {"s: string", "substr: string"},
		"str_starts_with": {"s: string", "prefix: string"},
		"str_ends_with":   {"s: string", "suffix: string"},
		"str_trim":        {"s: string"},
		"str_split":       {"s: string", "delim: string"},
		"str_replace":     {"s: string", "old: string", "new: string"},
		"str_index_of":    {"s: string", "substr: string"},
		"int_to_str":      {"val: i64"},
		"str_to_int":      {"s: string"},
		"read_file":       {"path: string"},
		"write_file":      {"path: string", "content: string"},
		"env_get":         {"key: string"},
		"env_set":         {"key: string", "val: string"},
		"trim":            {},
		"starts_with":     {"prefix: string"},
		"ends_with":       {"suffix: string"},
		"index_of":        {"substr: string"},
		"split":           {"delim: string"},
	}

	var params []paramInfo
	var label string

	if bParams, exists := builtinSignatures[fnName]; exists {
		for _, param := range bParams {
			params = append(params, paramInfo{Label: param})
		}
		label = fnName + "(" + strings.Join(bParams, ", ") + ")"
	} else {
		tokens, err := compiler.NewLexer("", source).Tokenize()
		if err == nil {
			if prog, err := compiler.NewParser(tokens).ParseProgram(); err == nil {
				for _, decl := range prog.Decls {
					if fn, ok := decl.(*compiler.FnDecl); ok && fn.Name == fnName {
						var paramLabels []string
						for _, param := range fn.Params {
							pStr := param.Name + ": " + param.Type.String()
							params = append(params, paramInfo{Label: pStr})
							paramLabels = append(paramLabels, pStr)
						}
						ret := ""
						if fn.RetType != nil && fn.RetType.String() != "void" {
							ret = " -> " + fn.RetType.String()
						}
						label = "fn " + fnName + "(" + strings.Join(paramLabels, ", ") + ")" + ret
						break
					}
				}
			}
		}
	}

	if label == "" {
		return nil
	}

	activeParam := uint32(commaCount)
	if activeParam >= uint32(len(params)) && len(params) > 0 {
		activeParam = uint32(len(params) - 1)
	}

	return map[string]any{
		"signatures": []signatureInformation{
			{
				Label:      label,
				Parameters: params,
			},
		},
		"activeSignature": 0,
		"activeParameter": activeParam,
	}
}
