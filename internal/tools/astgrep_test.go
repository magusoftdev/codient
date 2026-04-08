package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterAstGrepTools_EmptyPath(t *testing.T) {
	r := NewRegistry()
	registerAstGrepTools(r, t.TempDir(), "")
	for _, name := range r.Names() {
		if name == "find_references" {
			t.Fatal("find_references should not be registered with empty sgPath")
		}
	}
}

func TestRegisterAstGrepTools_WithPath(t *testing.T) {
	r := NewRegistry()
	registerAstGrepTools(r, t.TempDir(), "/usr/local/bin/ast-grep")
	found := false
	for _, name := range r.Names() {
		if name == "find_references" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("find_references should be registered when sgPath is set")
	}
}

func TestDetectLang_Go(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if lang := detectLang(dir); lang != "go" {
		t.Fatalf("expected go, got %q", lang)
	}
}

func TestDetectLang_TypeScript(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(`{}`), 0o644)
	if lang := detectLang(dir); lang != "typescript" {
		t.Fatalf("expected typescript, got %q", lang)
	}
}

func TestDetectLang_JavaScript(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x"}`), 0o644)
	if lang := detectLang(dir); lang != "javascript" {
		t.Fatalf("expected javascript, got %q", lang)
	}
}

func TestDetectLang_Python(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]"), 0o644)
	if lang := detectLang(dir); lang != "python" {
		t.Fatalf("expected python, got %q", lang)
	}
}

func TestDetectLang_Rust(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]"), 0o644)
	if lang := detectLang(dir); lang != "rust" {
		t.Fatalf("expected rust, got %q", lang)
	}
}

func TestDetectLang_Unknown(t *testing.T) {
	if lang := detectLang(t.TempDir()); lang != "" {
		t.Fatalf("expected empty, got %q", lang)
	}
}

func TestFormatAstGrepOutput(t *testing.T) {
	root := "/workspace"
	jsonInput := `[
		{"file":"/workspace/internal/tools/workspace.go","range":{"start":{"line":35,"column":0}},"lines":"func absUnderRoot(root, rel string) (abs string, err error) {"},
		{"file":"/workspace/internal/tools/exec.go","range":{"start":{"line":237,"column":1}},"lines":"\tworkDir, err := absUnderRoot(workspaceRoot, cwd)"}
	]`
	out, err := formatAstGrepOutput(jsonInput, root, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out, "internal/tools/workspace.go:36:") {
		t.Fatalf("expected workspace.go:36, got:\n%s", out)
	}
	if !contains(out, "internal/tools/exec.go:238:") {
		t.Fatalf("expected exec.go:238, got:\n%s", out)
	}
}

func TestFormatAstGrepOutput_Truncate(t *testing.T) {
	root := "/workspace"
	jsonInput := `[
		{"file":"/workspace/a.go","range":{"start":{"line":0,"column":0}},"lines":"a"},
		{"file":"/workspace/b.go","range":{"start":{"line":0,"column":0}},"lines":"b"},
		{"file":"/workspace/c.go","range":{"start":{"line":0,"column":0}},"lines":"c"}
	]`
	out, err := formatAstGrepOutput(jsonInput, root, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out, "[truncated at 2 matches]") {
		t.Fatalf("expected truncation notice, got:\n%s", out)
	}
}

func TestFormatAstGrepOutput_Empty(t *testing.T) {
	out, err := formatAstGrepOutput("[]", "/workspace", 50)
	if err != nil {
		t.Fatal(err)
	}
	if out != "(no matches)" {
		t.Fatalf("expected no matches, got: %q", out)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
