// Package openaiclient wraps an OpenAI-compatible HTTP API (openai-go client + helpers).
//
// max_concurrent in the agent config limits how many in-flight HTTP requests hit the server;
// the server's own concurrency limits are separate—tune both together.
package openaiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
	"io"
	"net/http"
	"strings"

	"codient/internal/config"
)

// Client is the narrow surface used by the agent.
type Client struct {
	oa     openai.Client
	base   string
	apiKey string
	model  shared.ChatModel
	llmSem *semaphore
}

type semaphore struct {
	ch chan struct{}
}

func newSemaphore(n int) *semaphore {
	return &semaphore{ch: make(chan struct{}, n)}
}

func (s *semaphore) acquire(ctx context.Context) error {
	select {
	case s.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *semaphore) release() {
	<-s.ch
}

// NewFromParams builds an OpenAI API client from explicit connection parameters.
func NewFromParams(baseURL, apiKey, model string, maxConcurrent int) *Client {
	base := strings.TrimRight(baseURL, "/")
	if maxConcurrent < 1 {
		maxConcurrent = 3
	}
	oa := openai.NewClient(
		option.WithBaseURL(base),
		option.WithAPIKey(apiKey),
	)
	return &Client{
		oa:     oa,
		base:   base,
		apiKey: apiKey,
		model:  shared.ChatModel(model),
		llmSem: newSemaphore(maxConcurrent),
	}
}

// New builds an OpenAI API client using top-level Config defaults.
func New(cfg *config.Config) *Client {
	return NewFromParams(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.MaxConcurrent)
}

// NewForMode builds a client using per-mode connection overrides (falling back to top-level).
func NewForMode(cfg *config.Config, mode string) *Client {
	base, key, model := cfg.ConnectionForMode(mode)
	return NewFromParams(base, key, model, cfg.MaxConcurrent)
}

// Model returns the configured model id.
func (c *Client) Model() string {
	return string(c.model)
}

// setAuthHeaders sets both OpenAI-style (Authorization: Bearer) and Anthropic-style
// (x-api-key + anthropic-version) headers so manual HTTP requests work with either provider.
func (c *Client) setAuthHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
}

// PingModels GETs /v1/models relative to the configured base URL (health / discovery).
func (c *Client) PingModels(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/models", nil)
	if err != nil {
		return err
	}
	c.setAuthHeaders(req)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("models endpoint: %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// ChatCompletion performs a non-streaming chat completion (acquires LLM semaphore).
func (c *Client) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	if err := c.llmSem.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.llmSem.release()
	return c.oa.Chat.Completions.New(ctx, params)
}

// StreamChatCompletion streams a chat completion with no tools; writes assistant text deltas to w.
// Returns the accumulated completion (including usage when the server sends it).
// Acquires the same LLM semaphore as non-streaming calls.
func (c *Client) StreamChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams, w io.Writer) (*openai.ChatCompletion, error) {
	if err := c.llmSem.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.llmSem.release()

	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
		IncludeUsage: openai.Bool(true),
	}
	stream := c.oa.Chat.Completions.NewStreaming(ctx, params)
	var acc openai.ChatCompletionAccumulator
	for stream.Next() {
		chunk := stream.Current()
		if !acc.AddChunk(chunk) {
			return nil, fmt.Errorf("chat stream: chunk accumulation failed")
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				if _, err := io.WriteString(w, ch.Delta.Content); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	out := acc.ChatCompletion
	return &out, nil
}

// StreamOption configures ChatCompletionStream.
type StreamOption func(*streamConfig)

type streamConfig struct {
	onReasoningDelta func(string)
}

// WithStreamReasoningDelta invokes f for each non-empty reasoning fragment in SSE deltas
// (provider-specific fields such as reasoning_content).
func WithStreamReasoningDelta(f func(string)) StreamOption {
	return func(c *streamConfig) {
		c.onReasoningDelta = f
	}
}

// ChatCompletionStream streams a completion (with or without tools), writes assistant
// content deltas to w, and returns the accumulated completion (same shape as non-streaming).
// Used by the agent so long replies show tokens as they arrive while tool rounds still work.
func (c *Client) ChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams, w io.Writer, opts ...StreamOption) (*openai.ChatCompletion, error) {
	if err := c.llmSem.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.llmSem.release()

	var cfg streamConfig
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}

	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
		IncludeUsage: openai.Bool(true),
	}
	stream := c.oa.Chat.Completions.NewStreaming(ctx, params)
	var acc openai.ChatCompletionAccumulator
	for stream.Next() {
		chunk := stream.Current()
		if !acc.AddChunk(chunk) {
			return nil, fmt.Errorf("chat stream: chunk accumulation failed")
		}
		for _, ch := range chunk.Choices {
			if cfg.onReasoningDelta != nil {
				if frag := reasoningFragmentFromDelta(ch.Delta); frag != "" {
					cfg.onReasoningDelta(frag)
				}
			}
			if ch.Delta.Content != "" {
				if _, err := io.WriteString(w, ch.Delta.Content); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	out := acc.ChatCompletion
	return &out, nil
}

func reasoningFragmentFromDelta(delta openai.ChatCompletionChunkChoiceDelta) string {
	raw := delta.RawJSON()
	if raw == "" {
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	for _, key := range []string{"reasoning_content", "reasoning"} {
		v, ok := m[key]
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal(v, &s) == nil && s != "" {
			return s
		}
		var obj struct {
			Text    string `json:"text"`
			Content string `json:"content"`
			Summary string `json:"summary"`
		}
		if json.Unmarshal(v, &obj) == nil {
			if obj.Text != "" {
				return obj.Text
			}
			if obj.Content != "" {
				return obj.Content
			}
			if obj.Summary != "" {
				return obj.Summary
			}
		}
	}
	return ""
}

// ProbeContextWindow tries to discover the loaded model's context window in tokens.
// It queries the LM Studio native REST API (GET /api/v1/models) which is separate from
// the OpenAI-compatible /v1/models. If the server is not LM Studio or the endpoint is
// unavailable, returns (0, nil); the session leaves ContextWindowTokens unchanged (still from config, possibly zero).
func (c *Client) ProbeContextWindow(ctx context.Context, modelID string) (int, error) {
	nativeBase := c.nativeBaseURL()
	if nativeBase == "" {
		return 0, nil
	}
	endpoint := nativeBase + "/api/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, nil
	}
	c.setAuthHeaders(req)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return 0, nil
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 512*1024))
	if err != nil {
		return 0, nil
	}
	return parseContextFromNativeModels(body, modelID), nil
}

// nativeBaseURL derives the LM Studio native API root from the OpenAI-compat base.
// e.g. "http://127.0.0.1:1234/v1" -> "http://127.0.0.1:1234"
func (c *Client) nativeBaseURL() string {
	b := c.base
	if strings.HasSuffix(b, "/v1") {
		return strings.TrimSuffix(b, "/v1")
	}
	if strings.Contains(b, "/v1/") {
		return b[:strings.LastIndex(b, "/v1/")]
	}
	return b
}

// nativeModelsResponse mirrors the LM Studio GET /api/v1/models shape (relevant fields only).
type nativeModelsResponse struct {
	Models []nativeModel `json:"models"`
}

type nativeModel struct {
	Key             string           `json:"key"`
	MaxContextLen   int              `json:"max_context_length"`
	LoadedInstances []loadedInstance `json:"loaded_instances"`
}

type loadedInstance struct {
	ID     string         `json:"id"`
	Config instanceConfig `json:"config"`
}

type instanceConfig struct {
	ContextLength int `json:"context_length"`
}

func parseContextFromNativeModels(data []byte, modelID string) int {
	var resp nativeModelsResponse
	if json.Unmarshal(data, &resp) != nil {
		return 0
	}
	modelID = strings.TrimSpace(modelID)
	for _, m := range resp.Models {
		if !modelKeyMatches(m.Key, modelID) {
			continue
		}
		for _, inst := range m.LoadedInstances {
			if inst.Config.ContextLength > 0 {
				return inst.Config.ContextLength
			}
		}
		if m.MaxContextLen > 0 {
			return m.MaxContextLen
		}
	}
	// Also try matching loaded instance IDs directly.
	for _, m := range resp.Models {
		for _, inst := range m.LoadedInstances {
			if inst.ID == modelID && inst.Config.ContextLength > 0 {
				return inst.Config.ContextLength
			}
		}
	}
	return 0
}

func modelKeyMatches(key, modelID string) bool {
	if key == modelID {
		return true
	}
	if strings.EqualFold(key, modelID) {
		return true
	}
	return false
}

// CreateEmbedding calls /v1/embeddings with the given model and input strings.
// Inputs are batched in groups of batchSize. Returns one []float64 per input, in order.
func (c *Client) CreateEmbedding(ctx context.Context, model string, inputs []string) ([][]float64, error) {
	const batchSize = 64
	all := make([][]float64, len(inputs))
	for start := 0; start < len(inputs); start += batchSize {
		end := start + batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := inputs[start:end]
		if err := c.llmSem.acquire(ctx); err != nil {
			return nil, err
		}
		res, err := c.oa.Embeddings.New(ctx, openai.EmbeddingNewParams{
			Model: openai.EmbeddingModel(model),
			Input: openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: batch},
		})
		c.llmSem.release()
		if err != nil {
			return nil, fmt.Errorf("embedding batch %d-%d: %w", start, end-1, err)
		}
		for _, e := range res.Data {
			idx := start + int(e.Index)
			if idx < len(all) {
				all[idx] = e.Embedding
			}
		}
	}
	return all, nil
}

// ExtractModelIDsFromJSON parses GET /models JSON used by OpenAI-compat and several local servers:
// top-level "data" and/or "models" arrays of strings or objects. Object fields recognized in order:
// id, model, key (LM Studio catalog entries often expose only key), name.
func ExtractModelIDsFromJSON(body []byte) ([]string, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, fmt.Errorf("empty models body")
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}

	seen := make(map[string]struct{})
	var ids []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		ids = append(ids, s)
	}

	for _, top := range []string{"data", "models"} {
		raw, ok := probe[top]
		if !ok || len(raw) == 0 || string(raw) == "null" {
			continue
		}
		appendIDsFromJSONArray(raw, add)
	}

	return ids, nil
}

func appendIDsFromJSONArray(raw json.RawMessage, add func(string)) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return
	}
	for _, el := range arr {
		var s string
		if json.Unmarshal(el, &s) == nil {
			add(s)
			continue
		}
		var obj map[string]any
		if json.Unmarshal(el, &obj) != nil {
			continue
		}
		var pick string
		for _, key := range []string{"id", "model", "key", "name"} {
			if v, ok := obj[key]; ok && v != nil {
				switch t := v.(type) {
				case string:
					pick = t
				case float64:
					pick = fmt.Sprint(int64(t))
				default:
					pick = fmt.Sprint(v)
				}
				pick = strings.TrimSpace(pick)
				if pick != "" {
					break
				}
			}
		}
		add(pick)
	}
}

// ListModels fetches model ids from GET /models relative to client base URL.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/models", nil)
	if err != nil {
		return nil, err
	}
	c.setAuthHeaders(req)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("models: %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	return ExtractModelIDsFromJSON(body)
}
