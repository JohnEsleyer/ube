package compiler_test

import (
	"strings"
	"testing"
)

func TestPhilosophy_NoneLiteralForOptional(t *testing.T) {
	src := `
fn find_user(active: bool) -> string? {
	if active {
		return "alex"
	}
	return none
}

fn main() {
	let u = find_user(false)
	if let name = u {
		println(name)
	} else {
		println("no user")
	}
}
`
	out := runSourceNative(t, src)
	if !strings.Contains(out, "no user") {
		t.Fatalf("expected 'no user', got %q", out)
	}
}

func TestPhilosophy_AnonymousStructEmbedding(t *testing.T) {
	src := `
struct Transform {
	x: i32,
	y: i32
}

struct Entity {
	id: i32,
	Transform
}

fn main() {
	let e = Entity{
		id: 42,
		Transform: Transform{ x: 10, y: 20 }
	}
	println(e.id)
	println(e.Transform.x)
}
`
	out := runSourceNative(t, src)
	if !strings.Contains(out, "42") || !strings.Contains(out, "10") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestPhilosophy_CompoundTraitBounds(t *testing.T) {
	src := `
trait Reader {
	fn read() -> string
}

trait Resetter {
	fn reset()
}

struct Device {
	msg: string
}

fn (d Device) read() -> string {
	return d.msg
}

fn (d Device) reset() {
	println("resetting")
}

struct Consumer[T: Reader + Resetter] {
	target: T
}

fn main() {
	let d = Device{ msg: "sensor ready" }
	let c = Consumer[Device]{ target: d }
	println(c.target.read())
	c.target.reset()
}
`
	out := runSourceNative(t, src)
	if !strings.Contains(out, "sensor ready") || !strings.Contains(out, "resetting") {
		t.Fatalf("unexpected output: %q", out)
	}
}
