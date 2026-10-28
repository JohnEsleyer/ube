package compiler_test

import (
	"strings"
	"testing"
)

func TestPhase1_UseAfterMoveRejected(t *testing.T) {
	src := `struct Resource {
		id: i32
	}

	fn consume(r: Resource) {
		println(r.id)
	}

	fn main() {
		let res = Resource{ id: 42 }
		consume(res)
		println(res.id)
	}`

	err := typecheckSource(t, src)
	if err == nil {
		t.Fatal("expected compiler to reject use of moved value 'res'")
	}
	if !strings.Contains(err.Error(), "use of moved value 'res'") {
		t.Fatalf("expected 'use of moved value' diagnostic, got: %v", err)
	}
}

func TestPhase1_CopyTypesNotMoved(t *testing.T) {
	src := `fn consume_int(x: i32) {
		println(x)
	}

	fn main() {
		let n = 100
		consume_int(n)
		println(n)
	}`

	err := typecheckSource(t, src)
	if err != nil {
		t.Fatalf("primitive copy types should not trigger move errors: %v", err)
	}
}

func TestPhase1_ReassignAfterMoveValid(t *testing.T) {
	src := `struct Resource {
		id: i32
	}

	fn consume(r: Resource) {
		println(r.id)
	}

	fn main() {
		let mut res = Resource{ id: 10 }
		consume(res)
		res = Resource{ id: 20 }
		println(res.id)
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("reassigned variable after move should be valid: %v", err)
	}
	if got, want := runBinary(t, bin), "10\n20"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
