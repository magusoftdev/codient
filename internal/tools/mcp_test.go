package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"codient/internal/tools"
)

// fakeMCPSource implements tools.MCPToolSource for testing.
type fakeMCPSource struct {
	toolList []tools.MCPToolInfo
	callLog  []callEntry
}

type callEntry struct {
	ServerID string
	ToolName string
	Args     string
}

func (f *fakeMCPSource) Tools() []tools.MCPToolInfo {
	return f.toolList
}

func (f *fakeMCPSource) CallTool(_ context.Context, serverID, toolName string, argsJSON json.RawMessage) (string, error) {
	f.callLog = append(f.callLog, callEntry{ServerID: serverID, ToolName: toolName, Args: string(argsJSON)})
	return "ok from " + serverID + "/" + toolName, nil
}

func TestRegisterMCPTools_NamesAndDescriptions(t *testing.T) {
	src := &fakeMCPSource{
		toolList: []tools.MCPToolInfo{
			{
				ServerID:    "fs",
				Name:        "list_dir",
				Description: "List directory contents",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"path": map[string]any{"type": "string"}},
				},
			},
			{
				ServerID:    "api",
				Name:        "query",
				Description: "Run a query",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		},
	}

	reg := tools.NewRegistry()
	tools.RegisterMCPTools(reg, src)

	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(names), names)
	}
	if names[0] != "mcp__fs__list_dir" {
		t.Errorf("names[0] = %q, want mcp__fs__list_dir", names[0])
	}
	if names[1] != "mcp__api__query" {
		t.Errorf("names[1] = %q, want mcp__api__query", names[1])
	}

	oaiTools := reg.OpenAITools()
	if len(oaiTools) != 2 {
		t.Fatalf("expected 2 OpenAI tools, got %d", len(oaiTools))
	}
	if oaiTools[0].OfFunction == nil {
		t.Fatal("expected OfFunction to be set")
	}
	fnName := oaiTools[0].OfFunction.Function.Name
	if fnName != "mcp__fs__list_dir" {
		t.Errorf("function name = %q, want mcp__fs__list_dir", fnName)
	}
}

func TestRegisterMCPTools_CallDispatch(t *testing.T) {
	src := &fakeMCPSource{
		toolList: []tools.MCPToolInfo{
			{
				ServerID:    "myserver",
				Name:        "greet",
				Description: "Say hello",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"name": map[string]any{"type": "string"}},
				},
			},
		},
	}

	reg := tools.NewRegistry()
	tools.RegisterMCPTools(reg, src)

	args := json.RawMessage(`{"name":"world"}`)
	result, err := reg.Run(context.Background(), "mcp__myserver__greet", args)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result != "ok from myserver/greet" {
		t.Errorf("result = %q, want %q", result, "ok from myserver/greet")
	}
	if len(src.callLog) != 1 {
		t.Fatalf("expected 1 call, got %d", len(src.callLog))
	}
	if src.callLog[0].ServerID != "myserver" || src.callLog[0].ToolName != "greet" {
		t.Errorf("call dispatched to %s/%s, want myserver/greet", src.callLog[0].ServerID, src.callLog[0].ToolName)
	}
}

func TestRegisterMCPTools_NilSource(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterMCPTools(reg, nil)
	if len(reg.Names()) != 0 {
		t.Errorf("expected 0 tools with nil source, got %d", len(reg.Names()))
	}
}
