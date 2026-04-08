package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseMappedPort(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"standard", "0.0.0.0:8888->8080/tcp", 8888},
		{"ipv6", ":::8888->8080/tcp", 8888},
		{"custom port", "0.0.0.0:9090->8080/tcp", 9090},
		{"multiple mappings", "0.0.0.0:8888->8080/tcp, 0.0.0.0:8443->8443/tcp", 8888},
		{"no arrow", "8080/tcp", 0},
		{"empty", "", 0},
		{"no ip prefix", "8888->8080/tcp", 8888},
		{"garbage", "not-a-port->8080/tcp", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseMappedPort(tc.input)
			if got != tc.want {
				t.Errorf("parseMappedPort(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestSearxngAssetsDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)

	got, err := searxngAssetsDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "docker", "searxng")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	composePath := filepath.Join(got, "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("docker-compose.yml not written: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("docker-compose.yml is empty")
	}

	settingsPath := filepath.Join(got, "settings.yml")
	data, err = os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.yml not written: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("settings.yml is empty")
	}
}

func TestSearxngAssetsDir_Idempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)

	d1, err := searxngAssetsDir()
	if err != nil {
		t.Fatal(err)
	}
	d2, err := searxngAssetsDir()
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("not idempotent: %q vs %q", d1, d2)
	}
}

func TestWaitForSearxng_ImmediateOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := waitForSearxng(srv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestWaitForSearxng_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := waitForSearxng(srv.URL, 2*time.Second)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWaitForSearxng_EventuallyReady(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := waitForSearxng(srv.URL, 10*time.Second)
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if calls < 3 {
		t.Fatalf("expected at least 3 calls, got %d", calls)
	}
}

func TestSearxngAssetsDir_ContentsMatchEmbedded(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)

	got, err := searxngAssetsDir()
	if err != nil {
		t.Fatal(err)
	}

	compose, err := os.ReadFile(filepath.Join(got, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(compose) != searxngComposeYML {
		t.Fatal("docker-compose.yml content does not match embedded constant")
	}

	settings, err := os.ReadFile(filepath.Join(got, "settings.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(settings) != searxngSettingsYML {
		t.Fatal("settings.yml content does not match embedded constant")
	}
}
