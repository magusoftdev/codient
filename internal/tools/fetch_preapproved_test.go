package tools

import "testing"

func TestPreapprovedFetchAllows_DocsPython(t *testing.T) {
	if !PreapprovedFetchAllows("docs.python.org", "/3/library/os.html") {
		t.Fatal("expected docs.python.org allowed")
	}
	if !PreapprovedFetchAllows("docs.python.org", "/") {
		t.Fatal("expected root path allowed")
	}
}

func TestPreapprovedFetchAllows_VercelDocsPathOnly(t *testing.T) {
	if !PreapprovedFetchAllows("vercel.com", "/docs") {
		t.Fatal("expected /docs")
	}
	if !PreapprovedFetchAllows("vercel.com", "/docs/foo") {
		t.Fatal("expected /docs/foo")
	}
	if PreapprovedFetchAllows("vercel.com", "/") {
		t.Fatal("root should not match path-scoped vercel preset")
	}
	if PreapprovedFetchAllows("vercel.com", "/blog") {
		t.Fatal("/blog should not match")
	}
	if PreapprovedFetchAllows("vercel.com", "/docsome") {
		t.Fatal("segment boundary: /docsome must not match /docs")
	}
}

func TestPreapprovedFetchAllows_PkgGoDev(t *testing.T) {
	if !PreapprovedFetchAllows("pkg.go.dev", "/std") {
		t.Fatal("expected pkg.go.dev on preset list")
	}
}
