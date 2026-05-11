//go:build integration

package lspclient

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"codient/internal/config"
)

// TestGoplsIntegration drives a real gopls language server.
// Gated by:
//   - CODIENT_INTEGRATION=1 (set by make test-integration)
//   - exec.LookPath("gopls") succeeding
func TestGoplsIntegration(t *testing.T) {
	if os.Getenv("CODIENT_INTEGRATION") != "1" {
		t.Skip("CODIENT_INTEGRATION not set")
	}
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not in PATH")
	}

	// Create a minimal Go module in a temp directory.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module testmod\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import "fmt"

func Hello() string {
	return "hello"
}

func main() {
	fmt.Println(Hello())
}
`)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mgr := NewManager()
	lspCfg := map[string]config.LSPServerConfig{
		"go": {
			Command:        "gopls",
			Args:           []string{"serve"},
			FileExtensions: []string{".go"},
		},
	}
	warns := mgr.Connect(ctx, lspCfg, dir)
	for _, w := range warns {
		t.Fatalf("connect warning: %s", w)
	}
	defer mgr.Close()

	ids := mgr.ServerIDs()
	if len(ids) != 1 || ids[0] != "go" {
		t.Fatalf("expected [go], got %v", ids)
	}

	sc := mgr.PickServer("main.go", "go", lspCfg)
	if sc == nil {
		t.Fatal("PickServer returned nil")
	}

	// Give gopls a moment to index.
	time.Sleep(2 * time.Second)

	// Test definition: Hello at line 10 (0-based 9), character 14 (0-based 13) in fmt.Println(Hello())
	locs, err := mgr.Definition(ctx, sc, filepath.Join(dir, "main.go"), 9, 13)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) == 0 {
		t.Fatal("Definition returned no locations")
	}
	t.Logf("Definition: %+v", locs)

	// Test references.
	refs, err := mgr.References(ctx, sc, filepath.Join(dir, "main.go"), 4, 5)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(refs) < 2 {
		t.Fatalf("References: expected >= 2, got %d", len(refs))
	}
	t.Logf("References: %d locations", len(refs))

	// Test hover.
	hover, err := mgr.Hover(ctx, sc, filepath.Join(dir, "main.go"), 4, 5)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil || hover.Contents.Value == "" {
		t.Fatal("Hover returned no content")
	}
	t.Logf("Hover: %s", hover.Contents.Value)

	// Test workspace symbols.
	syms, err := mgr.WorkspaceSymbols(ctx, sc, "Hello")
	if err != nil {
		t.Fatalf("WorkspaceSymbols: %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("WorkspaceSymbols returned no symbols")
	}
	t.Logf("WorkspaceSymbols: %d symbols", len(syms))

	// Test rename.
	edit, err := mgr.Rename(ctx, sc, filepath.Join(dir, "main.go"), 4, 5, "Greeting")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if edit == nil || len(edit.Changes) == 0 {
		t.Fatal("Rename returned no changes")
	}
	t.Logf("Rename: %d files changed", len(edit.Changes))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
