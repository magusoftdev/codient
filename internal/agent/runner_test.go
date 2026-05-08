package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"codient/internal/config"
	"codient/internal/openaiclient"
	"codient/internal/tokentracker"
	"codient/internal/tools"
)

type mockLLM struct {
	model string
	calls int
	// script returns JSON completions in order (tool round then final text, etc.)
	script []string
}

func (m *mockLLM) Model() string { return m.model }

func (m *mockLLM) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	if m.calls >= len(m.script) {
		return nil, context.Canceled
	}
	raw := m.script[m.calls]
	m.calls++
	var out openai.ChatCompletion
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// assertStreamUnusedLLM records whether ChatCompletionStream was invoked. The agent must use
// non-streaming ChatCompletion when the request includes tools and StreamWithTools is false
// (local servers often drop tool_calls over SSE).
type assertStreamUnusedLLM struct {
	t           *testing.T
	model       string
	script      []string
	calls       int
	streamCalls int
}

func (m *assertStreamUnusedLLM) Model() string { return m.model }

func (m *assertStreamUnusedLLM) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	if m.calls >= len(m.script) {
		return nil, context.Canceled
	}
	raw := m.script[m.calls]
	m.calls++
	var out openai.ChatCompletion
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *assertStreamUnusedLLM) ChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams, w io.Writer, _ ...openaiclient.StreamOption) (*openai.ChatCompletion, error) {
	m.streamCalls++
	m.t.Fatalf("ChatCompletionStream should not run for tool requests when StreamWithTools is false (would drop tool_calls on many local servers)")
	return nil, context.Canceled
}

func TestRunner_WithStreamWriterUsesChatCompletionWhenToolsPresent(t *testing.T) {
	toolRound := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": {
          "name": "echo",
          "arguments": "{\"message\":\"tool-out\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}`
	final := `{
  "id": "y",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "done"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &assertStreamUnusedLLM{t: t, model: "m", script: []string{toolRound, final}}
	reg := tools.NewRegistry()
	reg.Register(mustEchoTool(t))
	cfg := &config.Config{StreamWithTools: false}
	r := &Runner{LLM: llm, Cfg: cfg, Tools: reg}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("call echo"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if out != "done" {
		t.Fatalf("got %q", out)
	}
	if llm.streamCalls != 0 {
		t.Fatalf("expected no streaming calls, got %d", llm.streamCalls)
	}
}

func TestRunner_DirectReply(t *testing.T) {
	js := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "hello user"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &mockLLM{model: "m", script: []string{js}}
	reg := tools.NewRegistry()
	cfg := &config.Config{}
	r := &Runner{LLM: llm, Cfg: cfg, Tools: reg}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("hi"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello user" {
		t.Fatalf("got %q", out)
	}
}

func TestRunner_ToolThenReply(t *testing.T) {
	toolRound := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": {
          "name": "echo",
          "arguments": "{\"message\":\"tool-out\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}`
	final := `{
  "id": "y",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "done"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &mockLLM{model: "m", script: []string{toolRound, final}}
	reg := tools.NewRegistry()
	reg.Register(mustEchoTool(t))
	cfg := &config.Config{}
	r := &Runner{LLM: llm, Cfg: cfg, Tools: reg}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("call echo"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "done" {
		t.Fatalf("got %q", out)
	}
	if llm.calls != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", llm.calls)
	}
}

func TestRunner_ToolErrorSurfaced(t *testing.T) {
	toolRound := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": {
          "name": "missing_tool",
          "arguments": "{}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}`
	final := `{
  "id": "y",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "handled"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &mockLLM{model: "m", script: []string{toolRound, final}}
	reg := tools.NewRegistry()
	reg.Register(mustEchoTool(t))
	cfg := &config.Config{}
	r := &Runner{LLM: llm, Cfg: cfg, Tools: reg}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "handled" {
		t.Fatalf("got %q", out)
	}
}

func TestRunner_EmptyChoices(t *testing.T) {
	js := `{"id":"x","object":"chat.completion","created":1,"model":"m","choices":[]}`
	llm := &mockLLM{model: "m", script: []string{js}}
	r := &Runner{LLM: llm, Cfg: &config.Config{}, Tools: tools.NewRegistry()}
	_, _, err := r.Run(context.Background(), "", openai.UserMessage("hi"), nil)
	if err == nil || !strings.Contains(err.Error(), "empty choices") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunner_SystemPrompt(t *testing.T) {
	js := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "ok"},
    "finish_reason": "stop"
  }]
}`
	llm := &mockLLM{model: "m", script: []string{js}}
	r := &Runner{LLM: llm, Cfg: &config.Config{}, Tools: tools.NewRegistry()}
	_, _, err := r.Run(context.Background(), "sys", openai.UserMessage("user"), nil)
	if err != nil {
		t.Fatal(err)
	}
}

type captureLLM struct {
	model  string
	script []string
	calls  int
	// MsgJSON is the JSON encoding of params.Messages for each ChatCompletion call.
	MsgJSON []json.RawMessage
}

func (c *captureLLM) Model() string { return c.model }

func (c *captureLLM) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	if c.calls >= len(c.script) {
		return nil, context.Canceled
	}
	rawMsgs, _ := json.Marshal(params.Messages)
	c.MsgJSON = append(c.MsgJSON, rawMsgs)
	raw := c.script[c.calls]
	c.calls++
	var out openai.ChatCompletion
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func mustWriteFileTool(t *testing.T) tools.Tool {
	t.Helper()
	return tools.Tool{
		Name:        "write_file",
		Description: "write",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required":             []string{"path", "content"},
			"additionalProperties": false,
		},
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			return "wrote f (overwrite)", nil
		},
	}
}

func mustReadFileTool(t *testing.T) tools.Tool {
	t.Helper()
	return tools.Tool{
		Name:        "read_file",
		Description: "read",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		},
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			return "ok", nil
		},
	}
}

func mustEchoTool(t *testing.T) tools.Tool {
	t.Helper()
	return tools.Tool{
		Name:        "echo",
		Description: "echo",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string"},
			},
			"required":             []string{"message"},
			"additionalProperties": false,
		},
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", err
			}
			return p.Message, nil
		},
	}
}

func TestRunner_AutoCheckInjectsOnFailure(t *testing.T) {
	toolRound := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": {
          "name": "write_file",
          "arguments": "{\"path\":\"f.txt\",\"content\":\"x\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}`
	final := `{
  "id": "y",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "fixed"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &captureLLM{model: "m", script: []string{toolRound, final}}
	reg := tools.NewRegistry()
	reg.Register(mustWriteFileTool(t))
	cfg := &config.Config{}
	r := &Runner{
		LLM: llm, Cfg: cfg, Tools: reg,
		AutoCheck: func(context.Context) AutoCheckOutcome {
			return AutoCheckOutcome{Inject: "[auto-check] BUILD FAIL", Progress: "auto-check: test · exit=1"}
		},
	}
	_, _, err := r.Run(context.Background(), "", openai.UserMessage("edit"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(llm.MsgJSON) < 2 {
		t.Fatalf("expected 2 LLM calls, got %d", len(llm.MsgJSON))
	}
	if !strings.Contains(string(llm.MsgJSON[1]), "[auto-check] BUILD FAIL") {
		t.Fatalf("second request should include auto-check inject: %s", string(llm.MsgJSON[1]))
	}
}

func TestRunner_AutoCheckSilentOnSuccess(t *testing.T) {
	toolRound := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": {
          "name": "write_file",
          "arguments": "{\"path\":\"f.txt\",\"content\":\"x\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}`
	final := `{
  "id": "y",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "done"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &captureLLM{model: "m", script: []string{toolRound, final}}
	reg := tools.NewRegistry()
	reg.Register(mustWriteFileTool(t))
	cfg := &config.Config{}
	r := &Runner{
		LLM: llm, Cfg: cfg, Tools: reg,
		AutoCheck: func(context.Context) AutoCheckOutcome {
			return AutoCheckOutcome{Progress: "auto-check: ok"}
		},
	}
	_, _, err := r.Run(context.Background(), "", openai.UserMessage("edit"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(llm.MsgJSON) < 2 {
		t.Fatalf("expected 2 LLM calls, got %d", len(llm.MsgJSON))
	}
	if strings.Contains(string(llm.MsgJSON[1]), "[auto-check]") {
		t.Fatalf("should not inject on success: %s", string(llm.MsgJSON[1]))
	}
}

func TestRunner_AutoCheckSkipsReadOnly(t *testing.T) {
	toolRound := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": {
          "name": "read_file",
          "arguments": "{\"path\":\"f.txt\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}`
	final := `{
  "id": "y",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "done"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &captureLLM{model: "m", script: []string{toolRound, final}}
	reg := tools.NewRegistry()
	reg.Register(mustReadFileTool(t))
	cfg := &config.Config{}
	var runs int
	r := &Runner{
		LLM: llm, Cfg: cfg, Tools: reg,
		AutoCheck: func(context.Context) AutoCheckOutcome {
			runs++
			return AutoCheckOutcome{Inject: "should not run"}
		},
	}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("read"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if runs != 0 {
		t.Fatalf("auto-check should not run for read-only tools, runs=%d", runs)
	}
	if out != "done" {
		t.Fatalf("got %q", out)
	}
}

func TestRunner_PostReplyCheckInjects(t *testing.T) {
	first := `{
  "id": "a",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "initial suggestions"
    },
    "finish_reason": "stop"
  }]
}`
	second := `{
  "id": "b",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "verified summary"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &captureLLM{model: "m", script: []string{first, second}}
	reg := tools.NewRegistry()
	cfg := &config.Config{}
	r := &Runner{
		LLM: llm, Cfg: cfg, Tools: reg,
		PostReplyCheck: func(_ context.Context, info PostReplyCheckInfo) string {
			if strings.Contains(info.Reply, "initial") {
				return "[verify] check your suggestions"
			}
			return ""
		},
	}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("suggest improvements"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "verified summary" {
		t.Fatalf("expected final reply from second call, got %q", out)
	}
	if llm.calls != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", llm.calls)
	}
	if !strings.Contains(string(llm.MsgJSON[1]), "[verify] check your suggestions") {
		t.Fatalf("second request should contain injected verification message: %s", string(llm.MsgJSON[1]))
	}
}

func TestRunner_PostReplyCheckFiresOnce(t *testing.T) {
	first := `{
  "id": "a",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "suggestions"
    },
    "finish_reason": "stop"
  }]
}`
	second := `{
  "id": "b",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "final answer"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &captureLLM{model: "m", script: []string{first, second}}
	reg := tools.NewRegistry()
	cfg := &config.Config{}
	var calls int
	r := &Runner{
		LLM: llm, Cfg: cfg, Tools: reg,
		PostReplyCheck: func(_ context.Context, _ PostReplyCheckInfo) string {
			calls++
			return "verify"
		},
	}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("suggest"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("PostReplyCheck should fire exactly once, got %d", calls)
	}
	if out != "final answer" {
		t.Fatalf("expected second reply, got %q", out)
	}
}

func TestRunner_DelegateTaskIntegration(t *testing.T) {
	// The parent LLM calls delegate_task, then returns the sub-agent result.
	delegateCall := `{
  "id": "d1",
  "object": "chat.completion",
  "created": 1,
  "model": "parent-model",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "tool_calls": [{
        "id": "call_delegate",
        "type": "function",
        "function": {
          "name": "delegate_task",
          "arguments": "{\"mode\":\"ask\",\"task\":\"find main.go\",\"context\":\"look in the root\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}`
	parentFinal := `{
  "id": "d2",
  "object": "chat.completion",
  "created": 2,
  "model": "parent-model",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "The sub-agent found the file."
    },
    "finish_reason": "stop"
  }]
}`
	llm := &captureLLM{model: "parent-model", script: []string{delegateCall, parentFinal}}

	reg := tools.NewRegistry()
	reg.Register(mustEchoTool(t))

	var delegatedMode, delegatedTask, delegatedCtx string
	tools.RegisterDelegateTask(reg, "build", func(_ context.Context, mode, task, ctx string) (string, error) {
		delegatedMode = mode
		delegatedTask = task
		delegatedCtx = ctx
		return "found main.go at root/main.go", nil
	})

	cfg := &config.Config{}
	r := &Runner{LLM: llm, Cfg: cfg, Tools: reg}
	out, _, err := r.Run(context.Background(), "sys", openai.UserMessage("find main.go"), nil)
	if err != nil {
		t.Fatal(err)
	}

	if delegatedMode != "ask" {
		t.Fatalf("delegated mode: got %q want ask", delegatedMode)
	}
	if delegatedTask != "find main.go" {
		t.Fatalf("delegated task: got %q", delegatedTask)
	}
	if delegatedCtx != "look in the root" {
		t.Fatalf("delegated context: got %q", delegatedCtx)
	}
	if out != "The sub-agent found the file." {
		t.Fatalf("final reply: got %q", out)
	}
	if llm.calls != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", llm.calls)
	}
	// Verify the tool result was included in the second LLM call
	if !strings.Contains(string(llm.MsgJSON[1]), "found main.go at root/main.go") {
		t.Fatalf("second LLM call should contain delegate result: %s", string(llm.MsgJSON[1]))
	}
}

func TestRunner_DelegateTask_PrivilegeEscalationBlocked(t *testing.T) {
	// Ask-mode parent tries to delegate to build mode — should be rejected.
	delegateCall := `{
  "id": "d1",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "tool_calls": [{
        "id": "call_delegate",
        "type": "function",
        "function": {
          "name": "delegate_task",
          "arguments": "{\"mode\":\"build\",\"task\":\"write a file\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}`
	final := `{
  "id": "d2",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "escalation failed as expected"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &captureLLM{model: "m", script: []string{delegateCall, final}}
	reg := tools.NewRegistry()
	var delegateCalled bool
	tools.RegisterDelegateTask(reg, "ask", func(_ context.Context, _, _, _ string) (string, error) {
		delegateCalled = true
		return "should not reach here", nil
	})

	cfg := &config.Config{}
	r := &Runner{LLM: llm, Cfg: cfg, Tools: reg}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("try escalation"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if delegateCalled {
		t.Fatal("delegate runner should NOT have been called for disallowed mode")
	}
	// The tool error should be in the second LLM call messages
	if !strings.Contains(string(llm.MsgJSON[1]), "not allowed") {
		t.Fatalf("second LLM call should contain mode rejection error: %s", string(llm.MsgJSON[1]))
	}
	if out != "escalation failed as expected" {
		t.Fatalf("got %q", out)
	}
}

func TestRunner_DelegateTask_ParallelCalls(t *testing.T) {
	// Two parallel delegate_task calls in one round.
	parallelRound := `{
  "id": "p1",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "tool_calls": [
        {
          "id": "call_a",
          "type": "function",
          "function": {
            "name": "delegate_task",
            "arguments": "{\"mode\":\"ask\",\"task\":\"task A\"}"
          }
        },
        {
          "id": "call_b",
          "type": "function",
          "function": {
            "name": "delegate_task",
            "arguments": "{\"mode\":\"ask\",\"task\":\"task B\"}"
          }
        }
      ]
    },
    "finish_reason": "tool_calls"
  }]
}`
	final := `{
  "id": "p2",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "both tasks completed"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &captureLLM{model: "m", script: []string{parallelRound, final}}
	reg := tools.NewRegistry()

	var mu sync.Mutex
	var tasks []string
	tools.RegisterDelegateTask(reg, "build", func(_ context.Context, _, task, _ string) (string, error) {
		mu.Lock()
		tasks = append(tasks, task)
		mu.Unlock()
		return "result for " + task, nil
	})

	cfg := &config.Config{}
	r := &Runner{LLM: llm, Cfg: cfg, Tools: reg}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("do both"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "both tasks completed" {
		t.Fatalf("got %q", out)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 delegated tasks, got %d", len(tasks))
	}
	// The results should appear in the second LLM call
	msg2 := string(llm.MsgJSON[1])
	if !strings.Contains(msg2, "result for task A") {
		t.Fatalf("missing result for task A: %s", msg2)
	}
	if !strings.Contains(msg2, "result for task B") {
		t.Fatalf("missing result for task B: %s", msg2)
	}
}

func TestRunner_PostReplyCheckNilNoop(t *testing.T) {
	js := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "direct answer"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &mockLLM{model: "m", script: []string{js}}
	reg := tools.NewRegistry()
	cfg := &config.Config{}
	r := &Runner{LLM: llm, Cfg: cfg, Tools: reg}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("question"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "direct answer" {
		t.Fatalf("got %q", out)
	}
	if llm.calls != 1 {
		t.Fatalf("expected 1 LLM call, got %d", llm.calls)
	}
}

func TestRunner_RequestsFinalResponseWhenModelReturnsEmpty(t *testing.T) {
	empty := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": ""
    },
    "finish_reason": "stop"
  }]
}`
	final := `{
  "id": "y",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "final text"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &mockLLM{model: "m", script: []string{empty, final}}
	r := &Runner{LLM: llm, Cfg: &config.Config{}, Tools: tools.NewRegistry()}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("hello"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "final text" {
		t.Fatalf("got %q", out)
	}
	if llm.calls != 2 {
		t.Fatalf("expected 2 llm calls, got %d", llm.calls)
	}
}

func TestRunner_NudgesIntentOnlyProseThenRunsTools(t *testing.T) {
	intentOnly := `{
  "id": "a",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "Let me search the codebase for that symbol."
    },
    "finish_reason": "stop"
  }]
}`
	toolRound := `{
  "id": "b",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": {
          "name": "echo",
          "arguments": "{\"message\":\"hi\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}`
	final := `{
  "id": "c",
  "object": "chat.completion",
  "created": 3,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "Echo said hi."
    },
    "finish_reason": "stop"
  }]
}`
	llm := &mockLLM{model: "m", script: []string{intentOnly, toolRound, final}}
	reg := tools.NewRegistry()
	reg.Register(mustEchoTool(t))
	r := &Runner{LLM: llm, Cfg: &config.Config{}, Tools: reg}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("find it"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Echo said hi." {
		t.Fatalf("got %q", out)
	}
	if llm.calls != 3 {
		t.Fatalf("expected 3 llm calls, got %d", llm.calls)
	}
}

func TestRunner_NoToolIntentNudgeForDirectAnswer(t *testing.T) {
	direct := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "Use a larger buffer for the ring."
    },
    "finish_reason": "stop"
  }]
}`
	llm := &mockLLM{model: "m", script: []string{direct}}
	reg := tools.NewRegistry()
	reg.Register(mustEchoTool(t))
	r := &Runner{LLM: llm, Cfg: &config.Config{}, Tools: reg}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("why fail?"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Use a larger buffer for the ring." {
		t.Fatalf("got %q", out)
	}
	if llm.calls != 1 {
		t.Fatalf("expected 1 llm call, got %d", llm.calls)
	}
}

func TestRunner_ParsesToolCallsFromReasoningContentWhenContentEmpty(t *testing.T) {
	withReasoningToolCall := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "reasoning_content": "<tool_call><function=echo><parameter=message>hi</parameter></function></tool_call>",
      "tool_calls": []
    },
    "finish_reason": "stop"
  }]
}`
	final := `{
  "id": "y",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "done"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &mockLLM{model: "m", script: []string{withReasoningToolCall, final}}
	reg := tools.NewRegistry()
	reg.Register(mustEchoTool(t))
	r := &Runner{LLM: llm, Cfg: &config.Config{}, Tools: reg}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("hello"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "done" {
		t.Fatalf("got %q", out)
	}
	if llm.calls != 2 {
		t.Fatalf("expected 2 llm calls, got %d", llm.calls)
	}
}

func TestRunner_EmitsIntentForToolRound(t *testing.T) {
	toolRound := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "I will inspect one file first.",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": {
          "name": "echo",
          "arguments": "{\"message\":\"x\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}`
	final := `{
  "id": "y",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "done"
    },
    "finish_reason": "stop"
  }]
}`
	llm := &mockLLM{model: "m", script: []string{toolRound, final}}
	reg := tools.NewRegistry()
	reg.Register(mustEchoTool(t))
	var gotIntent string
	r := &Runner{
		LLM: llm, Cfg: &config.Config{}, Tools: reg,
		OnIntent: func(text string) { gotIntent = text },
	}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("hi"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "done" {
		t.Fatalf("got %q", out)
	}
	if gotIntent != "I will inspect one file first." {
		t.Fatalf("intent = %q", gotIntent)
	}
}

func TestRunner_NoSyntheticIntentWhenContentEmpty(t *testing.T) {
	toolRound := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": {
          "name": "read_file",
          "arguments": "{\"path\":\"main.go\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }],
  "usage": { "prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2 }
}`
	final := `{
  "id": "y",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "done"
    },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2 }
}`
	llm := &mockLLM{model: "m", script: []string{toolRound, final}}
	reg := tools.NewRegistry()
	reg.Register(mustEchoTool(t))
	var gotIntent string
	r := &Runner{
		LLM: llm, Cfg: &config.Config{}, Tools: reg,
		OnIntent: func(text string) { gotIntent = text },
	}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("read main.go"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "done" {
		t.Fatalf("got %q", out)
	}
	if gotIntent != "" {
		t.Fatalf("no synthetic intent expected when model provides no prose; got %q", gotIntent)
	}
}

func TestRunner_ReplyFromReasoningContentWhenContentEmpty(t *testing.T) {
	js := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "reasoning_content": "Here is my response to your question."
    },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2 }
}`
	llm := &mockLLM{model: "m", script: []string{js}}
	reg := tools.NewRegistry()
	cfg := &config.Config{}
	tr := &tokentracker.Tracker{}
	r := &Runner{LLM: llm, Cfg: cfg, Tools: reg, Tracker: tr}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("hi"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Here is my response to your question." {
		t.Fatalf("expected reasoning_content as reply, got %q", out)
	}
}

func TestRunner_EmitsIntentFromReasoningContent(t *testing.T) {
	toolRound := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "reasoning_content": "I will analyze the project structure first.",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": {
          "name": "echo",
          "arguments": "{\"message\":\"x\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }],
  "usage": { "prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2 }
}`
	final := `{
  "id": "y",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "done"
    },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2 }
}`
	llm := &mockLLM{model: "m", script: []string{toolRound, final}}
	reg := tools.NewRegistry()
	reg.Register(mustEchoTool(t))
	var gotIntent string
	r := &Runner{
		LLM: llm, Cfg: &config.Config{}, Tools: reg,
		OnIntent: func(text string) { gotIntent = text },
	}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("hi"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "done" {
		t.Fatalf("got %q", out)
	}
	if gotIntent != "I will analyze the project structure first." {
		t.Fatalf("intent = %q, want reasoning_content text", gotIntent)
	}
}

func TestRunner_NoSyntheticIntentForTextToolCallPath(t *testing.T) {
	// Simulates models like Qwen3.5 that embed XML tool calls in
	// reasoning_content with no natural language surrounding them.
	// No synthetic intent should be fabricated — only real model prose
	// should be surfaced as intent.
	toolRound := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "reasoning_content": "<tool_call>{\"name\":\"read_file\",\"arguments\":{\"path\":\"main.go\"}}</tool_call>"
    },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2 }
}`
	final := `{
  "id": "y",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "Here is the file."
    },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2 }
}`
	llm := &mockLLM{model: "m", script: []string{toolRound, final}}
	reg := tools.NewRegistry()
	reg.Register(mustReadFileTool(t))
	var gotIntent string
	r := &Runner{
		LLM: llm, Cfg: &config.Config{}, Tools: reg,
		OnIntent: func(text string) { gotIntent = text },
	}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("show main.go"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Here is the file." {
		t.Fatalf("got %q", out)
	}
	if gotIntent != "" {
		t.Fatalf("no synthetic intent expected for text tool call path; got %q", gotIntent)
	}
}

func TestRunner_MaxTurnsAllowsSingleCompletion(t *testing.T) {
	js := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": { "role": "assistant", "content": "ok" },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2 }
}`
	llm := &mockLLM{model: "m", script: []string{js}}
	reg := tools.NewRegistry()
	cfg := &config.Config{}
	tr := &tokentracker.Tracker{}
	r := &Runner{LLM: llm, Cfg: cfg, Tools: reg, MaxTurns: 1, Tracker: tr}
	out, _, err := r.Run(context.Background(), "", openai.UserMessage("hi"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok" {
		t.Fatalf("got %q", out)
	}
}

func TestRunner_MaxTurnsBlocksSecondLLMRound(t *testing.T) {
	toolRound := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": { "name": "echo", "arguments": "{\"message\":\"x\"}" }
      }]
    },
    "finish_reason": "tool_calls"
  }],
  "usage": { "prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2 }
}`
	final := `{
  "id": "y",
  "object": "chat.completion",
  "created": 2,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": { "role": "assistant", "content": "done" },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2 }
}`
	llm := &mockLLM{model: "m", script: []string{toolRound, final}}
	reg := tools.NewRegistry()
	reg.Register(mustEchoTool(t))
	cfg := &config.Config{}
	tr := &tokentracker.Tracker{}
	r := &Runner{LLM: llm, Cfg: cfg, Tools: reg, MaxTurns: 1, Tracker: tr}
	_, _, err := r.Run(context.Background(), "", openai.UserMessage("call echo"), io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrMaxTurns) {
		t.Fatalf("expected ErrMaxTurns, got %v", err)
	}
}

func TestRunner_MaxCostExceeded(t *testing.T) {
	js := `{
  "id": "x",
  "object": "chat.completion",
  "created": 1,
  "model": "m",
  "choices": [{
    "index": 0,
    "message": { "role": "assistant", "content": "ok" },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 1000000, "completion_tokens": 0, "total_tokens": 1000000 }
}`
	llm := &mockLLM{model: "m", script: []string{js}}
	reg := tools.NewRegistry()
	cfg := &config.Config{}
	tr := &tokentracker.Tracker{}
	r := &Runner{
		LLM: llm, Cfg: cfg, Tools: reg, Tracker: tr,
		MaxCostUSD: 0.001,
		EstimateSessionCost: func(u tokentracker.Usage) (float64, bool) {
			// $2 per MTok input => 1M tokens = $2
			return 2.0 * float64(u.PromptTokens) / 1e6, true
		},
	}
	_, _, err := r.Run(context.Background(), "", openai.UserMessage("hi"), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrMaxCost) {
		t.Fatalf("expected ErrMaxCost, got %v", err)
	}
}
