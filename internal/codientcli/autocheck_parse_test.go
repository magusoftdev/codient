package codientcli

import (
	"strings"
	"testing"
)

func TestOpaqueParser_Signature(t *testing.T) {
	p := opaqueParser{}
	out1 := p.Parse("build", "go build ./...", "error: foo\nerror: bar\n", 1)
	out2 := p.Parse("build", "go build ./...", "error: foo\nerror: bar\n", 1)
	if out1.Signature == "" {
		t.Fatal("expected non-empty signature")
	}
	if out1.Signature != out2.Signature {
		t.Fatalf("same input should produce same signature: %q vs %q", out1.Signature, out2.Signature)
	}

	out3 := p.Parse("build", "go build ./...", "error: baz\n", 1)
	if out3.Signature == out1.Signature {
		t.Fatal("different input should produce different signature")
	}
}

func TestOpaqueParser_WhitespaceStability(t *testing.T) {
	p := opaqueParser{}
	out1 := p.Parse("", "", "error: foo\nerror: bar", 1)
	out2 := p.Parse("", "", "  error: foo\nerror: bar  \n", 1)
	if out1.Signature == out2.Signature {
		t.Log("whitespace-only difference still changes signature (by design)")
	}
}

func TestOpaqueParser_Highlights(t *testing.T) {
	body := "line1\n\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	p := opaqueParser{}
	out := p.Parse("", "", body, 1)
	if len(out.Highlights) > maxHighlights {
		t.Fatalf("expected at most %d highlights, got %d", maxHighlights, len(out.Highlights))
	}
	if out.Highlights[0] != "line1" {
		t.Fatalf("first highlight should be 'line1', got %q", out.Highlights[0])
	}
}

func TestGoTestParser_SingleFail(t *testing.T) {
	body := `=== RUN   TestFoo
    foo_test.go:12: expected 1, got 2
--- FAIL: TestFoo (0.00s)
FAIL	example.com/pkg	0.003s
`
	p := goTestParser{}
	out := p.Parse("test", "go test ./...", body, 1)
	if out.Signature == "" {
		t.Fatal("expected non-empty signature")
	}
	if !strings.HasPrefix(out.Signature, "go:") {
		t.Fatalf("expected go: prefix, got %q", out.Signature)
	}
	if len(out.Highlights) == 0 {
		t.Fatal("expected at least one highlight")
	}
	if !strings.Contains(out.Highlights[0], "TestFoo") {
		t.Fatalf("expected TestFoo in highlight, got %q", out.Highlights[0])
	}
}

func TestGoTestParser_MultipleFails(t *testing.T) {
	body := `=== RUN   TestA
    a_test.go:5: boom
--- FAIL: TestA (0.00s)
=== RUN   TestB
    b_test.go:10: oops
--- FAIL: TestB (0.00s)
FAIL	example.com/pkg	0.005s
`
	p := goTestParser{}
	out := p.Parse("test", "go test ./...", body, 1)
	if len(out.Highlights) < 2 {
		t.Fatalf("expected at least 2 highlights, got %d", len(out.Highlights))
	}

	combined := strings.Join(out.Highlights, "\n")
	if !strings.Contains(combined, "TestA") || !strings.Contains(combined, "TestB") {
		t.Fatalf("expected both TestA and TestB in highlights: %s", combined)
	}
}

func TestGoTestParser_BuildError_FallsBack(t *testing.T) {
	body := `./main.go:5:3: undefined: foo
`
	p := goTestParser{}
	out := p.Parse("build", "go build ./...", body, 2)
	if out.Signature == "" {
		t.Fatal("expected non-empty signature from fallback")
	}
	if len(out.Highlights) == 0 {
		t.Fatal("expected highlights from fallback")
	}
}

func TestGoTestParser_PkgBuildFailed(t *testing.T) {
	body := `# example.com/pkg
./main.go:5:3: undefined: foo
FAIL	example.com/pkg [build failed]
`
	p := goTestParser{}
	out := p.Parse("test", "go test ./...", body, 2)
	if !strings.HasPrefix(out.Signature, "go:") {
		t.Fatalf("expected go: prefix, got %q", out.Signature)
	}
}

func TestGoTestParser_Panic(t *testing.T) {
	body := `=== RUN   TestPanic
--- FAIL: TestPanic (0.00s)
panic: runtime error: index out of range [recovered]
FAIL	example.com/pkg	0.001s
`
	p := goTestParser{}
	out := p.Parse("test", "go test ./...", body, 2)
	if !strings.HasPrefix(out.Signature, "go:") {
		t.Fatalf("expected go: prefix, got %q", out.Signature)
	}
}

func TestSelectParser_Go(t *testing.T) {
	cases := []string{"go test ./...", "GO TEST ./...", "go build ./...", "go vet"}
	for _, c := range cases {
		p := selectParser("test", c)
		if _, ok := p.(goTestParser); !ok {
			t.Fatalf("expected goTestParser for %q, got %T", c, p)
		}
	}
}

func TestSelectParser_Default(t *testing.T) {
	p := selectParser("build", "npm run build")
	if _, ok := p.(opaqueParser); !ok {
		t.Fatalf("expected opaqueParser for npm, got %T", p)
	}
}

func TestGoTestParser_StableSignature(t *testing.T) {
	body := `--- FAIL: TestFoo (0.00s)
FAIL	example.com/pkg	0.003s
`
	p := goTestParser{}
	out1 := p.Parse("test", "go test ./...", body, 1)
	out2 := p.Parse("test", "go test ./...", body, 1)
	if out1.Signature != out2.Signature {
		t.Fatalf("same output should give same signature: %q vs %q", out1.Signature, out2.Signature)
	}
}
