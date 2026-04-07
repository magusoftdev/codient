package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openai/openai-go/v3/shared"
)

const (
	defaultSearchResults    = 5
	maxSearchResults        = 10
	defaultSearchTimeoutSec = 30
	maxSearchBodyBytes      = 512 * 1024
)

// SearchOptions configures the optional web_search tool backed by a SearXNG instance.
type SearchOptions struct {
	BaseURL    string // SearXNG base URL (e.g. "http://localhost:8080").
	MaxResults int    // Default 5, max 10.
	TimeoutSec int    // Per-request timeout (default 30s).
}

func registerWebSearch(r *Registry, opts *SearchOptions) {
	if opts == nil || strings.TrimSpace(opts.BaseURL) == "" {
		return
	}

	maxN := opts.MaxResults
	if maxN < 1 {
		maxN = defaultSearchResults
	}
	if maxN > maxSearchResults {
		maxN = maxSearchResults
	}
	timeout := time.Duration(opts.TimeoutSec) * time.Second
	if timeout < time.Second {
		timeout = defaultSearchTimeoutSec * time.Second
	}
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")

	r.Register(Tool{
		Name: "web_search",
		Description: "Search the web for documentation, error messages, API references, or library usage. " +
			"Backed by SearXNG. Returns a numbered list of results with title, URL, and snippet. " +
			"Chain with fetch_url to read full page content from a result URL. " +
			"Prefer this over guessing about unfamiliar libraries or APIs.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query.",
				},
				"max_results": map[string]any{
					"type":        "integer",
					"description": "Number of results to return (default 5, max 10).",
				},
			},
			"required":             []string{"query"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Query      string `json:"query"`
				MaxResults *int   `json:"max_results"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			n := maxN
			if p.MaxResults != nil && *p.MaxResults > 0 {
				n = *p.MaxResults
				if n > maxSearchResults {
					n = maxSearchResults
				}
			}
			return searxngSearch(ctx, baseURL, p.Query, n, timeout)
		},
	})
}

func searxngSearch(ctx context.Context, baseURL, query string, n int, timeout time.Duration) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	u, err := url.Parse(baseURL + "/search")
	if err != nil {
		return "", fmt.Errorf("invalid searxng base URL: %w", err)
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	q.Set("categories", "general")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSearchBodyBytes))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("searxng returned HTTP %d: %s", resp.StatusCode, truncateBytes(body, 256))
	}

	var data struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("searxng: invalid JSON: %w", err)
	}
	limit := n
	if limit > len(data.Results) {
		limit = len(data.Results)
	}
	results := make([]searchResult, limit)
	for i := 0; i < limit; i++ {
		r := data.Results[i]
		results[i] = searchResult{Title: r.Title, URL: r.URL, Snippet: r.Content}
	}
	return formatSearchResults(query, results), nil
}

type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

func formatSearchResults(query string, results []searchResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("No results found for %q.", query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Search results for %q:\n\n", query)
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.Title, r.URL)
		if s := strings.TrimSpace(r.Snippet); s != "" {
			fmt.Fprintf(&b, "   %s\n", s)
		}
		if i < len(results)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func truncateBytes(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
