package codientcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codient/internal/imageutil"
)

func TestBuildUserMessage_textOnly(t *testing.T) {
	m, err := buildUserMessage("", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.OfUser == nil || !m.OfUser.Content.OfString.Valid() {
		t.Fatalf("expected string user content")
	}
}

func TestBuildUserMessage_withImageParts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.png")
	// Minimal valid 1x1 PNG
	b := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := imageutil.LoadImage(path, imageutil.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	m, err := buildUserMessage(dir, "describe this", []imageutil.ImageAttachment{img})
	if err != nil {
		t.Fatal(err)
	}
	if m.OfUser == nil || len(m.OfUser.Content.OfArrayOfContentParts) < 2 {
		t.Fatalf("expected multipart user message")
	}
}

func TestBuildUserMessage_withFileRef(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n")

	m, err := buildUserMessage(dir, "describe @main.go please", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.OfUser == nil || !m.OfUser.Content.OfString.Valid() {
		t.Fatalf("expected string user content")
	}
	content := m.OfUser.Content.OfString.Value
	if !strings.Contains(content, "<referenced_files>") {
		t.Fatalf("expected referenced_files block: %q", content)
	}
	if !strings.Contains(content, "--- main.go ---") {
		t.Fatalf("expected file header: %q", content)
	}
	if !strings.Contains(content, "package main") {
		t.Fatalf("expected file content: %q", content)
	}
	if !strings.Contains(content, "describe") {
		t.Fatalf("expected original text preserved: %q", content)
	}
	if strings.Contains(content, "@main.go") {
		t.Fatalf("@ref should be stripped: %q", content)
	}
}

func TestBuildUserMessage_fileRefAndImage(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "util.go"), "package util\n")
	// Minimal 1x1 PNG
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
	pngPath := filepath.Join(dir, "t.png")
	if err := os.WriteFile(pngPath, png, 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := imageutil.LoadImage(pngPath, imageutil.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}

	m, err := buildUserMessage(dir, "look at @util.go and this image", []imageutil.ImageAttachment{img})
	if err != nil {
		t.Fatal(err)
	}
	if m.OfUser == nil {
		t.Fatal("expected user message")
	}
	parts := m.OfUser.Content.OfArrayOfContentParts
	if len(parts) < 2 {
		t.Fatalf("expected multipart (text + image), got %d", len(parts))
	}
	// First part should be text with referenced_files
	textPart := parts[0]
	if textPart.OfText == nil {
		t.Fatal("first part should be text")
	}
	if !strings.Contains(textPart.OfText.Text, "<referenced_files>") {
		t.Fatalf("expected file ref in text: %q", textPart.OfText.Text)
	}
}

func TestBuildUserMessage_fileRefMissing(t *testing.T) {
	dir := t.TempDir()

	// Missing file should produce a warning but not an error.
	m, err := buildUserMessage(dir, "check @nonexistent.go", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.OfUser == nil {
		t.Fatal("expected user message")
	}
	// Text should not contain referenced_files since the file was not found.
	content := m.OfUser.Content.OfString.Value
	if strings.Contains(content, "<referenced_files>") {
		t.Fatalf("should not have file refs for missing file: %q", content)
	}
}

func TestBuildUserMessage_imageRefNotProcessedAsFileRef(t *testing.T) {
	dir := t.TempDir()
	// Minimal 1x1 PNG
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(filepath.Join(dir, "img.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := buildUserMessage(dir, "look at @image:img.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Should be multipart with an image, not text with referenced_files.
	if m.OfUser == nil {
		t.Fatal("expected user message")
	}
	parts := m.OfUser.Content.OfArrayOfContentParts
	if len(parts) < 1 {
		t.Fatal("expected at least one part")
	}
	// No referenced_files block should appear.
	for _, p := range parts {
		if p.OfText != nil && strings.Contains(p.OfText.Text, "<referenced_files>") {
			t.Fatal("@image: should not be treated as @path file ref")
		}
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
