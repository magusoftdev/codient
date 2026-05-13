package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"codient/internal/config"
	"codient/internal/tools"
	"github.com/openai/openai-go/v3"
)

func TestRunner_SequentialToolExecution(t *testing.T) {
	var mu sync.Mutex
	var events []string

	reg := tools.NewRegistry()
	reg.Register(tools.Tool{
		Name: "slow_tool",
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			mu.Lock()
			events = append(events, "slow_start")
			mu.Unlock()
			time.Sleep(100 * time.Millisecond)
			mu.Lock()
			events = append(events, "slow_end")
			mu.Unlock()
			return "slow-done", nil
		},
	})
	reg.Register(tools.Tool{
		Name: "fast_tool",
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			mu.Lock()
			events = append(events, "fast_start")
			mu.Unlock()
			return "fast-done", nil
		},
	})

	sequentialRound := `{
  "choices": [{
    "message": {
      "role": "assistant",
      "tool_calls": [
        {
          "id": "call_1",
          "type": "function",
          "function": {
            "name": "slow_tool",
            "arguments": "{}"
          }
        },
        {
          "id": "call_2",
          "type": "function",
          "function": {
            "name": "fast_tool",
            "arguments": "{\"wait_for_previous\": true}"
          }
        }
      ]
    },
    "finish_reason": "tool_calls"
  }]
}`
	final := `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`

	llm := &mockLLM{script: []string{sequentialRound, final}}
	runner := &Runner{
		LLM:   llm,
		Tools: reg,
		Cfg:   &config.Config{},
	}

	_, _, err := runner.Run(context.Background(), "sys", openai.UserMessage("go"), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %v", events)
	}
	// Expected order: slow_start, slow_end, fast_start
	if events[0] != "slow_start" || events[1] != "slow_end" || events[2] != "fast_start" {
		t.Errorf("unexpected event order: %v", events)
	}
}

func TestRunner_ParallelToolExecution(t *testing.T) {
	var mu sync.Mutex
	var events []string

	reg := tools.NewRegistry()
	reg.Register(tools.Tool{
		Name: "slow_tool",
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			mu.Lock()
			events = append(events, "slow_start")
			mu.Unlock()
			time.Sleep(200 * time.Millisecond)
			mu.Lock()
			events = append(events, "slow_end")
			mu.Unlock()
			return "slow-done", nil
		},
	})
	reg.Register(tools.Tool{
		Name: "fast_tool",
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			// Small delay to ensure slow_tool starts first if they were truly sequential
			time.Sleep(50 * time.Millisecond)
			mu.Lock()
			events = append(events, "fast_start")
			mu.Unlock()
			return "fast-done", nil
		},
	})

	parallelRound := `{
  "choices": [{
    "message": {
      "role": "assistant",
      "tool_calls": [
        {
          "id": "call_1",
          "type": "function",
          "function": {
            "name": "slow_tool",
            "arguments": "{}"
          }
        },
        {
          "id": "call_2",
          "type": "function",
          "function": {
            "name": "fast_tool",
            "arguments": "{}"
          }
        }
      ]
    },
    "finish_reason": "tool_calls"
  }]
}`
	final := `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`

	llm := &mockLLM{script: []string{parallelRound, final}}
	runner := &Runner{
		LLM:   llm,
		Tools: reg,
		Cfg:   &config.Config{},
	}

	_, _, err := runner.Run(context.Background(), "sys", openai.UserMessage("go"), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %v", events)
	}
	// Expected order: slow_start, fast_start, slow_end (because fast_tool only sleeps 50ms, slow_tool sleeps 200ms)
	if events[0] != "slow_start" || events[1] != "fast_start" || events[2] != "slow_end" {
		t.Errorf("unexpected parallel event order: %v", events)
	}
}

func TestRunner_SequentialXMLToolExecution(t *testing.T) {
	var mu sync.Mutex
	var events []string

	reg := tools.NewRegistry()
	reg.Register(tools.Tool{
		Name: "slow_tool",
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			mu.Lock()
			events = append(events, "slow_start")
			mu.Unlock()
			time.Sleep(100 * time.Millisecond)
			mu.Lock()
			events = append(events, "slow_end")
			mu.Unlock()
			return "slow-done", nil
		},
	})
	reg.Register(tools.Tool{
		Name: "fast_tool",
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			mu.Lock()
			events = append(events, "fast_start")
			mu.Unlock()
			return "fast-done", nil
		},
	})

	// XML-style tool calls (used by some local models)
	xmlRound := `{
  "choices": [{
    "message": {
      "role": "assistant",
      "content": "I'll run these in order:\n\n<tool_call>\n{\"name\": \"slow_tool\", \"arguments\": {}}\n</tool_call>\n\n<tool_call>\n{\"name\": \"fast_tool\", \"arguments\": {\"wait_for_previous\": true}}\n</tool_call>"
    },
    "finish_reason": "stop"
  }]
}`
	final := `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`

	llm := &mockLLM{script: []string{xmlRound, final}}
	runner := &Runner{
		LLM:   llm,
		Tools: reg,
		Cfg:   &config.Config{},
	}

	_, _, err := runner.Run(context.Background(), "sys", openai.UserMessage("go"), nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %v", events)
	}
	// Expected order: slow_start, slow_end, fast_start
	if events[0] != "slow_start" || events[1] != "slow_end" || events[2] != "fast_start" {
		t.Errorf("unexpected XML event order: %v", events)
	}
}
