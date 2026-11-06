package main

import (
	"strings"
	"testing"
)

// TestDeferRunsOnTryPropagation verifies that `?` runs every active defer
// before returning the error to the caller. This uses the embedded wasmtime
// runner so it exercises real execution even when wasmtime is not on PATH.
func TestDeferRunsOnTryPropagation(t *testing.T) {
	source := `package main

fn mark(s: string) {
    println(s)
}

fn boom() -> i32 ! string {
    return error("boom")
}

fn work() -> i32 ! string {
    defer mark("defer")
    let x = boom()?
    return x
}

fn main() {
    let r = work() or_else 7
    println(r)
}
`
	out := runSourceForTest(t, "defer_try", source)
	if got := strings.TrimSpace(out); got != "defer\n7" {
		t.Fatalf("expected defer to run before error propagation (\"defer\\n7\"), got %q", got)
	}
}
