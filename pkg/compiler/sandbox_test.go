package compiler_test

import (
	"strings"
	"testing"
)

// The language is permanently sandboxed: these constructs must be rejected
// before any code generation can happen.
func TestSandbox_ForbiddenLowLevelConstructs(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "unsafe block",
			src:  "fn main() {\n\tunsafe {\n\t\tprintln(\"nope\")\n\t}\n}",
			want: "'unsafe' is forbidden",
		},
		{
			name: "unsafe expression",
			src:  "fn main() {\n\tlet x = unsafe { 1 }\n}",
			want: "'unsafe' expressions are forbidden",
		},
		{
			name: "raw pointer type",
			src:  "extern fn puts(s: *u8) -> i32\nfn main() {}",
			want: "raw pointers '*T' are forbidden",
		},
		{
			name: "address of",
			src:  "fn main() {\n\tlet x = 1\n\tlet p = &x\n}",
			want: "address-of operator '&' is forbidden",
		},
		{
			name: "pointer dereference",
			src:  "fn main() {\n\tlet x = 1\n\tlet p = *x\n}",
			want: "pointer dereference '*' is forbidden",
		},
		{
			name: ".ptr member",
			src:  "fn main() {\n\tlet s = \"hello\"\n\tlet p = s.ptr\n}",
			want: "'.ptr' is forbidden",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compilePipeline(t, tc.src)
			if err == nil {
				t.Fatalf("expected %q to be rejected, got nil error", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got: %v", tc.want, err)
			}
		})
	}
}

func TestSandbox_ExternHostImportNeedsNoUnsafe(t *testing.T) {
	src := `extern fn host_print(msg: string)

fn main() {
	host_print("sandboxed")
}`
	if _, err := compilePipeline(t, src); err != nil {
		t.Fatalf("Wasm host imports must be callable without unsafe: %v", err)
	}
}
