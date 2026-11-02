package compiler_test

import (
	"strings"
	"testing"
)

func TestV071_MonotonicTimeBuiltins(t *testing.T) {
	src := `
fn main() {
    let t0 = time_now_ms()
    let t1 = time_now_ns()
    println(t0 >= 0)
    println(t1 >= 0)
}
`
	out := runSourceNative(t, src)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 || lines[0] != "true" || lines[1] != "true" {
		t.Fatalf("unexpected monotonic time output: %q", out)
	}
}

func TestV071_StringInstanceMethods(t *testing.T) {
	src := `
fn main() {
    let s = "  hello world  "
    let trimmed = s.trim()
    println(trimmed)
    println(trimmed.starts_with("hello"))
    println(trimmed.ends_with("world"))
    println(trimmed.index_of("world"))

    let parts = trimmed.split(" ")
    println(parts.len)
    println(parts[0])
    println(parts[1])
}
`
	out := runSourceNative(t, src)
	expected := "hello world\ntrue\ntrue\n6\n2\nhello\nworld"
	if strings.TrimSpace(out) != expected {
		t.Fatalf("expected:\n%s\ngot:\n%s", expected, strings.TrimSpace(out))
	}
}

func TestV071_PromoteResultType(t *testing.T) {
	src := `
fn compute(ok: bool) -> string ! string {
    if ok {
        return "success"
    }
    return error("failure")
}

fn main() {
    let mut persistent: string ! string = error("")

    arena {
        let res = compute(true)
        persistent = promote(res)
    }

    println(persistent or_else "missing")
}
`
	out := runSourceNative(t, src)
	if strings.TrimSpace(out) != "success" {
		t.Fatalf("expected 'success', got: %q", strings.TrimSpace(out))
	}
}
