// File: pkg/lsp/signature.go

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

	// Recover the qualified callee (alias.member) from the text before '('.
	// The scan above stops at '.', so math.clamp would otherwise be seen as
	// clamp only.
	if q := strings.TrimSpace(fnPart); q != "" {
		start := len(q)
		for start > 0 {
			c := q[start-1]
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' {
				start--
			} else {
				break
			}
		}
		if start < len(q) {
			fnName = q[start:]
		}
	}

	commaCount := strings.Count(sub[lastOpen:], ",")

	if strings.Contains(fnName, ".") {
		if prog, err := getProgram("", source); err == nil && prog != nil {
			parts := strings.SplitN(fnName, ".", 2)
			qualifier, member := parts[0], parts[1]
			canonicalPkg := prog.ModuleAliases[qualifier]
			if canonicalPkg == "" {
				canonicalPkg = qualifier
			}
			for _, decl := range prog.Decls {
				fn, ok := decl.(*compiler.FnDecl)
				if !ok {
					continue
				}
				if fn.Name != canonicalPkg+"."+member && !(fn.Package == canonicalPkg && shortName(fn.Name) == member) {
					continue
				}
				var params []paramInfo
				var labels []string
				for _, param := range fn.Params {
					prefix := ""
					if param.IsMut {
						prefix = "mut "
					} else if param.IsView {
						prefix = "view "
					}
					pStr := prefix + param.Name + ": " + param.Type.String()
					params = append(params, paramInfo{Label: pStr})
					labels = append(labels, pStr)
				}
				ret := ""
				if fn.RetType != nil && fn.RetType.String() != "void" {
					ret = " -> " + fn.RetType.String()
				}
				active := uint32(commaCount)
				if active >= uint32(len(params)) && len(params) > 0 {
					active = uint32(len(params) - 1)
				}
				return map[string]any{
					"signatures": []signatureInformation{
						{
							Label:      "fn " + fnName + "(" + strings.Join(labels, ", ") + ") " + ret,
							Parameters: params,
						},
					},
					"activeSignature": 0,
					"activeParameter": active,
				}
			}
		}
	}

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
		"clamp":           {"val: i32", "low: i32", "high: i32"},
	}

	var params []paramInfo
	var label string

	// Strip a package qualifier so math.clamp resolves to the clamp entry.
	lookupKey := fnName
	if i := strings.LastIndexByte(lookupKey, '.'); i >= 0 {
		lookupKey = lookupKey[i+1:]
	}

	if bParams, exists := builtinSignatures[lookupKey]; exists {
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
							prefix := ""
							if param.IsMut {
								prefix = "mut "
							} else if param.IsView {
								prefix = "view "
							}
							pStr := prefix + param.Name + ": " + param.Type.String()
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
