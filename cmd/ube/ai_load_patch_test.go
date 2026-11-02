package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativePatch_ReplaceAndCreate(t *testing.T) {
	tmpDir := t.TempDir()
	origFile := filepath.Join(tmpDir, "main.ube")
	err := os.WriteFile(origFile, []byte("fn main() {\n    let mut x = 1\n    x += 1\n}\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	patch := `<replace path="` + origFile + `">
<search>
    let mut x = 1
    x += 1
</search>
<with>
    let mut x = 10
    x += 5
</with>
</replace>

<file path="` + filepath.Join(tmpDir, "helper.ube") + `">
pub fn add(a: i32, b: i32) -> i32 {
    return a + b
}
</file>`

	patchFile := filepath.Join(tmpDir, "patch.xml")
	os.WriteFile(patchFile, []byte(patch), 0644)

	err = runPatch([]string{patchFile})
	if err != nil {
		t.Fatalf("runPatch failed: %v", err)
	}

	content, _ := os.ReadFile(origFile)
	if !strings.Contains(string(content), "let mut x = 10") {
		t.Errorf("patch did not replace correctly:\n%s", string(content))
	}

	helperContent, err := os.ReadFile(filepath.Join(tmpDir, "helper.ube"))
	if err != nil || !strings.Contains(string(helperContent), "pub fn add") {
		t.Errorf("file was not created correctly: %v", err)
	}
}

func TestNativePatch_DryRunDoesNotModify(t *testing.T) {
	tmpDir := t.TempDir()
	origFile := filepath.Join(tmpDir, "test.ube")
	os.WriteFile(origFile, []byte("fn run() {}\n"), 0644)

	patch := `<replace path="` + origFile + `">
<search>
fn run() {}
</search>
<with>
fn run() { println(1) }
</with>
</replace>`

	patchFile := filepath.Join(tmpDir, "patch.xml")
	os.WriteFile(patchFile, []byte(patch), 0644)

	err := runPatch([]string{patchFile, "--dry-run"})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	content, _ := os.ReadFile(origFile)
	if string(content) != "fn run() {}\n" {
		t.Errorf("dry run modified file on disk!")
	}
}

func TestNativeLoad_ProcessRequest(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	src := `struct Point { x: i32, y: i32 }
fn compute(p: Point) -> i32 {
    return p.x + p.y
}
`
	os.WriteFile("geom.ube", []byte(src), 0644)

	req := `<def symbol="compute" />
<def symbol="Point" />`

	out, err := processLoadRequest(req)
	if err != nil {
		t.Fatalf("processLoadRequest failed: %v", err)
	}

	if !strings.Contains(out, "fn compute") {
		t.Errorf("missing compute in output:\n%s", out)
	}
	if !strings.Contains(out, "struct Point") {
		t.Errorf("missing struct Point in output:\n%s", out)
	}
}

func TestNativeLoad_SelectCaseAndMethods(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	src := `enum Message {
    Ping,
    Quit,
}

struct Server {
    port: i32,
}

fn (s Server) start() {
    println(s.port)
}

fn (s Server) shutdown() {
    println("stop")
}

fn handle(msg: Message) {
    match msg {
        Message::Ping => {
            println("pong")
        }
        Message::Quit => {
            println("bye")
        }
    }
}
`
	os.WriteFile("server.ube", []byte(src), 0644)

	// Test case isolation
	reqCase := `<select path="server.ube" fn="handle" case="Message::Ping" />`
	outCase, err := processLoadRequest(reqCase)
	if err != nil {
		t.Fatalf("processLoadRequest case failed: %v", err)
	}
	if !strings.Contains(outCase, "Message::Ping =>") || !strings.Contains(outCase, "println(\"pong\")") {
		t.Errorf("select case output unexpected:\n%s", outCase)
	}
	if strings.Contains(outCase, "Message::Quit") {
		t.Errorf("select case leaked Message::Quit into output:\n%s", outCase)
	}

	// Test method isolation
	reqMethods := `<select struct="Server" methods="start,shutdown" />`
	outMethods, err := processLoadRequest(reqMethods)
	if err != nil {
		t.Fatalf("processLoadRequest methods failed: %v", err)
	}
	if !strings.Contains(outMethods, "fn (s Server) start()") || !strings.Contains(outMethods, "fn (s Server) shutdown()") {
		t.Errorf("select methods output missing methods:\n%s", outMethods)
	}
}

func TestNativeLoad_XRef(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	src1 := `struct Config {
    debug: bool,
}

fn init_config() -> Config {
    return Config { debug: true }
}
`
	src2 := `fn run() {
    let mut cfg = init_config()
    cfg = init_config()
    println(cfg.debug)
}
`
	os.WriteFile("config.ube", []byte(src1), 0644)
	os.WriteFile("main.ube", []byte(src2), 0644)

	// XRef with write action
	reqWrite := `<xref symbol="cfg" action="write" />`
	outWrite, err := processLoadRequest(reqWrite)
	if err != nil {
		t.Fatalf("xref failed: %v", err)
	}
	if !strings.Contains(outWrite, "let mut cfg") && !strings.Contains(outWrite, "cfg =") {
		t.Errorf("xref write missed cfg assignments:\n%s", outWrite)
	}

	// XRef with call action
	reqCall := `<xref symbol="init_config" action="call" />`
	outCall, err := processLoadRequest(reqCall)
	if err != nil {
		t.Fatalf("xref failed: %v", err)
	}
	if !strings.Contains(outCall, "init_config()") {
		t.Errorf("xref call missed init_config calls:\n%s", outCall)
	}
}

func TestNativeLoad_FeatureContext(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	src := `struct Session {
    id: i32,
}

fn open_session() -> Session {
    return Session { id: 42 }
}
`
	os.WriteFile("session.ube", []byte(src), 0644)

	req := `<feature-context>
    <target-type name="Session" />
    <entrypoint symbol="open_session" depth="1" />
    <touchpoints pattern="Session" />
</feature-context>`

	out, err := processLoadRequest(req)
	if err != nil {
		t.Fatalf("feature-context failed: %v", err)
	}

	if !strings.Contains(out, "FEATURE CONTEXT BUNDLE") {
		t.Errorf("missing bundle header in output:\n%s", out)
	}
	if !strings.Contains(out, "struct Session") {
		t.Errorf("missing struct Session in output:\n%s", out)
	}
	if !strings.Contains(out, "fn open_session") {
		t.Errorf("missing open_session in output:\n%s", out)
	}
	if !strings.Contains(out, "GREP: Session") {
		t.Errorf("missing grep Session in output:\n%s", out)
	}
}
