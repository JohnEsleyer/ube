package formatter

import (
	"strings"
	"testing"
)

func TestFormatCanonicalSpacing(t *testing.T) {
	input := "fn test(  a:i32,b:string   )->i32{\nlet x=10+20\nreturn x\n}\n"
	got, err := Format("test.ube", input)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	for _, want := range []string{
		"fn test(a: i32, b: string) -> i32 {",
		"    let x = 10 + 20",
		"    return x",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted output missing %q:\n%s", want, got)
		}
	}
}

func TestFormatIsIdempotentAndPreservesComments(t *testing.T) {
	input := `// header
struct Pair[A,B]{first:A,second:B}

fn f(items:[]i32)->[]i32{
// body comment
let x=items[0] // trailing
return items
}
`
	first, err := Format("test.ube", input)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	second, err := Format("test.ube", first)
	if err != nil {
		t.Fatalf("Format (second pass): %v", err)
	}
	if first != second {
		t.Fatalf("formatter is not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if !strings.Contains(first, "// header") || !strings.Contains(first, "// body comment") || !strings.Contains(first, "// trailing") {
		t.Fatalf("comments were not preserved:\n%s", first)
	}
	if !strings.Contains(first, "items: []i32") {
		t.Fatalf("slice type spacing is wrong:\n%s", first)
	}
}

func TestFormatStructColumnAlignmentAndTags(t *testing.T) {
	input := "struct UserProfile {\n" +
		"id: i32 `json:\"id\"`\n" +
		"pub name: string `json:\"name\"`\n" +
		"pub mut is_active: bool\n" +
		"}"

	got, err := Format("test.ube", input)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}

	// Verify columnar alignment: all field types should start at the same column
	// (i.e. the space after each padded "name:" header is aligned).
	formattedLines := strings.Split(strings.TrimSpace(got), "\n")
	var fieldLines []string
	for _, l := range formattedLines {
		trimmed := strings.TrimSpace(l)
		if strings.Contains(trimmed, ":") && !strings.HasPrefix(trimmed, "struct") && !strings.HasPrefix(trimmed, "}") {
			fieldLines = append(fieldLines, l)
		}
	}

	if len(fieldLines) != 3 {
		t.Fatalf("expected 3 field lines, got %d:\n%s", len(fieldLines), got)
	}

	// Find the column at which the type starts (first non-space char after ": ").
	typeStartCol := func(line string) int {
		colonIdx := strings.LastIndex(line, ": ")
		if colonIdx == -1 {
			return -1
		}
		for j := colonIdx + 2; j < len(line); j++ {
			if line[j] != ' ' {
				return j
			}
		}
		return -1
	}

	col0 := typeStartCol(fieldLines[0])
	for i := 1; i < len(fieldLines); i++ {
		col := typeStartCol(fieldLines[i])
		if col != col0 {
			t.Fatalf("type column not aligned: line 0 col %d, line %d col %d\n%s", col0, i, col, got)
		}
	}

	// Raw string tags should be present.
	if !strings.Contains(got, "`json:\"id\"`") {
		t.Fatalf("expected raw tag json:\"id\" to be preserved:\n%s", got)
	}
	if !strings.Contains(got, "`json:\"name\"`") {
		t.Fatalf("expected raw tag json:\"name\" to be preserved:\n%s", got)
	}
}
