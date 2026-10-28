package lsp

import (
	"fmt"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

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
		"file_exists":     "`fn file_exists(path: string) -> bool`\n*Returns true if file exists at path.*",
		"path_join":       "`fn path_join(a: string, b: string) -> string`\n*Joins two path segments with a slash separator.*",
		"str_replace":     "`fn str_replace(s: string, old: string, new: string) -> string`\n*Replaces occurrences of old with new in string.*",
		"dir_list":        "`fn dir_list(path: string) -> []string ! string`\n*Lists files and directories at path.*",
		"dir_create":      "`fn dir_create(path: string) -> bool ! string`\n*Creates a directory (including parent directories).*",
		"file_remove":     "`fn file_remove(path: string) -> bool ! string`\n*Deletes a file.*",
		"env_set":         "`fn env_set(key: string, val: string) -> bool`\n*Sets an environment variable.*",
		"cmd_run":         "`fn cmd_run(cmd: string) -> i32`\n*Executes a system shell command and returns the exit status code.*",
		"abs":             "`fn abs(n: i64) -> i64`\n*Returns the absolute value of an integer.*",
		"min":             "`fn min(a: i64, b: i64) -> i64`\n*Returns the minimum of two integers.*",
		"max":             "`fn max(a: i64, b: i64) -> i64`\n*Returns the maximum of two integers.*",
		"sqrt":            "`fn sqrt(f: f64) -> f64`\n*Returns the square root of a floating-point number.*",
		"pow":             "`fn pow(b: f64, e: f64) -> f64`\n*Returns base raised to the power exp.*",
		"floor":           "`fn floor(f: f64) -> f64`\n*Returns the largest integer less than or equal to f.*",
		"ceil":            "`fn ceil(f: f64) -> f64`\n*Returns the smallest integer greater than or equal to f.*",
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
