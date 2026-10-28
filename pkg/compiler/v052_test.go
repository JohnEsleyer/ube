package compiler_test

import (
	"testing"
)

func TestV052_BitwiseOperators(t *testing.T) {
	src := `fn main() {
		let a = 0b1100
		let b = 0b1010
		println(a & b)
		println(a | b)
		println(a ^ b)
		println(a << 1)
		println(a >> 1)
		println(~0 & 0xFF)
	}`
	out := runSourceNative(t, src)
	want := "8\n14\n6\n24\n6\n255"
	if out != want {
		t.Fatalf("expected:\n%s\ngot:\n%s", want, out)
	}
}

func TestV052_MultiArgPrint(t *testing.T) {
	src := `fn main() {
		println("Hello", 42, true)
		print(1, 2, 3)
		println()
	}`
	out := runSourceNative(t, src)
	want := "Hello 42 true\n1 2 3"
	if out != want {
		t.Fatalf("expected:\n%s\ngot:\n%s", want, out)
	}
}

func TestV052_NewStringBuiltins(t *testing.T) {
	src := `fn main() {
		let s = "  ube systems  "
		println(str_trim(s))
		println(str_index_of("banana", "nan"))
		let parts = str_split("a,b,c", ",")
		for p in parts {
			println(p)
		}
	}`
	out := runSourceNative(t, src)
	want := "ube systems\n2\na\nb\nc"
	if out != want {
		t.Fatalf("expected:\n%s\ngot:\n%s", want, out)
	}
}

func TestV052_EnvGet(t *testing.T) {
	src := `fn main() {
		let value = env_get("UBE_V052_TEST_ENV") or_else "missing"
		println(value)
	}`
	t.Setenv("UBE_V052_TEST_ENV", "present")
	out := runSourceNative(t, src)
	if out != "present" {
		t.Fatalf("expected present, got %q", out)
	}
}

func TestV052_LoopDefersRunOnBreakAndContinue(t *testing.T) {
	src := `fn note(label: string) { println(label) }
	fn main() {
		let mut i = 0
		while i < 3 {
			defer note("loop")
			if i == 1 {
				i = i + 1
				continue
			}
			if i == 2 {
				break
			}
			i = i + 1
		}
		println("done")
	}`
	out := runSourceNative(t, src)
	want := "loop\nloop\nloop\ndone"
	if out != want {
		t.Fatalf("expected:\n%s\ngot:\n%s", want, out)
	}
}
