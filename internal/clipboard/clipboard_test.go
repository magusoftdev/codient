package clipboard

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeExec implements Executor for testing.
type fakeExec struct {
	lookPathResults map[string]bool
	runResults      map[string]runResult
	pipeData        []byte
	pipeErr         error
}

type runResult struct {
	out string
	err error
}

func (f *fakeExec) LookPath(name string) (string, error) {
	if f.lookPathResults[name] {
		return "/usr/bin/" + name, nil
	}
	return "", fmt.Errorf("not found: %s", name)
}

func (f *fakeExec) Run(_ context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	if r, ok := f.runResults[key]; ok {
		return r.out, r.err
	}
	if r, ok := f.runResults[name]; ok {
		return r.out, r.err
	}
	return "", fmt.Errorf("unexpected command: %s %v", name, args)
}

func (f *fakeExec) RunPipe(_ context.Context, w io.Writer, name string, args ...string) error {
	if f.pipeErr != nil {
		return f.pipeErr
	}
	if f.pipeData != nil {
		_, err := w.Write(f.pipeData)
		return err
	}
	return fmt.Errorf("no pipe data for %s", name)
}

func TestResolve_LinuxWayland(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	e := &fakeExec{lookPathResults: map[string]bool{"wl-paste": true}}
	b := resolve(e)
	if b.platform != "wayland" || b.tool != "wl-paste" {
		t.Fatalf("got %+v", b)
	}
}

func TestResolve_LinuxX11(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	t.Setenv("XDG_SESSION_TYPE", "x11")
	e := &fakeExec{lookPathResults: map[string]bool{"xclip": true}}
	b := resolve(e)
	if b.platform != "x11" || b.tool != "xclip" {
		t.Fatalf("got %+v", b)
	}
}

func TestResolve_LinuxNoTool(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	e := &fakeExec{lookPathResults: map[string]bool{}}
	b := resolve(e)
	if b.platform != "wayland" || b.tool != "" {
		t.Fatalf("got %+v", b)
	}
}

func TestResolve_LinuxUnknownSessionFallback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	t.Setenv("XDG_SESSION_TYPE", "")
	e := &fakeExec{lookPathResults: map[string]bool{"xclip": true}}
	b := resolve(e)
	if b.platform != "x11" || b.tool != "xclip" {
		t.Fatalf("got %+v", b)
	}
}

func TestHasImage_WaylandTrue(t *testing.T) {
	e := &fakeExec{
		lookPathResults: map[string]bool{"wl-paste": true},
		runResults: map[string]runResult{
			"wl-paste --list-types": {out: "image/png\ntext/plain\n"},
		},
	}
	b := backend{platform: "wayland", tool: "wl-paste"}
	has, err := hasImage(context.Background(), e, b)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected true")
	}
}

func TestHasImage_WaylandNoImage(t *testing.T) {
	e := &fakeExec{
		runResults: map[string]runResult{
			"wl-paste --list-types": {out: "text/plain\n"},
		},
	}
	b := backend{platform: "wayland", tool: "wl-paste"}
	has, err := hasImage(context.Background(), e, b)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected false")
	}
}

func TestHasImage_WaylandMissingTool(t *testing.T) {
	e := &fakeExec{}
	b := backend{platform: "wayland", tool: ""}
	_, err := hasImage(context.Background(), e, b)
	var unsup *UnsupportedError
	if err == nil {
		t.Fatal("expected error")
	}
	if !isUnsupportedError(err, &unsup) {
		t.Fatalf("expected UnsupportedError, got %T: %v", err, err)
	}
}

func TestHasImage_X11True(t *testing.T) {
	e := &fakeExec{
		runResults: map[string]runResult{
			"xclip -selection clipboard -t TARGETS -o": {out: "image/png\nTARGETS\n"},
		},
	}
	b := backend{platform: "x11", tool: "xclip"}
	has, err := hasImage(context.Background(), e, b)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected true")
	}
}

func TestHasImage_DarwinTrue(t *testing.T) {
	e := &fakeExec{
		runResults: map[string]runResult{
			"osascript -e clipboard info": {out: "«class PNGf», 1234\n"},
		},
	}
	b := backend{platform: "darwin", tool: "osascript"}
	has, err := hasImage(context.Background(), e, b)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected true")
	}
}

func TestHasImage_DarwinNoImage(t *testing.T) {
	e := &fakeExec{
		runResults: map[string]runResult{
			"osascript -e clipboard info": {out: "«class utf8», 42\n"},
		},
	}
	b := backend{platform: "darwin", tool: "osascript"}
	has, err := hasImage(context.Background(), e, b)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected false")
	}
}

func TestHasImage_WindowsTrue(t *testing.T) {
	e := &fakeExec{
		runResults: map[string]runResult{
			"powershell": {out: "image=True;files=False\n"},
		},
	}
	b := backend{platform: "windows", tool: "powershell"}
	has, err := hasImage(context.Background(), e, b)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected true")
	}
}

func TestHasImage_WindowsFalse(t *testing.T) {
	e := &fakeExec{
		runResults: map[string]runResult{
			"powershell": {out: "image=False;files=False\n"},
		},
	}
	b := backend{platform: "windows", tool: "powershell"}
	has, err := hasImage(context.Background(), e, b)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected false")
	}
}

func TestHasImage_UnsupportedPlatform(t *testing.T) {
	e := &fakeExec{}
	b := backend{platform: "freebsd"}
	_, err := hasImage(context.Background(), e, b)
	if err == nil {
		t.Fatal("expected error")
	}
	var unsup *UnsupportedError
	if !isUnsupportedError(err, &unsup) {
		t.Fatalf("expected UnsupportedError, got %T: %v", err, err)
	}
}

func TestHasImage_CommandError(t *testing.T) {
	e := &fakeExec{
		runResults: map[string]runResult{
			"wl-paste --list-types": {out: "", err: fmt.Errorf("exit status 1")},
		},
	}
	b := backend{platform: "wayland", tool: "wl-paste"}
	has, err := hasImage(context.Background(), e, b)
	if err != nil {
		t.Fatalf("command error should yield false, not error: %v", err)
	}
	if has {
		t.Fatal("expected false on command error")
	}
}

func TestHasImage_WaylandURIListImage(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "diagram.png")
	if err := os.WriteFile(imgPath, []byte("PNG\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	uriList := "file://" + imgPath + "\n"
	e := &fakeExec{
		runResults: map[string]runResult{
			"wl-paste --list-types": {out: "text/uri-list\ntext/plain;charset=utf-8\n"},
			"wl-paste --no-newline --type text/uri-list": {out: uriList},
		},
	}
	b := backend{platform: "wayland", tool: "wl-paste"}
	has, err := hasImage(context.Background(), e, b)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected true for uri-list pointing at an image file")
	}
}

func TestHasImage_X11URIListImage(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "shot.jpg")
	if err := os.WriteFile(imgPath, []byte("JPG"), 0o644); err != nil {
		t.Fatal(err)
	}
	uriList := "file://" + imgPath + "\n"
	e := &fakeExec{
		runResults: map[string]runResult{
			"xclip -selection clipboard -t TARGETS -o":       {out: "TARGETS\ntext/uri-list\nUTF8_STRING\n"},
			"xclip -selection clipboard -t text/uri-list -o": {out: uriList},
		},
	}
	b := backend{platform: "x11", tool: "xclip"}
	has, err := hasImage(context.Background(), e, b)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected true for uri-list pointing at an image file")
	}
}

func TestHasImage_URIListNonImage(t *testing.T) {
	dir := t.TempDir()
	txtPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txtPath, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	uriList := "file://" + txtPath + "\n"
	e := &fakeExec{
		runResults: map[string]runResult{
			"wl-paste --list-types": {out: "text/uri-list\ntext/plain\n"},
			"wl-paste --no-newline --type text/uri-list": {out: uriList},
		},
	}
	b := backend{platform: "wayland", tool: "wl-paste"}
	has, err := hasImage(context.Background(), e, b)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected false for uri-list with non-image entries")
	}
}

func TestHasImage_URIListMissingFile(t *testing.T) {
	uriList := "file:///tmp/does-not-exist-codient-clipboard-test.png\n"
	e := &fakeExec{
		runResults: map[string]runResult{
			"wl-paste --list-types": {out: "text/uri-list\n"},
			"wl-paste --no-newline --type text/uri-list": {out: uriList},
		},
	}
	b := backend{platform: "wayland", tool: "wl-paste"}
	has, err := hasImage(context.Background(), e, b)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected false when uri-list entry refers to a missing file")
	}
}

func TestSaveImage_WaylandURIList(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only resolve test")
	}
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	srcDir := t.TempDir()
	imgPath := filepath.Join(srcDir, "hero.png")
	if err := os.WriteFile(imgPath, []byte("PNG\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	uriList := "file://" + imgPath + "\n"

	e := &fakeExec{
		lookPathResults: map[string]bool{"wl-paste": true},
		runResults: map[string]runResult{
			"wl-paste --list-types": {out: "text/uri-list\ntext/plain\n"},
			"wl-paste --no-newline --type text/uri-list": {out: uriList},
		},
	}

	clipDir := t.TempDir()
	got, err := SaveImage(context.Background(), e, clipDir)
	if err != nil {
		t.Fatalf("SaveImage: %v", err)
	}
	if got != imgPath {
		t.Fatalf("expected original path %q, got %q", imgPath, got)
	}
}

func TestSaveImage_X11URIList(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only resolve test")
	}
	t.Setenv("XDG_SESSION_TYPE", "x11")
	srcDir := t.TempDir()
	imgPath := filepath.Join(srcDir, "graph.jpeg")
	if err := os.WriteFile(imgPath, []byte("JPG"), 0o644); err != nil {
		t.Fatal(err)
	}
	uriList := "file://" + imgPath + "\n"

	e := &fakeExec{
		lookPathResults: map[string]bool{"xclip": true},
		runResults: map[string]runResult{
			"xclip -selection clipboard -t TARGETS -o":       {out: "TARGETS\ntext/uri-list\n"},
			"xclip -selection clipboard -t text/uri-list -o": {out: uriList},
		},
	}

	clipDir := t.TempDir()
	got, err := SaveImage(context.Background(), e, clipDir)
	if err != nil {
		t.Fatalf("SaveImage: %v", err)
	}
	if got != imgPath {
		t.Fatalf("expected original path %q, got %q", imgPath, got)
	}
}

func TestSaveImage_NoImageNoFiles(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only resolve test")
	}
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	e := &fakeExec{
		lookPathResults: map[string]bool{"wl-paste": true},
		runResults: map[string]runResult{
			"wl-paste --list-types": {out: "text/plain\n"},
		},
	}
	clipDir := t.TempDir()
	_, err := SaveImage(context.Background(), e, clipDir)
	if err == nil {
		t.Fatal("expected error when clipboard has no image and no file URIs")
	}
}

func TestParseURIList(t *testing.T) {
	t.Run("multiple URIs with comments", func(t *testing.T) {
		input := "# comment\nfile:///tmp/a.png\nfile:///tmp/b.jpg\n\nhttp://example.com/c.png\n"
		got := parseURIList(input)
		want := []string{"/tmp/a.png", "/tmp/b.jpg"}
		if len(got) != len(want) {
			t.Fatalf("got %d paths, want %d (%v)", len(got), len(want), got)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
			}
		}
	})
	t.Run("percent-encoded path", func(t *testing.T) {
		got := parseURIList("file:///tmp/my%20pic.png\n")
		want := []string{"/tmp/my pic.png"}
		if len(got) != 1 || got[0] != want[0] {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("CRLF line endings", func(t *testing.T) {
		got := parseURIList("file:///tmp/a.png\r\nfile:///tmp/b.png\r\n")
		if len(got) != 2 || got[0] != "/tmp/a.png" || got[1] != "/tmp/b.png" {
			t.Fatalf("unexpected: %v", got)
		}
	})
	t.Run("non-file URIs ignored", func(t *testing.T) {
		got := parseURIList("ftp://example.com/x.png\nhttp://example.com/y.png\n")
		if len(got) != 0 {
			t.Fatalf("expected empty, got %v", got)
		}
	})
}

func TestFileURIToPath(t *testing.T) {
	t.Run("simple path", func(t *testing.T) {
		p, ok := fileURIToPath("file:///home/me/pic.png")
		if !ok || p != "/home/me/pic.png" {
			t.Fatalf("got (%q, %v)", p, ok)
		}
	})
	t.Run("encoded path", func(t *testing.T) {
		p, ok := fileURIToPath("file:///home/me/a%20b.png")
		if !ok || p != "/home/me/a b.png" {
			t.Fatalf("got (%q, %v)", p, ok)
		}
	})
	t.Run("non-file scheme", func(t *testing.T) {
		_, ok := fileURIToPath("http://example.com/x.png")
		if ok {
			t.Fatal("expected http:// to be rejected")
		}
	})
	t.Run("malformed", func(t *testing.T) {
		_, ok := fileURIToPath("not a url")
		if ok {
			t.Fatal("expected garbage to be rejected")
		}
	})
}

func TestSaveImage_ViaPipe(t *testing.T) {
	dir := t.TempDir()
	pngData := fakePNG()
	e := &fakeExec{
		runResults: map[string]runResult{
			"wl-paste --list-types": {out: "image/png\n"},
		},
		pipeData: pngData,
	}
	b := backend{platform: "wayland", tool: "wl-paste"}

	// Test saveViaPipe directly.
	dest := filepath.Join(dir, "clipboard-test.png")
	got, err := saveViaPipe(context.Background(), e, dest, "wl-paste", "--no-newline", "--type", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if got != dest {
		t.Fatalf("got %q, want %q", got, dest)
	}
	info, _ := os.Stat(dest)
	if info.Size() == 0 {
		t.Fatal("file is empty")
	}
	_ = b
}

func TestSaveImage_PipeError(t *testing.T) {
	dir := t.TempDir()
	e := &fakeExec{
		pipeErr: fmt.Errorf("broken pipe"),
	}
	dest := filepath.Join(dir, "clipboard-test.png")
	_, err := saveViaPipe(context.Background(), e, dest, "wl-paste", "--no-newline")
	if err == nil {
		t.Fatal("expected error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("temp file should be cleaned up on error")
	}
}

func TestSaveImage_PipeEmpty(t *testing.T) {
	dir := t.TempDir()
	e := &fakeExec{
		pipeData: []byte{},
	}
	dest := filepath.Join(dir, "clipboard-test.png")
	_, err := saveViaPipe(context.Background(), e, dest, "xclip")
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected 'empty' in error: %v", err)
	}
}

func TestCleanup(t *testing.T) {
	dir := t.TempDir()

	oldFile := filepath.Join(dir, "clipboard-1000.png")
	newFile := filepath.Join(dir, "clipboard-9999.png")
	nonClip := filepath.Join(dir, "other.png")

	os.WriteFile(oldFile, []byte("old"), 0o644)
	os.WriteFile(newFile, []byte("new"), 0o644)
	os.WriteFile(nonClip, []byte("x"), 0o644)

	// Back-date the old file.
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(oldFile, past, past)

	Cleanup(dir, 1*time.Hour)

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("old file should have been removed")
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatal("new file should remain")
	}
	if _, err := os.Stat(nonClip); err != nil {
		t.Fatal("non-clipboard file should remain")
	}
}

func TestCleanup_MissingDir(t *testing.T) {
	Cleanup(filepath.Join(t.TempDir(), "nonexistent"), 1*time.Hour)
}

func TestClipboardDir(t *testing.T) {
	got := ClipboardDir("/home/user/project")
	want := filepath.Join("/home/user/project", ".codient", "clipboard")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// isUnsupportedError is a helper using errors.As semantics.
func isUnsupportedError(err error, target **UnsupportedError) bool {
	if e, ok := err.(*UnsupportedError); ok {
		*target = e
		return true
	}
	return false
}

// fakePNG returns minimal valid PNG bytes.
func fakePNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // signature
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, // RGB, CRC
		0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, 0x54, // IDAT chunk
		0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00,
		0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc, 0x33, // data + CRC
		0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, // IEND chunk
		0xae, 0x42, 0x60, 0x82,
	}
}
