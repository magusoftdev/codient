//go:build integration

package clipboard

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestIntegration_HasImage calls HasImage with the real system clipboard.
// Run manually with either raw image data or a file URI on the clipboard:
//
//	# raw image bytes (browser "Copy Image", screenshot tool)
//	CODIENT_INTEGRATION=1 go test -tags integration -run TestIntegration_HasImage -v ./internal/clipboard/
//
//	# file reference (Nautilus/Dolphin/Finder "Copy", or simulated):
//	#   printf 'file://%s' /abs/path/to/pic.png | wl-copy --type text/uri-list
//	CODIENT_INTEGRATION=1 go test -tags integration -run TestIntegration_HasImage -v ./internal/clipboard/
func TestIntegration_HasImage(t *testing.T) {
	if os.Getenv("CODIENT_INTEGRATION") == "" {
		t.Skip("set CODIENT_INTEGRATION=1 to run live clipboard tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	has, err := HasImage(ctx, DefaultExecutor())
	if err != nil {
		t.Logf("HasImage error (may be expected if no display): %v", err)
		return
	}
	t.Logf("HasImage = %v", has)
}

// TestIntegration_SaveImage extracts clipboard image data to a temp file.
// Run manually with an image in the clipboard.
func TestIntegration_SaveImage(t *testing.T) {
	if os.Getenv("CODIENT_INTEGRATION") == "" {
		t.Skip("set CODIENT_INTEGRATION=1 to run live clipboard tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dir := t.TempDir()
	path, err := SaveImage(ctx, DefaultExecutor(), dir)
	if err != nil {
		t.Logf("SaveImage error (expected if clipboard has no image): %v", err)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved file: %v", err)
	}
	t.Logf("saved %s (%d bytes)", path, info.Size())
}
