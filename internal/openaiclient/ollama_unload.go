package openaiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ollamaUnloadSkipHosts avoids POSTing to Ollama's native path on obvious cloud OpenAI-compat bases.
var ollamaUnloadSkipHosts = []string{
	"api.openai.com",
	"api.anthropic.com",
	"generativelanguage.googleapis.com",
}

func shouldProbeOllamaUnload(apiBase string) bool {
	u, err := url.Parse(apiBase)
	if err != nil {
		return false
	}
	h := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if h == "" {
		return false
	}
	for _, skip := range ollamaUnloadSkipHosts {
		if h == skip {
			return false
		}
	}
	return !strings.HasSuffix(h, ".openai.azure.com")
}

// TryOllamaUnloadModel asks an Ollama server to unload modelID from memory (POST /api/generate with keep_alive 0).
// It is a best-effort no-op when modelID is empty, the base URL is not …/v1, the host looks like a cloud API,
// or the server does not implement Ollama's native API (e.g. HTTP 404).
func (c *Client) TryOllamaUnloadModel(ctx context.Context, modelID string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil
	}
	if !shouldProbeOllamaUnload(c.base) {
		return nil
	}
	root := c.nativeBaseURL()
	if root == "" || root == c.base {
		return nil
	}
	u := strings.TrimRight(root, "/") + "/api/generate"
	body, err := json.Marshal(map[string]any{
		"model":      modelID,
		"keep_alive": 0,
	})
	if err != nil {
		return nil
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil
	}
	c.setAuthHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil
	}
	return nil
}
