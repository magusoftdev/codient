package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMemory_NoFiles(t *testing.T) {
	dir := t.TempDir()
	ws := t.TempDir()
	got, err := LoadMemory(dir, ws)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestLoadMemory_GlobalOnly(t *testing.T) {
	dir := t.TempDir()
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, MemoryFileName), []byte("- prefer tabs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMemory(dir, ws)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Global memory") {
		t.Fatalf("missing global heading: %s", got)
	}
	if !strings.Contains(got, "prefer tabs") {
		t.Fatalf("missing content: %s", got)
	}
	if strings.Contains(got, "Workspace memory") {
		t.Fatal("unexpected workspace heading")
	}
}

func TestLoadMemory_WorkspaceOnly(t *testing.T) {
	dir := t.TempDir()
	ws := t.TempDir()
	wsDir := filepath.Join(ws, ".codient")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, MemoryFileName), []byte("- use gofmt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMemory(dir, ws)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Workspace memory") {
		t.Fatalf("missing workspace heading: %s", got)
	}
	if !strings.Contains(got, "use gofmt") {
		t.Fatalf("missing content: %s", got)
	}
	if strings.Contains(got, "Global memory") {
		t.Fatal("unexpected global heading")
	}
}

func TestLoadMemory_BothScopes(t *testing.T) {
	dir := t.TempDir()
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, MemoryFileName), []byte("global note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wsDir := filepath.Join(ws, ".codient")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, MemoryFileName), []byte("workspace note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMemory(dir, ws)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Global memory") || !strings.Contains(got, "Workspace memory") {
		t.Fatalf("missing headings: %s", got)
	}
	globalIdx := strings.Index(got, "Global memory")
	wsIdx := strings.Index(got, "Workspace memory")
	if globalIdx >= wsIdx {
		t.Fatalf("global should appear before workspace: %s", got)
	}
}

func TestLoadMemory_Truncation(t *testing.T) {
	dir := t.TempDir()
	ws := t.TempDir()
	big := strings.Repeat("x", MaxMemoryBytesPerFile+1000)
	if err := os.WriteFile(filepath.Join(dir, MemoryFileName), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMemory(dir, ws)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "[truncated]") {
		t.Fatalf("expected truncation marker: len=%d", len(got))
	}
}

func TestLoadMemory_EmptyDirs(t *testing.T) {
	got, err := LoadMemory("", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestGlobalMemoryPath(t *testing.T) {
	p := GlobalMemoryPath("/home/user/.codient")
	if !strings.HasSuffix(p, MemoryFileName) {
		t.Fatalf("unexpected path: %s", p)
	}
}

func TestWorkspaceMemoryPath(t *testing.T) {
	p := WorkspaceMemoryPath("/home/user/project")
	if !strings.Contains(p, ".codient") || !strings.HasSuffix(p, MemoryFileName) {
		t.Fatalf("unexpected path: %s", p)
	}
}
