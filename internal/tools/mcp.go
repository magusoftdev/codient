package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3/shared"
)

// MCPToolSource provides MCP tools and call dispatch.
// Implemented by mcpclient.Manager to avoid a circular import.
type MCPToolSource interface {
	Tools() []MCPToolInfo
	CallTool(ctx context.Context, serverID, toolName string, argsJSON json.RawMessage) (string, error)
}

// MCPToolInfo is the tool metadata the bridge needs from the MCP manager.
type MCPToolInfo struct {
	ServerID    string
	Name        string
	Description string
	InputSchema map[string]any
}

// RegisterMCPTools converts every tool from src into a tools.Tool and registers
// it in r with a namespaced name: mcp__<serverID>__<toolName>.
func RegisterMCPTools(r *Registry, src MCPToolSource) {
	if src == nil {
		return
	}
	for _, t := range src.Tools() {
		regName := "mcp__" + t.ServerID + "__" + t.Name
		desc := fmt.Sprintf("[MCP: %s] %s", t.ServerID, t.Description)

		schema := shared.FunctionParameters(t.InputSchema)
		if schema == nil {
			schema = shared.FunctionParameters{
				"type":       "object",
				"properties": map[string]any{},
			}
		}

		sid, tn := t.ServerID, t.Name
		r.Register(Tool{
			Name:        regName,
			Description: desc,
			Parameters:  schema,
			Run: func(ctx context.Context, args json.RawMessage) (string, error) {
				return src.CallTool(ctx, sid, tn, args)
			},
		})
	}
}
