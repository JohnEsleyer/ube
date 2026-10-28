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
