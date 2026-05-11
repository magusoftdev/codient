package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDelegateTask_BuildParent_AllModesAllowed(t *testing.T) {
	r := NewRegistry()
	var gotMode, gotTask, gotCtx string
	RegisterDelegateTask(r, "build", nil, func(_ context.Context, mode, task, extraContext, _ string) (string, error) {
		gotMode = mode
		gotTask = task
		gotCtx = extraContext
		return "sub-agent reply", nil
	})

	for _, mode := range []string{"build", "ask", "plan"} {
		args, _ := json.Marshal(map[string]string{"mode": mode, "task": "do stuff", "context": "some context"})
		out, err := r.Run(context.Background(), "delegate_task", args)
		if err != nil {
			t.Fatalf("mode %s: %v", mode, err)
		}
		if gotMode != mode {
			t.Fatalf("mode %s: got mode %q", mode, gotMode)
		}
		if gotTask != "do stuff" {
			t.Fatalf("mode %s: got task %q", mode, gotTask)
		}
		if gotCtx != "some context" {
			t.Fatalf("mode %s: got context %q", mode, gotCtx)
		}
		if out != "sub-agent reply" {
			t.Fatalf("mode %s: got output %q", mode, out)
		}
	}
}

func TestDelegateTask_AskParent_OnlyAskAllowed(t *testing.T) {
	r := NewRegistry()
	RegisterDelegateTask(r, "ask", nil, func(_ context.Context, mode, task, _, _ string) (string, error) {
		return "ok", nil
	})

	args, _ := json.Marshal(map[string]string{"mode": "ask", "task": "research"})
	_, err := r.Run(context.Background(), "delegate_task", args)
	if err != nil {
		t.Fatalf("ask mode should be allowed: %v", err)
	}

	// build should be rejected
	args, _ = json.Marshal(map[string]string{"mode": "build", "task": "write code"})
	_, err = r.Run(context.Background(), "delegate_task", args)
	if err == nil {
		t.Fatal("build mode should be rejected for ask parent")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDelegateTask_PlanParent_OnlyAskAllowed(t *testing.T) {
	r := NewRegistry()
	RegisterDelegateTask(r, "plan", nil, func(_ context.Context, mode, task, _, _ string) (string, error) {
		return "ok", nil
	})

	args, _ := json.Marshal(map[string]string{"mode": "ask", "task": "research"})
	_, err := r.Run(context.Background(), "delegate_task", args)
	if err != nil {
		t.Fatalf("ask mode should be allowed for plan parent: %v", err)
	}

	args, _ = json.Marshal(map[string]string{"mode": "build", "task": "write code"})
	_, err = r.Run(context.Background(), "delegate_task", args)
	if err == nil {
		t.Fatal("build mode should be rejected for plan parent")
	}
}

func TestDelegateTask_PlanParent_PlanModeRejected(t *testing.T) {
	r := NewRegistry()
	RegisterDelegateTask(r, "plan", nil, func(_ context.Context, _, _, _, _ string) (string, error) {
		return "ok", nil
	})

	args, _ := json.Marshal(map[string]string{"mode": "plan", "task": "design something"})
	_, err := r.Run(context.Background(), "delegate_task", args)
	if err == nil {
		t.Fatal("plan mode should be rejected for plan parent (only ask allowed)")
	}
}

func TestDelegateTask_EmptyTask_Rejected(t *testing.T) {
	r := NewRegistry()
	RegisterDelegateTask(r, "build", nil, func(_ context.Context, _, _, _, _ string) (string, error) {
		return "ok", nil
	})

	args, _ := json.Marshal(map[string]string{"mode": "ask", "task": ""})
	_, err := r.Run(context.Background(), "delegate_task", args)
	if err == nil {
		t.Fatal("empty task should be rejected")
	}
}

func TestDelegateTask_EmptyMode_Rejected(t *testing.T) {
	r := NewRegistry()
	RegisterDelegateTask(r, "build", nil, func(_ context.Context, _, _, _, _ string) (string, error) {
		return "ok", nil
	})

	args, _ := json.Marshal(map[string]string{"mode": "", "task": "do stuff"})
	_, err := r.Run(context.Background(), "delegate_task", args)
	if err == nil {
		t.Fatal("empty mode should be rejected")
	}
}

func TestDelegateTask_WhitespaceOnlyTask_Rejected(t *testing.T) {
	r := NewRegistry()
	RegisterDelegateTask(r, "build", nil, func(_ context.Context, _, _, _, _ string) (string, error) {
		return "ok", nil
	})

	args, _ := json.Marshal(map[string]string{"mode": "ask", "task": "   "})
	_, err := r.Run(context.Background(), "delegate_task", args)
	if err == nil {
		t.Fatal("whitespace-only task should be rejected")
	}
}

func TestDelegateTask_InvalidJSON_Rejected(t *testing.T) {
	r := NewRegistry()
	RegisterDelegateTask(r, "build", nil, func(_ context.Context, _, _, _, _ string) (string, error) {
		return "ok", nil
	})

	_, err := r.Run(context.Background(), "delegate_task", json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("invalid JSON should be rejected")
	}
}

func TestDelegateTask_OptionalContext(t *testing.T) {
	r := NewRegistry()
	var gotCtx string
	RegisterDelegateTask(r, "build", nil, func(_ context.Context, _, _, extraContext, _ string) (string, error) {
		gotCtx = extraContext
		return "ok", nil
	})

	args, _ := json.Marshal(map[string]string{"mode": "ask", "task": "research"})
	_, err := r.Run(context.Background(), "delegate_task", args)
	if err != nil {
		t.Fatal(err)
	}
	if gotCtx != "" {
		t.Fatalf("expected empty context, got %q", gotCtx)
	}
}

func TestDelegateTask_ModeNormalized(t *testing.T) {
	r := NewRegistry()
	var gotMode string
	RegisterDelegateTask(r, "build", nil, func(_ context.Context, mode, _, _, _ string) (string, error) {
		gotMode = mode
		return "ok", nil
	})

	args, _ := json.Marshal(map[string]string{"mode": "  ASK  ", "task": "research"})
	_, err := r.Run(context.Background(), "delegate_task", args)
	if err != nil {
		t.Fatal(err)
	}
	if gotMode != "ask" {
		t.Fatalf("expected normalized 'ask', got %q", gotMode)
	}
}

func TestDelegateTask_Schema_BuildParent(t *testing.T) {
	r := NewRegistry()
	RegisterDelegateTask(r, "build", nil, func(_ context.Context, _, _, _, _ string) (string, error) {
		return "", nil
	})

	oaiTools := r.OpenAITools()
	var found bool
	for _, tool := range oaiTools {
		if tool.OfFunction == nil {
			continue
		}
		fn := tool.OfFunction.Function
		if fn.Name != "delegate_task" {
			continue
		}
		found = true

		raw, err := json.Marshal(fn.Parameters)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatal("missing properties")
		}
		modeProp, ok := props["mode"].(map[string]any)
		if !ok {
			t.Fatal("missing mode property")
		}
		enumRaw, ok := modeProp["enum"].([]any)
		if !ok {
			t.Fatal("missing mode enum")
		}
		enumVals := make(map[string]bool)
		for _, v := range enumRaw {
			enumVals[v.(string)] = true
		}
		for _, expected := range []string{"build", "ask", "plan"} {
			if !enumVals[expected] {
				t.Errorf("build parent schema missing enum value %q", expected)
			}
		}

		required, ok := schema["required"].([]any)
		if !ok {
			t.Fatal("missing required")
		}
		reqSet := make(map[string]bool)
		for _, v := range required {
			reqSet[v.(string)] = true
		}
		if !reqSet["mode"] || !reqSet["task"] {
			t.Fatalf("expected mode and task in required, got %v", required)
		}
	}
	if !found {
		t.Fatal("delegate_task not found in OpenAITools output")
	}
}

func TestDelegateTask_SandboxProfile_Passed(t *testing.T) {
	r := NewRegistry()
	var gotProfile string
	RegisterDelegateTask(r, "build", []string{"go-build", "node"}, func(_ context.Context, _, _, _, profile string) (string, error) {
		gotProfile = profile
		return "ok", nil
	})

	args, _ := json.Marshal(map[string]string{"mode": "build", "task": "compile", "sandbox_profile": "go-build"})
	_, err := r.Run(context.Background(), "delegate_task", args)
	if err != nil {
		t.Fatal(err)
	}
	if gotProfile != "go-build" {
		t.Fatalf("expected go-build, got %q", gotProfile)
	}
}

func TestDelegateTask_SandboxProfile_Invalid(t *testing.T) {
	r := NewRegistry()
	RegisterDelegateTask(r, "build", []string{"go-build"}, func(_ context.Context, _, _, _, _ string) (string, error) {
		return "ok", nil
	})

	args, _ := json.Marshal(map[string]string{"mode": "build", "task": "compile", "sandbox_profile": "nonexistent"})
	_, err := r.Run(context.Background(), "delegate_task", args)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected sandbox_profile not allowed error, got %v", err)
	}
}

func TestDelegateTask_SandboxProfile_SchemaHasEnum(t *testing.T) {
	r := NewRegistry()
	RegisterDelegateTask(r, "build", []string{"go-build", "node"}, func(_ context.Context, _, _, _, _ string) (string, error) {
		return "", nil
	})

	oaiTools := r.OpenAITools()
	for _, tool := range oaiTools {
		if tool.OfFunction == nil || tool.OfFunction.Function.Name != "delegate_task" {
			continue
		}
		raw, _ := json.Marshal(tool.OfFunction.Function.Parameters)
		var schema map[string]any
		json.Unmarshal(raw, &schema)
		props := schema["properties"].(map[string]any)
		profProp, ok := props["sandbox_profile"].(map[string]any)
		if !ok {
			t.Fatal("sandbox_profile property missing when profiles provided")
		}
		enumRaw := profProp["enum"].([]any)
		if len(enumRaw) != 2 {
			t.Fatalf("expected 2 profile enum values, got %v", enumRaw)
		}
		return
	}
	t.Fatal("delegate_task not found")
}

func TestDelegateTask_SandboxProfile_OmittedWhenNoProfiles(t *testing.T) {
	r := NewRegistry()
	RegisterDelegateTask(r, "build", nil, func(_ context.Context, _, _, _, _ string) (string, error) {
		return "", nil
	})

	oaiTools := r.OpenAITools()
	for _, tool := range oaiTools {
		if tool.OfFunction == nil || tool.OfFunction.Function.Name != "delegate_task" {
			continue
		}
		raw, _ := json.Marshal(tool.OfFunction.Function.Parameters)
		var schema map[string]any
		json.Unmarshal(raw, &schema)
		props := schema["properties"].(map[string]any)
		if _, ok := props["sandbox_profile"]; ok {
			t.Fatal("sandbox_profile should be absent when no profiles configured")
		}
		return
	}
	t.Fatal("delegate_task not found")
}

func TestDelegateTask_Schema_AskParent_EnumLockedToAsk(t *testing.T) {
	r := NewRegistry()
	RegisterDelegateTask(r, "ask", nil, func(_ context.Context, _, _, _, _ string) (string, error) {
		return "", nil
	})

	oaiTools := r.OpenAITools()
	for _, tool := range oaiTools {
		if tool.OfFunction == nil {
			continue
		}
		fn := tool.OfFunction.Function
		if fn.Name != "delegate_task" {
			continue
		}
		raw, _ := json.Marshal(fn.Parameters)
		var schema map[string]any
		json.Unmarshal(raw, &schema)
		props := schema["properties"].(map[string]any)
		modeProp := props["mode"].(map[string]any)
		enumRaw := modeProp["enum"].([]any)
		if len(enumRaw) != 1 || enumRaw[0].(string) != "ask" {
			t.Fatalf("ask parent enum should be [ask], got %v", enumRaw)
		}
		return
	}
	t.Fatal("delegate_task not found")
}
