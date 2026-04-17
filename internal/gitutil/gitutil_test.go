package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestIsRepo_NotARepo(t *testing.T) {
	dir := t.TempDir()
	if IsRepo(dir) {
		t.Fatal("expected false for a plain temp dir")
	}
}

func TestIsRepo_ActualRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if !IsRepo(dir) {
		t.Fatal("expected true after git init")
	}
}

func TestDiffSummary_NoChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	summary, err := DiffSummary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if summary != "" {
		t.Fatalf("expected empty diff in fresh repo, got: %q", summary)
	}
}

func initRepoWithFile(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "hello.txt")
	run("commit", "-m", "init")
	return dir
}

func TestDiffFiles(t *testing.T) {
	dir := initRepoWithFile(t)

	files, err := DiffFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no diff files, got %v", files)
	}

	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("changed\n"), 0o644)
	files, err = DiffFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "hello.txt" {
		t.Fatalf("expected [hello.txt], got %v", files)
	}
}

func TestUntrackedFiles(t *testing.T) {
	dir := initRepoWithFile(t)

	files, err := UntrackedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no untracked files, got %v", files)
	}

	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644)
	files, err = UntrackedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "new.txt" {
		t.Fatalf("expected [new.txt], got %v", files)
	}
}

func TestRestoreFiles(t *testing.T) {
	dir := initRepoWithFile(t)

	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("changed\n"), 0o644)
	if err := RestoreFiles(dir, []string{"hello.txt"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "hello.txt"))
	got := strings.TrimSpace(string(data))
	if got != "hello" {
		t.Fatalf("expected restored content 'hello', got %q", got)
	}
}

func TestRestoreFiles_Empty(t *testing.T) {
	if err := RestoreFiles(t.TempDir(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestCleanUntracked(t *testing.T) {
	dir := initRepoWithFile(t)

	os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("x\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)
	os.WriteFile(filepath.Join(dir, "subdir", "deep.txt"), []byte("d\n"), 0o644)

	if err := CleanUntracked(dir); err != nil {
		t.Fatal(err)
	}
	files, _ := UntrackedFiles(dir)
	if len(files) != 0 {
		t.Fatalf("expected no untracked after clean, got %v", files)
	}
}

func TestDiffStatHead(t *testing.T) {
	dir := initRepoWithFile(t)

	stat, err := DiffStatHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stat != "" {
		t.Fatalf("expected empty stat in clean repo, got: %q", stat)
	}

	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("changed\n"), 0o644)
	stat, err = DiffStatHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stat, "hello.txt") {
		t.Fatalf("expected stat to mention hello.txt, got: %q", stat)
	}
	if !strings.Contains(stat, "1 file changed") {
		t.Fatalf("expected stat summary line, got: %q", stat)
	}
}

func TestSplitNonEmpty(t *testing.T) {
	got := splitNonEmpty("  a.txt\nb.txt\n\n  c.txt  \n")
	sort.Strings(got)
	want := []string{"a.txt", "b.txt", "c.txt"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q, want %q", i, got[i], want[i])
		}
	}

	if got := splitNonEmpty(""); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
	if got := splitNonEmpty("  \n  \n"); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}
