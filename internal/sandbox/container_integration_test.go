//go:build integration

package sandbox

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Live Docker/Podman tests. Run with:
//
//	CODIENT_INTEGRATION=1 go test -tags=integration -run TestIntegration_Container ./internal/sandbox/...
//
// Skipped when CODIENT_INTEGRATION is not set or no container runtime is available.

func skipUnlessContainerIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("CODIENT_INTEGRATION") != "1" {
		t.Skip("set CODIENT_INTEGRATION=1 to run container integration tests")
	}
	if testing.Short() {
		t.Skip("skipping container integration in -short mode")
	}
	c := NewContainerRunner("")
	if !c.Available() {
		t.Skip("docker or podman not available on PATH")
	}
}

func TestIntegration_ContainerRunner_BasicCommand(t *testing.T) {
	skipUnlessContainerIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var stdout, stderr bytes.Buffer
	c := NewContainerRunner("")
	dir := t.TempDir()
	code, err := c.Exec(ctx, Policy{}, dir, []string{"sh", "-c", "echo hello"}, nil, 60*time.Second, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Exec: %v stderr=%s", err, stderr.String())
	}
	if code != 0 {
		t.Fatalf("exit code %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "hello") {
		t.Fatalf("stdout=%q want hello", stdout.String())
	}
}

func TestIntegration_ContainerRunner_WorkspaceIsolation(t *testing.T) {
	skipUnlessContainerIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "in_workspace.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "host_only.txt"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := NewContainerRunner("")
	var out bytes.Buffer

	code, err := c.Exec(ctx, Policy{}, ws, []string{"cat", "in_workspace.txt"}, nil, 60*time.Second, &out, os.Stderr)
	if err != nil || code != 0 {
		t.Fatalf("read workspace file: code=%d err=%v out=%q", code, err, out.String())
	}
	if !strings.Contains(out.String(), "ok") {
		t.Fatalf("expected workspace file contents, got %q", out.String())
	}

	out.Reset()
	code, err = c.Exec(ctx, Policy{}, ws, []string{"sh", "-c", "test ! -f ../outside/host_only.txt && test ! -f /outside/host_only.txt"}, nil, 60*time.Second, &out, os.Stderr)
	if err != nil || code != 0 {
		t.Fatalf("host-only file should not be visible in container: code=%d err=%v stderr check failed", code, err)
	}
}

func TestIntegration_ContainerSession_PersistsState(t *testing.T) {
	skipUnlessContainerIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ws := t.TempDir()
	sess, err := StartContainerSession(ctx, "", ws, Policy{})
	if err != nil {
		t.Fatalf("StartContainerSession: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		sess.Close(stopCtx)
	}()

	// First exec: write a marker file.
	var stdout1, stderr1 bytes.Buffer
	code, err := sess.Exec(ctx, []string{"sh", "-c", "echo session_marker > /workspace/marker.txt"}, nil, &stdout1, &stderr1, 30*time.Second)
	if err != nil {
		t.Fatalf("first exec: %v stderr=%s", err, stderr1.String())
	}
	if code != 0 {
		t.Fatalf("first exec: exit %d stderr=%s", code, stderr1.String())
	}

	// Second exec: read it back. State persists because it's the same container.
	var stdout2, stderr2 bytes.Buffer
	code, err = sess.Exec(ctx, []string{"cat", "/workspace/marker.txt"}, nil, &stdout2, &stderr2, 30*time.Second)
	if err != nil {
		t.Fatalf("second exec: %v stderr=%s", err, stderr2.String())
	}
	if code != 0 {
		t.Fatalf("second exec: exit %d stderr=%s", code, stderr2.String())
	}
	if !strings.Contains(stdout2.String(), "session_marker") {
		t.Fatalf("expected session_marker, got %q", stdout2.String())
	}

	// The file should also be visible on the host via the bind mount.
	hostData, err := os.ReadFile(filepath.Join(ws, "marker.txt"))
	if err != nil {
		t.Fatalf("read host marker: %v", err)
	}
	if !strings.Contains(string(hostData), "session_marker") {
		t.Fatalf("host marker: got %q", string(hostData))
	}
}

func TestIntegration_ContainerSession_SessionRunner(t *testing.T) {
	skipUnlessContainerIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ws := t.TempDir()
	sess, err := StartContainerSession(ctx, "", ws, Policy{})
	if err != nil {
		t.Fatalf("StartContainerSession: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		sess.Close(stopCtx)
	}()

	runner := &SessionRunner{Sess: sess}
	if !runner.Available() {
		t.Fatal("expected Available() true")
	}
	if runner.Name() != "container-session" {
		t.Fatalf("Name: got %q", runner.Name())
	}

	var stdout, stderr bytes.Buffer
	code, err := runner.Exec(ctx, Policy{}, ws, []string{"echo", "via-runner"}, nil, 30*time.Second, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "via-runner") {
		t.Fatalf("stdout: got %q", stdout.String())
	}
}
