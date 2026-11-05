package compiler_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func writeUbeFile(t *testing.T, dir, rel, source string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func loadCheckEmit(t *testing.T, target string) *compiler.Program {
	t.Helper()
	prog, err := compiler.NewModuleLoader().Load(target)
	if err != nil {
		t.Fatalf("load %s: %v", target, err)
	}
	if err := compiler.NewTypechecker().Check(prog); err != nil {
		t.Fatalf("typecheck %s: %v", target, err)
	}
	if _, err := compiler.EmitWasm(prog); err != nil {
		t.Fatalf("emit %s: %v", target, err)
	}
	return prog
}

func TestPackageClauseRequired(t *testing.T) {
	source := "fn main() {}\n"
	tokens, err := compiler.NewLexer("test.ube", source).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	if _, err := compiler.NewParser(tokens).ParseProgram(); err == nil {
		t.Fatal("expected missing package clause error")
	} else if !strings.Contains(err.Error(), "expected 'package <name>'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPackageClauseAllowsLeadingCommentsAndBlankLines(t *testing.T) {
	source := "\n// package header\n\npackage main\n\nfn main() {}\n"
	tokens, err := compiler.NewLexer("test.ube", source).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := compiler.NewParser(tokens).ParseProgram()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if prog.Package != "main" {
		t.Fatalf("expected package main, got %q", prog.Package)
	}
}

func TestPackageDirectoryNameMismatch(t *testing.T) {
	dir := t.TempDir()
	writeUbeFile(t, dir, "a.ube", "package alpha\n\nfn a() {}\n")
	writeUbeFile(t, dir, "b.ube", "package beta\n\nfn b() {}\n")

	_, err := compiler.NewModuleLoader().Load(dir)
	if err == nil {
		t.Fatal("expected package-name mismatch error")
	}
	if !strings.Contains(err.Error(), "found packages 'alpha'") || !strings.Contains(err.Error(), "'beta'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPackageCrossFileScopeIncludesUnexportedDeclarations(t *testing.T) {
	dir := t.TempDir()
	writeUbeFile(t, dir, "model.ube", `package main

struct Pair {
    left: i32,
    right: i32
}

fn double(n: i32) -> i32 {
    return n * 2
}

fn (p Pair) total() -> i32 {
    return p.left + p.right
}
`)
	writeUbeFile(t, dir, "main.ube", `package main

fn main() {
    let p = Pair{ left: 10, right: 11 }
    println(double(p.total()))
}
`)

	loadCheckEmit(t, dir)
}

func TestPackageImportUsesRelativeDirectoryAndDefaultQualifier(t *testing.T) {
	dir := t.TempDir()
	writeUbeFile(t, dir, "util/util.ube", `package util

pub struct Point {
    pub x: i32,
    pub y: i32
}

pub fn double(n: i32) -> i32 {
    return n * 2
}

fn hidden() -> i32 {
    return 0
}
`)
	writeUbeFile(t, dir, "app/main.ube", `package main

import "../util"

fn main() {
    let p = util.Point{ x: 10, y: 11 }
    println(util.double(p.x + p.y))
}
`)

	loadCheckEmit(t, filepath.Join(dir, "app"))
}

func TestPackageImportAlias(t *testing.T) {
	dir := t.TempDir()
	writeUbeFile(t, dir, "util/util.ube", `package util

pub fn double(n: i32) -> i32 {
    return n * 2
}
`)
	writeUbeFile(t, dir, "app/main.ube", `package main

import "../util" as u

fn main() {
    println(u.double(21))
}
`)

	loadCheckEmit(t, filepath.Join(dir, "app", "main.ube"))
}

func TestPackageRejectsUnexportedFunction(t *testing.T) {
	dir := t.TempDir()
	writeUbeFile(t, dir, "util/util.ube", `package util

fn hidden() -> i32 {
    return 0
}
`)
	writeUbeFile(t, dir, "app/main.ube", `package main

import "../util"

fn main() {
    println(util.hidden())
}
`)

	prog, err := compiler.NewModuleLoader().Load(filepath.Join(dir, "app"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	err = compiler.NewTypechecker().Check(prog)
	if err == nil {
		t.Fatal("expected unexported function error")
	}
	if !strings.Contains(err.Error(), "cannot access unexported function") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPackageRejectsUnexportedFieldInitialization(t *testing.T) {
	dir := t.TempDir()
	writeUbeFile(t, dir, "util/util.ube", `package util

pub struct Point {
    pub x: i32,
    hidden: i32
}
`)
	writeUbeFile(t, dir, "app/main.ube", `package main

import "../util"

fn main() {
    let p = util.Point{ x: 1, hidden: 2 }
    println(p.x)
}
`)

	prog, err := compiler.NewModuleLoader().Load(filepath.Join(dir, "app"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	err = compiler.NewTypechecker().Check(prog)
	if err == nil {
		t.Fatal("expected unexported field error")
	}
	if !strings.Contains(err.Error(), "cannot initialize unexported field") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPackageImportsRejectExecutablePackage(t *testing.T) {
	dir := t.TempDir()
	writeUbeFile(t, dir, "tool/main.ube", "package main\n\nfn main() {}\n")
	writeUbeFile(t, dir, "app/main.ube", `package main

import "../tool"

fn main() {}
`)

	_, err := compiler.NewModuleLoader().Load(filepath.Join(dir, "app"))
	if err == nil {
		t.Fatal("expected executable-package import rejection")
	}
	if !strings.Contains(err.Error(), "package main is the executable package") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPackageImportsRejectUbeFiles(t *testing.T) {
	dir := t.TempDir()
	writeUbeFile(t, dir, "util/util.ube", "package util\n\npub fn ok() {}\n")
	writeUbeFile(t, dir, "app/main.ube", `package main

import "../util/util.ube"

fn main() {}
`)

	_, err := compiler.NewModuleLoader().Load(filepath.Join(dir, "app"))
	if err == nil {
		t.Fatal("expected file-import rejection")
	}
	if !strings.Contains(err.Error(), "imports must name package directories") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStdImportsRejectUbeFiles(t *testing.T) {
	dir := t.TempDir()
	writeUbeFile(t, dir, "app/main.ube", `package main

import "std/math.ube"

fn main() {}
`)

	_, err := compiler.NewModuleLoader().Load(filepath.Join(dir, "app"))
	if err == nil {
		t.Fatal("expected standard-library file-import rejection")
	}
	if !strings.Contains(err.Error(), "imports must name package directories") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTestsIncludesTestFilesInPackage(t *testing.T) {
	dir := t.TempDir()
	writeUbeFile(t, dir, "main.ube", `package main

fn helper() -> i32 {
    return 7
}
`)
	writeUbeFile(t, dir, "main_test.ube", `package main

fn test_cross_file() {
    if helper() != 7 {
        println("bad")
    }
}
`)

	normal, err := compiler.NewModuleLoader().Load(dir)
	if err != nil {
		t.Fatalf("load without tests: %v", err)
	}
	if hasTestDecl(normal) {
		t.Fatal("Load must exclude _test.ube files")
	}

	withTests, err := compiler.NewModuleLoader().LoadTests(dir)
	if err != nil {
		t.Fatalf("load with tests: %v", err)
	}
	if !hasTestDecl(withTests) {
		t.Fatal("LoadTests must include _test.ube files")
	}
	if err := compiler.NewTypechecker().Check(withTests); err != nil {
		t.Fatalf("typecheck package tests: %v", err)
	}
}

func hasTestDecl(prog *compiler.Program) bool {
	for _, decl := range prog.Decls {
		fn, ok := decl.(*compiler.FnDecl)
		if !ok {
			continue
		}
		name := fn.Name
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			name = name[i+1:]
		}
		if strings.HasPrefix(name, "test_") {
			return true
		}
	}
	return false
}
