package codeextract

import (
	"strings"
	"testing"
)

func TestOutlineGo_GenericsAndTypes(t *testing.T) {
	src := `package p

import "fmt"

type Pair[A any, B comparable] struct {
	X A
	y B ` + "`json:\"y\"`" + `
}

type Stringer interface {
	String() string
}

func Foo[T int | string](x T) T {
	fmt.Println("body")
	return x
}

func (p *Pair[A, B]) Method() {}
`
	out, err := outlineGo("x.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "package p") {
		t.Fatalf("missing package: %s", out)
	}
	if !strings.Contains(out, "type Pair[A any, B comparable] struct") {
		t.Fatalf("missing struct: %s", out)
	}
	if !strings.Contains(out, "type Stringer interface") {
		t.Fatalf("missing interface: %s", out)
	}
	if !strings.Contains(out, "func Foo[T int | string](x T) T") {
		t.Fatalf("missing generic func sig: %s", out)
	}
	if strings.Contains(out, `fmt.Println("body")`) {
		t.Fatalf("function body should not appear: %s", out)
	}
	if !strings.Contains(out, "func (p *Pair[A, B]) Method()") {
		t.Fatalf("missing method: %s", out)
	}
}

func TestOutlineGo_ParseError(t *testing.T) {
	_, err := outlineGo("bad.go", []byte("package p\nfunc {{{\n"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Fatalf("got %v", err)
	}
}

func TestOutline_Truncated(t *testing.T) {
	_, err := Outline("a.go", "go", []byte("package p\n"), true)
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("got %v", err)
	}
}

func TestOutline_UnsupportedExt(t *testing.T) {
	_, err := Outline("README.md", "", []byte("# hi"), false)
	if err == nil || !strings.Contains(err.Error(), "full") {
		t.Fatalf("got %v", err)
	}
}

func TestOutlineHeuristic_Python(t *testing.T) {
	src := `class Box:
    def __init__(self):
        self.x = 1

def top():
    return 2
`
	out, err := outlineHeuristic("python", "m.py", src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "class Box:") {
		t.Fatalf("missing class: %s", out)
	}
	if !strings.Contains(out, "def top():") {
		t.Fatalf("missing def: %s", out)
	}
}
