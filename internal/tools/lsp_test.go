package tools_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codient/internal/config"
	"codient/internal/lspclient"
	"codient/internal/tools"
)

// fakeLSPClient implements tools.LSPClient for testing.
type fakeLSPClient struct {
	defResult    []lspclient.Location
	refResult    []lspclient.Location
	hoverResult  *lspclient.HoverResult
	symResult    []lspclient.SymbolInformation
	renameResult *lspclient.WorkspaceEdit
	calls        []string
}

func (f *fakeLSPClient) PickServer(file string, lang string, _ map[string]config.LSPServerConfig) interface{} {
	if lang == "missing" {
		return nil
	}
	return "fake-handle"
}

func (f *fakeLSPClient) Definition(_ context.Context, _ interface{}, _ string, _, _ int) ([]lspclient.Location, error) {
	f.calls = append(f.calls, "definition")
	return f.defResult, nil
}

func (f *fakeLSPClient) TypeDefinition(_ context.Context, _ interface{}, _ string, _, _ int) ([]lspclient.Location, error) {
	f.calls = append(f.calls, "typeDefinition")
	return f.defResult, nil
}

func (f *fakeLSPClient) Implementation(_ context.Context, _ interface{}, _ string, _, _ int) ([]lspclient.Location, error) {
	f.calls = append(f.calls, "implementation")
	return f.defResult, nil
}

func (f *fakeLSPClient) References(_ context.Context, _ interface{}, _ string, _, _ int) ([]lspclient.Location, error) {
	f.calls = append(f.calls, "references")
	return f.refResult, nil
}

func (f *fakeLSPClient) Hover(_ context.Context, _ interface{}, _ string, _, _ int) (*lspclient.HoverResult, error) {
	f.calls = append(f.calls, "hover")
	return f.hoverResult, nil
}

func (f *fakeLSPClient) DocumentSymbols(_ context.Context, _ interface{}, _ string) ([]lspclient.SymbolInformation, error) {
	f.calls = append(f.calls, "documentSymbols")
	return f.symResult, nil
}

func (f *fakeLSPClient) WorkspaceSymbols(_ context.Context, _ interface{}, _ string) ([]lspclient.SymbolInformation, error) {
	f.calls = append(f.calls, "workspaceSymbols")
	return f.symResult, nil
}

func (f *fakeLSPClient) Rename(_ context.Context, _ interface{}, _ string, _, _ int, _ string) (*lspclient.WorkspaceEdit, error) {
	f.calls = append(f.calls, "rename")
	return f.renameResult, nil
}

var testLSPCfg = map[string]config.LSPServerConfig{
	"go": {Command: "gopls", FileExtensions: []string{".go"}},
}

func TestRegisterLSPReadTools_Names(t *testing.T) {
	fake := &fakeLSPClient{
		defResult: []lspclient.Location{{URI: "file:///tmp/w/main.go", Range: lspclient.Range{Start: lspclient.Position{Line: 5}}}},
	}
	reg := tools.NewRegistry()
	tools.RegisterLSPReadTools(reg, "/tmp/w", fake, testLSPCfg)

	names := reg.Names()
	expected := []string{
		"lsp_definition", "lsp_references", "lsp_hover",
		"lsp_type_definition", "lsp_implementation",
		"lsp_document_symbols", "lsp_workspace_symbols",
	}
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	for _, want := range expected {
		if !nameSet[want] {
			t.Errorf("missing read tool %q in registry: %v", want, names)
		}
	}
	if nameSet["lsp_rename"] {
		t.Error("lsp_rename should not be in read-only registry")
	}
}

func TestRegisterLSPMutatingTools_Names(t *testing.T) {
	fake := &fakeLSPClient{}
	reg := tools.NewRegistry()
	tools.RegisterLSPMutatingTools(reg, "/tmp/w", fake, testLSPCfg)

	names := reg.Names()
	if len(names) != 1 || names[0] != "lsp_rename" {
		t.Fatalf("expected [lsp_rename], got %v", names)
	}
}

func TestLSPDefinition_Dispatch(t *testing.T) {
	fake := &fakeLSPClient{
		defResult: []lspclient.Location{{
			URI:   "file:///tmp/w/main.go",
			Range: lspclient.Range{Start: lspclient.Position{Line: 9, Character: 4}},
		}},
	}
	reg := tools.NewRegistry()
	tools.RegisterLSPReadTools(reg, "/tmp/w", fake, testLSPCfg)

	args := json.RawMessage(`{"file":"main.go","line":6,"character":5}`)
	result, err := reg.Run(context.Background(), "lsp_definition", args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(result, "main.go") {
		t.Fatalf("result: %s", result)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "definition" {
		t.Fatalf("calls: %v", fake.calls)
	}
}

func TestLSPTools_NilClient(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterLSPReadTools(reg, "/tmp/w", nil, testLSPCfg)
	tools.RegisterLSPMutatingTools(reg, "/tmp/w", nil, testLSPCfg)
	if len(reg.Names()) != 0 {
		t.Fatalf("expected 0 tools with nil client, got %v", reg.Names())
	}
}

func TestLSPTools_EmptyConfig(t *testing.T) {
	fake := &fakeLSPClient{}
	reg := tools.NewRegistry()
	tools.RegisterLSPReadTools(reg, "/tmp/w", fake, nil)
	tools.RegisterLSPMutatingTools(reg, "/tmp/w", fake, nil)
	if len(reg.Names()) != 0 {
		t.Fatalf("expected 0 tools with empty config, got %v", reg.Names())
	}
}

func TestLSPRename_AbsentFromReadOnly(t *testing.T) {
	fake := &fakeLSPClient{}
	reg := tools.DefaultReadOnly("/tmp/w", "", nil, nil, "", nil, nil, fake, testLSPCfg)
	for _, n := range reg.Names() {
		if n == "lsp_rename" {
			t.Fatal("lsp_rename should not appear in read-only registry")
		}
	}
}

func TestLSPRename_AbsentFromReadOnlyPlan(t *testing.T) {
	fake := &fakeLSPClient{}
	reg := tools.DefaultReadOnlyPlan("/tmp/w", "", nil, nil, "", nil, nil, fake, testLSPCfg)
	for _, n := range reg.Names() {
		if n == "lsp_rename" {
			t.Fatal("lsp_rename should not appear in plan registry")
		}
	}
}

func TestLSPRename_PresentInDefault(t *testing.T) {
	fake := &fakeLSPClient{}
	reg := tools.Default("/tmp/w", "", nil, nil, nil, "", nil, nil, nil, fake, testLSPCfg)
	found := false
	for _, n := range reg.Names() {
		if n == "lsp_rename" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected lsp_rename in Default registry: %v", reg.Names())
	}
}

func TestLSPRename_LocksOriginalFileOnlyOnce(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("func Foo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	fake := &fakeLSPClient{
		renameResult: &lspclient.WorkspaceEdit{
			Changes: map[string][]lspclient.TextEdit{
				uri: {{
					Range: lspclient.Range{
						Start: lspclient.Position{Line: 0, Character: 5},
						End:   lspclient.Position{Line: 0, Character: 8},
					},
					NewText: "Bar",
				}},
			},
		},
	}
	reg := tools.NewRegistry()
	tools.RegisterLSPMutatingTools(reg, root, fake, testLSPCfg)

	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := reg.Run(context.Background(), "lsp_rename", json.RawMessage(`{"file":"main.go","line":1,"character":6,"new_name":"Bar"}`))
		done <- result{out: out, err: err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("Run: %v", res.err)
		}
		if !strings.Contains(res.out, "Renamed across 1 file") {
			t.Fatalf("unexpected output: %s", res.out)
		}
	case <-time.After(time.Second):
		t.Fatal("lsp_rename deadlocked while locking original file")
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != "func Bar() {}\n" {
		t.Fatalf("renamed content = %q", got)
	}
}
