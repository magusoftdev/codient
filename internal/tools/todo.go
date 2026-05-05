package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3/shared"
)

// TodoItem is one row in the session todo list (OpenCode-style task tracking).
type TodoItem struct {
	Content  string `json:"content"`
	Status   string `json:"status"`   // pending | in_progress | completed | cancelled
	Priority string `json:"priority"` // high | medium | low (optional)
}

// TodoWriter persists todos from the todo_write tool (implemented by the CLI session).
type TodoWriter func(items []TodoItem) error

// RegisterTodoWrite adds the todo_write tool when writer is non-nil.
func RegisterTodoWrite(reg *Registry, writer TodoWriter) {
	if writer == nil {
		return
	}
	reg.Register(Tool{
		Name: "todo_write",
		Description: "Replace the session todo list. Use often for multi-step work: set status to in_progress for the active item and completed as soon as each item is done. " +
			"Statuses: pending, in_progress, completed, cancelled. Priority: high, medium, low.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"todos": map[string]any{
					"type":        "array",
					"description": "Full todo list for this session (replaces any previous list).",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"content":  map[string]any{"type": "string", "description": "Brief task description"},
							"status":   map[string]any{"type": "string", "description": "pending | in_progress | completed | cancelled"},
							"priority": map[string]any{"type": "string", "description": "high | medium | low"},
						},
						"required": []string{"content", "status"},
					},
				},
			},
			"required":             []string{"todos"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var req struct {
				Todos []TodoItem `json:"todos"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return "", fmt.Errorf("todo_write: %w", err)
			}
			for i := range req.Todos {
				req.Todos[i].Content = strings.TrimSpace(req.Todos[i].Content)
				req.Todos[i].Status = strings.TrimSpace(strings.ToLower(req.Todos[i].Status))
				req.Todos[i].Priority = strings.TrimSpace(strings.ToLower(req.Todos[i].Priority))
				if req.Todos[i].Content == "" {
					return "", fmt.Errorf("todo_write: item %d: content is required", i)
				}
				switch req.Todos[i].Status {
				case "pending", "in_progress", "completed", "cancelled":
				default:
					return "", fmt.Errorf("todo_write: item %d: invalid status %q", i, req.Todos[i].Status)
				}
				if req.Todos[i].Priority != "" {
					switch req.Todos[i].Priority {
					case "high", "medium", "low":
					default:
						return "", fmt.Errorf("todo_write: item %d: invalid priority %q", i, req.Todos[i].Priority)
					}
				}
			}
			if err := writer(req.Todos); err != nil {
				return "", err
			}
			n := len(req.Todos)
			open := 0
			for _, t := range req.Todos {
				if t.Status != "completed" && t.Status != "cancelled" {
					open++
				}
			}
			return fmt.Sprintf("todo_write: saved %d task(s) (%d open).", n, open), nil
		},
	})
}
