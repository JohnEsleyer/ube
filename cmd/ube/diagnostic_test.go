package main

import "testing"

func TestClassifyTypeMismatch(t *testing.T) {
	class, fields := classifyMessage("Type mismatch: expected 'i32', got 'string'")
	if class != "type_mismatch" {
		t.Fatalf("class = %q", class)
	}
	if fields != "expected:i32|got:string" {
		t.Fatalf("fields = %q", fields)
	}
}

func TestClassifyUnscopedLoopAlloc(t *testing.T) {
	msg := "unscoped loop allocation: runaway bump in loop for 'scratch'"
	class, fields := classifyMessage(msg)
	if class != "unscoped_loop_alloc" {
		t.Fatalf("expected unscoped_loop_alloc, got %s", class)
	}
	if fields != "var:scratch" {
		t.Fatalf("expected var:scratch, got %s", fields)
	}
}

func TestClassifyViewDiagnostics(t *testing.T) {
	class, fields := classifyMessage("cannot return view parameter 'tokens' (views cannot escape the call frame)")
	if class != "escape_error" || fields != "view:tokens" {
		t.Fatalf("expected escape_error|view:tokens, got %s|%s", class, fields)
	}

	class, fields = classifyMessage("cannot store view parameter 'tokens' into struct field 'items' (views are second-class)")
	if class != "second_class_violation" || fields != "view:tokens" {
		t.Fatalf("expected second_class_violation|view:tokens, got %s|%s", class, fields)
	}

	class, fields = classifyMessage("cannot use 'move' when passing to view parameter 'data'")
	if class != "invalid_move" || fields != "param:data" {
		t.Fatalf("expected invalid_move|param:data, got %s|%s", class, fields)
	}
}
