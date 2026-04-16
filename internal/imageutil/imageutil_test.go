package imageutil

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupportedImageExt(t *testing.T) {
	if !SupportedImageExt("x.PNG") || !SupportedImageExt("a.jpeg") {
		t.Fatal("expected supported")
	}
	if SupportedImageExt("a.txt") || SupportedImageExt("noext") {
		t.Fatal("expected unsupported")
	}
}

func TestLoadImage_validPNG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.png")
	if err := writeTestPNG(path, 64, 48); err != nil {
		t.Fatal(err)
	}
	a, err := LoadImage(path, DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if a.MimeType != "image/png" || a.OrigBytes < 10 {
		t.Fatalf("mime=%s bytes=%d", a.MimeType, a.OrigBytes)
	}
	if !strings.HasPrefix(a.DataURI, "data:image/png;base64,") {
		t.Fatalf("bad data uri prefix: %q", a.DataURI[:min(40, len(a.DataURI))])
	}
}

func TestLoadImage_missing(t *testing.T) {
	_, err := LoadImage(filepath.Join(t.TempDir(), "nope.png"), DefaultMaxBytes)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestLoadImage_oversized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.png")
	if err := writeTestPNG(path, 4, 4); err != nil {
		t.Fatal(err)
	}
	_, err := LoadImage(path, 1)
	if err == nil || !strings.Contains(err.Error(), "max size") {
		t.Fatalf("want max size error, got %v", err)
	}
}

func TestLoadImage_badExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadImage(path, DefaultMaxBytes)
	if err == nil || !strings.Contains(err.Error(), "unsupported image extension") {
		t.Fatalf("got %v", err)
	}
}

func TestParseInlineImages(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.png")
	p2 := filepath.Join(dir, "b.png")
	if err := writeTestPNG(p1, 8, 8); err != nil {
		t.Fatal(err)
	}
	if err := writeTestPNG(p2, 8, 8); err != nil {
		t.Fatal(err)
	}
	text := `Hello @image:` + p1 + ` more @image:"` + p2 + `" end`
	clean, atts, err := ParseInlineImages(text, dir, DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 2 {
		t.Fatalf("attachments: %d", len(atts))
	}
	if !strings.Contains(clean, "Hello") || !strings.Contains(clean, "more") || !strings.Contains(clean, "end") {
		t.Fatalf("clean: %q", clean)
	}
	if strings.Contains(clean, "@image:") {
		t.Fatalf("should strip @image: %q", clean)
	}
}

func TestEstimateImageTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.png")
	if err := writeTestPNG(path, 512, 512); err != nil {
		t.Fatal(err)
	}
	a, err := LoadImage(path, DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	n := EstimateImageTokens(a.DataURI)
	if n < 85 || n > 500 {
		t.Fatalf("tokens %d out of range", n)
	}
}

func writeTestPNG(path string, w, h int) error {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
