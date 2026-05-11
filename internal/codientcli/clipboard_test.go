package codientcli

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codient/internal/config"
	"codient/internal/imageutil"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTUIModel_CtrlV_DispatchesClipboardCmd(t *testing.T) {
	ic := newInputCloser()
	ts := &tuiSetup{
		input:         ic,
		clipWorkspace: t.TempDir(),
		done:          make(chan struct{}),
	}
	m := newTUIModel(ic, "build", true, true)
	m.tuiOwner = ts

	// Ready the model with a window size.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)

	// Ctrl+V should produce a non-nil command when clipWorkspace is set.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	m = updated.(tuiModel)
	if cmd == nil {
		t.Fatal("expected non-nil cmd from Ctrl+V")
	}
}

func TestTUIModel_CtrlV_NoOwner(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "build", true, true)
	// No tuiOwner set; Ctrl+V should not panic and should fall through safely.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	_ = updated.(tuiModel)
}

func TestTUIModel_CtrlV_NoWorkspace(t *testing.T) {
	ic := newInputCloser()
	ts := &tuiSetup{input: ic, done: make(chan struct{})}
	m := newTUIModel(ic, "build", true, true)
	m.tuiOwner = ts
	// No workspace set; Ctrl+V should not dispatch a clipboard command.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	_ = updated.(tuiModel)
	// No clipboard images should have been pushed.
	if imgs := ts.drainClipImages(); len(imgs) != 0 {
		t.Fatalf("expected 0 clipboard images, got %d", len(imgs))
	}
}

func TestTUIModel_ClipImageMsg(t *testing.T) {
	ic := newInputCloser()
	ts := &tuiSetup{input: ic, done: make(chan struct{})}
	m := newTUIModel(ic, "build", true, true)
	m.tuiOwner = ts

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)

	att := imageutil.ImageAttachment{
		Path:      "test.png",
		MimeType:  "image/png",
		DataURI:   "data:image/png;base64,abc",
		OrigBytes: 100,
	}
	updated, _ = m.Update(tuiClipImageMsg{attach: att})
	m = updated.(tuiModel)

	if !strings.Contains(m.content.String(), "pasted clipboard image") {
		t.Fatalf("expected clipboard confirmation in viewport, got %q", m.content.String())
	}

	imgs := ts.drainClipImages()
	if len(imgs) != 1 {
		t.Fatalf("expected 1 clipboard image, got %d", len(imgs))
	}
	if imgs[0].Path != "test.png" {
		t.Fatalf("unexpected path: %q", imgs[0].Path)
	}
}

func TestTUIModel_ClipErrorMsg(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "build", true, true)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)

	updated, _ = m.Update(tuiClipErrorMsg{err: os.ErrNotExist})
	m = updated.(tuiModel)

	if !strings.Contains(m.content.String(), "clipboard:") {
		t.Fatalf("expected clipboard error in viewport, got %q", m.content.String())
	}
}

func TestTUISetup_DrainClipImages(t *testing.T) {
	ts := &tuiSetup{done: make(chan struct{})}
	a1 := imageutil.ImageAttachment{Path: "a.png"}
	a2 := imageutil.ImageAttachment{Path: "b.png"}
	ts.pushClipImage(a1)
	ts.pushClipImage(a2)

	got := ts.drainClipImages()
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	// Second drain should be empty.
	got = ts.drainClipImages()
	if len(got) != 0 {
		t.Fatalf("expected 0 after drain, got %d", len(got))
	}
}

func TestUserMessageForTurn_WithClipboardImages(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "clip.png")
	writeTinyPNG(t, pngPath)
	att, err := imageutil.LoadImage(pngPath, imageutil.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}

	ts := &tuiSetup{done: make(chan struct{})}
	ts.pushClipImage(att)

	s := &session{
		tui: ts,
		cfg: &config.Config{Workspace: dir},
	}

	msg, line, err := s.userMessageForTurn("describe this")
	if err != nil {
		t.Fatal(err)
	}
	if msg.OfUser == nil || len(msg.OfUser.Content.OfArrayOfContentParts) < 2 {
		t.Fatal("expected multipart message with image")
	}
	if !strings.Contains(line, "describe this") {
		t.Fatalf("unexpected line: %q", line)
	}

	// Drain should now be empty.
	if imgs := ts.drainClipImages(); len(imgs) != 0 {
		t.Fatalf("expected 0 after turn, got %d", len(imgs))
	}
}

func writeTinyPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
