package fileref

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitPastedPaths_Empty(t *testing.T) {
	if paths := SplitPastedPaths(""); paths != nil {
		t.Fatalf("expected nil, got %v", paths)
	}
	if paths := SplitPastedPaths("   "); paths != nil {
		t.Fatalf("expected nil for whitespace, got %v", paths)
	}
}

func TestSplitPastedPaths_Single(t *testing.T) {
	paths := SplitPastedPaths("/path/to/file.go")
	if len(paths) != 1 || paths[0] != "/path/to/file.go" {
		t.Fatalf("got %v", paths)
	}
}

func TestSplitPastedPaths_Multiple(t *testing.T) {
	paths := SplitPastedPaths("/a.go /b.go /c.go")
	if len(paths) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(paths), paths)
	}
	if paths[0] != "/a.go" || paths[1] != "/b.go" || paths[2] != "/c.go" {
		t.Fatalf("got %v", paths)
	}
}

func TestSplitPastedPaths_DoubleQuoted(t *testing.T) {
	paths := SplitPastedPaths(`"/path/to/my file.go"`)
	if len(paths) != 1 || paths[0] != "/path/to/my file.go" {
		t.Fatalf("got %v", paths)
	}
}

func TestSplitPastedPaths_SingleQuoted(t *testing.T) {
	paths := SplitPastedPaths(`'/path/to/my file.go'`)
	if len(paths) != 1 || paths[0] != "/path/to/my file.go" {
		t.Fatalf("got %v", paths)
	}
}

func TestSplitPastedPaths_EscapedSpaces(t *testing.T) {
	paths := SplitPastedPaths(`/path/to/my\ file.go`)
	if len(paths) != 1 || paths[0] != "/path/to/my file.go" {
		t.Fatalf("got %v", paths)
	}
}

func TestSplitPastedPaths_Mixed(t *testing.T) {
	paths := SplitPastedPaths(`"/my img1.go" /my\ img2.go /plain.go`)
	if len(paths) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(paths), paths)
	}
	if paths[0] != "/my img1.go" || paths[1] != "/my img2.go" || paths[2] != "/plain.go" {
		t.Fatalf("got %v", paths)
	}
}

func TestSplitPastedPaths_Newlines(t *testing.T) {
	paths := SplitPastedPaths("/a.go\n/b.go\n/c.go")
	if len(paths) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(paths), paths)
	}
}

func TestSplitPastedPaths_ConsecutiveSpaces(t *testing.T) {
	paths := SplitPastedPaths("/a.go   /b.go")
	if len(paths) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(paths), paths)
	}
}

func TestLooksLikePath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"/usr/bin/ls", true},
		{"~/file.go", true},
		{"./relative.go", true},
		{"../parent.go", true},
		{"just-a-word", false},
		{"", false},
		{"hello world", false},
	}
	for _, tt := range tests {
		if got := looksLikePath(tt.input); got != tt.want {
			t.Errorf("looksLikePath(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDetectPastedPaths_AllValid(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.go")
	f2 := filepath.Join(dir, "b.go")
	writeTestFile(t, f1, "aaa")
	writeTestFile(t, f2, "bbb")

	text := f1 + " " + f2
	result, ok := DetectPastedPaths(text)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !strings.Contains(result, "@"+f1) || !strings.Contains(result, "@"+f2) {
		t.Fatalf("expected @ prefixes: %q", result)
	}
}

func TestDetectPastedPaths_SingleFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.go")
	writeTestFile(t, f, "content")

	result, ok := DetectPastedPaths(f)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if result != "@"+f {
		t.Fatalf("got %q", result)
	}
}

func TestDetectPastedPaths_MixedInvalid(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.go")
	writeTestFile(t, f, "content")

	_, ok := DetectPastedPaths(f + " not-a-path")
	if ok {
		t.Fatal("expected ok=false when non-path text is mixed in")
	}
}

func TestDetectPastedPaths_NonexistentFile(t *testing.T) {
	_, ok := DetectPastedPaths("/nonexistent/file.go")
	if ok {
		t.Fatal("expected ok=false for nonexistent file")
	}
}

func TestDetectPastedPaths_Directory(t *testing.T) {
	dir := t.TempDir()
	_, ok := DetectPastedPaths(dir)
	if ok {
		t.Fatal("expected ok=false for directory")
	}
}

func TestDetectPastedPaths_Empty(t *testing.T) {
	_, ok := DetectPastedPaths("")
	if ok {
		t.Fatal("expected ok=false for empty")
	}
}

func TestDetectPastedPaths_PathWithSpaces(t *testing.T) {
	dir := t.TempDir()
	spacePath := filepath.Join(dir, "my file.go")
	writeTestFile(t, spacePath, "content")

	result, ok := DetectPastedPaths(`"` + spacePath + `"`)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !strings.Contains(result, `@"`+spacePath+`"`) {
		t.Fatalf("expected quoted @ prefix: %q", result)
	}
}

func TestDetectPastedPaths_NonPathText(t *testing.T) {
	_, ok := DetectPastedPaths("hello world this is normal text")
	if ok {
		t.Fatal("expected ok=false for plain text")
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
