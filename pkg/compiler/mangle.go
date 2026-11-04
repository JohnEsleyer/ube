// File: pkg/compiler/mangle.go
package compiler

import "strings"

// mangleTypeName sanitizes a human-readable type string (as produced by
// UbeType.String()) into a valid identifier-safe token, suitable for use in
// monomorphized symbol names. Called from monomorphize.go's specializeName.
func mangleTypeName(s string) string {
	if s == "" {
		return "void"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r == '[' || r == ']':
			b.WriteRune('_')
		case r == '.' || r == ' ' || r == '<' || r == '>' || r == ',' || r == '?' || r == '!':
			b.WriteRune('_')
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	// Collapse runs of underscores.
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	out = strings.Trim(out, "_")
	if out == "" {
		return "void"
	}
	return out
}
