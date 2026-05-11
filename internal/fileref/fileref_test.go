package fileref

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAndLoad_SingleFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "hello.go"), "package main\n")

	clean, refs, warns, err := ParseAndLoad("describe @hello.go please", dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].Path != "hello.go" {
		t.Fatalf("path = %q", refs[0].Path)
	}
	if !strings.Contains(refs[0].Content, "package main") {
		t.Fatalf("content = %q", refs[0].Content)
	}
	if !strings.Contains(clean, "describe") || !strings.Contains(clean, "please") {
		t.Fatalf("clean = %q", clean)
	}
	if strings.Contains(clean, "@hello.go") {
		t.Fatalf("should strip @ref from clean: %q", clean)
	}
}

func TestParseAndLoad_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "aaa")
	writeFile(t, filepath.Join(dir, "b.txt"), "bbb")

	clean, refs, warns, err := ParseAndLoad("read @a.txt and @b.txt", dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
	if !strings.Contains(clean, "read") || !strings.Contains(clean, "and") {
		t.Fatalf("clean = %q", clean)
	}
}

func TestParseAndLoad_QuotedPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "my file.go"), "content")

	clean, refs, warns, err := ParseAndLoad(`look at @"my file.go"`, dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) > 0 {
		t.Fatalf("warnings: %v", warns)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].Content != "content" {
		t.Fatalf("content = %q", refs[0].Content)
	}
	if strings.Contains(clean, "my file.go") {
		t.Fatalf("should strip quoted ref: %q", clean)
	}
}

func TestParseAndLoad_SkipsImageRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "screenshot.png"), "not-really-png")

	clean, refs, _, err := ParseAndLoad("see @image:screenshot.png here", dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("should skip @image: refs, got %d", len(refs))
	}
	if !strings.Contains(clean, "@image:screenshot.png") {
		t.Fatalf("should preserve @image: in clean text: %q", clean)
	}
}

func TestParseAndLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()

	_, refs, warns, err := ParseAndLoad("@nonexistent.go", dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs for missing file, got %d", len(refs))
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "file not found") {
		t.Fatalf("expected 'file not found' warning, got %v", warns)
	}
}

func TestParseAndLoad_BinaryFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bin.dat"), []byte{0x00, 0xFF, 0x80}, 0o644)

	_, refs, warns, err := ParseAndLoad("@bin.dat", dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs for binary file")
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "not a text file") {
		t.Fatalf("expected 'not a text file' warning, got %v", warns)
	}
}

func TestParseAndLoad_Directory(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)

	_, refs, warns, err := ParseAndLoad("@subdir", dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs for directory")
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "directories not supported") {
		t.Fatalf("expected directory warning, got %v", warns)
	}
}

func TestParseAndLoad_EscapedAt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "file.go"), "content")

	clean, refs, _, err := ParseAndLoad(`\@file.go is literal`, dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("escaped @ should not be parsed, got %d refs", len(refs))
	}
	if !strings.Contains(clean, `\@file.go`) {
		t.Fatalf("escaped @ should remain: %q", clean)
	}
}

func TestParseAndLoad_WorkspaceConfinement(t *testing.T) {
	dir := t.TempDir()

	_, refs, warns, err := ParseAndLoad("@../../etc/passwd", dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("should not load file outside workspace")
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "escapes workspace") {
		t.Fatalf("expected workspace escape warning, got %v", warns)
	}
}

func TestParseAndLoad_Truncation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "big.txt"), strings.Repeat("x", 1000))

	_, refs, _, err := ParseAndLoad("@big.txt", dir, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if len(refs[0].Content) != 100 {
		t.Fatalf("content should be truncated to 100, got %d", len(refs[0].Content))
	}
	if refs[0].TruncatedBytes != 900 {
		t.Fatalf("truncated = %d, want 900", refs[0].TruncatedBytes)
	}
}

func TestParseAndLoad_AggregateLimit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), strings.Repeat("a", 600))
	writeFile(t, filepath.Join(dir, "b.txt"), strings.Repeat("b", 600))

	_, refs, warns, err := ParseAndLoad("@a.txt @b.txt", dir, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref (second should be skipped), got %d", len(refs))
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "exceeded") {
		t.Fatalf("expected aggregate limit warning, got %v", warns)
	}
}

func TestParseAndLoad_RelativePath(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0o755)
	writeFile(t, filepath.Join(sub, "nested.txt"), "nested content")

	_, refs, _, err := ParseAndLoad("@sub/nested.txt", dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].Content != "nested content" {
		t.Fatalf("content = %q", refs[0].Content)
	}
}

func TestFormatReferences_Empty(t *testing.T) {
	if s := FormatReferences(nil); s != "" {
		t.Fatalf("expected empty, got %q", s)
	}
}

func TestFormatReferences_Single(t *testing.T) {
	refs := []FileReference{{Path: "main.go", Content: "package main\n"}}
	s := FormatReferences(refs)
	if !strings.Contains(s, "<referenced_files>") {
		t.Fatalf("missing opening tag")
	}
	if !strings.Contains(s, "--- main.go ---") {
		t.Fatalf("missing file header")
	}
	if !strings.Contains(s, "package main") {
		t.Fatalf("missing content")
	}
	if !strings.Contains(s, "</referenced_files>") {
		t.Fatalf("missing closing tag")
	}
}

func TestFormatReferences_Truncated(t *testing.T) {
	refs := []FileReference{{Path: "big.txt", Content: "abc", TruncatedBytes: 500}}
	s := FormatReferences(refs)
	if !strings.Contains(s, "500 bytes omitted") {
		t.Fatalf("missing truncation note: %q", s)
	}
}

func TestFormatReferences_Multiple(t *testing.T) {
	refs := []FileReference{
		{Path: "a.go", Content: "aaa"},
		{Path: "b.go", Content: "bbb"},
	}
	s := FormatReferences(refs)
	if !strings.Contains(s, "--- a.go ---") || !strings.Contains(s, "--- b.go ---") {
		t.Fatalf("missing file headers: %q", s)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
