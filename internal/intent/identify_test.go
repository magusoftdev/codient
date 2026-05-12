package intent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"codient/internal/config"
	"codient/internal/openaiclient"
	"codient/internal/tokentracker"
)

// chatRequest captures the subset of /v1/chat/completions fields the
// supervisor tests care about (the budget, the user prompt).
type chatRequest struct {
	Model               string `json:"model"`
	MaxCompletionTokens int    `json:"max_completion_tokens"`
	Messages            []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// stubChatHandler returns an httptest server that decodes each
// /v1/chat/completions request and replies with the next entry of replies (the
// last entry is reused once the slice is exhausted). It also records the
// MaxCompletionTokens the client sent on each request.
type stubReply struct {
	content          string
	finishReason     string
	reasoningContent string // populated → response includes `reasoning_content` field (string)
}

type stubServer struct {
	srv      *httptest.Server
	replies  []stubReply
	budgets  []int
	prompts  []string
	requests atomic.Int32
}

func newStubServer(t *testing.T, replies []stubReply) *stubServer {
	t.Helper()
	s := &stubServer{replies: replies}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusInternalServerError)
			return
		}
		var req chatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
			return
		}
		s.budgets = append(s.budgets, req.MaxCompletionTokens)
		for _, m := range req.Messages {
			if m.Role == "user" {
				s.prompts = append(s.prompts, m.Content)
			}
		}
		i := int(s.requests.Add(1)) - 1
		var reply stubReply
		switch {
		case i < len(s.replies):
			reply = s.replies[i]
		case len(s.replies) > 0:
			reply = s.replies[len(s.replies)-1]
		}
		writeChatCompletion(w, reply)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// writeChatCompletion writes a minimal OpenAI-shaped non-streaming response.
// When reply.reasoningContent is non-empty the response message includes a
// non-standard `reasoning_content` field — mirroring LM Studio / vLLM /
// patched Ollama servers that emit a model's hidden chain-of-thought in a
// separate channel from `content`.
func writeChatCompletion(w http.ResponseWriter, reply stubReply) {
	w.Header().Set("Content-Type", "application/json")
	message := map[string]any{
		"role":    "assistant",
		"content": reply.content,
	}
	if reply.reasoningContent != "" {
		message["reasoning_content"] = reply.reasoningContent
	}
	resp := map[string]any{
		"id":      "test-completion",
		"object":  "chat.completion",
		"created": 1,
		"model":   "test-model",
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": reply.finishReason,
		}},
		"usage": map[string]any{
			"prompt_tokens":     7,
			"completion_tokens": 13,
			"total_tokens":      20,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func testClient(t *testing.T, srv *httptest.Server) *openaiclient.Client {
	t.Helper()
	cfg := &config.Config{
		BaseURL:       strings.TrimRight(srv.URL, "/") + "/v1",
		APIKey:        "test-key",
		Model:         "test-model",
		MaxConcurrent: 2,
	}
	return openaiclient.New(cfg)
}

// supervisorOnlyOpts disables the pre-LLM heuristic fast path so tests that
// exercise supervisor-LLM behavior (retry, salvage, fallback) actually reach
// the stub server regardless of the prompt's surface structure.
func supervisorOnlyOpts() Options { return Options{DisableHeuristic: true} }

// supervisorAmbiguousPrompt is a string that intentionally does NOT match
// heuristicQuickClassify (no edit verb at start, no design phrase, no
// trailing "?", no QUERY phrase prefix) so tests that want the supervisor
// path don't need to override DisableHeuristic.
const supervisorAmbiguousPrompt = "the orchestrator and the supervisor"

// TestIdentifyIntent_HappyPath: the first attempt returns clean JSON and we do
// not retry. Disable the heuristic so the supervisor LLM is actually called.
func TestIdentifyIntent_HappyPath(t *testing.T) {
	stub := newStubServer(t, []stubReply{{
		content:      `{"category":"QUERY","reasoning":"explain"}`,
		finishReason: "stop",
	}})
	cli := testClient(t, stub.srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := IdentifyIntent(ctx, cli, "what does this repo do?", supervisorOnlyOpts())
	if err != nil {
		t.Fatalf("IdentifyIntent: %v", err)
	}
	if id.Fallback {
		t.Fatalf("unexpected fallback: %+v", id)
	}
	if id.Category != CategoryQuery {
		t.Fatalf("category = %q, want QUERY", id.Category)
	}
	if id.Source != SourceSupervisor {
		t.Fatalf("source = %q, want %q", id.Source, SourceSupervisor)
	}
	if got := stub.requests.Load(); got != 1 {
		t.Fatalf("expected exactly 1 HTTP request, got %d", got)
	}
	if stub.budgets[0] != defaultSupervisorMaxCompletionTokens {
		t.Fatalf("budget = %d, want default %d", stub.budgets[0], defaultSupervisorMaxCompletionTokens)
	}
}

// TestIdentifyIntent_StripsThinkingTagsNoRetry: the first attempt returns a
// well-formed answer with a `<think>` block in front. We accept it without a
// retry — the strip logic handles it.
func TestIdentifyIntent_StripsThinkingTagsNoRetry(t *testing.T) {
	stub := newStubServer(t, []stubReply{{
		content:      "<think>Looks like a design question.</think>{\"category\":\"DESIGN\",\"reasoning\":\"arch\"}",
		finishReason: "stop",
	}})
	cli := testClient(t, stub.srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := IdentifyIntent(ctx, cli, "how should I structure plugins?", supervisorOnlyOpts())
	if err != nil {
		t.Fatalf("IdentifyIntent: %v", err)
	}
	if id.Fallback {
		t.Fatalf("unexpected fallback: %+v", id)
	}
	if id.Category != CategoryDesign {
		t.Fatalf("category = %q, want DESIGN", id.Category)
	}
	if got := stub.requests.Load(); got != 1 {
		t.Fatalf("strip path should not retry; got %d requests", got)
	}
}

// TestIdentifyIntent_RetriesOnLengthFinish: the first attempt is truncated
// inside a <think> block (no JSON), so IdentifyIntent retries with a larger
// budget. The second attempt returns clean JSON.
func TestIdentifyIntent_RetriesOnLengthFinish(t *testing.T) {
	stub := newStubServer(t, []stubReply{
		{content: "<think>The user wants a small change…", finishReason: "length"},
		{content: `{"category":"SIMPLE_FIX","reasoning":"typo"}`, finishReason: "stop"},
	})
	cli := testClient(t, stub.srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := IdentifyIntent(ctx, cli, "fix the README typo", supervisorOnlyOpts())
	if err != nil {
		t.Fatalf("IdentifyIntent: %v", err)
	}
	if id.Fallback {
		t.Fatalf("expected non-fallback after retry, got %+v", id)
	}
	if id.Category != CategorySimpleFix {
		t.Fatalf("category = %q, want SIMPLE_FIX", id.Category)
	}
	if got := stub.requests.Load(); got != 2 {
		t.Fatalf("expected 2 requests (retry), got %d", got)
	}
	if stub.budgets[0] != defaultSupervisorMaxCompletionTokens {
		t.Fatalf("first budget = %d, want %d", stub.budgets[0], defaultSupervisorMaxCompletionTokens)
	}
	if stub.budgets[1] <= stub.budgets[0] {
		t.Fatalf("retry budget %d should exceed initial %d", stub.budgets[1], stub.budgets[0])
	}
	if stub.budgets[1] > maxSupervisorRetryBudget {
		t.Fatalf("retry budget %d exceeds cap %d", stub.budgets[1], maxSupervisorRetryBudget)
	}
}

// TestIdentifyIntent_RetriesUseConfiguredBudgets: passing explicit budgets
// overrides the defaults on both attempts.
func TestIdentifyIntent_RetriesUseConfiguredBudgets(t *testing.T) {
	stub := newStubServer(t, []stubReply{
		{content: "<think>still thinking…", finishReason: "length"},
		{content: `{"category":"COMPLEX_TASK","reasoning":"big refactor"}`, finishReason: "stop"},
	})
	cli := testClient(t, stub.srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := IdentifyIntent(ctx, cli, "refactor the auth module", Options{
		MaxCompletionTokens:      256,
		RetryMaxCompletionTokens: 1500,
		DisableHeuristic:         true,
	})
	if err != nil {
		t.Fatalf("IdentifyIntent: %v", err)
	}
	if id.Fallback {
		t.Fatalf("expected non-fallback after retry, got %+v", id)
	}
	if id.Category != CategoryComplexTask {
		t.Fatalf("category = %q, want COMPLEX_TASK", id.Category)
	}
	if got := stub.requests.Load(); got != 2 {
		t.Fatalf("expected 2 requests, got %d", got)
	}
	if stub.budgets[0] != 256 {
		t.Fatalf("first budget = %d, want 256 (configured)", stub.budgets[0])
	}
	if stub.budgets[1] != 1500 {
		t.Fatalf("retry budget = %d, want 1500 (configured)", stub.budgets[1])
	}
}

// TestIdentifyIntent_NoRetryWhenFinishReasonStop: a non-"length" finish that
// still produced no JSON should NOT trigger a retry — that's a real model bug
// and another attempt won't fix it.
func TestIdentifyIntent_NoRetryWhenFinishReasonStop(t *testing.T) {
	stub := newStubServer(t, []stubReply{{
		content:      "I cannot help with that.",
		finishReason: "stop",
	}})
	cli := testClient(t, stub.srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Use an ambiguous prompt so the heuristic fast-path doesn't intercept.
	id, _ := IdentifyIntent(ctx, cli, supervisorAmbiguousPrompt, Options{})
	if !id.Fallback {
		t.Fatalf("expected fallback when no JSON and finish=stop, got %+v", id)
	}
	if got := stub.requests.Load(); got != 1 {
		t.Fatalf("expected no retry when finish=stop, got %d requests", got)
	}
	if id.Category != CategoryQuery {
		t.Fatalf("fallback category = %q, want QUERY", id.Category)
	}
	if id.Source != SourceHeuristicFallback {
		t.Fatalf("source = %q, want %q", id.Source, SourceHeuristicFallback)
	}
}

// TestIdentifyIntent_RetryDisabledByRetryBudgetLessThanInitial: when the
// caller explicitly sets RetryMaxCompletionTokens <= MaxCompletionTokens we
// skip the retry even on a length truncation.
func TestIdentifyIntent_RetryDisabledByRetryBudgetLessThanInitial(t *testing.T) {
	stub := newStubServer(t, []stubReply{
		{content: "<think>partial", finishReason: "length"},
		{content: `{"category":"QUERY","reasoning":"never reached"}`, finishReason: "stop"},
	})
	cli := testClient(t, stub.srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, _ := IdentifyIntent(ctx, cli, supervisorAmbiguousPrompt, Options{
		MaxCompletionTokens:      200,
		RetryMaxCompletionTokens: 50, // smaller => disable retry
	})
	if !id.Fallback {
		t.Fatalf("expected fallback when retry budget <= initial, got %+v", id)
	}
	if got := stub.requests.Load(); got != 1 {
		t.Fatalf("expected exactly 1 request (no retry), got %d", got)
	}
}

// TestIdentifyIntent_RetryFailsThenFallback: both attempts truncate with no
// JSON. We must end up on the fallback path (CategoryQuery, Fallback=true)
// without crashing.
func TestIdentifyIntent_RetryFailsThenFallback(t *testing.T) {
	stub := newStubServer(t, []stubReply{
		{content: "<think>round 1…", finishReason: "length"},
		{content: "<think>round 2 even longer thoughts…", finishReason: "length"},
	})
	cli := testClient(t, stub.srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, _ := IdentifyIntent(ctx, cli, supervisorAmbiguousPrompt, Options{})
	if !id.Fallback {
		t.Fatalf("expected fallback after two truncations, got %+v", id)
	}
	if id.Category != CategoryQuery {
		t.Fatalf("fallback category = %q, want QUERY", id.Category)
	}
	if got := stub.requests.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
	if !strings.Contains(id.Reasoning, "after retry") {
		t.Fatalf("fallback reasoning should mention retry: %q", id.Reasoning)
	}
}

// TestIdentifyIntent_TokenTrackerSumsBothAttempts confirms that token usage
// from BOTH the initial and the retry attempts is recorded.
func TestIdentifyIntent_TokenTrackerSumsBothAttempts(t *testing.T) {
	stub := newStubServer(t, []stubReply{
		{content: "<think>…", finishReason: "length"},
		{content: `{"category":"QUERY","reasoning":"q"}`, finishReason: "stop"},
	})
	cli := testClient(t, stub.srv)

	tracker := &tokentracker.Tracker{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// "hello" doesn't match any heuristic pattern, so this naturally
	// reaches the supervisor without needing DisableHeuristic.
	_, err := IdentifyIntent(ctx, cli, "hello", Options{Tracker: tracker})
	if err != nil {
		t.Fatalf("IdentifyIntent: %v", err)
	}
	u := tracker.Session()
	if u.TotalTokens != 40 {
		t.Fatalf("expected 40 total tokens after retry, got %d", u.TotalTokens)
	}
}

// TestIdentifyIntent_NilClientFallback covers the early-return safety branch.
func TestIdentifyIntent_NilClientFallback(t *testing.T) {
	id, err := IdentifyIntent(context.Background(), nil, "hi", Options{})
	if err == nil {
		t.Fatalf("expected error on nil client")
	}
	if !id.Fallback || id.Category != CategoryQuery {
		t.Fatalf("expected fallback QUERY, got %+v", id)
	}
}

// TestIdentifyIntent_EmptyPromptFallback covers the empty-prompt guard.
func TestIdentifyIntent_EmptyPromptFallback(t *testing.T) {
	stub := newStubServer(t, []stubReply{{content: "ignored", finishReason: "stop"}})
	cli := testClient(t, stub.srv)
	id, err := IdentifyIntent(context.Background(), cli, "   \n  ", Options{})
	if err == nil {
		t.Fatalf("expected error on empty prompt")
	}
	if !id.Fallback || id.Category != CategoryQuery {
		t.Fatalf("expected fallback QUERY, got %+v", id)
	}
	if got := stub.requests.Load(); got != 0 {
		t.Fatalf("empty prompt must not hit the model; got %d requests", got)
	}
}

// TestIdentifyIntent_AppendsNoThinkDirective verifies that every supervisor
// request appends `/no_think` to the user message. Thinking-capable models
// (Qwen3-Thinking, DeepSeek-R1) recognise this directive and skip their
// hidden reasoning channel — exactly the channel that was burning the
// supervisor's tight budget before the JSON answer could be emitted.
func TestIdentifyIntent_AppendsNoThinkDirective(t *testing.T) {
	stub := newStubServer(t, []stubReply{{
		content:      `{"category":"QUERY","reasoning":"info"}`,
		finishReason: "stop",
	}})
	cli := testClient(t, stub.srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Use DisableHeuristic so the supervisor is consulted regardless of
	// prompt structure — we're checking the request body, not classification.
	if _, err := IdentifyIntent(ctx, cli, "what does this repo do?", supervisorOnlyOpts()); err != nil {
		t.Fatal(err)
	}
	if len(stub.prompts) == 0 {
		t.Fatal("server received no user prompts")
	}
	got := stub.prompts[0]
	if !strings.HasSuffix(got, "/no_think") {
		t.Fatalf("user message should end with /no_think, got %q", got)
	}
	if !strings.HasPrefix(got, "what does this repo do?") {
		t.Fatalf("original prompt should be preserved at the start, got %q", got)
	}
}

// TestIdentifyIntent_SalvagesFromReasoningContent: the server stuffs the JSON
// answer into a `reasoning_content` field (mimicking LM Studio / vLLM with
// thinking models). The supervisor must still recover the classification
// without consuming the retry budget.
func TestIdentifyIntent_SalvagesFromReasoningContent(t *testing.T) {
	stub := newStubServer(t, []stubReply{{
		content:          "",
		reasoningContent: `{"category":"DESIGN","reasoning":"arch"}`,
		finishReason:     "stop",
	}})
	cli := testClient(t, stub.srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// DisableHeuristic so the salvage path (a supervisor-LLM concern) is
	// exercised regardless of prompt structure.
	id, err := IdentifyIntent(ctx, cli, "how should I structure plugins?", supervisorOnlyOpts())
	if err != nil {
		t.Fatalf("IdentifyIntent: %v", err)
	}
	if id.Fallback {
		t.Fatalf("expected non-fallback after reasoning-channel salvage, got %+v", id)
	}
	if id.Category != CategoryDesign {
		t.Fatalf("category = %q, want DESIGN", id.Category)
	}
	if got := stub.requests.Load(); got != 1 {
		t.Fatalf("salvage should not trigger a retry, got %d requests", got)
	}
}

// TestIdentifyIntent_HeuristicFastPath_NoLLMCall: a prompt that matches the
// pre-LLM heuristic with high confidence (here: "Create a plan for X")
// should NOT hit the supervisor at all. This is the latency win the
// heuristic exists for.
func TestIdentifyIntent_HeuristicFastPath_NoLLMCall(t *testing.T) {
	stub := newStubServer(t, []stubReply{{
		content:      `{"category":"QUERY","reasoning":"should not be reached"}`,
		finishReason: "stop",
	}})
	cli := testClient(t, stub.srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := IdentifyIntent(ctx, cli, "Create a plan for caching the repo map.", Options{})
	if err != nil {
		t.Fatalf("IdentifyIntent: %v", err)
	}
	if id.Fallback {
		t.Fatalf("unexpected fallback on heuristic match: %+v", id)
	}
	if id.Category != CategoryDesign {
		t.Fatalf("category = %q, want DESIGN", id.Category)
	}
	if id.Source != SourceHeuristic {
		t.Fatalf("source = %q, want %q", id.Source, SourceHeuristic)
	}
	if got := stub.requests.Load(); got != 0 {
		t.Fatalf("heuristic fast-path must skip the LLM; got %d HTTP requests", got)
	}
}

// TestIdentifyIntent_HeuristicFastPath_PerCategory walks one representative
// prompt for each of the four categories and verifies the heuristic short-
// circuits the LLM with the expected category + source.
func TestIdentifyIntent_HeuristicFastPath_PerCategory(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   Category
	}{
		{"design", "Create a plan for adding observability hooks", CategoryDesign},
		{"complex", "Refactor session.go across every file", CategoryComplexTask},
		{"simple", "Please fix the typo in README.md", CategorySimpleFix},
		{"query", "What does the orchestrator do?", CategoryQuery},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newStubServer(t, []stubReply{{
				content:      `{"category":"QUERY","reasoning":"unreachable"}`,
				finishReason: "stop",
			}})
			cli := testClient(t, stub.srv)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			id, err := IdentifyIntent(ctx, cli, tc.prompt, Options{})
			if err != nil {
				t.Fatalf("IdentifyIntent: %v", err)
			}
			if id.Category != tc.want {
				t.Fatalf("category = %q, want %q (reason=%q)", id.Category, tc.want, id.Reasoning)
			}
			if id.Source != SourceHeuristic {
				t.Fatalf("source = %q, want %q", id.Source, SourceHeuristic)
			}
			if got := stub.requests.Load(); got != 0 {
				t.Fatalf("heuristic match must skip the LLM; got %d requests", got)
			}
		})
	}
}

// TestIdentifyIntent_DisableHeuristic_AlwaysCallsLLM: when DisableHeuristic
// is set the supervisor LLM must be consulted even on a prompt the fast
// path would otherwise match.
func TestIdentifyIntent_DisableHeuristic_AlwaysCallsLLM(t *testing.T) {
	stub := newStubServer(t, []stubReply{{
		content:      `{"category":"DESIGN","reasoning":"llm-decided"}`,
		finishReason: "stop",
	}})
	cli := testClient(t, stub.srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := IdentifyIntent(ctx, cli, "Create a plan for caching", supervisorOnlyOpts())
	if err != nil {
		t.Fatalf("IdentifyIntent: %v", err)
	}
	if id.Source != SourceSupervisor {
		t.Fatalf("source = %q, want %q", id.Source, SourceSupervisor)
	}
	if got := stub.requests.Load(); got != 1 {
		t.Fatalf("DisableHeuristic should force the LLM; got %d requests", got)
	}
	if !strings.Contains(id.Reasoning, "llm-decided") {
		t.Fatalf("expected LLM reasoning to be preserved, got %q", id.Reasoning)
	}
}

// TestIdentifyIntent_HeuristicFallback_FallbackKeepsAmbiguousAsQuery: when
// the prompt does NOT match the heuristic (so the LLM is consulted) and
// then the LLM fails, the post-LLM heuristic fallback must default to
// QUERY as a safety net.
func TestIdentifyIntent_HeuristicFallback_FallbackKeepsAmbiguousAsQuery(t *testing.T) {
	stub := newStubServer(t, []stubReply{
		{content: "<think>round 1…", finishReason: "length"},
		{content: "", finishReason: "length"},
	})
	cli := testClient(t, stub.srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, _ := IdentifyIntent(ctx, cli, supervisorAmbiguousPrompt, Options{})
	if !id.Fallback {
		t.Fatalf("expected fallback, got %+v", id)
	}
	if id.Category != CategoryQuery {
		t.Fatalf("ambiguous prompt should fall back to QUERY, got %q", id.Category)
	}
	if id.Source != SourceHeuristicFallback {
		t.Fatalf("source = %q, want %q", id.Source, SourceHeuristicFallback)
	}
}

// TestIdentifyIntent_FallbackReasoningIncludesAttemptDiagnostics: the fallback
// reasoning shown to the user must surface both attempts' finish_reason and
// body summary so they can distinguish "model truncated mid-thought" from
// "server returned empty body" from "model returned non-JSON prose".
func TestIdentifyIntent_FallbackReasoningIncludesAttemptDiagnostics(t *testing.T) {
	stub := newStubServer(t, []stubReply{
		{content: "<think>round 1…", finishReason: "length"},
		{content: "", finishReason: "length"},
	})
	cli := testClient(t, stub.srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, _ := IdentifyIntent(ctx, cli, supervisorAmbiguousPrompt, Options{})
	if !id.Fallback {
		t.Fatalf("expected fallback")
	}
	// First-attempt finish=length, second-attempt finish=length and body=<empty>.
	for _, want := range []string{"budget=", `finish="length"`, "<empty>", "retry"} {
		if !strings.Contains(id.Reasoning, want) {
			t.Fatalf("fallback reasoning missing %q; got %q", want, id.Reasoning)
		}
	}
}
