package projectinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetect_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	got := Detect(dir)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestDetect_EmptyRoot(t *testing.T) {
	if got := Detect(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestDetect_GoProject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/myapp\n\ngo 1.23\n")
	writeFile(t, filepath.Join(dir, "Makefile"), "all:\n\tgo build\n")

	got := Detect(dir)
	assertContains(t, got, "Language: Go (1.23)")
	assertContains(t, got, "Module: example.com/myapp")
	assertContains(t, got, "Build: Make")
}

func TestDetect_GoProject_NoVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module mymod\n")

	got := Detect(dir)
	assertContains(t, got, "Language: Go")
	assertContains(t, got, "Module: mymod")
}

func TestDetect_NodeTS(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"my-app","dependencies":{"react":"^18","next":"^14"}}`)
	writeFile(t, filepath.Join(dir, "tsconfig.json"), "{}")
	writeFile(t, filepath.Join(dir, "yarn.lock"), "")

	got := Detect(dir)
	assertContains(t, got, "Language: TypeScript")
	assertContains(t, got, "Package: my-app")
	assertContains(t, got, "next")
	assertContains(t, got, "react")
	assertContains(t, got, "Package manager: yarn")
}

func TestDetect_NodeJS(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"api","dependencies":{"express":"^4"}}`)
	writeFile(t, filepath.Join(dir, "package-lock.json"), "{}")

	got := Detect(dir)
	assertContains(t, got, "Language: JavaScript")
	assertContains(t, got, "Package manager: npm")
	assertContains(t, got, "express")
}

func TestDetect_NodePNPM(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"x"}`)
	writeFile(t, filepath.Join(dir, "pnpm-lock.yaml"), "")

	got := Detect(dir)
	assertContains(t, got, "Package manager: pnpm")
}

func TestDetect_NodeBun(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"x"}`)
	writeFile(t, filepath.Join(dir, "bun.lockb"), "")

	got := Detect(dir)
	assertContains(t, got, "Package manager: bun")
}

func TestDetect_Rust(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Cargo.toml"), `[package]
name = "mybin"
edition = "2021"
`)

	got := Detect(dir)
	assertContains(t, got, "Language: Rust (edition 2021)")
	assertContains(t, got, "Crate: mybin")
}

func TestDetect_Python_Pyproject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pyproject.toml"), `[project]
name = "mylib"

[build-system]
build-backend = "hatchling.build"
`)

	got := Detect(dir)
	assertContains(t, got, "Language: Python")
	assertContains(t, got, "Package: mylib")
	assertContains(t, got, "Build backend: hatchling.build")
}

func TestDetect_Python_Requirements(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "requirements.txt"), "flask==3.0\n")

	got := Detect(dir)
	assertContains(t, got, "Language: Python")
}

func TestDetect_Docker(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module x\n\ngo 1.22\n")
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM golang:1.22\n")

	got := Detect(dir)
	assertContains(t, got, "Infra: Docker")
}

func TestDetect_GitHubActions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module x\n\ngo 1.22\n")
	wfDir := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wfDir, "ci.yml"), "name: CI\n")

	got := Detect(dir)
	assertContains(t, got, "GitHub Actions")
}

func TestDetect_OptOut(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module x\n\ngo 1.22\n")
	t.Setenv("CODIENT_PROJECT_CONTEXT", "off")

	got := Detect(dir)
	if got != "" {
		t.Fatalf("expected empty with opt-out, got %q", got)
	}
}

func TestDetect_OptOut_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module x\n\ngo 1.22\n")
	t.Setenv("CODIENT_PROJECT_CONTEXT", "OFF")

	got := Detect(dir)
	if got != "" {
		t.Fatalf("expected empty with opt-out, got %q", got)
	}
}

func TestDetect_MaxOutput(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module "+strings.Repeat("a", 2000)+"\n\ngo 1.22\n")

	got := Detect(dir)
	if len(got) > maxOutput {
		t.Fatalf("output %d bytes exceeds cap %d", len(got), maxOutput)
	}
}

func TestDetect_MultiStack(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module backend\n\ngo 1.22\n")
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"frontend","dependencies":{"react":"^18"}}`)

	got := Detect(dir)
	assertContains(t, got, "Language: Go")
	assertContains(t, got, "Language: JavaScript")
	assertContains(t, got, "react")
}

func TestDetect_InvalidPackageJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), "not json at all {{{")

	got := Detect(dir)
	if got != "" {
		t.Fatalf("expected empty for invalid package.json, got %q", got)
	}
}

func TestDetect_DockerCompose(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module x\n\ngo 1.22\n")
	writeFile(t, filepath.Join(dir, "docker-compose.yaml"), "version: '3'\n")

	got := Detect(dir)
	assertContains(t, got, "Infra: Docker")
}

// --- helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("output missing %q:\n%s", want, got)
	}
}
