package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearxngSearch_ParsesResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("format") != "json" {
			t.Error("missing format=json")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [
				{"title": "Result One", "url": "https://example.com/one", "content": "First result snippet."},
				{"title": "Result Two", "url": "https://example.com/two", "content": "Second result snippet."},
				{"title": "Result Three", "url": "https://example.com/three", "content": "Third."}
			]
		}`))
	}))
	defer srv.Close()

	out, err := searxngSearch(context.Background(), srv.URL, "test query", 2, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Result One") {
		t.Errorf("missing first result: %s", out)
	}
	if !strings.Contains(out, "Result Two") {
		t.Errorf("missing second result: %s", out)
	}
	if strings.Contains(out, "Result Three") {
		t.Errorf("should have been capped to 2 results: %s", out)
	}
}

func TestSearxngSearch_AllResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [
				{"title": "Only Result", "url": "https://example.com/only", "content": "The only one."}
			]
		}`))
	}))
	defer srv.Close()

	out, err := searxngSearch(context.Background(), srv.URL, "singleton", 5, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Only Result") {
		t.Errorf("missing result: %s", out)
	}
}

func TestSearxngSearch_EmptyQuery(t *testing.T) {
	_, err := searxngSearch(context.Background(), "http://localhost:9999", "", 5, 10*time.Second)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "query is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSearxngSearch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	_, err := searxngSearch(context.Background(), srv.URL, "test", 5, 10*time.Second)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestRegisterWebSearch_NilOpts(t *testing.T) {
	r := NewRegistry()
	registerWebSearch(r, nil, nil)
	for _, n := range r.Names() {
		if n == "web_search" {
			t.Fatal("web_search should not be registered with nil opts")
		}
	}
}

func TestRegisterWebSearch_EmptyURL(t *testing.T) {
	r := NewRegistry()
	registerWebSearch(r, &SearchOptions{}, nil)
	for _, n := range r.Names() {
		if n == "web_search" {
			t.Fatal("web_search should not be registered without base URL")
		}
	}
}

func TestRegisterWebSearch_WithURL(t *testing.T) {
	r := NewRegistry()
	registerWebSearch(r, &SearchOptions{BaseURL: "http://localhost:8080"}, nil)
	found := false
	for _, n := range r.Names() {
		if n == "web_search" {
			found = true
		}
	}
	if !found {
		t.Fatal("web_search should be registered with a base URL")
	}
}

func TestFormatSearchResults_Empty(t *testing.T) {
	out := formatSearchResults("test", nil)
	if !strings.Contains(out, "No results") {
		t.Errorf("expected no-results message, got: %s", out)
	}
}
