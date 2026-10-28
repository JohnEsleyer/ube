package compiler_test

import (
	"strings"
	"testing"
)

// TEST 1: Array-to-Slice Coercion when passing to functions
func TestV053_ArrayToSlicePassing(t *testing.T) {
	src := `fn sum_slice(nums: []i32) -> i32 {
		let mut sum = 0
		for n in nums {
			sum = sum + n
		}
		return sum
	}

	fn main() {
		let arr = [4]i32{10, 20, 30, 40}
		println(sum_slice(arr))
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("Array to slice passing failed: %v", err)
	}
	if got, want := runBinary(t, bin), "100"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TEST 2: Array-to-Slice Assignment and Return
func TestV053_ArrayToSliceAssignmentAndReturn(t *testing.T) {
	src := `fn get_slice() -> []i32 {
		let arr = [3]i32{1, 2, 3}
		return arr
	}

	fn main() {
		let arr = [2]i32{5, 9}
		let s: []i32 = arr
		println(s[0])
		println(s[1])

		let s2 = get_slice()
		println(s2[2])
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("Array to slice assignment failed: %v", err)
	}
	if got, want := runBinary(t, bin), "5\n9\n3"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TEST 3: New Standard Builtins: file_exists, path_join, str_replace
func TestV053_NewStandardBuiltins(t *testing.T) {
	src := `fn main() {
		let joined = path_join("usr/local", "bin/ube")
		println(joined)

		let replaced = str_replace("sweet, sweet language", "sweet", "grounded")
		println(replaced)

		let exists = file_exists("non_existent_file_xyz.txt")
		println(exists)
	}`

	bin, _, err := compileToNativeBinary(t, src)
	if err != nil {
		t.Fatalf("Builtins test failed: %v", err)
	}
	if got, want := strings.TrimSpace(runBinary(t, bin)), "usr/local/bin/ube\ngrounded, grounded language\nfalse"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
