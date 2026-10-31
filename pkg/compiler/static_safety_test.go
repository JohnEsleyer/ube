package compiler_test

import (
	"strings"
	"testing"
)

func TestStaticSafety_MoveOnAssignmentRejected(t *testing.T) {
	src := `
	struct Resource {
		id: i32
	}
	fn main() {
		let a = Resource{id: 42}
		let b = a
		let c = a.id
	}
	`
	err := typecheckSource(t, src)
	if err == nil {
		t.Fatalf("expected compile error for use after move on assignment, got nil")
	}
	if !strings.Contains(err.Error(), "use of moved variable 'a'") {
		t.Fatalf("expected 'use of moved variable a', got: %v", err)
	}
}

func TestStaticSafety_ReassignAfterMoveValid(t *testing.T) {
	src := `
	struct Resource {
		id: i32
	}
	fn main() {
		let mut a = Resource{id: 1}
		let b = a
		a = Resource{id: 2}
		let c = a.id
	}
	`
	if err := typecheckSource(t, src); err != nil {
		t.Fatalf("reassignment after move should be valid, got: %v", err)
	}
}

func TestStaticSafety_ChannelSendMovesVariable(t *testing.T) {
	src := `
	struct Packet {
		val: i32
	}
	fn main() {
		let ch = Chan[Packet](1)
		let p = Packet{val: 100}
		ch.send(p)
		let x = p.val
	}
	`
	err := typecheckSource(t, src)
	if err == nil {
		t.Fatalf("expected compile error when reading variable after channel send, got nil")
	}
	if !strings.Contains(err.Error(), "use of moved variable 'p'") {
		t.Fatalf("expected use of moved variable error, got: %v", err)
	}
}

func TestStaticSafety_PoolFreeMovesHandle(t *testing.T) {
	src := `
	struct Bullet {
		x: f32
	}
	fn main() {
		let mut bullets = Pool[Bullet](16)
		let h = bullets.alloc(Bullet{x: 10.0})
		bullets.free(h)
		let b = bullets.get(h)
	}
	`
	err := typecheckSource(t, src)
	if err == nil {
		t.Fatalf("expected compile error when using handle after pool.free, got nil")
	}
	if !strings.Contains(err.Error(), "use of moved variable 'h'") {
		t.Fatalf("expected use of moved variable error, got: %v", err)
	}
}

func TestStaticSafety_PoolDoubleFreeRejected(t *testing.T) {
	src := `
	struct Bullet {
		x: f32
	}
	fn main() {
		let mut bullets = Pool[Bullet](16)
		let h = bullets.alloc(Bullet{x: 10.0})
		bullets.free(h)
		bullets.free(h)
	}
	`
	err := typecheckSource(t, src)
	if err == nil {
		t.Fatalf("expected compile error on double free, got nil")
	}
	if !strings.Contains(err.Error(), "use of moved variable 'h'") {
		t.Fatalf("expected use of moved variable error, got: %v", err)
	}
}

func TestStaticSafety_DisjointMutableArgumentsEnforced(t *testing.T) {
	src := `
	struct Counter {
		val: i32
	}
	fn swap_or_add(mut a: Counter, mut b: Counter) {
		a.val += b.val
	}
	fn main() {
		let mut c = Counter{val: 10}
		swap_or_add(mut c, mut c)
	}
	`
	err := typecheckSource(t, src)
	if err == nil {
		t.Fatalf("expected compile error when passing same variable to two mutable parameters, got nil")
	}
	if !strings.Contains(err.Error(), "aliasing violation") {
		t.Fatalf("expected aliasing violation error, got: %v", err)
	}
}

func TestStaticSafety_DisjointReceiverAndArgumentEnforced(t *testing.T) {
	src := `
	struct Node {
		val: i32
	}
	fn (mut n Node) merge(other: Node) {
		n.val += other.val
	}
	fn main() {
		let mut my_node = Node{val: 5}
		my_node.merge(my_node)
	}
	`
	err := typecheckSource(t, src)
	if err == nil {
		t.Fatalf("expected compile error when passing mutable receiver as call argument, got nil")
	}
	if !strings.Contains(err.Error(), "aliasing violation") {
		t.Fatalf("expected aliasing violation error, got: %v", err)
	}
}

func TestStaticSafety_LoopUseAfterMoveRejected(t *testing.T) {
	src := `
	struct Item {
		id: i32
	}
	fn consume(it: Item) {}
	fn main() {
		let x = Item{id: 99}
		let mut i = 0
		while i < 3 {
			consume(x)
			i += 1
		}
	}
	`
	err := typecheckSource(t, src)
	if err == nil {
		t.Fatalf("expected compile error when moving outer affine variable inside a loop, got nil")
	}
	if !strings.Contains(err.Error(), "cannot move variable 'x' inside a loop") {
		t.Fatalf("expected loop move error, got: %v", err)
	}
}

func TestStaticSafety_ArenaExpressionWithoutPromoteRejected(t *testing.T) {
	src := `
	fn main() {
		let s = arena {
			let mut items = []i32{}
			items.push(10)
			items
		}
	}
	`
	err := typecheckSource(t, src)
	if err == nil {
		t.Fatalf("expected compile error for unpromoted arena escape, got nil")
	}
	if !strings.Contains(err.Error(), "arena expression cannot return unpromoted value") {
		t.Fatalf("expected unpromoted arena return error, got: %v", err)
	}
}

func TestStaticSafety_ArenaExpressionWithPromoteValid(t *testing.T) {
	src := `
	fn main() {
		let s = arena {
			let mut items = []i32{}
			items.push(10)
			promote(items)
		}
		println(s.len)
	}
	`
	if err := typecheckSource(t, src); err != nil {
		t.Fatalf("promoted arena expression should compile cleanly, got: %v", err)
	}
}

func TestStaticSafety_SliceMoveOnAssignmentRejected(t *testing.T) {
	src := `
	fn main() {
		let a = []i32{1, 2, 3}
		let b = a
		println(a.len)
	}
	`
	err := typecheckSource(t, src)
	if err == nil {
		t.Fatalf("expected compile error for use after slice move, got nil")
	}
	if !strings.Contains(err.Error(), "use of moved variable 'a'") {
		t.Fatalf("expected use of moved variable error, got: %v", err)
	}
}
