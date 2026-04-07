package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/openai/openai-go/v3/shared"
)

const (
	defaultFetchMaxBytes   = 1024 * 1024
	maxFetchMaxBytes       = 10 * 1024 * 1024
	defaultFetchTimeoutSec = 30
	maxFetchRedirects      = 5
)

// FetchOptions configures the optional fetch_url tool (HTTPS GET with host allowlist).
type FetchOptions struct {
	AllowHosts  []string // Lowercase hostnames; subdomains match (e.g. api.example.com matches example.com).
	MaxBytes    int      // Response body cap (default 1MiB, max 10MiB).
	TimeoutSec  int      // Per-request timeout (default 30s).
}

func registerFetchURL(r *Registry, opts *FetchOptions) {
	if opts == nil || len(opts.AllowHosts) == 0 {
		return
	}
	maxB := opts.MaxBytes
	if maxB < 1 {
		maxB = defaultFetchMaxBytes
	}
	if maxB > maxFetchMaxBytes {
		maxB = maxFetchMaxBytes
	}
	timeout := time.Duration(opts.TimeoutSec) * time.Second
	if timeout < time.Second {
		timeout = defaultFetchTimeoutSec * time.Second
	}
	allow := append([]string(nil), opts.AllowHosts...)

	r.Register(Tool{
		Name: "fetch_url",
		Description: "HTTPS GET of a URL (text response only). " +
			"Host must be allowlisted via CODIENT_FETCH_ALLOW_HOSTS (comma-separated). " +
			"Redirects are followed only if each hop stays on an allowlisted host and uses HTTPS. " +
			"Response body is capped by max_bytes (default 1MiB). " +
			"Use for public documentation or stable APIs—never for secrets or internal networks.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "HTTPS URL to fetch (GET only).",
				},
				"max_bytes": map[string]any{
					"type":        "integer",
					"description": "Optional cap on response bytes (default from config, max 10MiB).",
				},
			},
			"required":             []string{"url"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				URL      string `json:"url"`
				MaxBytes *int   `json:"max_bytes"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			limit := maxB
			if p.MaxBytes != nil && *p.MaxBytes > 0 {
				limit = *p.MaxBytes
				if limit > maxFetchMaxBytes {
					limit = maxFetchMaxBytes
				}
			}
			return fetchURL(ctx, p.URL, allow, limit, timeout)
		},
	})
}

func fetchURL(ctx context.Context, raw string, allowHosts []string, maxBytes int, timeout time.Duration) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid URL")
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("only https URLs are allowed")
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("missing host")
	}
	if !hostAllowedFetch(host, allowHosts) {
		return "", fmt.Errorf("host %q is not allowlisted (set CODIENT_FETCH_ALLOW_HOSTS)", host)
	}
	if timeout <= 0 {
		timeout = defaultFetchTimeoutSec * time.Second
	}

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxFetchRedirects {
				return fmt.Errorf("stopped after %d redirects", maxFetchRedirects)
			}
			if req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to non-https URL forbidden")
			}
			h := req.URL.Hostname()
			if !hostAllowedFetch(h, allowHosts) {
				return fmt.Errorf("redirect to non-allowlisted host %q", h)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "codient-fetch/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var body []byte
	body, err = io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return "", err
	}
	truncated := len(body) > maxBytes
	if truncated {
		body = body[:maxBytes]
	}
	if !utf8.Valid(body) {
		return "", fmt.Errorf("response is not valid UTF-8 (refusing to return binary)")
	}
	s := string(body)
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		s = htmlToMarkdown(s)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "HTTP %s\nContent-Type: %s\n", resp.Status, resp.Header.Get("Content-Type"))
	if truncated {
		fmt.Fprintf(&out, "[truncated: exceeded max_bytes=%d]\n", maxBytes)
	}
	out.WriteString("\n")
	out.WriteString(s)
	return out.String(), nil
}

func hostAllowedFetch(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if ip := net.ParseIP(host); ip != nil {
		if isDisallowedFetchIP(ip) {
			return false
		}
	}
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if host == a || strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}

func isDisallowedFetchIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// IPv4-mapped / documentation / etc.
		if ip4[0] == 0 {
			return true
		}
	}
	return false
}
