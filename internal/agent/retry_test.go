package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"

	"codient/internal/config"
	"codient/internal/tools"
)

type failThenSucceedLLM struct {
	model     string
	failCount int
	calls     int
	failErr   error
	success   string
}

func (m *failThenSucceedLLM) Model() string { return m.model }

func (m *failThenSucceedLLM) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	m.calls++
	if m.calls <= m.failCount {
		return nil, m.failErr
	}
	var out openai.ChatCompletion
	if err := json.Unmarshal([]byte(m.success), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func TestCallLLMWithRetry_TransientThenSuccess(t *testing.T) {
	okJSON := `{
		"id":"x","object":"chat.completion","created":1,"model":"m",
		"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]
	}`
	llm := &failThenSucceedLLM{
		model:     "m",
		failCount: 1,
		failErr:   &net.OpError{Op: "dial", Err: fmt.Errorf("connection refused")},
		success:   okJSON,
	}
	r := &Runner{
		LLM:   llm,
		Cfg:   &config.Config{MaxLLMRetries: 2},
		Tools: tools.NewRegistry(),
	}
	params := openai.ChatCompletionNewParams{}
	res, _, err := r.callLLMWithRetry(context.Background(), params, nil)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if res == nil || len(res.Choices) == 0 {
		t.Fatal("expected non-empty response")
	}
	if llm.calls != 2 {
		t.Fatalf("expected 2 calls, got %d", llm.calls)
	}
}

func TestCallLLMWithRetry_NonTransientFails(t *testing.T) {
	llm := &failThenSucceedLLM{
		model:     "m",
		failCount: 5,
		failErr:   fmt.Errorf("bad request: invalid model"),
		success:   "{}",
	}
	r := &Runner{
		LLM:   llm,
		Cfg:   &config.Config{MaxLLMRetries: 3},
		Tools: tools.NewRegistry(),
	}
	params := openai.ChatCompletionNewParams{}
	_, _, err := r.callLLMWithRetry(context.Background(), params, nil)
	if err == nil {
		t.Fatal("expected error for non-transient failure")
	}
	if llm.calls != 1 {
		t.Fatalf("expected 1 call (no retries for non-transient), got %d", llm.calls)
	}
}

func TestCallLLMWithRetry_ExhaustsRetries(t *testing.T) {
	llm := &failThenSucceedLLM{
		model:     "m",
		failCount: 10,
		failErr:   &net.OpError{Op: "dial", Err: fmt.Errorf("connection refused")},
		success:   "{}",
	}
	r := &Runner{
		LLM:   llm,
		Cfg:   &config.Config{MaxLLMRetries: 2},
		Tools: tools.NewRegistry(),
	}
	params := openai.ChatCompletionNewParams{}
	_, _, err := r.callLLMWithRetry(context.Background(), params, nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if llm.calls != 3 { // 1 initial + 2 retries
		t.Fatalf("expected 3 calls, got %d", llm.calls)
	}
}

// streamVsChatRecorder implements ChatClient + streamChatClient and records which path ran.
type streamVsChatRecorder struct {
	model       string
	chatCalls   int
	streamCalls int
}

func (m *streamVsChatRecorder) Model() string { return m.model }

func (m *streamVsChatRecorder) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	m.chatCalls++
	return &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatCompletionMessage{Role: "assistant", Content: "ok"},
		}},
	}, nil
}

func (m *streamVsChatRecorder) ChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams, w io.Writer) (*openai.ChatCompletion, error) {
	m.streamCalls++
	return &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatCompletionMessage{Role: "assistant", Content: "ok"},
		}},
	}, nil
}

func TestCallLLMOnce_StreamsWithoutTools(t *testing.T) {
	llm := &streamVsChatRecorder{model: "m"}
	r := &Runner{LLM: llm, Cfg: &config.Config{StreamWithTools: false}, Tools: tools.NewRegistry()}
	params := openai.ChatCompletionNewParams{}
	_, streamed, err := r.callLLMOnce(context.Background(), params, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !streamed || llm.streamCalls != 1 || llm.chatCalls != 0 {
		t.Fatalf("want streaming only: streamed=%v stream=%d chat=%d", streamed, llm.streamCalls, llm.chatCalls)
	}
}

func TestCallLLMOnce_NonStreamingWhenToolsUnlessOptIn(t *testing.T) {
	llm := &streamVsChatRecorder{model: "m"}
	r := &Runner{LLM: llm, Cfg: &config.Config{StreamWithTools: false}, Tools: tools.NewRegistry()}
	params := openai.ChatCompletionNewParams{
		Tools: []openai.ChatCompletionToolUnionParam{{}},
	}
	_, streamed, err := r.callLLMOnce(context.Background(), params, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if streamed || llm.chatCalls != 1 || llm.streamCalls != 0 {
		t.Fatalf("want non-streaming with tools: streamed=%v stream=%d chat=%d", streamed, llm.streamCalls, llm.chatCalls)
	}
}

func TestCallLLMOnce_StreamsWithToolsWhenOptIn(t *testing.T) {
	llm := &streamVsChatRecorder{model: "m"}
	r := &Runner{LLM: llm, Cfg: &config.Config{StreamWithTools: true}, Tools: tools.NewRegistry()}
	params := openai.ChatCompletionNewParams{
		Tools: []openai.ChatCompletionToolUnionParam{{}},
	}
	_, streamed, err := r.callLLMOnce(context.Background(), params, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !streamed || llm.streamCalls != 1 || llm.chatCalls != 0 {
		t.Fatalf("want streaming with tools when opted in: streamed=%v stream=%d chat=%d", streamed, llm.streamCalls, llm.chatCalls)
	}
}

func TestCallLLMOnce_Timeout(t *testing.T) {
	llm := &streamVsChatRecorder{model: "m"}
	r := &Runner{LLM: llm, Cfg: &config.Config{StreamWithTools: true, MaxCompletionSeconds: 1}, Tools: tools.NewRegistry()}

	// Create a context that's already expired
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Wait for context to expire
	<-ctx.Done()

	params := openai.ChatCompletionNewParams{}
	_, _, err := r.callLLMOnce(ctx, params, io.Discard)
	if !strings.Contains(err.Error(), "timeout") && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}
