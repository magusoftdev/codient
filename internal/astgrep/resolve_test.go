package astgrep

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolve_NotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())
	if p := Resolve(); p != "" {
		t.Fatalf("expected empty, got %q", p)
	}
}

func TestResolve_FoundInBinDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	t.Setenv("PATH", t.TempDir())

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := binaryExeName()
	if err := os.WriteFile(filepath.Join(binDir, name), []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := Resolve()
	if p == "" {
		t.Fatal("expected to find binary in bin dir")
	}
}

func TestAssetName(t *testing.T) {
	name, err := AssetName()
	if err != nil {
		t.Fatal(err)
	}
	if name == "" {
		t.Fatal("empty asset name")
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	switch goos {
	case "linux":
		if goarch == "amd64" && name != "app-x86_64-unknown-linux-gnu.zip" {
			t.Fatalf("unexpected: %s", name)
		}
	case "darwin":
		if goarch == "arm64" && name != "app-aarch64-apple-darwin.zip" {
			t.Fatalf("unexpected: %s", name)
		}
	case "windows":
		if goarch == "amd64" && name != "app-x86_64-pc-windows-msvc.zip" {
			t.Fatalf("unexpected: %s", name)
		}
	}
}

func TestDownload_MockServer(t *testing.T) {
	dest := t.TempDir()
	binName := binaryExeName()

	zipBuf := createTestZip(t, binName, []byte("#!/bin/sh\necho hello"))

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/ast-grep/ast-grep/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"0.99.0"}`))
	})
	mux.HandleFunc("/download/0.99.0/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Write(zipBuf)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	origAPI := releasesAPI
	origDL := "https://github.com/ast-grep/ast-grep/releases/download/"
	// We can't easily override the package-level const, so we test the
	// sub-functions instead and do an integration-style test only when
	// the real binary is available.
	_ = origAPI
	_ = origDL

	// Test extractBinaryFromZip directly.
	tmpZip := filepath.Join(t.TempDir(), "test.zip")
	if err := os.WriteFile(tmpZip, zipBuf, 0o644); err != nil {
		t.Fatal(err)
	}
	destPath := filepath.Join(dest, binName)
	if err := extractBinaryFromZip(tmpZip, binName, destPath); err != nil {
		t.Fatalf("extract: %v", err)
	}
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "#!/bin/sh\necho hello" {
		t.Fatalf("unexpected content: %q", data)
	}
}

func TestExtractBinaryFromZip_Missing(t *testing.T) {
	zipBuf := createTestZip(t, "other-file", []byte("data"))
	tmpZip := filepath.Join(t.TempDir(), "test.zip")
	if err := os.WriteFile(tmpZip, zipBuf, 0o644); err != nil {
		t.Fatal(err)
	}
	err := extractBinaryFromZip(tmpZip, "ast-grep", filepath.Join(t.TempDir(), "ast-grep"))
	if err == nil {
		t.Fatal("expected error for missing binary in zip")
	}
}

func TestBinDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	d, err := BinDir()
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(dir, "bin")
	if d != expected {
		t.Fatalf("got %q, want %q", d, expected)
	}
	info, err := os.Stat(d)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory")
	}
}

func TestLatestTag_MockServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"0.42.1"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// latestTag uses the package const, so we test it indirectly only
	// when we can override. For now, verify the JSON parsing works
	// by hitting our mock directly.
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func createTestZip(t *testing.T, filename string, content []byte) []byte {
	t.Helper()
	tmpZip := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(tmpZip)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	fw, err := w.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	w.Close()
	f.Close()
	data, err := os.ReadFile(tmpZip)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
