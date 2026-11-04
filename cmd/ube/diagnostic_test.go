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
