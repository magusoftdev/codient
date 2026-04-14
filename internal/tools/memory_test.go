package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeMemoryRegistry(t *testing.T) (*Registry, string, string) {
	t.Helper()
	stateDir := t.TempDir()
	wsRoot := t.TempDir()
	opts := &MemoryOptions{
		StateDir:      stateDir,
		WorkspaceRoot: wsRoot,
	}
	r := NewRegistry()
	registerMemoryUpdate(r, opts)
	return r, stateDir, wsRoot
}

func runMemoryTool(t *testing.T, r *Registry, args map[string]any) string {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Run(context.Background(), "memory_update", b)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMemoryUpdate_AppendGlobal_CreatesFile(t *testing.T) {
	r, stateDir, _ := makeMemoryRegistry(t)
	result := runMemoryTool(t, r, map[string]any{
		"scope":   "global",
		"action":  "append",
		"content": "- user prefers concise output",
	})
	if !strings.Contains(result, "created") {
		t.Fatalf("expected 'created' in result: %s", result)
	}
	got := readFile(t, filepath.Join(stateDir, "memory.md"))
	if !strings.Contains(got, "user prefers concise output") {
		t.Fatalf("content not found: %s", got)
	}
}

func TestMemoryUpdate_AppendWorkspace(t *testing.T) {
	r, _, wsRoot := makeMemoryRegistry(t)

	runMemoryTool(t, r, map[string]any{
		"scope":   "workspace",
		"action":  "append",
		"content": "- uses Go 1.22",
	})
	runMemoryTool(t, r, map[string]any{
		"scope":   "workspace",
		"action":  "append",
		"content": "- prefers table-driven tests",
	})

	got := readFile(t, filepath.Join(wsRoot, ".codient", "memory.md"))
	if !strings.Contains(got, "uses Go 1.22") || !strings.Contains(got, "prefers table-driven tests") {
		t.Fatalf("missing content: %s", got)
	}
	idx1 := strings.Index(got, "uses Go 1.22")
	idx2 := strings.Index(got, "prefers table-driven tests")
	if idx1 >= idx2 {
		t.Fatalf("first append should appear before second: %s", got)
	}
}

func TestMemoryUpdate_ReplaceSection_NewSection(t *testing.T) {
	r, stateDir, _ := makeMemoryRegistry(t)

	runMemoryTool(t, r, map[string]any{
		"scope":   "global",
		"action":  "append",
		"content": "## Preferences\n\n- dark mode",
	})
	result := runMemoryTool(t, r, map[string]any{
		"scope":   "global",
		"action":  "replace_section",
		"section": "Build Commands",
		"content": "- go build ./...\n- go test ./...",
	})
	if !strings.Contains(result, "added") {
		t.Fatalf("expected 'added' in result: %s", result)
	}

	got := readFile(t, filepath.Join(stateDir, "memory.md"))
	if !strings.Contains(got, "## Build Commands") {
		t.Fatalf("missing new section heading: %s", got)
	}
	if !strings.Contains(got, "go build") {
		t.Fatalf("missing section content: %s", got)
	}
	if !strings.Contains(got, "## Preferences") {
		t.Fatalf("original content should be preserved: %s", got)
	}
}

func TestMemoryUpdate_ReplaceSection_ExistingSection(t *testing.T) {
	r, stateDir, _ := makeMemoryRegistry(t)

	runMemoryTool(t, r, map[string]any{
		"scope":   "global",
		"action":  "append",
		"content": "## Style\n\n- old style note\n\n## Other\n\n- keep this",
	})

	result := runMemoryTool(t, r, map[string]any{
		"scope":   "global",
		"action":  "replace_section",
		"section": "Style",
		"content": "- new style note",
	})
	if !strings.Contains(result, "replaced") {
		t.Fatalf("expected 'replaced' in result: %s", result)
	}

	got := readFile(t, filepath.Join(stateDir, "memory.md"))
	if strings.Contains(got, "old style note") {
		t.Fatalf("old content should be replaced: %s", got)
	}
	if !strings.Contains(got, "new style note") {
		t.Fatalf("new content should be present: %s", got)
	}
	if !strings.Contains(got, "keep this") {
		t.Fatalf("other section should be preserved: %s", got)
	}
}

func TestMemoryUpdate_ReplaceSection_RequiresSection(t *testing.T) {
	r, _, _ := makeMemoryRegistry(t)
	b, _ := json.Marshal(map[string]any{
		"scope":   "global",
		"action":  "replace_section",
		"content": "stuff",
	})
	_, err := r.Run(context.Background(), "memory_update", b)
	if err == nil {
		t.Fatal("expected error for missing section")
	}
	if !strings.Contains(err.Error(), "section is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMemoryUpdate_InvalidScope(t *testing.T) {
	r, _, _ := makeMemoryRegistry(t)
	b, _ := json.Marshal(map[string]any{
		"scope":   "invalid",
		"action":  "append",
		"content": "stuff",
	})
	_, err := r.Run(context.Background(), "memory_update", b)
	if err == nil {
		t.Fatal("expected error for invalid scope")
	}
}

func TestMemoryUpdate_InvalidAction(t *testing.T) {
	r, _, _ := makeMemoryRegistry(t)
	b, _ := json.Marshal(map[string]any{
		"scope":   "global",
		"action":  "delete",
		"content": "stuff",
	})
	_, err := r.Run(context.Background(), "memory_update", b)
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
}

func TestMemoryUpdate_NilOptions(t *testing.T) {
	r := NewRegistry()
	registerMemoryUpdate(r, nil)
	names := r.Names()
	for _, n := range names {
		if n == "memory_update" {
			t.Fatal("memory_update should not be registered with nil options")
		}
	}
}
