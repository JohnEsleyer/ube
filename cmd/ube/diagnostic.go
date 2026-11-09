package main

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reExpectedGot      = regexp.MustCompile(`expected '([^']+)', got '([^']+)'`)
	reQuotedVar        = regexp.MustCompile(`'([^']+)'`)
	reViewParamMessage = regexp.MustCompile(`passing to view parameter '([^']+)'`)
)

// formatDiagnosticAI renders compiler errors as dense one-line-per-error
// diagnostics suitable for feeding back to an LLM:
//
//	ERR|file:line:col|class|k=v|k=v
//
// Class and key-value fields are inferred from the message text. The compiler
// does not currently tag errors with structured metadata, so this is
// best-effort. If the compiler gains a TaggedError type, this function should
// read the tag directly and stop guessing.
func formatDiagnosticAI(rawErr error) string {
	if rawErr == nil {
		return ""
	}
	var lines []string
	for _, l := range strings.Split(rawErr.Error(), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		m := errLocPattern.FindStringSubmatch(l)
		if len(m) != 5 {
			lines = append(lines, "RAW|"+l)
			continue
		}
		file, line, col, msg := m[1], m[2], m[3], m[4]
		class, fields := classifyMessage(msg)
		out := fmt.Sprintf("ERR|%s:%s:%s|%s", file, line, col, class)
		if fields != "" {
			out += "|" + fields
		}
		lines = append(lines, out)
	}
	return strings.Join(lines, "\n")
}

// classifyMessage maps a compiler message string to a class tag and optional
// key-value fields. Unrecognized messages fall through to class "error".
func classifyMessage(msg string) (class, fields string) {
	switch {
	case strings.HasPrefix(msg, "Type mismatch"):
		if m := reExpectedGot.FindStringSubmatch(msg); m != nil {
			return "type_mismatch", "expected:" + m[1] + "|got:" + m[2]
		}
		return "type_mismatch", ""
	case strings.Contains(msg, "without explicit 'move'"):
		if m := reQuotedVar.FindStringSubmatch(msg); m != nil {
			return "move_required", "var:" + m[1]
		}
		return "move_required", ""
	case strings.Contains(msg, "use of moved value"):
		if m := reQuotedVar.FindStringSubmatch(msg); m != nil {
			return "use_after_move", "var:" + m[1]
		}
		return "use_after_move", ""
	case strings.Contains(msg, "cannot return view parameter"):
		if m := reQuotedVar.FindStringSubmatch(msg); m != nil {
			return "escape_error", "view:" + m[1]
		}
		return "escape_error", ""
	case strings.Contains(msg, "cannot store view parameter"):
		if m := reQuotedVar.FindStringSubmatch(msg); m != nil {
			return "second_class_violation", "view:" + m[1]
		}
		return "second_class_violation", ""
	case strings.Contains(msg, "cannot use 'move' when passing to view"):
		if m := reViewParamMessage.FindStringSubmatch(msg); m != nil {
			return "invalid_move", "param:" + m[1]
		}
		return "invalid_move", ""
	case strings.Contains(msg, "cannot move from view parameter"):
		if m := reQuotedVar.FindStringSubmatch(msg); m != nil {
			return "move_from_view", "var:" + m[1]
		}
		return "move_from_view", ""
	case strings.Contains(msg, "cannot assign to immutable view"):
		if m := reQuotedVar.FindStringSubmatch(msg); m != nil {
			return "immutable_reassign", "var:" + m[1]
		}
		return "immutable_reassign", ""
	case strings.Contains(msg, "cannot capture view parameter"):
		if m := reQuotedVar.FindStringSubmatch(msg); m != nil {
			return "second_class_violation", "view:" + m[1]
		}
		return "second_class_violation", ""
	case strings.Contains(msg, "immutable"):
		if m := reQuotedVar.FindStringSubmatch(msg); m != nil {
			return "immutable_reassign", "var:" + m[1]
		}
		return "immutable_reassign", ""
	case strings.Contains(msg, "arena"):
		return "arena_escape", ""
	case strings.Contains(msg, "not flat"):
		return "flat_violation", ""
	case strings.Contains(msg, "cyclic import"):
		return "cycle", ""
	case strings.Contains(msg, "unknown function"),
		strings.HasPrefix(msg, "undefined"),
		strings.Contains(msg, "not declared"):
		if m := reQuotedVar.FindStringSubmatch(msg); m != nil {
			return "undefined_symbol", "name:" + m[1]
		}
		return "undefined_symbol", ""
	case strings.Contains(msg, "unknown trait"),
		strings.Contains(msg, "does not satisfy trait"):
		if m := reQuotedVar.FindStringSubmatch(msg); m != nil {
			return "trait_mismatch", "trait:" + m[1]
		}
		return "trait_mismatch", ""
	default:
		return "error", ""
	}
}
