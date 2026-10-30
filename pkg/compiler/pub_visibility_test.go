package compiler_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/compiler"
)

func TestPub_CrossFileVisibility(t *testing.T) {
	tmpDir := t.TempDir()

	modPath := filepath.Join(tmpDir, "mathlib.ube")
	modSource := `
pub fn add(a: i32, b: i32) -> i32 {
	return a + b
}

fn secret_helper() -> i32 {
	return 42
}

pub struct Vector {
	pub x: i32,
	y: i32
}

pub fn make_vec(x: i32, y: i32) -> Vector {
	return Vector{x: x, y: y}
}

pub fn (v Vector) sum() -> i32 {
	return v.x + v.y
}

fn (v Vector) secret_method() -> i32 {
	return v.y
}
`
	if err := os.WriteFile(modPath, []byte(modSource), 0644); err != nil {
		t.Fatalf("failed to write mathlib.ube: %v", err)
	}

	mainPath := filepath.Join(tmpDir, "main.ube")
	mainSource := `
import "mathlib.ube"

fn main() {
	let res = add(10, 20)
	println(res)

	let v = make_vec(1, 2)
	println(v.x)
	println(v.sum())
}
`
	if err := os.WriteFile(mainPath, []byte(mainSource), 0644); err != nil {
		t.Fatalf("failed to write main.ube: %v", err)
	}

	loader := compiler.NewModuleLoader()
	prog, err := loader.Load(mainPath)
	if err != nil {
		t.Fatalf("failed to load module: %v", err)
	}

	tc := compiler.NewTypechecker()
	if err := tc.Check(prog); err != nil {
		t.Fatalf("expected valid typecheck on public items, got error: %v", err)
	}
}

func TestPub_RejectsPrivateFunctionCall(t *testing.T) {
	tmpDir := t.TempDir()

	modPath := filepath.Join(tmpDir, "lib.ube")
	modSource := `
fn hidden() -> i32 {
	return 100
}
`
	if err := os.WriteFile(modPath, []byte(modSource), 0644); err != nil {
		t.Fatalf("failed to write lib.ube: %v", err)
	}

	mainPath := filepath.Join(tmpDir, "main.ube")
	mainSource := `
import "lib.ube"

fn main() {
	let x = hidden()
	println(x)
}
`
	if err := os.WriteFile(mainPath, []byte(mainSource), 0644); err != nil {
		t.Fatalf("failed to write main.ube: %v", err)
	}

	loader := compiler.NewModuleLoader()
	prog, err := loader.Load(mainPath)
	if err != nil {
		t.Fatalf("failed to load module: %v", err)
	}

	tc := compiler.NewTypechecker()
	err = tc.Check(prog)
	if err == nil {
		t.Fatalf("expected typechecker to reject call to private function 'hidden', but it succeeded")
	}
	if !strings.Contains(err.Error(), "cannot access private function 'hidden'") {
		t.Fatalf("expected private function error message, got: %v", err)
	}
}

func TestPub_RejectsPrivateFieldAccess(t *testing.T) {
	tmpDir := t.TempDir()

	modPath := filepath.Join(tmpDir, "types.ube")
	modSource := `
pub struct Account {
	pub id: i32,
	balance: i32
}

pub fn make_account(id: i32, b: i32) -> Account {
	return Account{id: id, balance: b}
}
`
	if err := os.WriteFile(modPath, []byte(modSource), 0644); err != nil {
		t.Fatalf("failed to write types.ube: %v", err)
	}

	mainPath := filepath.Join(tmpDir, "main.ube")
	mainSource := `
import "types.ube"

fn main() {
	let a = make_account(1, 500)
	println(a.balance)
}
`
	if err := os.WriteFile(mainPath, []byte(mainSource), 0644); err != nil {
		t.Fatalf("failed to write main.ube: %v", err)
	}

	loader := compiler.NewModuleLoader()
	prog, err := loader.Load(mainPath)
	if err != nil {
		t.Fatalf("failed to load module: %v", err)
	}

	tc := compiler.NewTypechecker()
	err = tc.Check(prog)
	if err == nil {
		t.Fatalf("expected typechecker to reject private field access, but it succeeded")
	}
	if !strings.Contains(err.Error(), "cannot access private field 'balance'") {
		t.Fatalf("expected private field error message, got: %v", err)
	}
}

func TestPub_RejectsPrivateMethodCall(t *testing.T) {
	tmpDir := t.TempDir()

	modPath := filepath.Join(tmpDir, "robot.ube")
	modSource := `
pub struct Robot {
	pub name: string
}

fn (r Robot) secret_code() -> i32 {
	return 999
}
`
	if err := os.WriteFile(modPath, []byte(modSource), 0644); err != nil {
		t.Fatalf("failed to write robot.ube: %v", err)
	}

	mainPath := filepath.Join(tmpDir, "main.ube")
	mainSource := `
import "robot.ube"

fn main() {
	let r = Robot{name: "beep"}
	let c = r.secret_code()
	println(c)
}
`
	if err := os.WriteFile(mainPath, []byte(mainSource), 0644); err != nil {
		t.Fatalf("failed to write main.ube: %v", err)
	}

	loader := compiler.NewModuleLoader()
	prog, err := loader.Load(mainPath)
	if err != nil {
		t.Fatalf("failed to load module: %v", err)
	}

	tc := compiler.NewTypechecker()
	err = tc.Check(prog)
	if err == nil {
		t.Fatalf("expected typechecker to reject private method call, but it succeeded")
	}
	if !strings.Contains(err.Error(), "cannot access private method 'secret_code'") {
		t.Fatalf("expected private method error message, got: %v", err)
	}
}

func TestPub_RejectsPrivateStructInstantiation(t *testing.T) {
	tmpDir := t.TempDir()

	modPath := filepath.Join(tmpDir, "internal.ube")
	modSource := `
struct SecretConfig {
	api_key: string
}
`
	if err := os.WriteFile(modPath, []byte(modSource), 0644); err != nil {
		t.Fatalf("failed to write internal.ube: %v", err)
	}

	mainPath := filepath.Join(tmpDir, "main.ube")
	mainSource := `
import "internal.ube"

fn main() {
	let cfg = SecretConfig{api_key: "abc"}
	println(cfg.api_key)
}
`
	if err := os.WriteFile(mainPath, []byte(mainSource), 0644); err != nil {
		t.Fatalf("failed to write main.ube: %v", err)
	}

	loader := compiler.NewModuleLoader()
	prog, err := loader.Load(mainPath)
	if err != nil {
		t.Fatalf("failed to load module: %v", err)
	}

	tc := compiler.NewTypechecker()
	err = tc.Check(prog)
	if err == nil {
		t.Fatalf("expected typechecker to reject instantiating private struct 'SecretConfig'")
	}
	if !strings.Contains(err.Error(), "cannot access private struct 'SecretConfig'") &&
		!strings.Contains(err.Error(), "cannot instantiate private struct 'SecretConfig'") {
		t.Fatalf("expected private struct error message, got: %v", err)
	}
}

func TestPub_RejectsPrivateFieldInit(t *testing.T) {
	tmpDir := t.TempDir()

	modPath := filepath.Join(tmpDir, "user.ube")
	modSource := `
pub struct User {
	pub name: string,
	password_hash: string
}
`
	if err := os.WriteFile(modPath, []byte(modSource), 0644); err != nil {
		t.Fatalf("failed to write user.ube: %v", err)
	}

	mainPath := filepath.Join(tmpDir, "main.ube")
	mainSource := `
import "user.ube"

fn main() {
	let u = User{name: "Alice", password_hash: "hash123"}
	println(u.name)
}
`
	if err := os.WriteFile(mainPath, []byte(mainSource), 0644); err != nil {
		t.Fatalf("failed to write main.ube: %v", err)
	}

	loader := compiler.NewModuleLoader()
	prog, err := loader.Load(mainPath)
	if err != nil {
		t.Fatalf("failed to load module: %v", err)
	}

	tc := compiler.NewTypechecker()
	err = tc.Check(prog)
	if err == nil {
		t.Fatalf("expected typechecker to reject initializing private field 'password_hash'")
	}
	if !strings.Contains(err.Error(), "cannot initialize private field 'password_hash'") {
		t.Fatalf("expected private field init error message, got: %v", err)
	}
}

func TestPub_RejectsPrivateEnumAccess(t *testing.T) {
	tmpDir := t.TempDir()

	modPath := filepath.Join(tmpDir, "states.ube")
	modSource := `
enum Status {
	Draft,
	Active
}
`
	if err := os.WriteFile(modPath, []byte(modSource), 0644); err != nil {
		t.Fatalf("failed to write states.ube: %v", err)
	}

	mainPath := filepath.Join(tmpDir, "main.ube")
	mainSource := `
import "states.ube"

fn main() {
	let s = Status.Draft
}
`
	if err := os.WriteFile(mainPath, []byte(mainSource), 0644); err != nil {
		t.Fatalf("failed to write states.ube: %v", err)
	}

	loader := compiler.NewModuleLoader()
	prog, err := loader.Load(mainPath)
	if err != nil {
		t.Fatalf("failed to load module: %v", err)
	}

	tc := compiler.NewTypechecker()
	err = tc.Check(prog)
	if err == nil {
		t.Fatalf("expected typechecker to reject accessing private enum 'Status'")
	}
	if !strings.Contains(err.Error(), "cannot access private enum 'Status'") {
		t.Fatalf("expected private enum error message, got: %v", err)
	}
}

func TestPub_RejectsPrivateTraitAccess(t *testing.T) {
	tmpDir := t.TempDir()

	modPath := filepath.Join(tmpDir, "workers.ube")
	modSource := `
trait InternalTask {
	fn perform() -> i32
}
`
	if err := os.WriteFile(modPath, []byte(modSource), 0644); err != nil {
		t.Fatalf("failed to write workers.ube: %v", err)
	}

	mainPath := filepath.Join(tmpDir, "main.ube")
	mainSource := `
import "workers.ube"

fn run_task(t: InternalTask) {
	println(t.perform())
}

fn main() {
}
`
	if err := os.WriteFile(mainPath, []byte(mainSource), 0644); err != nil {
		t.Fatalf("failed to write main.ube: %v", err)
	}

	loader := compiler.NewModuleLoader()
	prog, err := loader.Load(mainPath)
	if err != nil {
		t.Fatalf("failed to load module: %v", err)
	}

	tc := compiler.NewTypechecker()
	err = tc.Check(prog)
	if err == nil {
		t.Fatalf("expected typechecker to reject using private trait 'InternalTask' in signature")
	}
	if !strings.Contains(err.Error(), "cannot access private trait 'InternalTask'") {
		t.Fatalf("expected private trait error message, got: %v", err)
	}
}
