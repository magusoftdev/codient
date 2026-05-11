package openaiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"codient/internal/config"
)

func testConfig(baseURL, model string) *config.Config {
	return &config.Config{
		BaseURL:       strings.TrimRight(baseURL, "/"),
		APIKey:        "test-key",
		Model:         model,
		MaxConcurrent: 3,
	}
}

func TestPingModels_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"alpha","object":"model"}]}`))
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL+"/v1", "alpha"))
	if err := c.PingModels(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPingModels_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL+"/v1", "m"))
	if err := c.PingModels(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestProbeContextWindow_LoadedInstance(t *testing.T) {
	nativeJSON := `{
		"models": [{
			"key": "my-model",
			"max_context_length": 32768,
			"loaded_instances": [{
				"id": "my-model",
				"config": {"context_length": 8192, "eval_batch_size": 512}
			}]
		}]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			_, _ = w.Write([]byte(nativeJSON))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL+"/v1", "my-model"))
	n, err := c.ProbeContextWindow(context.Background(), "my-model")
	if err != nil {
		t.Fatal(err)
	}
	if n != 8192 {
		t.Fatalf("got %d, want 8192", n)
	}
}

func TestProbeContextWindow_FallsBackToMaxContext(t *testing.T) {
	nativeJSON := `{
		"models": [{
			"key": "offline-model",
			"max_context_length": 32768,
			"loaded_instances": []
		}]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			_, _ = w.Write([]byte(nativeJSON))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL+"/v1", "offline-model"))
	n, err := c.ProbeContextWindow(context.Background(), "offline-model")
	if err != nil {
		t.Fatal(err)
	}
	if n != 32768 {
		t.Fatalf("got %d, want 32768", n)
	}
}

func TestParseContextFromNativeModels(t *testing.T) {
	data := []byte(`{
		"models": [
			{
				"key": "my-model",
				"max_context_length": 32768,
				"loaded_instances": [
					{"id": "my-model", "config": {"context_length": 4096}}
				]
			},
			{
				"key": "other-model",
				"max_context_length": 8192,
				"loaded_instances": []
			}
		]
	}`)
	if n := parseContextFromNativeModels(data, "my-model"); n != 4096 {
		t.Fatalf("loaded instance: got %d, want 4096", n)
	}
	if n := parseContextFromNativeModels(data, "other-model"); n != 8192 {
		t.Fatalf("max_context_length fallback: got %d, want 8192", n)
	}
	if n := parseContextFromNativeModels(data, "nonexistent"); n != 0 {
		t.Fatalf("unknown model: got %d, want 0", n)
	}
	if n := parseContextFromNativeModels([]byte("not json"), "x"); n != 0 {
		t.Fatalf("bad JSON: got %d, want 0", n)
	}
}

// TestParseContextFromNativeModels_NamespacedKey verifies that a key like
// "author/ModelName" still matches when the configured model ID is just "ModelName".
func TestParseContextFromNativeModels_NamespacedKey(t *testing.T) {
	data := []byte(`{
		"models": [{
			"key": "lmstudio-community/Qwen3.6-35B-A3B-GGUF",
			"max_context_length": 40960,
			"loaded_instances": [{
				"id": "lmstudio-community/Qwen3.6-35B-A3B-GGUF",
				"config": {"context_length": 32768}
			}]
		}]
	}`)
	if n := parseContextFromNativeModels(data, "Qwen3.6-35B-A3B-GGUF"); n != 32768 {
		t.Fatalf("namespaced key should match base name: got %d, want 32768", n)
	}
}

func TestParseContextFromOpenAIModels(t *testing.T) {
	data := []byte(`{"data":[
		{"id":"qwen-model","max_context_length":32768},
		{"id":"other","context_length":8192},
		{"id":"third","context_window":131072}
	]}`)
	if n := parseContextFromOpenAIModels(data, "qwen-model"); n != 32768 {
		t.Fatalf("max_context_length: got %d, want 32768", n)
	}
	if n := parseContextFromOpenAIModels(data, "other"); n != 8192 {
		t.Fatalf("context_length: got %d, want 8192", n)
	}
	if n := parseContextFromOpenAIModels(data, "third"); n != 131072 {
		t.Fatalf("context_window: got %d, want 131072", n)
	}
	if n := parseContextFromOpenAIModels(data, "missing"); n != 0 {
		t.Fatalf("missing: got %d, want 0", n)
	}
	// Namespaced key match.
	data2 := []byte(`{"data":[{"id":"author/MyModel","max_context_length":16384}]}`)
	if n := parseContextFromOpenAIModels(data2, "MyModel"); n != 16384 {
		t.Fatalf("namespaced id: got %d, want 16384", n)
	}
}

// TestProbeContextWindow_OpenAICompatFallback verifies that when the LM Studio
// native endpoint is absent, the standard /v1/models endpoint is used as a
// fallback to retrieve the context window.
func TestProbeContextWindow_OpenAICompatFallback(t *testing.T) {
	modelsJSON := `{"data":[{"id":"local-model","max_context_length":65536}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" || r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(modelsJSON))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL+"/v1", "local-model"))
	n, err := c.ProbeContextWindow(context.Background(), "local-model")
	if err != nil {
		t.Fatal(err)
	}
	if n != 65536 {
		t.Fatalf("OpenAI-compat fallback: got %d, want 65536", n)
	}
}

func TestParseContextFromOpenAIModels_MaxContextWindow(t *testing.T) {
	// Lemonade uses max_context_window, not max_context_length.
	data := []byte(`{"data":[{"id":"Qwen3-0.6B-GGUF","max_context_window":40960}]}`)
	if n := parseContextFromOpenAIModels(data, "Qwen3-0.6B-GGUF"); n != 40960 {
		t.Fatalf("max_context_window: got %d, want 40960", n)
	}
}

func TestParseContextFromLemonadeHealth(t *testing.T) {
	healthJSON := `{
		"status": "ok",
		"all_models_loaded": [
			{
				"model_name": "Qwen3-Coder-30B-A3B-Instruct-GGUF",
				"recipe": "llamacpp",
				"recipe_options": {"ctx_size": 32768, "llamacpp_backend": "vulkan"}
			},
			{
				"model_name": "nomic-embed-text-v1-GGUF",
				"recipe": "llamacpp",
				"recipe_options": {"ctx_size": 8192}
			}
		]
	}`
	data := []byte(healthJSON)
	if n := parseContextFromLemonadeHealth(data, "Qwen3-Coder-30B-A3B-Instruct-GGUF"); n != 32768 {
		t.Fatalf("health probe (LLM): got %d, want 32768", n)
	}
	if n := parseContextFromLemonadeHealth(data, "nomic-embed-text-v1-GGUF"); n != 8192 {
		t.Fatalf("health probe (embedding): got %d, want 8192", n)
	}
	if n := parseContextFromLemonadeHealth(data, "nonexistent"); n != 0 {
		t.Fatalf("health probe (missing): got %d, want 0", n)
	}
	// Health endpoint with no recipe_options.ctx_size should return 0.
	noCtx := `{"all_models_loaded":[{"model_name":"m","recipe_options":{}}]}`
	if n := parseContextFromLemonadeHealth([]byte(noCtx), "m"); n != 0 {
		t.Fatalf("health probe (no ctx_size): got %d, want 0", n)
	}
}

func TestParseContextFromSingleModel(t *testing.T) {
	// Lemonade GET /v1/models/{id} with recipe_options.ctx_size.
	withCtxSize := []byte(`{
		"id": "Qwen3-0.6B-GGUF",
		"max_context_window": 40960,
		"recipe_options": {"ctx_size": 8192}
	}`)
	if n := parseContextFromSingleModel(withCtxSize); n != 8192 {
		t.Fatalf("single model (recipe_options.ctx_size): got %d, want 8192", n)
	}
	// Without recipe_options, falls through to max_context_window.
	noRecipe := []byte(`{"id":"m","max_context_window":40960}`)
	if n := parseContextFromSingleModel(noRecipe); n != 40960 {
		t.Fatalf("single model (max_context_window): got %d, want 40960", n)
	}
}

func TestProbeContextWindow_LemonadeHealth(t *testing.T) {
	healthJSON := `{
		"status": "ok",
		"all_models_loaded": [{
			"model_name": "Qwen3-0.6B-GGUF",
			"recipe_options": {"ctx_size": 16384}
		}]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(healthJSON))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL+"/v1", "Qwen3-0.6B-GGUF"))
	n, err := c.ProbeContextWindow(context.Background(), "Qwen3-0.6B-GGUF")
	if err != nil {
		t.Fatal(err)
	}
	if n != 16384 {
		t.Fatalf("Lemonade health probe: got %d, want 16384", n)
	}
}

func TestProbeContextWindow_LemonadeSingleModel(t *testing.T) {
	modelJSON := `{"id":"Qwen3-0.6B-GGUF","max_context_window":40960,"recipe_options":{"ctx_size":8192}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models/Qwen3-0.6B-GGUF" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(modelJSON))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL+"/v1", "Qwen3-0.6B-GGUF"))
	n, err := c.ProbeContextWindow(context.Background(), "Qwen3-0.6B-GGUF")
	if err != nil {
		t.Fatal(err)
	}
	if n != 8192 {
		t.Fatalf("Lemonade single-model probe: got %d, want 8192", n)
	}
}

func TestProbeContextWindow_NonLMStudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL+"/v1", "m"))
	n, err := c.ProbeContextWindow(context.Background(), "m")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("got %d, want 0 when neither probe succeeds", n)
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"a"},{"id":"b"}]}`))
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL+"/v1", "m"))
	ids, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("got %#v", ids)
	}
}

func TestExtractModelIDsFromJSON_LMStudioStyleModelsKeyOnly(t *testing.T) {
	// LM Studio (and similar) often list entries under "models" with "key", not OpenAI's "data"[].id
	body := []byte(`{"models":[{"type":"llm","key":"google/gemma-mini"},{"type":"embedding","key":"nomic-embed"}]}`)
	ids, err := ExtractModelIDsFromJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(ids)
	want := []string{"google/gemma-mini", "nomic-embed"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("got %#v want %#v", ids, want)
	}
}

func TestExtractModelIDsFromJSON_DataModelsAndNameShapes(t *testing.T) {
	ids, err := ExtractModelIDsFromJSON([]byte(
		`{"data":[{"id":"a"}],"models":[{"name":"named-only"},{"id":"explicit-id"}]} `,
	))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(ids)
	want := []string{"a", "explicit-id", "named-only"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("got %#v want %#v", ids, want)
	}
}

func TestExtractModelIDsFromJSON_StringElements(t *testing.T) {
	ids, err := ExtractModelIDsFromJSON([]byte(`{"data":["qwen-mini","gpt-style"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "qwen-mini" || ids[1] != "gpt-style" {
		t.Fatalf("got %#v", ids)
	}
}

func TestChatCompletion_MockServer(t *testing.T) {
	body := `{
  "id": "c1",
  "object": "chat.completion",
  "created": 1,
  "model": "test-model",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "from-mock"},
    "finish_reason": "stop"
  }]
}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL+"/v1", "test-model"))
	res, err := c.ChatCompletion(context.Background(), openai.ChatCompletionNewParams{
		Model: shared.ChatModel("test-model"),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("hi"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Choices) != 1 || res.Choices[0].Message.Content != "from-mock" {
		t.Fatalf("got %+v", res.Choices)
	}
}

func TestChatCompletion_SemaphoreLimitsConcurrency(t *testing.T) {
	var mu sync.Mutex
	cur, peak := 0, 0
	delay := 30 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		cur++
		if cur > peak {
			peak = cur
		}
		mu.Unlock()
		time.Sleep(delay)
		mu.Lock()
		cur--
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"."},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL+"/v1", "m")
	cfg.MaxConcurrent = 2
	client := New(cfg)

	const n = 8
	var wg sync.WaitGroup
	var errCount atomic.Int32
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := client.ChatCompletion(context.Background(), openai.ChatCompletionNewParams{
				Model:    shared.ChatModel("m"),
				Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("x")},
			})
			if err != nil {
				errCount.Add(1)
			}
		}()
	}
	wg.Wait()
	if errCount.Load() != 0 {
		t.Fatalf("%d goroutines returned errors from ChatCompletion", errCount.Load())
	}

	if peak > 2 {
		t.Fatalf("peak concurrent requests was %d, want <= 2", peak)
	}
}

func TestStreamChatCompletion_MockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !bytes.Contains(b, []byte(`"stream":true`)) {
			http.Error(w, "expected stream", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		chunks := []string{
			`{"id":"s","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"Hel"},"finish_reason":""}]}`,
			`{"id":"s","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":""}]}`,
			`[DONE]`,
		}
		for _, ch := range chunks {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", ch)
			fl.Flush()
		}
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL+"/v1", "m"))
	var buf bytes.Buffer
	res, err := c.StreamChatCompletion(context.Background(), openai.ChatCompletionNewParams{
		Model:    shared.ChatModel("m"),
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("expected completion")
	}
	if buf.String() != "Hello" {
		t.Fatalf("got %q", buf.String())
	}
}

func TestNewFromParams(t *testing.T) {
	c := NewFromParams("http://example.com/v1", "test-key", "my-model", 5)
	if c.Model() != "my-model" {
		t.Fatalf("Model: got %q", c.Model())
	}
	if c.base != "http://example.com/v1" {
		t.Fatalf("base: got %q", c.base)
	}
	if c.apiKey != "test-key" {
		t.Fatalf("apiKey: got %q", c.apiKey)
	}
	if cap(c.llmSem.ch) != 5 {
		t.Fatalf("semaphore capacity: got %d", cap(c.llmSem.ch))
	}
}

func TestNewFromParams_DefaultsConcurrency(t *testing.T) {
	c := NewFromParams("http://example.com/v1", "key", "m", 0)
	if cap(c.llmSem.ch) != 3 {
		t.Fatalf("expected default concurrency 3, got %d", cap(c.llmSem.ch))
	}
}

func TestNewDelegatesToNewFromParams(t *testing.T) {
	cfg := testConfig("http://test/v1", "test-model")
	c := New(cfg)
	if c.Model() != "test-model" {
		t.Fatalf("Model: got %q", c.Model())
	}
	if c.base != "http://test/v1" {
		t.Fatalf("base: got %q", c.base)
	}
}

func TestNewForTier_UsesTierOverride(t *testing.T) {
	cfg := &config.Config{
		BaseURL:       "http://default/v1",
		APIKey:        "default-key",
		Model:         "default-model",
		MaxConcurrent: 2,
		HighReasoning: config.ReasoningTier{
			BaseURL: "http://hi-server/v1",
			APIKey:  "hi-key",
			Model:   "hi-model",
		},
	}
	c := NewForTier(cfg, config.TierHigh)
	if c.Model() != "hi-model" {
		t.Fatalf("Model: got %q want hi-model", c.Model())
	}
	if c.base != "http://hi-server/v1" {
		t.Fatalf("base: got %q want http://hi-server/v1", c.base)
	}
	if c.apiKey != "hi-key" {
		t.Fatalf("apiKey: got %q want hi-key", c.apiKey)
	}
}

func TestNewForTier_FallsBackToDefaults(t *testing.T) {
	cfg := &config.Config{
		BaseURL:       "http://default/v1",
		APIKey:        "default-key",
		Model:         "default-model",
		MaxConcurrent: 2,
	}
	c := NewForTier(cfg, config.TierLow)
	if c.Model() != "default-model" {
		t.Fatalf("Model: got %q want default-model", c.Model())
	}
	if c.base != "http://default/v1" {
		t.Fatalf("base: got %q", c.base)
	}
}

func TestNewForTier_PartialOverride(t *testing.T) {
	cfg := &config.Config{
		BaseURL:       "http://default/v1",
		APIKey:        "default-key",
		Model:         "default-model",
		MaxConcurrent: 2,
		LowReasoning:  config.ReasoningTier{Model: "low-only-model"},
	}
	c := NewForTier(cfg, config.TierLow)
	if c.Model() != "low-only-model" {
		t.Fatalf("Model: got %q want low-only-model", c.Model())
	}
	if c.base != "http://default/v1" {
		t.Fatalf("base should inherit: got %q", c.base)
	}
	if c.apiKey != "default-key" {
		t.Fatalf("apiKey should inherit: got %q", c.apiKey)
	}
}

func TestNewForEmbedding_InheritsChatConnection(t *testing.T) {
	cfg := &config.Config{
		BaseURL:        "http://chat/v1",
		APIKey:         "chat-key",
		Model:          "chat-model",
		MaxConcurrent:  4,
		EmbeddingModel: "embed-model",
	}
	c := NewForEmbedding(cfg)
	if c.base != "http://chat/v1" {
		t.Fatalf("base: got %q want chat base", c.base)
	}
	if c.apiKey != "chat-key" {
		t.Fatalf("apiKey: got %q want chat key", c.apiKey)
	}
	if c.Model() != "embed-model" {
		t.Fatalf("Model: got %q want embed-model (configured EmbeddingModel)", c.Model())
	}
}

func TestNewForEmbedding_OverridesChatConnection(t *testing.T) {
	cfg := &config.Config{
		BaseURL:          "http://chat/v1",
		APIKey:           "chat-key",
		Model:            "chat-model",
		MaxConcurrent:    2,
		EmbeddingModel:   "nomic-embed",
		EmbeddingBaseURL: "http://emb/v1",
		EmbeddingAPIKey:  "emb-key",
	}
	c := NewForEmbedding(cfg)
	if c.base != "http://emb/v1" {
		t.Fatalf("base: got %q want emb override", c.base)
	}
	if c.apiKey != "emb-key" {
		t.Fatalf("apiKey: got %q want emb override", c.apiKey)
	}
}

func TestCreateEmbedding_MockServerHitsConfiguredBase(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" && r.Method == http.MethodPost {
			hits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
  "object":"list",
  "model":"emb-model",
  "data":[
    {"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]},
    {"object":"embedding","index":1,"embedding":[0.4,0.5,0.6]}
  ]
}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := &config.Config{
		BaseURL:          "http://wrong-chat-server/v1",
		APIKey:           "should-not-be-used",
		Model:            "chat-model",
		MaxConcurrent:    2,
		EmbeddingModel:   "emb-model",
		EmbeddingBaseURL: srv.URL + "/v1",
		EmbeddingAPIKey:  "emb-key",
	}
	c := NewForEmbedding(cfg)
	vecs, err := c.CreateEmbedding(context.Background(), "emb-model", []string{"hello", "world"})
	if err != nil {
		t.Fatalf("CreateEmbedding: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 POST /v1/embeddings on emb base, got %d", hits.Load())
	}
	if len(vecs) != 2 {
		t.Fatalf("got %d vectors, want 2", len(vecs))
	}
	if len(vecs[0]) != 3 || vecs[0][0] != 0.1 {
		t.Fatalf("vector 0 mismatch: %v", vecs[0])
	}
	if len(vecs[1]) != 3 || vecs[1][2] != 0.6 {
		t.Fatalf("vector 1 mismatch: %v", vecs[1])
	}
}

func TestTryOllamaUnloadModel_PostsToNativeAPI(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/generate" && r.Method == http.MethodPost {
			hits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"m","created_at":"t","response":"","done":true,"done_reason":"unload"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL+"/v1", "base-model"))
	if err := c.TryOllamaUnloadModel(context.Background(), "llama3"); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 POST /api/generate, got %d", hits.Load())
	}
}

func TestTryOllamaUnloadModel_SkipsCloudHost(t *testing.T) {
	c := New(testConfig("https://api.openai.com/v1", "gpt-4o-mini"))
	if err := c.TryOllamaUnloadModel(context.Background(), "gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}
}

func TestReasoningFragmentFromDelta_JSON(t *testing.T) {
	var d openai.ChatCompletionChunkChoiceDelta
	if err := json.Unmarshal([]byte(`{"reasoning_content":"step 1"}`), &d); err != nil {
		t.Fatal(err)
	}
	if got := reasoningFragmentFromDelta(d); got != "step 1" {
		t.Fatalf("got %q", got)
	}
	var d2 openai.ChatCompletionChunkChoiceDelta
	if err := json.Unmarshal([]byte(`{"reasoning":{"text":"nested"}}`), &d2); err != nil {
		t.Fatal(err)
	}
	if got := reasoningFragmentFromDelta(d2); got != "nested" {
		t.Fatalf("got %q", got)
	}
}
