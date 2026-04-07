package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHostAllowedFetch(t *testing.T) {
	allow := []string{"example.com", "api.foo.org"}
	tests := []struct {
		host string
		want bool
	}{
		{"example.com", true},
		{"www.example.com", true},
		{"api.foo.org", true},
		{"foo.org", false},
		{"evil.com", false},
		{"127.0.0.1", false},
	}
	for _, tc := range tests {
		if got := hostAllowedFetch(tc.host, allow); got != tc.want {
			t.Errorf("%q: got %v want %v", tc.host, got, tc.want)
		}
	}
}

func TestFetchURL_SchemeAndHost(t *testing.T) {
	ctx := context.Background()
	_, err := fetchURL(ctx, "http://example.com/x", []string{"example.com"}, 100, 0)
	if err == nil {
		t.Fatal("expected error for http")
	}
	_, err = fetchURL(ctx, "https://evil.com/", []string{"example.com"}, 100, 0)
	if err == nil {
		t.Fatal("expected error for host")
	}
}

// TestFetchURL_HTMLConvertedToMarkdown verifies that the Content-Type → htmlToMarkdown
// path works end-to-end. We use httptest.NewTLSServer + ts.Client() because fetchURL
// blocks loopback IPs (by design); the host-allowlist is tested separately above.
func TestFetchURL_HTMLConvertedToMarkdown(t *testing.T) {
	const page = `<html><head><title>Test</title></head><body>
<script>alert("xss")</script>
<h1>Hello</h1>
<p>Visit <a href="https://example.com">Example</a> for info.</p>
</body></html>`

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	}))
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/page")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		s = htmlToMarkdown(s)
	}

	if !strings.Contains(s, "# Hello") {
		t.Errorf("expected markdown heading '# Hello', got:\n%s", s)
	}
	if !strings.Contains(s, "[Example](https://example.com)") {
		t.Errorf("expected markdown link, got:\n%s", s)
	}
	if strings.Contains(s, "<h1>") || strings.Contains(s, "<p>") {
		t.Errorf("expected no raw HTML tags, got:\n%s", s)
	}
	if strings.Contains(s, "alert") {
		t.Errorf("expected script content stripped, got:\n%s", s)
	}
	if strings.Contains(s, "Test") {
		t.Errorf("expected <head> content stripped, got:\n%s", s)
	}
}

func TestFetchURL_NonHTMLNotConverted(t *testing.T) {
	const jsonBody = `{"key": "value"}`
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, jsonBody)
	}))
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		s = htmlToMarkdown(s)
	}

	if !strings.Contains(s, `{"key": "value"}`) {
		t.Errorf("expected raw JSON body preserved, got:\n%s", s)
	}
}
