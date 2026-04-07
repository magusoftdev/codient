package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureDirWorkspace(t *testing.T) {
	dir := t.TempDir()
	if err := ensureDirWorkspace(dir, filepath.Join("a", "b", "c")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "a", "b", "c"))
	if err != nil || !st.IsDir() {
		t.Fatalf("dir: %v isDir=%v", err, st != nil && st.IsDir())
	}
	err = ensureDirWorkspace(dir, "../outside")
	if err == nil {
		t.Fatal("expected escape")
	}
}

func TestAbsUnderRoot(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	abs, err := absUnderRoot(dir, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(abs) != "sub" {
		t.Fatalf("got %s", abs)
	}
	_, err = absUnderRoot(dir, "../outside")
	if err == nil {
		t.Fatal("expected escape")
	}
}

func TestReadFileWorkspace_Truncation(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("a", 100)
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := readFileWorkspace(dir, "big.txt", 40, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[truncated") {
		t.Fatalf("expected truncation marker: %q", out)
	}
	if len(out) > 200 {
		t.Fatalf("output unexpectedly long: %d", len(out))
	}
}

func TestReadFileWorkspace_InvalidUTF8(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readFileWorkspace(dir, "b.bin", 1024, 0, 0)
	if err == nil {
		t.Fatal("expected utf-8 error")
	}
}

func TestListDirWorkspace_NotDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := listDirWorkspace(dir, "f.txt", 0, 10)
	if err == nil {
		t.Fatal("expected not a directory")
	}
}

func TestListDirWorkspace_SkipsNoiseDirectories(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{".git", "node_modules", "src"} {
		os.MkdirAll(filepath.Join(dir, d), 0o755)
		os.WriteFile(filepath.Join(dir, d, "file.txt"), []byte("x"), 0o644)
	}
	out, err := listDirWorkspace(dir, ".", 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, ".git") {
		t.Fatalf("expected .git to be filtered, got:\n%s", out)
	}
	if strings.Contains(out, "node_modules") {
		t.Fatalf("expected node_modules to be filtered, got:\n%s", out)
	}
	if !strings.Contains(out, "src") {
		t.Fatalf("expected src to be present, got:\n%s", out)
	}
}

func TestSearchFilesWorkspace_SkipsNoiseDirectories(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "objects", "data.txt"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "data.txt"), []byte("x"), 0o644)
	out, err := searchFilesWorkspace(dir, "", "data", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, ".git") {
		t.Fatalf("expected .git paths to be filtered, got:\n%s", out)
	}
	if !strings.Contains(out, "src") {
		t.Fatalf("expected src/data.txt to be present, got:\n%s", out)
	}
}

func TestSearchFilesWorkspace_NeedsFilter(t *testing.T) {
	dir := t.TempDir()
	_, err := searchFilesWorkspace(dir, "", "", "", 10)
	if err == nil {
		t.Fatal("expected error without substring/suffix")
	}
}

func TestStrReplaceWorkspace_UniqueMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg, err := strReplaceWorkspace(dir, "f.go", "hello", "goodbye", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "replaced 1 occurrence") {
		t.Fatalf("unexpected msg: %s", msg)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.go"))
	if string(data) != "goodbye world\n" {
		t.Fatalf("got %q", string(data))
	}
}

func TestStrReplaceWorkspace_NoMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := strReplaceWorkspace(dir, "f.go", "missing", "x", false)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestStrReplaceWorkspace_MultipleWithout_ReplaceAll(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte("aaa\naaa\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := strReplaceWorkspace(dir, "f.go", "aaa", "bbb", false)
	if err == nil || !strings.Contains(err.Error(), "2 matches") {
		t.Fatalf("expected multiple match error, got %v", err)
	}
}

func TestStrReplaceWorkspace_ReplaceAll(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte("aaa\naaa\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg, err := strReplaceWorkspace(dir, "f.go", "aaa", "bbb", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "replaced 2 occurrences") {
		t.Fatalf("unexpected msg: %s", msg)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.go"))
	if string(data) != "bbb\nbbb\n" {
		t.Fatalf("got %q", string(data))
	}
}

func TestWriteFileWorkspace_InvalidMode(t *testing.T) {
	dir := t.TempDir()
	err := writeFileWorkspace(dir, "z.txt", "x", "wipe")
	if err == nil {
		t.Fatal("expected invalid mode")
	}
}
