package compiler_test

import (
	"strings"
	"testing"
)

func TestFeatures_TraitsAndDynamicDispatch(t *testing.T) {
	src := `
	trait Greeter {
		fn greet() -> string
	}

	struct Human {
		name: string
	}

	fn (h Human) greet() -> string {
		return "Hello from " + h.name
	}

	fn say_it(g: Greeter) {
		println(g.greet())
	}

	fn main() {
		let h = Human{ name: "Alice" }
		let g: Greeter = h
		say_it(g)
	}
	`
	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("Trait test failed to compile: %v", err)
	}
	if got, want := runBinary(t, bin), "Hello from Alice"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFeatures_MonomorphizedGenerics(t *testing.T) {
	src := `
	struct Container[T] {
		item: T
	}

	fn get_val[T](c: Container[T]) -> T {
		return c.item
	}

	fn main() {
		let c1 = Container[i32]{ item: 99 }
		let c2 = Container[string]{ item: "sweet" }
		println(get_val[i32](c1))
		println(get_val[string](c2))
	}
	`
	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("Generics test failed to compile: %v", err)
	}
	if got, want := strings.TrimSpace(runBinary(t, bin)), "99\nsweet"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFeatures_StringBuilder(t *testing.T) {
	src := `
	fn main() {
		let mut sb = StringBuilder.new(8)
		sb.append("score: ")
		sb.append_int(100)
		println(sb.to_string())
	}
	`
	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("StringBuilder test failed to compile: %v", err)
	}
	if got, want := strings.TrimSpace(runBinary(t, bin)), "score: 100"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
