package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveMoveCopyWorkspace(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.go"), []byte("package sub"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyPathWorkspace(dir, "sub", "sub2"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "sub2", "b.go"))
	if err != nil || string(b) != "package sub" {
		t.Fatalf("copy dir: %v %q", err, b)
	}

	if err := movePathWorkspace(dir, "a.txt", "moved.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "moved.txt")); err != nil {
		t.Fatal(err)
	}

	if err := removePathWorkspace(dir, "sub2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub2")); err == nil {
		t.Fatal("expected sub2 removed")
	}
}

func TestPathStatWorkspace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.go")
	if err := os.WriteFile(p, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := pathStatWorkspace(dir, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "exists: true") || !strings.Contains(s, "kind: file") {
		t.Fatalf("got %q", s)
	}
	s2, err := pathStatWorkspace(dir, "nope")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s2, "exists: false") {
		t.Fatalf("got %q", s2)
	}
}

func TestGlobFilesWorkspace(t *testing.T) {
	dir := t.TempDir()
	_ = os.Mkdir(filepath.Join(dir, "pkg"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "pkg", "a_test.go"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "pkg", "a.go"), []byte("y"), 0o644)

	out, err := globFilesWorkspace(dir, ".", "*_test.go", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "pkg/a_test.go") || strings.Contains(out, "a.go") {
		t.Fatalf("basename glob: %q", out)
	}

	out2, err := globFilesWorkspace(dir, "pkg", "a.go", 50)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out2) != "a.go" {
		t.Fatalf("full path glob: %q", out2)
	}
}

func TestMutatingToolsViaRegistry(t *testing.T) {
	dir := t.TempDir()
	r := Default(dir, nil, nil, nil, "")
	_, err := r.Run(context.Background(), "write_file", json.RawMessage(`{"path":"t.txt","content":"z","mode":"create"}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(context.Background(), "copy_path", json.RawMessage(`{"from":"t.txt","to":"u.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "copied") {
		t.Fatalf("got %q", out)
	}
	_, err = r.Run(context.Background(), "remove_path", json.RawMessage(`{"path":"u.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
}
