package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTodoWrite_validation(t *testing.T) {
	var got []TodoItem
	reg := NewRegistry()
	RegisterTodoWrite(reg, func(items []TodoItem) error {
		got = items
		return nil
	})
	_, err := reg.Run(context.Background(), "todo_write", json.RawMessage(`{"todos":[{"content":"x","status":"bogus"}]}`))
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
	out, err := reg.Run(context.Background(), "todo_write", json.RawMessage(`{"todos":[{"content":"Fix bug","status":"pending"},{"content":"Ship","status":"in_progress","priority":"high"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Content != "Fix bug" || got[1].Priority != "high" {
		t.Fatalf("unexpected persisted todos: %+v", got)
	}
	if out == "" {
		t.Fatal("expected non-empty tool reply")
	}
}
