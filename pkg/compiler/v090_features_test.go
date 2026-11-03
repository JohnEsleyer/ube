package compiler_test

import (
	"strings"
	"testing"
)

func TestV090_NamespaceIsolation(t *testing.T) {
	src := `
import "std/math" as math

fn min(a: i32, b: i32) -> i32 {
    return 999
}

fn main() {
    let native_min = min(1, 2)
    let math_min = math.min(10, 20)
    println(native_min)
    println(math_min)
}
`
	out := runSourceNative(t, src)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || lines[0] != "999" || lines[1] != "10" {
		t.Fatalf("unexpected namespace output: %q", out)
	}
}

func TestV090_StringBytesConversion(t *testing.T) {
	src := `
fn main() {
    let s = "Hello"
    let bytes = s.bytes()
    println(bytes.len)
    println(bytes[0])
    println(bytes[1])

    if let reconstructed = string.from_bytes(bytes) {
        println(reconstructed)
    }

    let mut invalid = []u8{}
    invalid.push(255)
    invalid.push(255)
    let res = string.from_bytes(invalid)
    if let ok_str = res {
        println("unexpected ok")
    } else {
        println("rejected invalid utf8")
    }
}
`
	out := runSourceNative(t, src)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 5 || lines[0] != "5" || lines[1] != "72" || lines[2] != "101" || lines[3] != "Hello" || lines[4] != "rejected invalid utf8" {
		t.Fatalf("unexpected string bytes output: %q", out)
	}
}

func TestV090_IteratorTraitForIn(t *testing.T) {
	src := `
struct Counter {
    cur: i32,
    limit: i32
}

fn (mut c Counter) next() -> i32? {
    if c.cur >= c.limit {
        return none
    }
    let val = c.cur
    c.cur += 1
    return val
}

fn main() {
    let mut c = Counter{ cur: 1, limit: 4 }
    let mut sum = 0
    for n in c {
        sum += n
    }
    println(sum)
}
`
	out := runSourceNative(t, src)
	if strings.TrimSpace(out) != "6" {
		t.Fatalf("expected sum 6 (1+2+3), got: %q", out)
	}
}

func TestV090_WasiFdWriteBytes(t *testing.T) {
	src := `
import "std/wasi"

fn main() {
    let msg = "WASI stdout test\n"
    let res = wasi.fd_write_bytes(wasi.wasi_stdout(), msg.bytes())
    if let n = res {
        println(n > 0)
    }
}
`
	out := runSourceNative(t, src)
	if !strings.Contains(out, "WASI stdout test") || !strings.Contains(out, "true") {
		t.Fatalf("expected wasi fd_write_bytes to write stdout, got: %q", out)
	}
}
