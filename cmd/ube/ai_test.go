package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAI_GuideCommand(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "ube_guide.md")

	err := execute([]string{"guide", "-o", outFile})
	if err != nil {
		t.Fatalf("execute guide failed: %v", err)
	}

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading guide file: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "Zero Garbage Collection") {
		t.Errorf("guide missing Zero GC explanation")
	}
	if !strings.Contains(text, "arena") || !strings.Contains(text, "promote") {
		t.Errorf("guide missing arena/promote section")
	}
	if !strings.Contains(text, "Chan[T]") {
		t.Errorf("guide missing channel explanation")
	}
}

func TestAI_PrimerAlias(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "primer.md")

	if err := execute([]string{"primer", "-o=" + outFile}); err != nil {
		t.Fatalf("execute primer failed: %v", err)
	}
	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading primer file: %v", err)
	}
	if !strings.Contains(string(content), "The Ube Programming Language") {
		t.Errorf("primer does not look like the Ube guide")
	}
}

func TestAI_ContextCommand(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "sample.ube")
	src := `
	struct Item {
		id: i32,
		name: string
	}

	fn helper(x: i32) -> i32 {
		return x * 2
	}

	fn main() {
		let res = helper(10)
		println(res)
		arena {
			let mut temp = []i32{}
			temp.push(42)
			let promoted = promote(temp)
		}
	}
	`
	if err := os.WriteFile(srcFile, []byte(src), 0644); err != nil {
		t.Fatalf("writing sample file: %v", err)
	}

	outFile := filepath.Join(tmpDir, "ube_context.txt")
	err := execute([]string{"context", srcFile, "-o", outFile})
	if err != nil {
		t.Fatalf("execute context failed: %v", err)
	}

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading context file: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "struct Item {") {
		t.Errorf("context missing struct Item")
	}
	if !strings.Contains(text, "fn helper(x: i32) -> i32") {
		t.Errorf("context missing fn helper signature")
	}
	if !strings.Contains(text, "calls: [helper, println]") {
		t.Errorf("context missing expected calls in main: got %s", text)
	}
	if !strings.Contains(text, "flow:  arena, promote") {
		t.Errorf("context missing flow tags: got %s", text)
	}
}

func TestAI_ContextDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	for name, src := range map[string]string{
		"a.ube": "enum Mode { On, Off }\n",
		"b.ube": "trait Named {\n    fn name() -> string\n}\n",
		"c.ube": "fn noop() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(src), 0644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	outFile := filepath.Join(tmpDir, "ctx.txt")
	if err := execute([]string{"context", tmpDir, "-o=" + outFile}); err != nil {
		t.Fatalf("execute context failed: %v", err)
	}

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading context file: %v", err)
	}
	text := string(content)
	for _, want := range []string{"enum Mode {", "trait Named {", "fn noop()"} {
		if !strings.Contains(text, want) {
			t.Errorf("context missing %q", want)
		}
	}
}

func TestAI_GitignoreIntegration(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// 1. When no .gitignore exists, ensureGitignore should succeed without creating one
	added, err := ensureGitignore("test_file.txt")
	if err != nil {
		t.Fatalf("ensureGitignore failed: %v", err)
	}
	if added {
		t.Errorf("expected added=false when no .gitignore exists")
	}
	if _, err := os.Stat(".gitignore"); err == nil {
		t.Errorf(".gitignore should not be created if none existed")
	}

	// 2. When .gitignore exists, ensure entry is appended without duplicates
	if err := os.WriteFile(".gitignore", []byte("/main\n*.c\n"), 0644); err != nil {
		t.Fatalf("writing .gitignore: %v", err)
	}

	added, err = ensureGitignore("ube_context.txt")
	if err != nil || !added {
		t.Fatalf("expected ensureGitignore to add entry, got added=%v err=%v", added, err)
	}

	giContent, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}
	if !strings.Contains(string(giContent), "ube_context.txt") {
		t.Errorf("expected ube_context.txt in .gitignore")
	}

	// 3. Running again should not duplicate entry
	added, err = ensureGitignore("ube_context.txt")
	if err != nil {
		t.Fatalf("second ensureGitignore failed: %v", err)
	}
	if added {
		t.Errorf("expected added=false for an entry that already exists")
	}
	giContent2, _ := os.ReadFile(".gitignore")
	if strings.Count(string(giContent2), "ube_context.txt") != 1 {
		t.Errorf("expected exactly 1 occurrence of ube_context.txt, got:\n%s", string(giContent2))
	}
}
