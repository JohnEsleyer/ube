package formatter_test

import (
	"strings"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/formatter"
)

func TestFormatter_BasicIndentation(t *testing.T) {
	input := `package main

fn main() {
let x = 10
if x > 5 {
println(x)
}
}`
	expected := `package main

fn main() {
    let x = 10
    if x > 5 {
        println(x)
    }
}
`

	got, err := formatter.Format("test.ube", input)
	if err != nil {
		t.Fatalf("unexpected formatting error: %v", err)
	}
	if got != expected {
		t.Errorf("formatted output mismatch:\nGOT:\n%s\nWANT:\n%s", got, expected)
	}
}

func TestFormatter_StructFieldAlignment(t *testing.T) {
	input := `package main

struct Player {
name: string
level: i32
health: i32
}`

	got, err := formatter.Format("test.ube", input)
	if err != nil {
		t.Fatalf("unexpected formatting error: %v", err)
	}

	// Columns should align colons and types evenly
	if !strings.Contains(got, "name:   string") {
		t.Errorf("expected aligned 'name:   string', got:\n%s", got)
	}
	if !strings.Contains(got, "level:  i32") {
		t.Errorf("expected aligned 'level:  i32', got:\n%s", got)
	}
	if !strings.Contains(got, "health: i32") {
		t.Errorf("expected aligned 'health: i32', got:\n%s", got)
	}
}

func TestFormatter_OperatorSpacing(t *testing.T) {
	input := `package main

fn calc() -> i32 {
    let a=1+2*3
    return a
}`
	got, err := formatter.Format("test.ube", input)
	if err != nil {
		t.Fatalf("unexpected formatting error: %v", err)
	}
	if !strings.Contains(got, "let a = 1 + 2 * 3") {
		t.Errorf("expected spaced binary operators, got:\n%s", got)
	}
}

func TestFormatter_SyntaxErrorRejection(t *testing.T) {
	broken := `package main

fn broken( {`
	_, err := formatter.Format("test.ube", broken)
	if err == nil {
		t.Fatal("expected syntax error when formatting invalid source, got nil")
	}
}
