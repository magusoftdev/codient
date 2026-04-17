package repomap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codient/internal/tokenest"
)

func TestExtractTags_Go(t *testing.T) {
	src := `package foo

func Bar() {}
type Baz struct { x int }

func (b *Baz) Quux() {}
`
	tags := ExtractTags("go", "x.go", src)
	if len(tags) < 3 {
		t.Fatalf("expected at least 3 tags, got %d", len(tags))
	}
	names := make(map[string]bool)
	for _, tg := range tags {
		names[tg.Name] = true
	}
	if !names["Bar"] || !names["Baz"] || !names["Quux"] {
		t.Fatalf("missing expected names, got %+v", tags)
	}
}

func TestExtractTags_Python(t *testing.T) {
	src := `class Foo:
    def bar(self):
        pass

def baz():
    pass
`
	tags := ExtractTags("python", "m.py", src)
	if len(tags) != 2 {
		t.Fatalf("expected 2 top-level tags, got %d: %+v", len(tags), tags)
	}
}

func TestBuildAndRender(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package main\n\nfunc F() {}\n")
	writeFile(t, dir, "b.go", "package main\n\ntype T struct{}\n")

	m := New(dir)
	ctx := context.Background()
	m.Build(ctx)

	if m.BuildErr() != nil {
		t.Fatal(m.BuildErr())
	}
	if m.FileCount() != 2 {
		t.Fatalf("file count: want 2, got %d", m.FileCount())
	}

	out := m.Render(8000)
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "func F") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestRenderPrefix(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pkg/x.go", "package pkg\n\nfunc X() {}\n")
	writeFile(t, dir, "other/y.go", "package other\n\nfunc Y() {}\n")

	m := New(dir)
	m.Build(context.Background())

	all := m.RenderPrefix("", 8000)
	if !strings.Contains(all, "pkg/x.go") || !strings.Contains(all, "other/y.go") {
		t.Fatalf("expected both files in full map:\n%s", all)
	}

	sub := m.RenderPrefix("pkg", 8000)
	if !strings.Contains(sub, "pkg/x.go") {
		t.Fatalf("expected pkg file:\n%s", sub)
	}
	if strings.Contains(sub, "other/y.go") {
		t.Fatalf("did not expect other file:\n%s", sub)
	}
}

func TestRender_TruncatesByTokenBudget(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("p/f%02d.go", i)
		writeFile(t, dir, name, fmt.Sprintf("package p\n\nfunc F%d() {}\n", i))
	}

	m := New(dir)
	m.Build(context.Background())

	lowBudget := m.Render(80)
	if !strings.Contains(lowBudget, "truncated") {
		t.Fatalf("expected truncation notice in:\n%s", lowBudget)
	}
	if tokenest.Estimate(lowBudget) > 400 {
		t.Fatalf("rendering should stay near budget, got ~%d tokens", tokenest.Estimate(lowBudget))
	}
}

func TestIncrementalCache(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package main\n\nfunc A() {}\n")

	m := New(dir)
	m.Build(context.Background())
	if m.TagCount() < 1 {
		t.Fatal("expected tags")
	}

	// Second build with same content should hit cache (no error, same result)
	m2 := New(dir)
	m2.Build(context.Background())
	if m2.TagCount() != m.TagCount() {
		t.Fatalf("tag count mismatch after rebuild")
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPromptText(t *testing.T) {
	if PromptText(-1, nil) != "" {
		t.Fatal("disabled should return empty")
	}
	dir := t.TempDir()
	writeFile(t, dir, "x.go", "package main\n\nfunc Main() {}\n")
	m := New(dir)
	m.Build(context.Background())
	if s := PromptText(0, m); s == "" || !strings.Contains(s, "Main") {
		t.Fatalf("expected map text, got %q", s)
	}
}

func TestAutoTokens(t *testing.T) {
	if AutoTokens(10) != 2000 {
		t.Fatal()
	}
	if AutoTokens(100) != 4000 {
		t.Fatal()
	}
	if AutoTokens(800) != 6000 {
		t.Fatal()
	}
	if AutoTokens(2000) != 8000 {
		t.Fatal()
	}
}
